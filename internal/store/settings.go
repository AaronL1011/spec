package store

import (
	"database/sql"
	"fmt"
	"time"
)

const focusedSpecSettingKey = "focused_spec_id"

// FocusedSpecGet returns the globally focused spec ID, if one is set.
func (db *DB) FocusedSpecGet() (string, error) {
	var specID string
	err := db.conn.QueryRow(
		"SELECT value FROM settings WHERE key = ?", focusedSpecSettingKey,
	).Scan(&specID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("focused spec get: %w", err)
	}
	return specID, nil
}

// FocusedSpecSet stores the globally focused spec ID.
func (db *DB) FocusedSpecSet(specID string) error {
	_, err := db.conn.Exec(
		`INSERT INTO settings (key, value, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		focusedSpecSettingKey, specID, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("focused spec set %q: %w", specID, err)
	}
	return nil
}

// FocusedSpecClear clears the globally focused spec ID.
func (db *DB) FocusedSpecClear() error {
	_, err := db.conn.Exec("DELETE FROM settings WHERE key = ?", focusedSpecSettingKey)
	if err != nil {
		return fmt.Errorf("focused spec clear: %w", err)
	}
	return nil
}

const lastFetchSettingPrefix = "last_fetch:"

// LastFetchGet returns the timestamp of the last successful fetch for a specs
// repo (keyed by "owner/repo"), or the zero time if none is recorded.
func (db *DB) LastFetchGet(repoKey string) (time.Time, error) {
	var unix int64
	err := db.conn.QueryRow(
		"SELECT value FROM settings WHERE key = ?", lastFetchSettingPrefix+repoKey,
	).Scan(&unix)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("last fetch get %q: %w", repoKey, err)
	}
	return time.Unix(unix, 0), nil
}

// LastFetchSet records the timestamp of a successful fetch for a specs repo.
func (db *DB) LastFetchSet(repoKey string, at time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO settings (key, value, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		lastFetchSettingPrefix+repoKey, at.Unix(), time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("last fetch set %q: %w", repoKey, err)
	}
	return nil
}
