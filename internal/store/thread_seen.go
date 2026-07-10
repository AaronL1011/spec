package store

import (
	"fmt"
	"strings"
	"time"
)

const localThreadViewer = "_local"

func threadViewer(handle string) string {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return localThreadViewer
	}
	return handle
}

// ThreadSeen returns the recorded last-seen timestamp per thread ID for a
// spec and viewer. A thread with no row has never been seen.
func (db *DB) ThreadSeen(specID, viewer string) (map[string]time.Time, error) {
	rows, err := db.conn.Query(
		`SELECT thread_id, last_seen FROM thread_seen WHERE spec_id = ? AND user_handle = ?`,
		specID, threadViewer(viewer))
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
		out[threadID] = time.UnixMilli(lastSeen).UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading thread read-state for %s: %w", specID, err)
	}
	return out, nil
}

// MarkThreadSeen records that a viewer has viewed a thread up to the given
// activity timestamp. The watermark is monotonic so out-of-order async writes
// cannot make an already-read thread unread again.
func (db *DB) MarkThreadSeen(specID, threadID, viewer string, at time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO thread_seen (spec_id, thread_id, user_handle, last_seen) VALUES (?, ?, ?, ?)
		 ON CONFLICT(spec_id, thread_id, user_handle) DO UPDATE
		 SET last_seen = MAX(thread_seen.last_seen, excluded.last_seen)`,
		specID, threadID, threadViewer(viewer), at.UnixMilli())
	if err != nil {
		return fmt.Errorf("marking thread %s seen for %s: %w", threadID, specID, err)
	}
	return nil
}

// MarkThreadUnseen removes a viewer's thread read-state row.
func (db *DB) MarkThreadUnseen(specID, threadID, viewer string) error {
	_, err := db.conn.Exec(
		`DELETE FROM thread_seen WHERE spec_id = ? AND thread_id = ? AND user_handle = ?`,
		specID, threadID, threadViewer(viewer))
	if err != nil {
		return fmt.Errorf("marking thread %s unread for %s: %w", threadID, specID, err)
	}
	return nil
}
