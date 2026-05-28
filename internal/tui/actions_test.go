package tui

import (
	"testing"

	"github.com/aaronl1011/spec/internal/store"
)

func TestYankSpecID(t *testing.T) {
	// yankSpecID returns a command — just verify it doesn't panic.
	cmd := yankSpecID("SPEC-042")
	if cmd == nil {
		t.Error("yankSpecID should return a non-nil command")
	}
}

func TestFocusSpec(t *testing.T) {
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cmd := focusSpec(db, "SPEC-001")
	if cmd == nil {
		t.Error("focusSpec should return a non-nil command")
	}
}

func TestOpenInBrowser_EmptyURL(t *testing.T) {
	cmd := openInBrowser("")
	if cmd == nil {
		t.Fatal("openInBrowser should return a command even for empty URL")
	}
	// Execute it — should return an error result.
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.Err == nil {
		t.Error("empty URL should produce an error")
	}
}

func TestResolveLocalSpecPath_Fallback(t *testing.T) {
	rc := testResolvedConfig()
	rc.SpecsRepoDir = "/tmp/nonexistent-specs"

	got := resolveLocalSpecPath(rc, "SPEC-001")
	// Should fall back to .spec/ or specsRepoDir path.
	if got == "" {
		t.Error("resolveLocalSpecPath should return a path")
	}
}

func TestFileExists(t *testing.T) {
	if fileExists("/nonexistent/path/to/nothing") {
		t.Error("nonexistent path should return false")
	}
}
