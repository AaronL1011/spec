package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Queued-push states.
const (
	QueueStatusQueued          = "queued"           // awaiting flush
	QueueStatusNeedsResolution = "needs-resolution" // same-section conflict; needs a human
)

// QueuedPush records a committed-but-unpushed operation that must be flushed
// to the remote on a later online operation. Each entry is reconciled
// independently so one conflict never strands the rest (SPEC-013 §7.1).
type QueuedPush struct {
	ID        int64
	RepoKey   string // owner/repo — scopes the queue to a clone
	Branch    string
	CommitSHA string // local commit to flush
	Surface   string
	Trigger   string
	SpecID    string
	Status    string // queued | needs-resolution
	Detail    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// QueuePushEnqueue records a queued (offline / contention-exhausted) push.
func (db *DB) QueuePushEnqueue(q QueuedPush) (int64, error) {
	now := time.Now().Unix()
	status := q.Status
	if status == "" {
		status = QueueStatusQueued
	}
	res, err := db.conn.Exec(
		`INSERT INTO sync_queue (repo_key, branch, commit_sha, surface, trigger, spec_id, status, detail, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.RepoKey, q.Branch, q.CommitSHA, q.Surface, q.Trigger, q.SpecID, status, q.Detail, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("enqueue push: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// QueuePushPending returns queued entries for a repo that should be flushed,
// oldest first. Entries marked needs-resolution are excluded — they require a
// human and must not be retried automatically.
func (db *DB) QueuePushPending(repoKey string) ([]QueuedPush, error) {
	rows, err := db.conn.Query(
		`SELECT id, repo_key, branch, commit_sha, surface, trigger, spec_id, status, detail, created_at, updated_at
		 FROM sync_queue
		 WHERE repo_key = ? AND status = ?
		 ORDER BY id ASC`,
		repoKey, QueueStatusQueued,
	)
	if err != nil {
		return nil, fmt.Errorf("queue pending: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanQueue(rows)
}

// QueuePushCount returns the number of queued (flushable) entries for a repo.
func (db *DB) QueuePushCount(repoKey string) (int, error) {
	var n int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM sync_queue WHERE repo_key = ? AND status = ?`,
		repoKey, QueueStatusQueued,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("queue count: %w", err)
	}
	return n, nil
}

// QueuePushResolve removes a queued entry once it has been flushed.
func (db *DB) QueuePushResolve(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM sync_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("resolve queued push %d: %w", id, err)
	}
	return nil
}

// QueuePushMark updates a queued entry's status (e.g. needs-resolution).
func (db *DB) QueuePushMark(id int64, status, detail string) error {
	_, err := db.conn.Exec(
		`UPDATE sync_queue SET status = ?, detail = ?, updated_at = ? WHERE id = ?`,
		status, detail, time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("mark queued push %d: %w", id, err)
	}
	return nil
}

func scanQueue(rows *sql.Rows) ([]QueuedPush, error) {
	var out []QueuedPush
	for rows.Next() {
		var q QueuedPush
		var created, updated int64
		var specID, detail sql.NullString
		if err := rows.Scan(&q.ID, &q.RepoKey, &q.Branch, &q.CommitSHA, &q.Surface, &q.Trigger, &specID, &q.Status, &detail, &created, &updated); err != nil {
			return nil, fmt.Errorf("scanning queue: %w", err)
		}
		q.SpecID = specID.String
		q.Detail = detail.String
		q.CreatedAt = time.Unix(created, 0)
		q.UpdatedAt = time.Unix(updated, 0)
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating queue: %w", err)
	}
	return out, nil
}
