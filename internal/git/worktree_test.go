package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initWorktreeRepo creates a temp repo with one commit on a known default
// branch and returns its path.
func initWorktreeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t.com"},
		{"config", "user.name", "T"},
	} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

// branchFrom creates branch at the current HEAD with a file unique to it, so
// merges produce independent changes.
func branchFrom(t *testing.T, repo, branch, file string) {
	t.Helper()
	ctx := context.Background()
	if _, err := Run(ctx, repo, "checkout", "-b", branch, "main"); err != nil {
		t.Fatalf("checkout -b %s: %v", branch, err)
	}
	if err := os.WriteFile(filepath.Join(repo, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, repo, "add "+file); err != nil {
		t.Fatalf("commit %s: %v", file, err)
	}
	if _, err := Run(ctx, repo, "checkout", "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
}

func TestAddAndRemoveWorktree(t *testing.T) {
	ctx := context.Background()
	repo := initWorktreeRepo(t)

	dir, err := AddWorktree(ctx, repo, "spec-1/step-1-feature", "main")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("worktree dir not created: %v", err)
	}
	// The new branch exists and the worktree carries the base file.
	if !BranchExists(ctx, repo, "spec-1/step-1-feature") {
		t.Error("branch should exist after AddWorktree")
	}
	if _, err := os.Stat(filepath.Join(dir, "base.txt")); err != nil {
		t.Errorf("worktree should contain base content: %v", err)
	}

	// Idempotent: a second add returns the same dir.
	dir2, err := AddWorktree(ctx, repo, "spec-1/step-1-feature", "main")
	if err != nil || dir2 != dir {
		t.Fatalf("re-add should be idempotent: dir2=%q err=%v", dir2, err)
	}

	if err := RemoveWorktree(ctx, dir); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be gone, stat err=%v", err)
	}
	// Removing a non-existent worktree is a no-op.
	if err := RemoveWorktree(ctx, dir); err != nil {
		t.Errorf("removing missing worktree should be nil: %v", err)
	}
}

func TestComputeBaseRef(t *testing.T) {
	ctx := context.Background()
	repo := initWorktreeRepo(t)

	t.Run("no parents → default branch", func(t *testing.T) {
		got, err := ComputeBaseRef(ctx, repo, "main", nil, "")
		if err != nil || got != "main" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("one parent → parent branch", func(t *testing.T) {
		got, err := ComputeBaseRef(ctx, repo, "main", []string{"spec-1/step-1"}, "")
		if err != nil || got != "spec-1/step-1" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestComputeBaseRef_DiamondMergesBothParents(t *testing.T) {
	ctx := context.Background()
	repo := initWorktreeRepo(t)

	// Two independent parent branches with distinct files.
	branchFrom(t, repo, "spec-1/step-2-left", "left.txt")
	branchFrom(t, repo, "spec-1/step-3-right", "right.txt")

	base, err := ComputeBaseRef(ctx, repo, "main",
		[]string{"spec-1/step-2-left", "spec-1/step-3-right"}, "spec-1/integrate-n4")
	if err != nil {
		t.Fatalf("ComputeBaseRef (diamond): %v", err)
	}
	if base != "spec-1/integrate-n4" {
		t.Fatalf("base = %q, want integration branch", base)
	}

	// The integration branch must contain BOTH parents' files.
	dir, err := AddWorktree(ctx, repo, "spec-1/step-4-merge", base)
	if err != nil {
		t.Fatalf("AddWorktree on integration base: %v", err)
	}
	defer func() { _ = RemoveWorktree(ctx, dir) }()
	for _, f := range []string{"left.txt", "right.txt", "base.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("integration base missing %s: %v", f, err)
		}
	}
}

func TestAddWorktree_EmptyBranchErrors(t *testing.T) {
	_, err := AddWorktree(context.Background(), initWorktreeRepo(t), "", "main")
	if err == nil {
		t.Fatal("expected error for empty branch")
	}
}
