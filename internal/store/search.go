package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SearchScope bounds a spec-search query to active, archived, or all specs.
type SearchScope int

const (
	ScopeAll SearchScope = iota
	ScopeActive
	ScopeArchived
)

// SearchSection is one indexable section of a spec. The store accepts these
// rather than internal/markdown.Section so the dependency arrow points
// store ← search (the store never imports markdown).
type SearchSection struct {
	Slug    string
	Heading string
	Body    string
}

// SearchDoc is a fully parsed spec ready for indexing: one row per section.
// The metadata fields (Author, Assignees, Cycle, EpicKey) are duplicated on
// every section row so any section hit carries them and MATCH can find specs
// by them.
type SearchDoc struct {
	SpecID    string
	Path      string
	Title     string
	Status    string
	Author    string
	Assignees []string
	Cycle     string
	EpicKey   string
	Archived  bool
	Sections  []SearchSection
}

// SearchRow is one ranked result row from a search query.
type SearchRow struct {
	SpecID         string
	Title          string
	Status         string
	Author         string
	Archived       bool
	SectionSlug    string
	SectionHeading string
	Snippet        string
	Score          float64 // bm25(): negative, lower is better
}

// bm25Weights are the per-column bm25 weights, in the spec_search column
// order established by migration v6:
// spec_id, section_slug, section_heading, title, status, body,
// author, assignees, cycle, epic_key, archived, path.
// A spec-id match ranks highest (it is near-unique), then heading, then
// title/author/assignees/epic, then body; UNINDEXED columns are 0.
var bm25Weights = []any{4.0, 0.0, 3.0, 2.5, 1.0, 1.0, 2.0, 2.0, 1.0, 2.0, 0.0, 0.0}

// snippetColumn is the snippet() column argument: -1 lets FTS5 pick the
// column containing the best match, so a hit on author or title excerpts
// that field instead of always excerpting the body.
const snippetColumn = -1

// UpsertSpecSections replaces all indexed rows for a spec with the given
// sections, then records the indexing state. It runs in a single transaction
// so a partial index never persists. The hash is the caller-computed content
// fingerprint stored for incremental reindex; mtime is the file's mod time.
func (db *DB) UpsertSpecSections(ctx context.Context, doc SearchDoc, mtime int64, hash string) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning spec_search upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search WHERE spec_id = ?`, doc.SpecID); err != nil {
		return fmt.Errorf("deleting old spec_search rows for %s: %w", doc.SpecID, err)
	}

	archived := "0"
	if doc.Archived {
		archived = "1"
	}

	insertStmt := `INSERT INTO spec_search
		(spec_id, section_slug, section_heading, title, status, body,
		 author, assignees, cycle, epic_key, archived, path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	assignees := strings.Join(doc.Assignees, " ")
	for _, sec := range doc.Sections {
		if _, err := tx.ExecContext(ctx, insertStmt,
			doc.SpecID, sec.Slug, sec.Heading, doc.Title, doc.Status, sec.Body,
			doc.Author, assignees, doc.Cycle, doc.EpicKey, archived, doc.Path,
		); err != nil {
			return fmt.Errorf("inserting spec_search row for %s/%s: %w", doc.SpecID, sec.Slug, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO spec_search_state (path, spec_id, mtime, hash, indexed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			spec_id    = excluded.spec_id,
			mtime      = excluded.mtime,
			hash       = excluded.hash,
			indexed_at = excluded.indexed_at`,
		doc.Path, doc.SpecID, mtime, hash, time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("upserting spec_search_state for %s: %w", doc.Path, err)
	}

	return tx.Commit()
}

// DeleteSpecSearch removes every indexed row and state entry for a spec.
// Used when a spec is deleted entirely.
func (db *DB) DeleteSpecSearch(ctx context.Context, specID string) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning spec_search delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search WHERE spec_id = ?`, specID); err != nil {
		return fmt.Errorf("deleting spec_search rows for %s: %w", specID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search_state WHERE spec_id = ?`, specID); err != nil {
		return fmt.Errorf("deleting spec_search_state for %s: %w", specID, err)
	}
	return tx.Commit()
}

// DeleteSpecSearchByPath removes indexed rows whose state records the given
// path. Used when a file is renamed/moved (the new file re-indexes under the
// same spec id) or removed from disk.
func (db *DB) DeleteSpecSearchByPath(ctx context.Context, path string) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning spec_search delete-by-path: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete the FTS rows whose spec_id matches the state entry for this path,
	// then drop the state row. This keeps both tables in sync for renames.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM spec_search WHERE spec_id IN (
			SELECT spec_id FROM spec_search_state WHERE path = ?
		)`, path); err != nil {
		return fmt.Errorf("deleting spec_search rows for path %s: %w", path, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search_state WHERE path = ?`, path); err != nil {
		return fmt.Errorf("deleting spec_search_state for path %s: %w", path, err)
	}
	return tx.Commit()
}

// QuerySpecSearch runs an FTS5 MATCH query against the index and returns
// bm25-ranked, section-anchored rows. match must be a valid FTS5 query
// expression (the caller tokenises/escapes user input). Lower bm25 score is
// better (the function returns negative scores).
func (db *DB) QuerySpecSearch(ctx context.Context, match string, scope SearchScope, limit int) ([]SearchRow, error) {
	if limit <= 0 {
		limit = 50
	}

	// Build the bm25() weight argument list once.
	var weightsArg string
	for i, w := range bm25Weights {
		if i > 0 {
			weightsArg += ", "
		}
		weightsArg += fmt.Sprintf("%v", w)
	}

	q := fmt.Sprintf(`
		SELECT spec_id, title, status, author, archived, section_slug, section_heading,
		       snippet(spec_search, %d, '⟨', '⟩', '…', 8),
		       bm25(spec_search, %s) AS score
		FROM spec_search
		WHERE spec_search MATCH ?
	`, snippetColumn, weightsArg)

	switch scope {
	case ScopeActive:
		q += `AND archived = '0' `
	case ScopeArchived:
		q += `AND archived = '1' `
	}
	q += `ORDER BY score ASC LIMIT ?`

	rows, err := db.conn.QueryContext(ctx, q, match, limit)
	if err != nil {
		return nil, fmt.Errorf("querying spec_search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SearchRow
	for rows.Next() {
		var r SearchRow
		var archived string
		if err := rows.Scan(&r.SpecID, &r.Title, &r.Status, &r.Author, &archived,
			&r.SectionSlug, &r.SectionHeading, &r.Snippet, &r.Score); err != nil {
			return nil, fmt.Errorf("scanning spec_search row: %w", err)
		}
		r.Archived = archived == "1"
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchStateDiff compares the on-disk path→mtime map against the indexed
// state and returns the paths that need (re)indexing and the paths that have
// been removed from disk (and so should be dropped from the index).
func (db *DB) SearchStateDiff(ctx context.Context, disk map[string]int64) (changed, removed []string, err error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT path, mtime FROM spec_search_state`)
	if err != nil {
		return nil, nil, fmt.Errorf("querying spec_search_state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	indexed := make(map[string]int64)
	for rows.Next() {
		var path string
		var mtime int64
		if err := rows.Scan(&path, &mtime); err != nil {
			return nil, nil, fmt.Errorf("scanning spec_search_state: %w", err)
		}
		indexed[path] = mtime
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	for path, mt := range disk {
		if prev, ok := indexed[path]; !ok || prev != mt {
			changed = append(changed, path)
		}
	}
	for path := range indexed {
		if _, ok := disk[path]; !ok {
			removed = append(removed, path)
		}
	}
	return changed, removed, nil
}

// SearchStateHash returns the stored content hash for a path, or "" if the
// path is not indexed. Lets the indexer cheaply skip mtime-only touches.
func (db *DB) SearchStateHash(ctx context.Context, path string) (string, error) {
	var hash string
	err := db.conn.QueryRowContext(ctx,
		`SELECT hash FROM spec_search_state WHERE path = ?`, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying spec_search_state hash: %w", err)
	}
	return hash, nil
}

// SearchIndexEmpty reports whether the index has never been populated. The
// search package uses this to decide whether to fall back to a live scan.
func (db *DB) SearchIndexEmpty(ctx context.Context) (bool, error) {
	var n int
	err := db.conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM spec_search_state LIMIT 1)`).Scan(&n)
	if err != nil {
		return true, fmt.Errorf("checking spec_search_state emptiness: %w", err)
	}
	return n == 0, nil
}

// TruncateSearchIndex drops all indexed rows and state. Used by `spec search
// --reindex` to force a clean full rebuild.
func (db *DB) TruncateSearchIndex(ctx context.Context) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning spec_search truncate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// FTS5 `DELETE` removes all rows when unqualified.
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search`); err != nil {
		return fmt.Errorf("deleting spec_search rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_search_state`); err != nil {
		return fmt.Errorf("deleting spec_search_state rows: %w", err)
	}
	return tx.Commit()
}
