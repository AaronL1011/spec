package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// openV6Schema builds an in-memory database frozen at v9 — the last schema
// before the pm_key rename — so the upgrade path is exercised against a
// populated legacy database rather than a fresh one.
func openV9Schema(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	db := &DB{conn: conn, path: ":memory:"}

	if _, err := conn.Exec(`CREATE TABLE migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`); err != nil {
		t.Fatalf("creating migrations table: %v", err)
	}
	for _, migrate := range []func() error{
		db.migrateV1, db.migrateV2, db.migrateV3, db.migrateV4,
		db.migrateV5, db.migrateV6, db.migrateV7, db.migrateV8, db.migrateV9,
	} {
		if err := migrate(); err != nil {
			t.Fatalf("seeding legacy schema: %v", err)
		}
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateV10_RenamesPMQueueColumnPreservingRows(t *testing.T) {
	db := openV9Schema(t)

	// A queued operation written under the old column name.
	if _, err := db.conn.Exec(
		`INSERT INTO pm_queue (spec_id, epic_key, op, payload, status, attempts, created_at, updated_at)
		 VALUES ('SPEC-009', 'PLAT-12', 'status', 'build', 'queued', 1, 0, 0)`); err != nil {
		t.Fatalf("seeding pm_queue: %v", err)
	}

	if err := db.migrateV10(); err != nil {
		t.Fatalf("migrateV10: %v", err)
	}

	items, err := db.PMQueuePending("")
	if err != nil {
		t.Fatalf("PMQueuePending: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d queued items, want the seeded one preserved", len(items))
	}
	if items[0].PMKey != "PLAT-12" {
		t.Errorf("PMKey = %q, want PLAT-12 carried across the rename", items[0].PMKey)
	}
	if items[0].SpecID != "SPEC-009" || items[0].Attempts != 1 {
		t.Errorf("row lost data across the rename: %+v", items[0])
	}
}

func TestMigrateV10_RecreatesSearchIndexAndClearsState(t *testing.T) {
	db := openV9Schema(t)
	ctx := context.Background()

	if _, err := db.conn.Exec(
		`INSERT INTO spec_search_state (path, spec_id, mtime, hash, indexed_at)
		 VALUES ('/specs/SPEC-009.md', 'SPEC-009', 1, 'abc', 1)`); err != nil {
		t.Fatalf("seeding search state: %v", err)
	}

	if err := db.migrateV10(); err != nil {
		t.Fatalf("migrateV10: %v", err)
	}

	// State must be empty, or the next reconcile skips every unchanged file
	// and the freshly recreated index stays empty forever.
	empty, err := db.SearchIndexEmpty(ctx)
	if err != nil {
		t.Fatalf("SearchIndexEmpty: %v", err)
	}
	if !empty {
		t.Error("spec_search_state should be cleared so the next reconcile fully reindexes")
	}

	// The recreated table must accept the new column set in the order
	// search.go's bm25 weights assume.
	if err := db.UpsertSpecSections(ctx, SearchDoc{
		SpecID: "SPEC-009",
		Path:   "/specs/SPEC-009.md",
		Title:  "Token bucket limiter",
		Status: "build",
		PMKey:  "PLAT-12",
		Sections: []SearchSection{
			{Slug: "problem_statement", Heading: "Problem", Body: "tenants saturate the gateway"},
		},
	}, 1, "hash"); err != nil {
		t.Fatalf("UpsertSpecSections after v10: %v", err)
	}

	rows, err := db.QuerySpecSearch(ctx, `"PLAT-12"`, ScopeAll, 10)
	if err != nil {
		t.Fatalf("QuerySpecSearch: %v", err)
	}
	if len(rows) != 1 || rows[0].SpecID != "SPEC-009" {
		t.Errorf("pm_key should be MATCH-able after the rebuild, got %+v", rows)
	}
}

func TestMigrateV10_IsIdempotent(t *testing.T) {
	db := openV9Schema(t)
	if err := db.migrateV10(); err != nil {
		t.Fatalf("first migrateV10: %v", err)
	}
	// A partially-applied or re-run migration must not be fatal; only the
	// duplicate version row makes the second call fail, so drop it first.
	if _, err := db.conn.Exec(`DELETE FROM migrations WHERE version = 10`); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateV10(); err != nil {
		t.Errorf("re-running v10 over an already-renamed column: %v", err)
	}
}

// A fresh database runs every migration in sequence and must end with the
// renamed column, not the one migrateV4 originally created.
func TestOpenMemory_HasPMKeyColumn(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.PMQueueEnqueue(PMQueueItem{SpecID: "SPEC-009", PMKey: "PLAT-12", Op: PMOpStatus}); err != nil {
		t.Fatalf("PMQueueEnqueue on a fresh db: %v", err)
	}
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM pm_queue WHERE pm_key = 'PLAT-12'`).Scan(&n); err != nil {
		t.Fatalf("querying pm_key: %v", err)
	}
	if n != 1 {
		t.Errorf("pm_key column holds %d matching rows, want 1", n)
	}
}
