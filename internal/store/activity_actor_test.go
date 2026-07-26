package store

import (
	"path/filepath"
	"testing"
	"time"
)

func actorTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "spec.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// A write through the CLI path is attributed to a human, and an authoring-port
// write to an agent, so the two are distinguishable in the log.
func TestActivityLog_ActorAttribution(t *testing.T) {
	db := actorTestDB(t)

	if err := db.ActivityLog("SPEC-001", "section_write", "human edit", "", "aaron"); err != nil {
		t.Fatalf("ActivityLog: %v", err)
	}
	if err := db.ActivityLogAs("SPEC-001", "section_write", "agent edit", "", "aaron", ActorAgent); err != nil {
		t.Fatalf("ActivityLogAs: %v", err)
	}

	entries, err := db.ActivityForSpec("SPEC-001", 10)
	if err != nil {
		t.Fatalf("ActivityForSpec: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	kinds := map[string]ActorKind{}
	for _, e := range entries {
		kinds[e.Summary] = e.ActorKind
	}
	if kinds["human edit"] != ActorHuman {
		t.Errorf("CLI write actor = %q, want human", kinds["human edit"])
	}
	if kinds["agent edit"] != ActorAgent {
		t.Errorf("authoring-port write actor = %q, want agent", kinds["agent edit"])
	}
}

// A caller that forgets to pass a kind must not create unattributed rows.
func TestActivityLogAs_EmptyKindDefaultsToHuman(t *testing.T) {
	db := actorTestDB(t)
	if err := db.ActivityLogAs("SPEC-002", "note", "s", "", "u", ""); err != nil {
		t.Fatalf("ActivityLogAs: %v", err)
	}
	entries, err := db.ActivityForSpec("SPEC-002", 1)
	if err != nil {
		t.Fatalf("ActivityForSpec: %v", err)
	}
	if len(entries) != 1 || entries[0].ActorKind != ActorHuman {
		t.Fatalf("empty kind should record human, got %+v", entries)
	}
}

// The column is added in two places: the CREATE TABLE schema (fresh databases)
// and migrateV9 (existing ones). This simulates a database created before the
// column existed and asserts the ALTER path runs, since updating only the
// CREATE TABLE would leave every existing install without the column.
func TestMigrateV9_AddsColumnToPreExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Recreate the pre-v9 shape: drop the column by rebuilding the table, and
	// roll the recorded version back so the migration runs again on reopen.
	stmts := []string{
		`DROP TABLE activity`,
		`CREATE TABLE activity (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			spec_id    TEXT NOT NULL,
			event_type TEXT NOT NULL,
			summary    TEXT NOT NULL,
			metadata   TEXT,
			user_name  TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`INSERT INTO activity (spec_id, event_type, summary, metadata, user_name, created_at)
		 VALUES ('SPEC-009', 'advance', 'pre-existing row', NULL, 'aaron', ` +
			itoa(time.Now().Unix()) + `)`,
		`DELETE FROM migrations WHERE version >= 9`,
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s); err != nil {
			t.Fatalf("preparing legacy schema: %v\nSQL: %s", err, s)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must migrate rather than fail.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen (migration should add the column): %v", err)
	}
	defer func() { _ = db2.Close() }()

	entries, err := db2.ActivityForSpec("SPEC-009", 10)
	if err != nil {
		t.Fatalf("reading migrated activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want the pre-existing row preserved", len(entries))
	}
	// Existing rows are human by definition: they predate agent authoring.
	if entries[0].ActorKind != ActorHuman {
		t.Errorf("migrated row actor = %q, want human", entries[0].ActorKind)
	}

	// And new agent writes work against the migrated table.
	if err := db2.ActivityLogAs("SPEC-009", "section_write", "agent", "", "aaron", ActorAgent); err != nil {
		t.Fatalf("ActivityLogAs after migration: %v", err)
	}
}

// Reopening a current database must be a no-op rather than failing on a
// duplicate column.
func TestMigrateV9_IdempotentOnFreshDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen should be a no-op, got: %v", err)
	}
	_ = db2.Close()
}

// itoa avoids importing strconv for one call in a test fixture.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
