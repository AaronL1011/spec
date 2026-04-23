package store

import (
	"fmt"
	"time"
)

// ActivityEntry represents a single event in the activity log.
type ActivityEntry struct {
	ID        int64
	SpecID    string
	EventType string
	Summary   string
	Metadata  string
	UserName  string
	CreatedAt time.Time
}

// ActivityLog appends an event to the activity log.
func (db *DB) ActivityLog(specID, eventType, summary, metadata, userName string) error {
	_, err := db.conn.Exec(
		`INSERT INTO activity (spec_id, event_type, summary, metadata, user_name, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		specID, eventType, summary, metadata, userName, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("activity log %q: %w", specID, err)
	}
	return nil
}

// ActivitySince returns activity entries since the given time.
func (db *DB) ActivitySince(since time.Time) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(
		`SELECT id, spec_id, event_type, summary, metadata, user_name, created_at
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
		`SELECT id, spec_id, event_type, summary, metadata, user_name, created_at
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
		if err := r.Scan(&e.ID, &e.SpecID, &e.EventType, &e.Summary, &metadata, &e.UserName, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning activity row: %w", err)
		}
		e.CreatedAt = time.Unix(createdAt, 0)
		if metadata != nil {
			e.Metadata = *metadata
		}
		entries = append(entries, e)
	}
	return entries, r.Err()
}
