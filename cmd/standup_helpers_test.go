package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/store"
)

func TestCollectBlockers(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Seed activity: one eject, one advance (should be ignored)
	_ = db.ActivityLog("SPEC-001", "eject", "blocked on upstream", `{"from_stage":"build"}`, "alice")
	_ = db.ActivityLog("SPEC-002", "advance", "advanced to review", `{"from_stage":"draft","to_stage":"review"}`, "bob")
	_ = db.ActivityLog("SPEC-003", "eject", "API contract unresolved", `{"from_stage":"review"}`, "carol")

	blockers := collectBlockers(db)

	if len(blockers) != 2 {
		t.Fatalf("expected 2 blockers, got %d: %v", len(blockers), blockers)
	}

	// Verify format
	found001, found003 := false, false
	for _, b := range blockers {
		if b == "SPEC-001: blocked on upstream" {
			found001 = true
		}
		if b == "SPEC-003: API contract unresolved" {
			found003 = true
		}
	}
	if !found001 || !found003 {
		t.Errorf("unexpected blockers: %v", blockers)
	}
}

func TestCollectBlockers_NilOnError(t *testing.T) {
	// Closed DB should return nil, not panic
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	_ = db.Close()

	blockers := collectBlockers(db)
	if blockers != nil {
		t.Errorf("expected nil for closed DB, got %v", blockers)
	}
}

func TestCollectOwnedSpecs(t *testing.T) {
	dir := t.TempDir()

	// Specs in various stages
	writeSpecFileWithOwner(t, dir, "SPEC-001.md", "draft", "pm")
	writeSpecFileWithOwner(t, dir, "SPEC-002.md", "build", "engineer")
	writeSpecFileWithOwner(t, dir, "SPEC-003.md", "done", "tl")         // terminal — should be excluded
	writeSpecFileWithOwner(t, dir, "SPEC-004.md", "pr-review", "engineer")

	pipe := config.DefaultPipeline()

	// Engineer should see build + pr-review specs
	owned := collectOwnedSpecs(dir, "engineer", pipe)
	if len(owned) != 2 {
		t.Fatalf("expected 2 owned specs for engineer, got %d: %v", len(owned), owned)
	}

	ids := make(map[string]bool)
	for _, s := range owned {
		ids[s.id] = true
	}
	if !ids["SPEC-002"] || !ids["SPEC-004"] {
		t.Errorf("expected SPEC-002 and SPEC-004, got %v", owned)
	}

	// PM should see draft spec
	owned = collectOwnedSpecs(dir, "pm", pipe)
	if len(owned) != 1 || owned[0].id != "SPEC-001" {
		t.Errorf("expected SPEC-001 for pm, got %v", owned)
	}

	// Empty role returns nothing
	owned = collectOwnedSpecs(dir, "", pipe)
	if len(owned) != 0 {
		t.Errorf("expected 0 for empty role, got %d", len(owned))
	}
}

func TestCollectOwnedSpecs_NonexistentDir(t *testing.T) {
	pipe := config.DefaultPipeline()
	owned := collectOwnedSpecs("/nonexistent", "engineer", pipe)
	if len(owned) != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", len(owned))
	}
}

func writeSpecFileWithOwner(t *testing.T, dir, name, status, _ string) {
	t.Helper()
	id := name[:len(name)-3]
	content := "---\nid: " + id + "\ntitle: Test Spec\nstatus: " + status + "\nversion: 0.1.0\nauthor: test\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\n\n# " + id + " - Test\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
