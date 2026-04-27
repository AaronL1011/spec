// Package store handles all SQLite persistence for spec.
// No other package opens the database or writes raw SQL.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// DB wraps a SQLite database connection.
type DB struct {
	conn *sql.DB
	path string
}

// Open opens or creates the SQLite database at the given path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	db := &DB{conn: conn, path: path}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// OpenMemory opens an in-memory SQLite database for testing.
func OpenMemory() (*DB, error) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("opening in-memory database: %w", err)
	}

	db := &DB{conn: conn, path: ":memory:"}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying sql.DB for direct queries.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

const schemaVersion = 2

func (db *DB) migrate() error {
	// Create migrations table if not exists
	if _, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL DEFAULT (unixepoch())
		)
	`); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	var currentVersion int
	err := db.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("checking migration version: %w", err)
	}

	if currentVersion < 1 {
		if err := db.migrateV1(); err != nil {
			return err
		}
	}
	if currentVersion < 2 {
		if err := db.migrateV2(); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) migrateV1() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v1: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		// Dashboard cache: stores aggregated signals with TTL
		`CREATE TABLE IF NOT EXISTS cache (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			fetched_at INTEGER NOT NULL,
			ttl        INTEGER NOT NULL DEFAULT 300
		)`,

		// Build sessions: one row per active spec build
		`CREATE TABLE IF NOT EXISTS sessions (
			spec_id    TEXT PRIMARY KEY,
			state      TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,

		// Activity log: append-only event log per spec
		`CREATE TABLE IF NOT EXISTS activity (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			spec_id    TEXT NOT NULL,
			event_type TEXT NOT NULL,
			summary    TEXT NOT NULL,
			metadata   TEXT,
			user_name  TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_spec ON activity(spec_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_time ON activity(created_at)`,

		// Embeddings: vector storage for semantic search
		`CREATE TABLE IF NOT EXISTS embeddings (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			spec_id    TEXT NOT NULL,
			section    TEXT NOT NULL,
			content    TEXT NOT NULL,
			vector     BLOB NOT NULL,
			model      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_embed_spec ON embeddings(spec_id)`,

		// Sync state: tracks last-synced hashes per section per spec
		`CREATE TABLE IF NOT EXISTS sync_state (
			spec_id   TEXT NOT NULL,
			section   TEXT NOT NULL,
			direction TEXT NOT NULL,
			hash      TEXT NOT NULL,
			synced_at INTEGER NOT NULL,
			PRIMARY KEY (spec_id, section, direction)
		)`,

		// Record migration
		`INSERT INTO migrations (version) VALUES (1)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v1 statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	return tx.Commit()
}

func (db *DB) migrateV2() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v2: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		// Settings: durable local CLI preferences and state.
		`CREATE TABLE IF NOT EXISTS settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,

		// Record migration
		`INSERT INTO migrations (version) VALUES (2)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v2 statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	return tx.Commit()
}

// DefaultDBPath returns the default database path.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".spec", "spec.db")
	}
	return filepath.Join(home, ".spec", "spec.db")
}
