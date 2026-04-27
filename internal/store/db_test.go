package store

import (
	"testing"
	"time"
)

func mustOpenMemory(t *testing.T) *DB {
	t.Helper()
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrations(t *testing.T) {
	db := mustOpenMemory(t)

	var version int
	err := db.conn.QueryRow("SELECT MAX(version) FROM migrations").Scan(&version)
	if err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("version = %d, want %d", version, schemaVersion)
	}
}

func TestCache(t *testing.T) {
	db := mustOpenMemory(t)

	// Miss
	val, fresh, err := db.CacheGet("missing")
	if err != nil {
		t.Fatalf("CacheGet: %v", err)
	}
	if val != "" || fresh {
		t.Error("expected empty miss")
	}

	// Set and get (fresh)
	if err := db.CacheSet("key1", `{"data":"test"}`, 300); err != nil {
		t.Fatalf("CacheSet: %v", err)
	}
	val, fresh, err = db.CacheGet("key1")
	if err != nil {
		t.Fatalf("CacheGet: %v", err)
	}
	if val != `{"data":"test"}` {
		t.Errorf("value = %q, want %q", val, `{"data":"test"}`)
	}
	if !fresh {
		t.Error("expected fresh")
	}

	// Set with 0 TTL → stale immediately
	if err := db.CacheSet("key2", "stale", 0); err != nil {
		t.Fatalf("CacheSet: %v", err)
	}
	_, fresh, err = db.CacheGet("key2")
	if err != nil {
		t.Fatalf("CacheGet: %v", err)
	}
	if fresh {
		t.Error("expected stale with 0 TTL")
	}
}

func TestSessions(t *testing.T) {
	db := mustOpenMemory(t)

	// No sessions
	id, err := db.SessionMostRecent()
	if err != nil {
		t.Fatalf("SessionMostRecent: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}

	// Create session
	if err := db.SessionSet("SPEC-001", `{"step":1}`); err != nil {
		t.Fatalf("SessionSet: %v", err)
	}

	state, err := db.SessionGet("SPEC-001")
	if err != nil {
		t.Fatalf("SessionGet: %v", err)
	}
	if state != `{"step":1}` {
		t.Errorf("state = %q, want %q", state, `{"step":1}`)
	}

	// Most recent
	id, err = db.SessionMostRecent()
	if err != nil {
		t.Fatalf("SessionMostRecent: %v", err)
	}
	if id != "SPEC-001" {
		t.Errorf("most recent = %q, want %q", id, "SPEC-001")
	}
}

func TestFocusedSpec(t *testing.T) {
	db := mustOpenMemory(t)

	id, err := db.FocusedSpecGet()
	if err != nil {
		t.Fatalf("FocusedSpecGet: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty focused spec, got %q", id)
	}

	if err := db.FocusedSpecSet("SPEC-001"); err != nil {
		t.Fatalf("FocusedSpecSet: %v", err)
	}

	id, err = db.FocusedSpecGet()
	if err != nil {
		t.Fatalf("FocusedSpecGet: %v", err)
	}
	if id != "SPEC-001" {
		t.Errorf("focused spec = %q, want SPEC-001", id)
	}

	if err := db.FocusedSpecSet("SPEC-002"); err != nil {
		t.Fatalf("FocusedSpecSet update: %v", err)
	}

	id, err = db.FocusedSpecGet()
	if err != nil {
		t.Fatalf("FocusedSpecGet after update: %v", err)
	}
	if id != "SPEC-002" {
		t.Errorf("focused spec after update = %q, want SPEC-002", id)
	}

	if err := db.FocusedSpecClear(); err != nil {
		t.Fatalf("FocusedSpecClear: %v", err)
	}

	id, err = db.FocusedSpecGet()
	if err != nil {
		t.Fatalf("FocusedSpecGet after clear: %v", err)
	}
	if id != "" {
		t.Errorf("expected cleared focused spec, got %q", id)
	}
}

func TestActivity(t *testing.T) {
	db := mustOpenMemory(t)

	if err := db.ActivityLog("SPEC-001", "advance", "advanced to build", "", "Aaron"); err != nil {
		t.Fatalf("ActivityLog: %v", err)
	}
	if err := db.ActivityLog("SPEC-001", "decide", "decision #001", `{"number":1}`, "Aaron"); err != nil {
		t.Fatalf("ActivityLog: %v", err)
	}

	entries, err := db.ActivityForSpec("SPEC-001", 10)
	if err != nil {
		t.Fatalf("ActivityForSpec: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// Since
	entries, err = db.ActivitySince(time.Now().Add(-1 * time.Hour))
	if err != nil {
		t.Fatalf("ActivitySince: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}

func TestEmbeddings(t *testing.T) {
	db := mustOpenMemory(t)

	v1 := []float32{1.0, 0.0, 0.0}
	v2 := []float32{0.0, 1.0, 0.0}
	v3 := []float32{0.9, 0.1, 0.0}

	if err := db.EmbeddingUpsert("SPEC-001", "full", "auth spec", v1, "test"); err != nil {
		t.Fatalf("EmbeddingUpsert: %v", err)
	}
	if err := db.EmbeddingUpsert("SPEC-002", "full", "billing spec", v2, "test"); err != nil {
		t.Fatalf("EmbeddingUpsert: %v", err)
	}

	results, err := db.EmbeddingSearch(v3, 2)
	if err != nil {
		t.Fatalf("EmbeddingSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	// v3 is more similar to v1 than v2
	if results[0].SpecID != "SPEC-001" {
		t.Errorf("top result = %q, want SPEC-001", results[0].SpecID)
	}
}
