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

func TestPushSpec_ReturnsCommand(t *testing.T) {
	rc := testResolvedConfig()
	cmd := pushSpec(rc, "SPEC-001")
	if cmd == nil {
		t.Error("pushSpec should return a non-nil command")
	}
}

func TestSyncSpec_NoDocsIntegration(t *testing.T) {
	rc := testResolvedConfig()
	reg := testRegistry()
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cmd := syncSpec(rc, reg, db, "SPEC-001", "engineer")
	if cmd == nil {
		t.Fatal("syncSpec should return a non-nil command")
	}

	// Execute — should fail because docs integration is not configured.
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.Err == nil {
		t.Error("syncSpec without docs integration should return an error")
	}
}

func TestFormatSyncDetail_NilPrepared(t *testing.T) {
	got := formatSyncDetail(nil)
	if got != "no changes" {
		t.Errorf("formatSyncDetail(nil) = %q, want %q", got, "no changes")
	}
}

func TestParseAssignInput(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"@ana", []string{"@ana"}},
		{"@ana @ben", []string{"@ana", "@ben"}},
		{"@ana, @ben", []string{"@ana", "@ben"}},
		{"@ana @Ana @ANA", []string{"@ana"}}, // dedupe, case-insensitive
		{"-", []string{}},                    // clear
		{"clear", []string{}},
		{"none", []string{}},
		{"   ", nil}, // whitespace → no tokens
	}
	for _, tt := range tests {
		got := parseAssignInput(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("parseAssignInput(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseAssignInput(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}
