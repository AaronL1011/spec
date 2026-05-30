package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Sync audit surfaces — which client triggered a sync action.
const (
	SurfaceCLI = "cli"
	SurfaceTUI = "tui"
	SurfaceMCP = "mcp"
)

// Sync audit outcomes.
const (
	OutcomeOK       = "ok"
	OutcomeQueued   = "queued"
	OutcomeConflict = "conflict"
	OutcomeError    = "error"
)

// maxAuditRows caps the sync_audit table; older rows are pruned on insert so
// the local DB never bloats. See SPEC-013 §7.2 (audit log growth).
const maxAuditRows = 2000

// SyncAuditEntry is one recorded sync operation (fetch / commit / push /
// deferral / auto-recovery) with actor, surface, trigger, and outcome.
type SyncAuditEntry struct {
	ID        int64
	Op        string // fetch | commit | push | recover | queue-flush
	Actor     string // git user.name
	Surface   string // cli | tui | mcp
	Trigger   string // command/action name
	SpecID    string // optional
	Outcome   string // ok | queued | conflict | error
	Detail    string // free-form context
	CreatedAt time.Time
}

// SyncAuditLog appends one sync audit entry and prunes the table to maxAuditRows.
func (db *DB) SyncAuditLog(entry SyncAuditEntry) error {
	now := time.Now().Unix()
	res, err := db.conn.Exec(
		`INSERT INTO sync_audit (op, actor, surface, trigger, spec_id, outcome, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Op, entry.Actor, entry.Surface, entry.Trigger, entry.SpecID, entry.Outcome, entry.Detail, now,
	)
	if err != nil {
		return fmt.Errorf("sync audit log: %w", err)
	}
	// Prune oldest rows beyond the cap. Cheap and bounded.
	if id, err := res.LastInsertId(); err == nil && id > maxAuditRows {
		_, _ = db.conn.Exec(
			`DELETE FROM sync_audit WHERE id <= ?`, id-maxAuditRows,
		)
	}
	return nil
}

// SyncAuditRecent returns the most recent sync audit entries, newest first.
func (db *DB) SyncAuditRecent(limit int) ([]SyncAuditEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(
		`SELECT id, op, actor, surface, trigger, spec_id, outcome, detail, created_at
		 FROM sync_audit
		 ORDER BY id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sync audit recent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []SyncAuditEntry
	for rows.Next() {
		var e SyncAuditEntry
		var createdAt int64
		var specID, detail sql.NullString
		if err := rows.Scan(&e.ID, &e.Op, &e.Actor, &e.Surface, &e.Trigger, &specID, &e.Outcome, &detail, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning sync audit: %w", err)
		}
		e.SpecID = specID.String
		e.Detail = detail.String
		e.CreatedAt = time.Unix(createdAt, 0)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync audit: %w", err)
	}
	return entries, nil
}
