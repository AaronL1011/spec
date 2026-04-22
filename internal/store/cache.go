package store

import (
	"database/sql"
	"fmt"
	"time"
)

// CacheEntry holds a cached value along with its metadata.
type CacheEntry struct {
	Value     string
	Fresh     bool
	FetchedAt time.Time
}

// CacheGet retrieves a cached value by key. Returns the value and whether
// the cache entry is still fresh (within TTL). Returns "", false if not found.
func (db *DB) CacheGet(key string) (value string, fresh bool, err error) {
	e, err := db.CacheGetEntry(key)
	if err != nil {
		return "", false, err
	}
	return e.Value, e.Fresh, nil
}

// CacheGetEntry retrieves a cache entry with full metadata including fetch time.
func (db *DB) CacheGetEntry(key string) (CacheEntry, error) {
	var value string
	var fetchedAt, ttl int64
	err := db.conn.QueryRow(
		"SELECT value, fetched_at, ttl FROM cache WHERE key = ?", key,
	).Scan(&value, &fetchedAt, &ttl)
	if err == sql.ErrNoRows {
		return CacheEntry{}, nil
	}
	if err != nil {
		return CacheEntry{}, fmt.Errorf("cache get %q: %w", key, err)
	}

	fresh := time.Now().Unix()-fetchedAt < ttl
	return CacheEntry{
		Value:     value,
		Fresh:     fresh,
		FetchedAt: time.Unix(fetchedAt, 0),
	}, nil
}

// CacheSet stores a value in the cache with the given TTL in seconds.
func (db *DB) CacheSet(key, value string, ttlSeconds int) error {
	_, err := db.conn.Exec(
		`INSERT INTO cache (key, value, fetched_at, ttl)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, fetched_at=excluded.fetched_at, ttl=excluded.ttl`,
		key, value, time.Now().Unix(), ttlSeconds,
	)
	if err != nil {
		return fmt.Errorf("cache set %q: %w", key, err)
	}
	return nil
}

// CacheDelete removes a cache entry.
func (db *DB) CacheDelete(key string) error {
	_, err := db.conn.Exec("DELETE FROM cache WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("cache delete %q: %w", key, err)
	}
	return nil
}

// CacheClear removes all cache entries.
func (db *DB) CacheClear() error {
	_, err := db.conn.Exec("DELETE FROM cache")
	return err
}
