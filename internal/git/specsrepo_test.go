package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindOverlap_NoConflict(t *testing.T) {
	ours := []string{"specs/SPEC-042.md", "specs/triage/TRIAGE-001.md"}
	theirs := []string{"specs/SPEC-077.md", "spec.config.yaml"}

	if got := findOverlap(ours, theirs); got != "" {
		t.Errorf("findOverlap() = %q, want empty", got)
	}
}

func TestFindOverlap_Conflict(t *testing.T) {
	ours := []string{"specs/SPEC-042.md"}
	theirs := []string{"specs/SPEC-077.md", "specs/SPEC-042.md"}

	got := findOverlap(ours, theirs)
	if got != "specs/SPEC-042.md" {
		t.Errorf("findOverlap() = %q, want specs/SPEC-042.md", got)
	}
}

func TestFindOverlap_EmptyInputs(t *testing.T) {
	if got := findOverlap(nil, nil); got != "" {
		t.Errorf("findOverlap(nil, nil) = %q, want empty", got)
	}
	if got := findOverlap([]string{"a.md"}, nil); got != "" {
		t.Errorf("findOverlap(a, nil) = %q, want empty", got)
	}
	if got := findOverlap(nil, []string{"b.md"}); got != "" {
		t.Errorf("findOverlap(nil, b) = %q, want empty", got)
	}
}

func TestGuardUnpushedChanges_UnpushedCommits(t *testing.T) {
	ctx := context.Background()

	// Set up a "remote" repo
	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}

	// Clone it
	local := filepath.Join(t.TempDir(), "local")
	if err := Clone(ctx, remote, local); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit and push
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ := CurrentBranch(ctx, local)
	if err := Push(ctx, local, branch); err != nil {
		t.Fatal(err)
	}

	// Clean state should pass
	if err := guardUnpushedChanges(ctx, local, branch); err != nil {
		t.Fatalf("clean repo should pass: %v", err)
	}

	// Make a local commit without pushing
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "local only"); err != nil {
		t.Fatal(err)
	}

	// Guard should auto-push the stranded commit and succeed.
	if err := guardUnpushedChanges(ctx, local, branch); err != nil {
		t.Fatalf("guard should auto-push stranded commits, got: %v", err)
	}

	// Verify the commit was actually pushed.
	has, err := HasUnpushedCommits(ctx, local, "origin/"+branch)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("commit should have been pushed by guard")
	}
}

func TestGuardUnpushedChanges_AutoPushFails(t *testing.T) {
	ctx := context.Background()

	// Set up a "remote" bare repo, then break it.
	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}

	local := filepath.Join(t.TempDir(), "local")
	if err := Clone(ctx, remote, local); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ := CurrentBranch(ctx, local)
	if err := Push(ctx, local, branch); err != nil {
		t.Fatal(err)
	}

	// Make a local commit, then break the remote so push fails.
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "stranded"); err != nil {
		t.Fatal(err)
	}

	// Point remote to a nonexistent path so push fails.
	if _, err := Run(ctx, local, "remote", "set-url", "origin", "/nonexistent/repo"); err != nil {
		t.Fatal(err)
	}

	err := guardUnpushedChanges(ctx, local, branch)
	if err == nil {
		t.Fatal("should error when auto-push fails")
	}
	if !containsSubstring(err.Error(), "could not be pushed") {
		t.Errorf("error should mention push failure, got: %v", err)
	}
	if !containsSubstring(err.Error(), "spec push") {
		t.Errorf("error should suggest 'spec push', got: %v", err)
	}
}

// containsSubstring is a test helper (strings.Contains without importing strings).
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRebaseAbort_NoRebaseInProgress(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatal(err)
	}

	// Should not panic or leave repo in bad state
	RebaseAbort(ctx, dir)

	// Verify repo is still usable
	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges after RebaseAbort: %v", err)
	}
	if has {
		t.Error("unexpected changes after RebaseAbort")
	}
}

func TestCommittedFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatal(err)
	}

	// Create a second commit touching two files
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "specs", "SPEC-042.md"), []byte("# spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "add spec and b"); err != nil {
		t.Fatal(err)
	}

	files, err := CommittedFiles(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("CommittedFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Check both files are present (order may vary)
	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found["b.txt"] {
		t.Errorf("expected b.txt in committed files: %v", files)
	}
	if !found["specs/SPEC-042.md"] {
		t.Errorf("expected specs/SPEC-042.md in committed files: %v", files)
	}
}

func TestDiffNameOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Commit 1
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "first"); err != nil {
		t.Fatal(err)
	}
	ref1, _ := RevParse(ctx, dir, "HEAD")

	// Commit 2
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "second"); err != nil {
		t.Fatal(err)
	}
	ref2, _ := RevParse(ctx, dir, "HEAD")

	files, err := DiffNameOnly(ctx, dir, ref1, ref2)
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}
	if len(files) != 1 || files[0] != "b.txt" {
		t.Errorf("expected [b.txt], got %v", files)
	}

	// Same ref should return nil
	files, err = DiffNameOnly(ctx, dir, ref2, ref2)
	if err != nil {
		t.Fatalf("DiffNameOnly same ref: %v", err)
	}
	if files != nil {
		t.Errorf("same ref should return nil, got %v", files)
	}
}

func TestHasUnpushedCommits(t *testing.T) {
	ctx := context.Background()

	// Set up a "remote" repo
	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}

	// Clone it
	local := filepath.Join(t.TempDir(), "local")
	if err := Clone(ctx, remote, local); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit and push
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ := CurrentBranch(ctx, local)
	if err := Push(ctx, local, branch); err != nil {
		t.Fatal(err)
	}

	// Should have no unpushed commits
	has, err := HasUnpushedCommits(ctx, local, "origin/"+branch)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if has {
		t.Error("expected no unpushed commits")
	}

	// Make a local commit
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "unpushed"); err != nil {
		t.Fatal(err)
	}

	has, err = HasUnpushedCommits(ctx, local, "origin/"+branch)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !has {
		t.Error("expected unpushed commits")
	}
}
