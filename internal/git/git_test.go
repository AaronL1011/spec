package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpecBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   *BranchInfo
	}{
		{"spec-042/step-1-token-bucket", &BranchInfo{"042", "1", "token-bucket"}},
		{"spec-001/step-3-add-auth", &BranchInfo{"001", "3", "add-auth"}},
		{"main", nil},
		{"feature/something", nil},
	}
	for _, tt := range tests {
		got := ParseSpecBranch(tt.branch)
		if tt.want == nil {
			if got != nil {
				t.Errorf("ParseSpecBranch(%q) = %+v, want nil", tt.branch, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("ParseSpecBranch(%q) = nil, want %+v", tt.branch, tt.want)
			continue
		}
		if got.SpecNumber != tt.want.SpecNumber || got.StepNumber != tt.want.StepNumber || got.Slug != tt.want.Slug {
			t.Errorf("ParseSpecBranch(%q) = %+v, want %+v", tt.branch, got, tt.want)
		}
	}
}

func TestSpecBranchName(t *testing.T) {
	tests := []struct {
		specID string
		step   int
		slug   string
		want   string
	}{
		{"SPEC-042", 1, "token bucket", "spec-042/step-1-token-bucket"},
		{"SPEC-001", 3, "Add Auth Service", "spec-001/step-3-add-auth-service"},
	}
	for _, tt := range tests {
		got := SpecBranchName(tt.specID, tt.step, tt.slug)
		if got != tt.want {
			t.Errorf("SpecBranchName(%q, %d, %q) = %q, want %q", tt.specID, tt.step, tt.slug, got, tt.want)
		}
	}
}

func TestGitOperations(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Init a repo
	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create a file and commit
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	hasChanges, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !hasChanges {
		t.Error("expected changes")
	}

	if err := Commit(ctx, dir, "initial commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	hasChanges, err = HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if hasChanges {
		t.Error("expected no changes after commit")
	}

	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "master" && branch != "main" {
		t.Errorf("branch = %q, want master or main", branch)
	}
}

// newTestClone sets up a bare remote + working clone with identity configured,
// an initial committed+pushed file, and returns (local dir, branch, cfg).
func newTestClone(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
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
	if err := os.MkdirAll(filepath.Join(local, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "specs", "SPEC-001.md"), []byte("# SPEC-001\n\n## 1. Problem Statement\n\norig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ := CurrentBranch(ctx, local)
	if err := Push(ctx, local, branch); err != nil {
		t.Fatal(err)
	}
	return local, branch
}

func TestAutoRecover_Clean(t *testing.T) {
	ctx := context.Background()
	local, branch := newTestClone(t)
	cfg := cfgForBranch{branch}.cfg()

	if err := autoRecover(ctx, cfg, local, SyncOptions{}.normalized(ctx)); err != nil {
		t.Errorf("clean repo should not error: %v", err)
	}
}

func TestAutoRecover_UncommittedAutoCommitsAndPushes(t *testing.T) {
	ctx := context.Background()
	local, branch := newTestClone(t)
	cfg := cfgForBranch{branch}.cfg()

	// Leave stranded uncommitted edits in the specs sub-tree.
	if err := os.WriteFile(filepath.Join(local, "specs", "SPEC-001.md"), []byte("# SPEC-001\n\n## 1. Problem Statement\n\nstranded\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := autoRecover(ctx, cfg, local, SyncOptions{}.normalized(ctx)); err != nil {
		t.Fatalf("autoRecover should commit+push stranded edits, got: %v", err)
	}
	// Working tree clean, nothing unpushed.
	if dirty, _ := HasChanges(ctx, local); dirty {
		t.Error("autoRecover should have committed stranded edits")
	}
	if has, _ := HasUnpushedCommits(ctx, local, "origin/"+branch); has {
		t.Error("autoRecover should have pushed stranded commits")
	}
}

func TestAutoRecover_OfflineQueuesNoError(t *testing.T) {
	ctx := context.Background()
	local, branch := newTestClone(t)
	cfg := cfgForBranch{branch}.cfg()

	// Make a stranded commit, then break the remote so the push can't land.
	if err := os.WriteFile(filepath.Join(local, "specs", "SPEC-001.md"), []byte("# SPEC-001\n\n## 1. Problem Statement\n\nstranded\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "stranded"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "remote", "set-url", "origin", "/nonexistent/repo"); err != nil {
		t.Fatal(err)
	}

	rec := &fakeRecorder{}
	opts := SyncOptions{Recorder: rec}.normalized(ctx)
	// Offline push failure must NOT error — it queues (SPEC-013 §Decision 009).
	if err := autoRecover(ctx, cfg, local, opts); err != nil {
		t.Fatalf("offline recovery should queue, not error, got: %v", err)
	}
	if rec.enqueued == 0 {
		t.Error("expected the stranded work to be queued on offline failure")
	}
}

func TestAutoRecover_ForceDiscards(t *testing.T) {
	ctx := context.Background()
	local, branch := newTestClone(t)
	cfg := cfgForBranch{branch}.cfg()

	if err := os.WriteFile(filepath.Join(local, "specs", "SPEC-001.md"), []byte("discard me"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPEC_FORCE", "1")
	if err := autoRecover(ctx, cfg, local, SyncOptions{}.normalized(ctx)); err != nil {
		t.Errorf("SPEC_FORCE should discard cleanly: %v", err)
	}
	if dirty, _ := HasChanges(ctx, local); dirty {
		t.Error("SPEC_FORCE=1 should have discarded stranded edits")
	}
}

func TestCreateAndCheckoutBranch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

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

	if err := CreateBranch(ctx, dir, "spec-042/step-1-test"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	branch, _ := CurrentBranch(ctx, dir)
	if branch != "spec-042/step-1-test" {
		t.Errorf("branch = %q, want spec-042/step-1-test", branch)
	}

	if !BranchExists(ctx, dir, "spec-042/step-1-test") {
		t.Error("branch should exist")
	}
}
