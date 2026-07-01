package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initPlainRepo returns a bare-bones git repo (no remote needed) for testing
// moveSidecarIfPresent directly — it only shells `git mv` in repoPath, so it
// needs no clone/push infrastructure.
func initPlainRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"}} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestMoveSidecarIfPresent_MovesTrackedSidecar proves ArchiveSpec/RestoreSpec's
// colocation invariant: when a spec's discussion sidecar exists, it moves
// alongside the spec, never left behind at its old path.
func TestMoveSidecarIfPresent_MovesTrackedSidecar(t *testing.T) {
	ctx := context.Background()
	dir := initPlainRepo(t)

	fromDir := filepath.Join(dir, "specs")
	toDir := filepath.Join(dir, "specs", "archive")
	if err := os.MkdirAll(fromDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(fromDir, "SPEC-001.threads.yaml")
	if err := os.WriteFile(sidecar, []byte("threads:\n    - id: T-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "seed sidecar"); err != nil {
		t.Fatal(err)
	}

	if err := moveSidecarIfPresent(ctx, dir, fromDir, toDir, "SPEC-001"); err != nil {
		t.Fatalf("moveSidecarIfPresent: %v", err)
	}

	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Error("sidecar should no longer exist at the old path")
	}
	if _, err := os.Stat(filepath.Join(toDir, "SPEC-001.threads.yaml")); err != nil {
		t.Errorf("sidecar should exist at the new path: %v", err)
	}
}

// TestMoveSidecarIfPresent_NoSidecarIsNotAnError proves the common case (a
// spec with no discussion yet) is a silent no-op, not a failure.
func TestMoveSidecarIfPresent_NoSidecarIsNotAnError(t *testing.T) {
	ctx := context.Background()
	dir := initPlainRepo(t)
	fromDir := filepath.Join(dir, "specs")
	toDir := filepath.Join(dir, "specs", "archive")
	if err := os.MkdirAll(fromDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := moveSidecarIfPresent(ctx, dir, fromDir, toDir, "SPEC-002"); err != nil {
		t.Fatalf("moveSidecarIfPresent should no-op without a sidecar, got: %v", err)
	}
}
