package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PM sync operation kinds queued for retry.
const (
	PMOpCreate = "create" // create-or-find the epic and persist its key
	PMOpLink   = "link"   // set the remote back-link on the epic
	PMOpStatus = "status" // transition the epic to the mapped board status
	PMOpStory  = "story"  // reconcile per-step stories under the epic
)

// PMQueueItem is a PM-tool operation that failed and must be retried so the
// board never silently drifts from spec state (docs/JIRA_HARDENING_PLAN.md §P5).
type PMQueueItem struct {
	ID        int64
	SpecID    string
	EpicKey   string
	Op        string
	Payload   string // op-specific: target status, spec URL, etc.
	Status    string // queued | needs-resolution
	Attempts  int
	Detail    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PMQueueEnqueue records a failed PM operation for later retry. It collapses
// duplicates: a queued op with the same (spec_id, op, payload) is updated
// rather than duplicated, so repeated failures don't pile up.
func (db *DB) PMQueueEnqueue(item PMQueueItem) (int64, error) {
	status := item.Status
	if status == "" {
		status = QueueStatusQueued
	}
	now := time.Now().Unix()

	var id int64
	err := db.conn.QueryRow(
		`SELECT id FROM pm_queue
		 WHERE spec_id = ? AND op = ? AND COALESCE(payload,'') = COALESCE(?,'') AND status = ?`,
		item.SpecID, item.Op, item.Payload, QueueStatusQueued,
	).Scan(&id)
	if err == nil {
		if _, err := db.conn.Exec(
			`UPDATE pm_queue SET epic_key = ?, attempts = attempts + 1, detail = ?, updated_at = ? WHERE id = ?`,
			item.EpicKey, item.Detail, now, id,
		); err != nil {
			return 0, fmt.Errorf("update pm queue %d: %w", id, err)
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("pm queue dedupe lookup: %w", err)
	}

	res, err := db.conn.Exec(
		`INSERT INTO pm_queue (spec_id, epic_key, op, payload, status, attempts, detail, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.SpecID, item.EpicKey, item.Op, item.Payload, status, item.Attempts, item.Detail, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("enqueue pm op: %w", err)
	}
	newID, _ := res.LastInsertId()
	return newID, nil
}

// PMQueuePending returns queued PM operations, oldest first. When specID is
// non-empty the result is scoped to that spec.
func (db *DB) PMQueuePending(specID string) ([]PMQueueItem, error) {
	query := `SELECT id, spec_id, epic_key, op, payload, status, attempts, detail, created_at, updated_at
		 FROM pm_queue WHERE status = ?`
	args := []interface{}{QueueStatusQueued}
	if specID != "" {
		query += ` AND spec_id = ?`
		args = append(args, specID)
	}
	query += ` ORDER BY id ASC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("pm queue pending: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPMQueue(rows)
}

// PMQueueCount returns the number of queued PM operations.
func (db *DB) PMQueueCount() (int, error) {
	var n int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM pm_queue WHERE status = ?`, QueueStatusQueued,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("pm queue count: %w", err)
	}
	return n, nil
}

// PMQueueResolve removes a queued PM operation once it has succeeded.
func (db *DB) PMQueueResolve(id int64) error {
	if _, err := db.conn.Exec(`DELETE FROM pm_queue WHERE id = ?`, id); err != nil {
		return fmt.Errorf("resolve pm queue %d: %w", id, err)
	}
	return nil
}

// PMQueueMark updates a queued PM operation's status and detail.
func (db *DB) PMQueueMark(id int64, status, detail string) error {
	if _, err := db.conn.Exec(
		`UPDATE pm_queue SET status = ?, detail = ?, attempts = attempts + 1, updated_at = ? WHERE id = ?`,
		status, detail, time.Now().Unix(), id,
	); err != nil {
		return fmt.Errorf("mark pm queue %d: %w", id, err)
	}
	return nil
}

func scanPMQueue(rows *sql.Rows) ([]PMQueueItem, error) {
	var out []PMQueueItem
	for rows.Next() {
		var it PMQueueItem
		var created, updated int64
		var epicKey, payload, detail sql.NullString
		if err := rows.Scan(&it.ID, &it.SpecID, &epicKey, &it.Op, &payload, &it.Status, &it.Attempts, &detail, &created, &updated); err != nil {
			return nil, fmt.Errorf("scanning pm queue: %w", err)
		}
		it.EpicKey = epicKey.String
		it.Payload = payload.String
		it.Detail = detail.String
		it.CreatedAt = time.Unix(created, 0)
		it.UpdatedAt = time.Unix(updated, 0)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating pm queue: %w", err)
	}
	return out, nil
}
