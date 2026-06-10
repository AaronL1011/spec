package store

import "testing"

func TestSyncStateGetSet_UpsertsEntry(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", "hash-1"); err != nil {
		t.Fatalf("SyncStateSet() error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", "hash-2"); err != nil {
		t.Fatalf("SyncStateSet() update error = %v", err)
	}

	entry, err := db.SyncStateGet("SPEC-001", "problem_statement", "out")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if entry == nil {
		t.Fatal("SyncStateGet() entry = nil")
		return
	}
	if entry.Hash != "hash-2" {
		t.Fatalf("SyncStateGet() hash = %q, want %q", entry.Hash, "hash-2")
	}
	if entry.SyncedAt.IsZero() {
		t.Fatal("SyncStateGet() SyncedAt is zero")
	}
}

func TestSyncStateGet_MissingEntry_ReturnsNil(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	entry, err := db.SyncStateGet("SPEC-001", "missing", "in")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if entry != nil {
		t.Fatalf("SyncStateGet() entry = %#v, want nil", entry)
	}
}

func TestSyncStateForSpec_GroupsBySectionAndDirection(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	sets := []struct {
		section   string
		direction string
		hash      string
	}{
		{"problem_statement", "in", "in-hash"},
		{"problem_statement", "out", "out-hash"},
		{"acceptance_criteria", "out", "criteria-hash"},
	}
	for _, set := range sets {
		if err := db.SyncStateSet("SPEC-001", set.section, set.direction, set.hash); err != nil {
			t.Fatalf("SyncStateSet(%s/%s) error = %v", set.section, set.direction, err)
		}
	}
	if err := db.SyncStateSet("SPEC-002", "problem_statement", "out", "other"); err != nil {
		t.Fatalf("SyncStateSet(other spec) error = %v", err)
	}

	entries, err := db.SyncStateForSpec("SPEC-001")
	if err != nil {
		t.Fatalf("SyncStateForSpec() error = %v", err)
	}
	if got := entries["problem_statement"]["in"].Hash; got != "in-hash" {
		t.Fatalf("problem_statement/in hash = %q, want %q", got, "in-hash")
	}
	if got := entries["problem_statement"]["out"].Hash; got != "out-hash" {
		t.Fatalf("problem_statement/out hash = %q, want %q", got, "out-hash")
	}
	if _, ok := entries["missing"]; ok {
		t.Fatal("SyncStateForSpec() included unexpected missing section")
	}
}
