package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression (SPEC-012 × SPEC-013): two users reply to threads on the same
// spec concurrently. B lands first; A's push must reconcile associatively
// (union of both reply sets), not abort — the loser's rebase resolves the
// sidecar collision via thread.Merge in rebaseWithSidecarUnion. Before that
// fix, the pre-rebase working-tree write made `git rebase` refuse to start
// ("You have unstaged changes") and the losing reply never published.
func TestPushWithRecovery_ConcurrentSidecarReplies_MergesUnion(t *testing.T) {
	ctx := context.Background()
	cloneA, cloneB, branch := setupSharedRemote(t)
	cfg := cfgForBranch{branch: branch}.cfg()

	sidecar := filepath.Join("specs", "SPEC-001.threads.yaml")

	// Seed a common sidecar with one open thread, push from A.
	seed := "threads:\n    - id: T-1\n      spec: SPEC-001\n      section: problem-statement\n      author: \"@pm\"\n      question: ok?\n      status: open\n      created: 2026-06-01T10:00:00Z\n      replies: []\n"
	if err := os.WriteFile(filepath.Join(cloneA, sidecar), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, cloneA, "seed sidecar"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneA, branch); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, cloneB, "pull", "origin", branch); err != nil {
		t.Fatal(err)
	}

	// B replies and pushes first (wins the race).
	bReply := strings.Replace(seed, "replies: []",
		"replies:\n        - author: \"@bob\"\n          created: 2026-06-01T10:01:00Z\n          body: from bob", 1)
	if err := os.WriteFile(filepath.Join(cloneB, sidecar), []byte(bReply), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, cloneB, "feat: update SPEC-001 (bob reply)"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, cloneB, branch); err != nil {
		t.Fatal(err)
	}

	// A replies concurrently (loses the race) — commit locally, then push via
	// the real recovery path, exactly as PushLocalEditsOpts does.
	aReply := strings.Replace(seed, "replies: []",
		"replies:\n        - author: \"@alice\"\n          created: 2026-06-01T10:01:30Z\n          body: from alice", 1)
	if err := os.WriteFile(filepath.Join(cloneA, sidecar), []byte(aReply), 0o644); err != nil {
		t.Fatal(err)
	}
	base, _ := RevParse(ctx, cloneA, "HEAD")
	if err := Commit(ctx, cloneA, "feat: update SPEC-001 (alice reply)"); err != nil {
		t.Fatal(err)
	}
	files, _ := CommittedFiles(ctx, cloneA, "HEAD")

	err := pushWithRecovery(ctx, cfg, cloneA, SyncOptions{}, base, files, false)
	t.Logf("pushWithRecovery err: %v", err)

	// What does the remote hold now? Both replies should survive.
	if _, err := Run(ctx, cloneB, "pull", "origin", branch); err != nil {
		t.Logf("pull err: %v", err)
	}
	final, _ := os.ReadFile(filepath.Join(cloneB, sidecar))
	t.Logf("remote sidecar after reconcile:\n%s", final)

	hasAlice := strings.Contains(string(final), "from alice")
	hasBob := strings.Contains(string(final), "from bob")
	t.Logf("RESULT: alice=%v bob=%v err=%v", hasAlice, hasBob, err)
	if err != nil || !hasAlice || !hasBob {
		t.Errorf("associative sidecar merge failed: err=%v alice=%v bob=%v", err, hasAlice, hasBob)
	}
}
