package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

	if !IsGitRepo(dir) {
		t.Error("expected IsGitRepo to be true")
	}
	if IsGitRepo(t.TempDir()) {
		t.Error("expected IsGitRepo to be false for non-repo")
	}
}

func TestGuardUnpushedChanges_Clean(t *testing.T) {
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

	if err := guardUnpushedChanges(ctx, dir); err != nil {
		t.Errorf("clean repo should not error: %v", err)
	}
}

func TestGuardUnpushedChanges_Dirty(t *testing.T) {
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

	// Dirty the repo
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := guardUnpushedChanges(ctx, dir)
	if err == nil {
		t.Fatal("dirty repo should return error")
	}
	if !strings.Contains(err.Error(), "unpushed local changes") {
		t.Errorf("error should mention unpushed changes, got: %v", err)
	}
	if !strings.Contains(err.Error(), "spec push") {
		t.Errorf("error should suggest 'spec push', got: %v", err)
	}
}

func TestGuardUnpushedChanges_ForceBypass(t *testing.T) {
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

	// Dirty the repo
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPEC_FORCE", "1")
	if err := guardUnpushedChanges(ctx, dir); err != nil {
		t.Errorf("SPEC_FORCE=1 should bypass guard: %v", err)
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
