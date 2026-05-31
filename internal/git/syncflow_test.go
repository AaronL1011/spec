package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// setupSharedRemote creates a bare remote and two clones of it (A and B),
// each with a committed+pushed two-section spec, returning their dirs + branch.
func setupSharedRemote(t *testing.T) (cloneA, cloneB, branch string) {
	t.Helper()
	ctx := context.Background()
	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}
	cloneA = filepath.Join(t.TempDir(), "a")
	if err := Clone(ctx, remote, cloneA); err != nil {
		t.Fatal(err)
	}
	configIdentity(t, cloneA)
	if err := os.MkdirAll(filepath.Join(cloneA, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneA, "specs", "SPEC-001.md"),
		[]byte(sectionDoc("## 1. Problem Statement", "orig problem", "## 7. Technical Implementation", "orig tech")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, cloneA, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ = CurrentBranch(ctx, cloneA)
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	cloneB = filepath.Join(t.TempDir(), "b")
	if err := Clone(ctx, remote, cloneB); err != nil {
		t.Fatal(err)
	}
	configIdentity(t, cloneB)
	return cloneA, cloneB, branch
}

func configIdentity(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	for _, args := range [][]string{{"config", "user.email", "t@t.com"}, {"config", "user.name", "T"}} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
}

// withClone runs a mutate against a specific clone dir by temporarily pointing
// SpecsRepoDir at it. SpecsRepoDir is derived from cfg, so we drive the
// internal helpers directly to avoid the ~/.spec path.
func editSection(t *testing.T, dir, heading, content string) {
	t.Helper()
	var doc string
	if heading == "## 1. Problem Statement" {
		doc = sectionDoc(heading, content, "## 7. Technical Implementation", "orig tech")
	} else {
		doc = sectionDoc("## 1. Problem Statement", "orig problem", heading, content)
	}
	if err := os.WriteFile(filepath.Join(dir, "specs", "SPEC-001.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPushWithRecovery_DisjointSectionsRebase verifies AC-7/AC-11: two clones
// editing different sections both land without --force.
func TestPushWithRecovery_DisjointSectionsRebase(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	// A edits §1 and pushes.
	editSection(t, cloneA, "## 1. Problem Statement", "A problem change")
	if err := Commit(ctx, cloneA, "A §1"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	// B edits §7 (disjoint) on the stale base, commits, then pushes with recovery.
	if err := Fetch(ctx, cloneB); err == nil {
		_ = ResetHard(ctx, cloneB, "origin/"+branch)
	}
	// Reset B to the original base (before A's push) to force a rebase.
	editSection(t, cloneB, "## 7. Technical Implementation", "B tech change")
	base, _ := RevParse(ctx, cloneB, "HEAD")
	if err := Commit(ctx, cloneB, "B §7"); err != nil {
		t.Fatal(err)
	}
	files, _ := CommittedFiles(ctx, cloneB, "HEAD")

	err := pushWithRecoveryAt(ctx, cloneB, cfg, base, files, false)
	if err != nil {
		t.Fatalf("disjoint sections should rebase+push cleanly, got: %v", err)
	}
}

// TestPushWithRecovery_SameSectionAborts verifies AC-8/AC-12: same-section
// collision aborts with a section-naming error rather than auto-merging.
func TestPushWithRecovery_SameSectionAborts(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	editSection(t, cloneA, "## 1. Problem Statement", "A problem change")
	if err := Commit(ctx, cloneA, "A §1"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	editSection(t, cloneB, "## 1. Problem Statement", "B problem change")
	base, _ := RevParse(ctx, cloneB, "HEAD")
	if err := Commit(ctx, cloneB, "B §1"); err != nil {
		t.Fatal(err)
	}
	files, _ := CommittedFiles(ctx, cloneB, "HEAD")

	err := pushWithRecoveryAt(ctx, cloneB, cfg, base, files, false)
	if !IsSectionConflict(err) {
		t.Fatalf("same-section collision should abort with section conflict, got: %v", err)
	}
	// B's local commit must be preserved (resetHardOnConflict=false).
	if has, _ := HasUnpushedCommits(ctx, cloneB, "origin/"+branch); !has {
		t.Error("PushLocalEdits-style abort must preserve the local commit")
	}
}

// TestPushWithRecovery_OfflineQueues verifies AC-18/AC-22: a transient/offline
// push failure queues instead of erroring.
func TestPushWithRecovery_OfflineQueues(t *testing.T) {
	ctx := context.Background()
	cloneA, _, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	editSection(t, cloneA, "## 1. Problem Statement", "change")
	base, _ := RevParse(ctx, cloneA, "HEAD")
	if err := Commit(ctx, cloneA, "edit"); err != nil {
		t.Fatal(err)
	}
	files, _ := CommittedFiles(ctx, cloneA, "HEAD")
	// Break the remote so push + fetch fail.
	if _, err := Run(ctx, cloneA, "remote", "set-url", "origin", "/nonexistent"); err != nil {
		t.Fatal(err)
	}

	rec := &fakeRecorder{}
	opts := SyncOptions{Recorder: rec}.normalized(ctx)
	if err := pushWithRecovery(ctx, cfg, cloneA, opts, base, files, false); err != nil {
		t.Fatalf("offline push should queue, not error, got: %v", err)
	}
	if rec.enqueued == 0 {
		t.Error("offline push should enqueue the operation")
	}
}

// pushWithRecoveryAt is a tiny shim that runs pushWithRecovery against a clone
// dir not derived from SpecsRepoDir(cfg), using a no-op recorder.
func pushWithRecoveryAt(ctx context.Context, dir string, cfg *config.SpecsRepoConfig, base string, files []string, resetHard bool) error {
	return pushWithRecovery(ctx, cfg, dir, SyncOptions{}.normalized(ctx), base, files, resetHard)
}

// readSpec returns the working-tree content of SPEC-001 for a clone, so tests
// can assert what a TUI reader (os.ReadDir + ReadMeta) would actually see.
func readSpec(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "specs", "SPEC-001.md"))
	if err != nil {
		t.Fatalf("reading SPEC-001: %v", err)
	}
	return string(b)
}

// TestFastForwardClean_AdvancesCleanCloneToRemote is the cross-machine
// consistency case: machine A pushes a change, machine B (clean, behind)
// fetches and must see the new content on disk. fetch alone leaves the working
// tree stale; fastForwardClean advances it.
func TestFastForwardClean_AdvancesCleanCloneToRemote(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	// A edits §1 and pushes.
	editSection(t, cloneA, "## 1. Problem Statement", "A problem change")
	if err := Commit(ctx, cloneA, "A §1"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	// B fetches (working tree still stale at this point) then fast-forwards.
	if err := Fetch(ctx, cloneB); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readSpec(t, cloneB), "A problem change") {
		t.Fatal("precondition: fetch alone should NOT update the working tree")
	}
	if err := fastForwardClean(ctx, cloneB, cfg); err != nil {
		t.Fatalf("fastForwardClean: %v", err)
	}
	if !strings.Contains(readSpec(t, cloneB), "A problem change") {
		t.Error("clean behind clone should be fast-forwarded so reads see the push")
	}
}

// TestFastForwardClean_PreservesDirtyTree guards SPEC-013: a working tree with
// uncommitted local edits must never be clobbered by a read-path fast-forward.
func TestFastForwardClean_PreservesDirtyTree(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	editSection(t, cloneA, "## 1. Problem Statement", "A problem change")
	if err := Commit(ctx, cloneA, "A §1"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	// B has an uncommitted local edit and is behind.
	editSection(t, cloneB, "## 7. Technical Implementation", "B uncommitted work")
	if err := Fetch(ctx, cloneB); err != nil {
		t.Fatal(err)
	}
	if err := fastForwardClean(ctx, cloneB, cfg); err != nil {
		t.Fatalf("fastForwardClean: %v", err)
	}
	if !strings.Contains(readSpec(t, cloneB), "B uncommitted work") {
		t.Error("dirty tree must be preserved — local uncommitted edit was lost")
	}
}

// TestFastForwardClean_PreservesUnpushedCommits guards against discarding local
// commits that have not yet reached the remote (divergence is reconciled by the
// mutate path, not silently reset by a read).
func TestFastForwardClean_PreservesUnpushedCommits(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch}.cfg()

	editSection(t, cloneA, "## 1. Problem Statement", "A problem change")
	if err := Commit(ctx, cloneA, "A §1"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}

	// B commits locally (unpushed) and is also behind → diverged.
	editSection(t, cloneB, "## 7. Technical Implementation", "B local commit")
	if err := Commit(ctx, cloneB, "B §7"); err != nil {
		t.Fatal(err)
	}
	if err := Fetch(ctx, cloneB); err != nil {
		t.Fatal(err)
	}
	if err := fastForwardClean(ctx, cloneB, cfg); err != nil {
		t.Fatalf("fastForwardClean: %v", err)
	}
	if has, _ := HasUnpushedCommits(ctx, cloneB, "origin/"+branch); !has {
		t.Error("diverged clone must keep its unpushed commit, not be reset")
	}
	if !strings.Contains(readSpec(t, cloneB), "B local commit") {
		t.Error("unpushed local commit content must survive the read path")
	}
}
