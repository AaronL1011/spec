// Package search orchestrates the FTS5-backed spec search index and exposes
// a single Search entry point shared by the TUI overlay and the CLI.
//
// The package never opens the database itself: all SQL goes through
// internal/store accessors (AGENTS.md: only internal/store touches SQLite).
// It does parse markdown via internal/markdown, so the dependency arrow is
// store ← search → markdown, with store and markdown independent of each
// other.
package search

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// DefaultLimit caps the number of hits returned by Search when the caller
// does not specify one. The TUI overlay and CLI both leave it at this.
const DefaultLimit = 50

// SearchScope is re-exported from store so callers depend on search, not
// store, for the public surface.
type SearchScope = store.SearchScope

const (
	ScopeAll      = store.ScopeAll
	ScopeActive   = store.ScopeActive
	ScopeArchived = store.ScopeArchived
)

// Options tunes a Search call.
type Options struct {
	Scope SearchScope
	Limit int // 0 → DefaultLimit
}

// Hit is one ranked, section-anchored search result. Both the TUI overlay and
// `spec search --json` consume this shape.
type Hit struct {
	SpecID         string  `json:"spec_id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	Author         string  `json:"author,omitempty"`
	Archived       bool    `json:"archived"`
	SectionSlug    string  `json:"section_slug"`
	SectionHeading string  `json:"section_heading"`
	Snippet        string  `json:"snippet"`
	Score          float64 `json:"score"`
}

// Stats summarises one Reconcile pass. The overlay shows `indexing…` while
// in flight and the CLI prints the final numbers for `--reindex`.
type Stats struct {
	Scanned   int           `json:"scanned"`
	Reindexed int           `json:"reindexed"`
	Deleted   int           `json:"deleted"`
	Skipped   int           `json:"skipped"` // unchanged (mtime+hash match)
	Failed    int           `json:"failed"`  // parse failures skipped, not fatal
	Elapsed   time.Duration `json:"elapsed"`
}

// Indexer scans the specs repo (active + archive) and keeps the FTS5 index in
// sync, then answers ranked queries. One instance per process; the TUI keeps
// it for the lifetime of the App.
type Indexer struct {
	rc *config.ResolvedConfig
	db *store.DB
}

// NewIndexer builds an Indexer over the resolved config and shared store. A
// nil db is tolerated: Search degrades to the live fallback scan and
// Reconcile is a no-op. This keeps the TUI working when store.Open failed.
func NewIndexer(rc *config.ResolvedConfig, db *store.DB) *Indexer {
	return &Indexer{rc: rc, db: db}
}

// Reconcile scans the specs repo (active + archive) and re-indexes only the
// files whose mtime changed since the last index (a hash check then skips
// mtime-only touches), removes index rows for files gone from disk, and
// returns aggregate stats. It is safe to call repeatedly — on an unchanged
// repo it does only the directory scans plus a diff and returns near-zero
// work. A single unparseable spec is counted in Stats.Failed and skipped; it
// never aborts the pass.
func (ix *Indexer) Reconcile(ctx context.Context) (Stats, error) {
	start := time.Now()
	var st Stats

	if ix.rc == nil || ix.rc.SpecsRepoDir == "" || ix.db == nil {
		return st, nil
	}

	disk := scanSpecFiles(ix.rc)
	st.Scanned = len(disk)

	changed, removed, err := ix.db.SearchStateDiff(ctx, disk)
	if err != nil {
		return st, fmt.Errorf("diffing spec_search_state: %w", err)
	}

	for _, path := range changed {
		s, err := ix.indexOne(ctx, path)
		if err != nil {
			st.Failed++
			continue
		}
		if s {
			st.Reindexed++
		} else {
			st.Skipped++
		}
	}
	for _, path := range removed {
		if err := ix.db.DeleteSpecSearchByPath(ctx, path); err != nil {
			st.Failed++
			continue
		}
		st.Deleted++
	}
	st.Elapsed = time.Since(start)
	return st, nil
}

// indexOne reads, hashes, and indexes a single spec file. Returns true when
// the index was actually written; false when the content hash matched the
// stored state (an mtime-only touch).
func (ix *Indexer) indexOne(ctx context.Context, path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, ix.db.DeleteSpecSearchByPath(ctx, path)
		}
		return false, fmt.Errorf("stating %s: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	// mtime moved but content identical → skip the reindex.
	if prev, _ := ix.db.SearchStateHash(ctx, path); prev == hash {
		return false, nil
	}

	meta, err := markdown.ParseMeta(string(data))
	if err != nil {
		return false, fmt.Errorf("parsing meta %s: %w", path, err)
	}
	sections := markdown.ExtractSections(markdown.Body(string(data)))
	if len(sections) == 0 {
		return false, nil // nothing to index
	}

	doc := store.SearchDoc{
		SpecID:    meta.ID,
		Path:      path,
		Title:     meta.Title,
		Status:    meta.Status,
		Author:    meta.Author,
		Assignees: meta.Assignees,
		Cycle:     meta.Cycle,
		EpicKey:   meta.EpicKey,
		Archived:  isArchivePath(ix.rc, path),
		Sections:  make([]store.SearchSection, 0, len(sections)),
	}
	for _, sec := range sections {
		// Only level-2 (##) sections are indexable: the detail reader's
		// readableSections only navigates level-2 headings, so deep-linking a
		// hit must land on one. The level-2 section's content already
		// includes its sub-sections' text (ExtractSections keeps deeper
		// headings inside the parent), so nothing is lost. The H1 document
		// title is skipped to avoid a whole-doc duplicate hit.
		if sec.Level != 2 {
			continue
		}
		doc.Sections = append(doc.Sections, store.SearchSection{
			Slug:    sec.Slug,
			Heading: sec.Heading,
			Body:    sec.Content,
		})
	}
	if err := ix.db.UpsertSpecSections(ctx, doc, info.ModTime().Unix(), hash); err != nil {
		return false, fmt.Errorf("upserting %s: %w", path, err)
	}
	return true, nil
}

// Search answers a ranked, section-anchored query. When the index has never
// been populated (cold start) it transparently falls back to a live substring
// scan over the same scope, so search is never broken while the background
// reconcile catches up. A nil db always uses the fallback.
func (ix *Indexer) Search(ctx context.Context, query string, opts Options) ([]Hit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	if ix.db != nil {
		empty, err := ix.db.SearchIndexEmpty(ctx)
		if err == nil && !empty {
			hits, err := ix.queryFTS(ctx, query, opts.Scope, limit)
			if err == nil {
				return hits, nil
			}
			// A query error (e.g. an edge-case MATCH expression) should not
			// leave the user empty-handed; fall through to the live scan.
		}
	}
	return fallbackScan(ctx, ix.rc, query, opts.Scope, limit), nil
}

// queryFTS builds a safe FTS5 MATCH expression from user input and returns
// ranked hits. It tries exact quoted tokens first; if there are zero hits it
// retries once with quoted-prefix queries so partial tokens still match.
func (ix *Indexer) queryFTS(ctx context.Context, query string, scope SearchScope, limit int) ([]Hit, error) {
	match := buildMatch(query)
	if match == "" {
		return nil, nil
	}
	rows, err := ix.db.QuerySpecSearch(ctx, match, scope, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if prefix := buildPrefixMatch(query); prefix != "" && prefix != match {
			rows, err = ix.db.QuerySpecSearch(ctx, prefix, scope, limit)
			if err != nil {
				return nil, err
			}
		}
	}
	hits := make([]Hit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, Hit{
			SpecID:         r.SpecID,
			Title:          r.Title,
			Status:         r.Status,
			Author:         r.Author,
			Archived:       r.Archived,
			SectionSlug:    r.SectionSlug,
			SectionHeading: r.SectionHeading,
			Snippet:        r.Snippet,
			Score:          r.Score,
		})
	}
	return hits, nil
}

// IndexEmpty reports whether the FTS index has never been populated, so the
// overlay can show its `indexing…` chip on a cold open. Nil-safe.
func (ix *Indexer) IndexEmpty() (bool, error) {
	if ix.db == nil {
		return true, nil
	}
	return ix.db.SearchIndexEmpty(context.Background())
}

// KeywordScan performs a literal keyword substring scan (the relocated
// cmd/search.go logic) and returns unranked hits. `spec context` uses this so
// its keyword-search behaviour is unchanged after the searchDir helper moved
// out of cmd/.
func KeywordScan(rc *config.ResolvedConfig, keyword string) []Hit {
	return fallbackScan(context.Background(), rc, keyword, ScopeAll, DefaultLimit)
}

// scanSpecFiles walks the active specs dir and the archive sub-dir, returning
// a path→mtime map of every .md file. Missing dirs (e.g. no archive yet) are
// silently skipped.
func scanSpecFiles(rc *config.ResolvedConfig) map[string]int64 {
	disk := make(map[string]int64)
	addDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return // missing dir is fine (empty archive, fresh install)
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			info, err := e.Info()
			if err != nil {
				continue
			}
			disk[path] = info.ModTime().Unix()
		}
	}
	addDir(rc.SpecsRepoDir)
	addDir(filepath.Join(rc.SpecsRepoDir, config.ArchiveDir(rc.Team)))
	return disk
}

// isArchivePath reports whether path lives under the archive directory.
func isArchivePath(rc *config.ResolvedConfig, path string) bool {
	archDir := filepath.Join(rc.SpecsRepoDir, config.ArchiveDir(rc.Team))
	rel, err := filepath.Rel(archDir, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
