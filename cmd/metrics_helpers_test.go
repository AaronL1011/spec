package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestParseSinceFlag(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantAgo time.Duration // approximate expected duration ago from now
		wantErr bool
	}{
		{"30 days", "30d", 30 * 24 * time.Hour, false},
		{"7 days", "7d", 7 * 24 * time.Hour, false},
		{"24 hours", "24h", 24 * time.Hour, false},
		{"1 hour", "1h", time.Hour, false},
		{"empty defaults to 90d", "", 90 * 24 * time.Hour, false},
		{"invalid", "garbage", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("since", tt.value, "")

			got, err := parseSinceFlag(cmd)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			elapsed := time.Since(got)
			tolerance := 5 * time.Second
			diff := elapsed - tt.wantAgo
			if diff < -tolerance || diff > tolerance {
				t.Errorf("expected ~%v ago, got %v ago (diff %v)", tt.wantAgo, elapsed, diff)
			}
		})
	}
}

func TestScanSpecsByStage(t *testing.T) {
	dir := t.TempDir()

	// Write some spec files with frontmatter
	writeSpecFile(t, dir, "SPEC-001.md", "draft")
	writeSpecFile(t, dir, "SPEC-002.md", "draft")
	writeSpecFile(t, dir, "SPEC-003.md", "review")
	writeSpecFile(t, dir, "SPEC-004.md", "done")

	// Non-spec files should be ignored
	writeSpecFile(t, dir, "README.md", "draft") // no SPEC- prefix in content
	notesPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notesPath, []byte("not markdown"), 0o644); err != nil {
		t.Fatalf("write %s: %v", notesPath, err)
	}

	counts := scanSpecsByStage(dir)

	if counts["draft"] != 2 {
		t.Errorf("expected 2 in draft, got %d", counts["draft"])
	}
	if counts["review"] != 1 {
		t.Errorf("expected 1 in review, got %d", counts["review"])
	}
	if counts["done"] != 1 {
		t.Errorf("expected 1 in done, got %d", counts["done"])
	}
}

func TestScanSpecsByStage_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	counts := scanSpecsByStage(dir)
	if len(counts) != 0 {
		t.Errorf("expected empty map, got %v", counts)
	}
}

func TestScanSpecsByStage_NonexistentDir(t *testing.T) {
	counts := scanSpecsByStage("/nonexistent/path")
	if len(counts) != 0 {
		t.Errorf("expected empty map, got %v", counts)
	}
}

func writeSpecFile(t *testing.T, dir, name, status string) {
	t.Helper()
	// Extract ID from filename
	id := name[:len(name)-3] // strip .md
	content := "---\nid: " + id + "\ntitle: Test Spec\nstatus: " + status + "\nversion: 0.1.0\nauthor: test\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\n\n# " + id + " - Test\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
