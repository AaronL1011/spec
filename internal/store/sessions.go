package store

import (
	"database/sql"
	"fmt"
	"time"
)

// SessionGet retrieves the session state JSON for a spec.
func (db *DB) SessionGet(specID string) (string, error) {
	var state string
	err := db.conn.QueryRow(
		"SELECT state FROM sessions WHERE spec_id = ?", specID,
	).Scan(&state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("session get %q: %w", specID, err)
	}
	return state, nil
}

// SessionSet stores session state for a spec.
func (db *DB) SessionSet(specID, state string) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(
		`INSERT INTO sessions (spec_id, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(spec_id) DO UPDATE SET state=excluded.state, updated_at=excluded.updated_at`,
		specID, state, now, now,
	)
	if err != nil {
		return fmt.Errorf("session set %q: %w", specID, err)
	}
	return nil
}

// SessionDelete removes a session.
func (db *DB) SessionDelete(specID string) error {
	_, err := db.conn.Exec("DELETE FROM sessions WHERE spec_id = ?", specID)
	return err
}

// SessionMostRecent returns the spec ID of the most recently updated session.
func (db *DB) SessionMostRecent() (string, error) {
	var specID string
	err := db.conn.QueryRow(
		"SELECT spec_id FROM sessions ORDER BY updated_at DESC LIMIT 1",
	).Scan(&specID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("session most recent: %w", err)
	}
	return specID, nil
}

// SessionList returns all active sessions.
func (db *DB) SessionList() ([]string, error) {
	rows, err := db.conn.Query("SELECT spec_id FROM sessions ORDER BY updated_at DESC")
	if err != nil {
		return nil, fmt.Errorf("session list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("session list scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
