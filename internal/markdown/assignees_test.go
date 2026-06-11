package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssigneesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-001.md")
	content := ScaffoldSpec("SPEC-001", "Test", "Ana", "C1", "direct")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.Assignees = []string{"@ana", "@ben"}
	meta.BlockedFrom = "build"
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assignees) != 2 || got.Assignees[0] != "@ana" || got.Assignees[1] != "@ben" {
		t.Errorf("assignees = %v, want [@ana @ben]", got.Assignees)
	}
	if got.BlockedFrom != "build" {
		t.Errorf("blocked_from = %q, want build", got.BlockedFrom)
	}
}

func TestAssigneesOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-001.md")
	content := ScaffoldSpec("SPEC-001", "Test", "Ana", "C1", "direct")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _ := ReadMeta(path)
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "assignees") {
		t.Errorf("expected no assignees key when empty, got:\n%s", data)
	}
	if strings.Contains(string(data), "blocked_from") {
		t.Errorf("expected no blocked_from key when empty, got:\n%s", data)
	}
}

func TestHasAssignee(t *testing.T) {
	meta := &SpecMeta{Assignees: []string{"@Ana", "Ben"}}
	tests := []struct {
		id   string
		want bool
	}{
		{"@ana", true},
		{"ana", true},
		{"ANA", true},
		{"ben", true},
		{"@ben", true},
		{"cleo", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := meta.HasAssignee(tt.id); got != tt.want {
			t.Errorf("HasAssignee(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
