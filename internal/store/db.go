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

const schemaVersion = 6

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
	if currentVersion < 3 {
		if err := db.migrateV3(); err != nil {
			return err
		}
	}
	if currentVersion < 4 {
		if err := db.migrateV4(); err != nil {
			return err
		}
	}
	if currentVersion < 5 {
		if err := db.migrateV5(); err != nil {
			return err
		}
	}
	if currentVersion < 6 {
		if err := db.migrateV6(); err != nil {
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

func (db *DB) migrateV3() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v3: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		// Sync audit: append-only log of every fetch/commit/push/recovery
		// with actor, surface, trigger, and outcome. Substrate for the
		// `spec status` freshness/health line. (SPEC-013 §Decision 007)
		`CREATE TABLE IF NOT EXISTS sync_audit (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			op         TEXT NOT NULL,
			actor      TEXT NOT NULL,
			surface    TEXT NOT NULL,
			trigger    TEXT NOT NULL,
			spec_id    TEXT,
			outcome    TEXT NOT NULL,
			detail     TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_audit_time ON sync_audit(created_at)`,

		// Sync queue: committed-but-unpushed operations awaiting an online
		// flush. Each entry is reconciled independently. (SPEC-013 §7.1)
		`CREATE TABLE IF NOT EXISTS sync_queue (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_key   TEXT NOT NULL,
			branch     TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			surface    TEXT NOT NULL,
			trigger    TEXT NOT NULL,
			spec_id    TEXT,
			status     TEXT NOT NULL,
			detail     TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_queue_repo ON sync_queue(repo_key, status)`,

		// Record migration
		`INSERT INTO migrations (version) VALUES (3)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v3 statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	return tx.Commit()
}

func (db *DB) migrateV4() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v4: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		// PM sync queue: PM-tool operations (epic create/link, status
		// transition, story sync) that failed and must be retried so the Jira
		// board never silently drifts from spec state.
		// (docs/JIRA_HARDENING_PLAN.md §P5)
		`CREATE TABLE IF NOT EXISTS pm_queue (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			spec_id    TEXT NOT NULL,
			epic_key   TEXT,
			op         TEXT NOT NULL,
			payload    TEXT,
			status     TEXT NOT NULL,
			attempts   INTEGER NOT NULL DEFAULT 0,
			detail     TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_queue_spec ON pm_queue(spec_id, status)`,

		`INSERT INTO migrations (version) VALUES (4)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v4 statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	return tx.Commit()
}

// migrateV5 creates the FTS5-backed spec search index (spec_search) and its
// incremental-reindex state table (spec_search_state). See SPEC-028.
func (db *DB) migrateV5() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v5: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		// Full-text search index for spec bodies, one row per (spec, section).
		// The TUI global search overlay (SPEC-028) queries this with bm25()
		// ranking and falls back to a live substring scan while it is cold.
		// UNINDEXED columns are stored but not tokenised: they participate in
		// SELECT/filter but not in MATCH scoring.
		`CREATE VIRTUAL TABLE IF NOT EXISTS spec_search USING fts5(
			spec_id UNINDEXED,
			section_slug UNINDEXED,
			section_heading,
			title,
			status UNINDEXED,
			body,
			archived UNINDEXED,
			path UNINDEXED,
			tokenize = 'unicode61 remove_diacritics 2'
		)`,
		// Reconciliation state: drives incremental reindex by mtime + content
		// hash so a no-op reconcile on an unchanged repo is near-free.
		`CREATE TABLE IF NOT EXISTS spec_search_state (
			path       TEXT PRIMARY KEY,
			spec_id    TEXT NOT NULL,
			mtime      INTEGER NOT NULL,
			hash       TEXT NOT NULL,
			indexed_at INTEGER NOT NULL
		)`,

		`INSERT INTO migrations (version) VALUES (5)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v5 statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	return tx.Commit()
}

// migrateV6 rebuilds the spec search index with searchable metadata: spec id,
// status, author, assignees, cycle, and epic key become MATCH-able columns
// (SPEC-028 follow-up — "search by title, authors and other useful
// metadata"). FTS5 virtual tables cannot ALTER their column set, so the table
// is dropped and recreated; clearing spec_search_state forces the next
// reconcile to fully re-populate it.
func (db *DB) migrateV6() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration v6: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is no-op after Commit

	statements := []string{
		`DROP TABLE IF EXISTS spec_search`,
		// Column order matters: bm25 weights and the snippet() column index in
		// internal/store/search.go mirror this exact order.
		`CREATE VIRTUAL TABLE spec_search USING fts5(
			spec_id,
			section_slug UNINDEXED,
			section_heading,
			title,
			status,
			body,
			author,
			assignees,
			cycle,
			epic_key,
			archived UNINDEXED,
			path UNINDEXED,
			tokenize = 'unicode61 remove_diacritics 2'
		)`,
		// Force a full re-index on the next reconcile: state rows are the
		// only thing that suppresses re-indexing of unchanged files.
		`DELETE FROM spec_search_state`,

		`INSERT INTO migrations (version) VALUES (6)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration v6 statement failed: %w\nSQL: %s", err, stmt)
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
