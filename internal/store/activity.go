package store

import (
	"fmt"
	"time"
)

// ActorKind classifies who performed a logged action. Agent tool calls are
// distinguished from human CLI actions so the activity log stays readable when
// both edit the same spec.
type ActorKind string

const (
	// ActorHuman is a person driving the CLI or TUI. The default for every
	// entry written before agents could author specs.
	ActorHuman ActorKind = "human"
	// ActorAgent is a coding agent acting through the MCP authoring port.
	ActorAgent ActorKind = "agent"
)

// ActivityEntry represents a single event in the activity log.
type ActivityEntry struct {
	ID        int64
	SpecID    string
	EventType string
	Summary   string
	Metadata  string
	UserName  string
	// ActorKind is who acted. Rows written before this column existed read
	// back as ActorHuman.
	ActorKind ActorKind
	CreatedAt time.Time
}

// ActivityLog appends a human-attributed event to the activity log. This is the
// CLI and TUI path; agent tool calls use ActivityLogAs.
func (db *DB) ActivityLog(specID, eventType, summary, metadata, userName string) error {
	return db.ActivityLogAs(specID, eventType, summary, metadata, userName, ActorHuman)
}

// ActivityLogAs appends an event attributed to a specific actor kind. An empty
// kind is recorded as human, so a caller that forgets cannot silently create
// unattributed rows.
func (db *DB) ActivityLogAs(specID, eventType, summary, metadata, userName string, kind ActorKind) error {
	if kind == "" {
		kind = ActorHuman
	}
	_, err := db.conn.Exec(
		`INSERT INTO activity (spec_id, event_type, summary, metadata, user_name, actor_kind, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		specID, eventType, summary, metadata, userName, string(kind), time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("activity log %q: %w", specID, err)
	}
	return nil
}

// ActivitySince returns activity entries since the given time.
func (db *DB) ActivitySince(since time.Time) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(
		`SELECT id, spec_id, event_type, summary, metadata, user_name, actor_kind, created_at
		 FROM activity WHERE created_at >= ? ORDER BY created_at DESC`,
		since.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("activity since: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanActivityRows(rows)
}

// ActivityForSpec returns activity entries for a specific spec.
func (db *DB) ActivityForSpec(specID string, limit int) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(
		`SELECT id, spec_id, event_type, summary, metadata, user_name, actor_kind, created_at
		 FROM activity WHERE spec_id = ? ORDER BY created_at DESC LIMIT ?`,
		specID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("activity for spec %q: %w", specID, err)
	}
	defer func() { _ = rows.Close() }()

	return scanActivityRows(rows)
}

// ActivityPrune removes activity older than the given duration.
func (db *DB) ActivityPrune(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Unix()
	result, err := db.conn.Exec("DELETE FROM activity WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("activity prune: %w", err)
	}
	return result.RowsAffected()
}

// ActivityCountByType returns event counts grouped by event_type since the given time.
func (db *DB) ActivityCountByType(since time.Time) (map[string]int, error) {
	rows, err := db.conn.Query(
		`SELECT event_type, COUNT(*) FROM activity WHERE created_at >= ? GROUP BY event_type`,
		since.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("activity count by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("scanning count row: %w", err)
		}
		counts[eventType] = count
	}
	return counts, rows.Err()
}

// ActivityForType returns all entries of a specific event type since the given time.
func (db *DB) ActivityForType(eventType string, since time.Time) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(
		`SELECT id, spec_id, event_type, summary, metadata, user_name, actor_kind, created_at
		 FROM activity WHERE event_type = ? AND created_at >= ? ORDER BY created_at ASC`,
		eventType, since.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("activity for type %q: %w", eventType, err)
	}
	defer func() { _ = rows.Close() }()

	return scanActivityRows(rows)
}

func scanActivityRows(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}) ([]ActivityEntry, error) {
	type scanner interface {
		Next() bool
		Scan(dest ...interface{}) error
		Err() error
	}
	r := rows.(scanner)

	var entries []ActivityEntry
	for r.Next() {
		var e ActivityEntry
		var createdAt int64
		var metadata *string
		var actorKind *string
		if err := r.Scan(&e.ID, &e.SpecID, &e.EventType, &e.Summary, &metadata, &e.UserName, &actorKind, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning activity row: %w", err)
		}
		e.CreatedAt = time.Unix(createdAt, 0)
		if metadata != nil {
			e.Metadata = *metadata
		}
		e.ActorKind = ActorHuman
		if actorKind != nil && *actorKind != "" {
			e.ActorKind = ActorKind(*actorKind)
		}
		entries = append(entries, e)
	}
	return entries, r.Err()
}
