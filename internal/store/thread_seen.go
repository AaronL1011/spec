package store

import (
	"fmt"
	"time"
)

// ThreadSeen returns the recorded last-seen timestamp per thread ID for a
// spec. A thread with no row has never been seen. Callers treat a thread as
// unread when its latest activity is after the recorded timestamp, or when no
// entry exists.
func (db *DB) ThreadSeen(specID string) (map[string]time.Time, error) {
	rows, err := db.conn.Query(
		`SELECT thread_id, last_seen FROM thread_seen WHERE spec_id = ?`, specID)
	if err != nil {
		return nil, fmt.Errorf("loading thread read-state for %s: %w", specID, err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]time.Time)
	for rows.Next() {
		var threadID string
		var lastSeen int64
		if err := rows.Scan(&threadID, &lastSeen); err != nil {
			return nil, fmt.Errorf("scanning thread read-state for %s: %w", specID, err)
		}
		out[threadID] = time.Unix(lastSeen, 0).UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading thread read-state for %s: %w", specID, err)
	}
	return out, nil
}

// MarkThreadSeen records that the user has viewed a thread up to the given
// activity timestamp. Upserts so repeat views only advance the watermark.
func (db *DB) MarkThreadSeen(specID, threadID string, at time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO thread_seen (spec_id, thread_id, last_seen) VALUES (?, ?, ?)
		 ON CONFLICT(spec_id, thread_id) DO UPDATE SET last_seen = excluded.last_seen`,
		specID, threadID, at.Unix())
	if err != nil {
		return fmt.Errorf("marking thread %s seen for %s: %w", threadID, specID, err)
	}
	return nil
}

// MarkThreadUnseen removes a thread's read-state row so it reads as unread
// again — the manual "keep this for later" toggle in the reader.
func (db *DB) MarkThreadUnseen(specID, threadID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM thread_seen WHERE spec_id = ? AND thread_id = ?`, specID, threadID)
	if err != nil {
		return fmt.Errorf("marking thread %s unread for %s: %w", threadID, specID, err)
	}
	return nil
}
