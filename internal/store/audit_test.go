package store

import "testing"

func TestSyncAuditLogAndRecent(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	for _, op := range []string{OutcomeOK, OutcomeQueued, OutcomeConflict} {
		if err := db.SyncAuditLog(SyncAuditEntry{
			Op: "push", Actor: "alice", Surface: SurfaceTUI, Trigger: "advance",
			SpecID: "SPEC-001", Outcome: op, Detail: "d",
		}); err != nil {
			t.Fatalf("SyncAuditLog: %v", err)
		}
	}

	entries, err := db.SyncAuditRecent(10)
	if err != nil {
		t.Fatalf("SyncAuditRecent: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Newest first.
	if entries[0].Outcome != OutcomeConflict {
		t.Errorf("expected newest-first ordering, got %q", entries[0].Outcome)
	}
	if entries[0].Surface != SurfaceTUI || entries[0].Trigger != "advance" {
		t.Errorf("surface/trigger not preserved: %+v", entries[0])
	}
}

func TestSyncQueueLifecycle(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	key := "owner/repo"
	id1, err := db.QueuePushEnqueue(QueuedPush{
		RepoKey: key, Branch: "main", CommitSHA: "abc", Surface: SurfaceCLI,
		Trigger: "advance", SpecID: "SPEC-001",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id2, err := db.QueuePushEnqueue(QueuedPush{
		RepoKey: key, Branch: "main", CommitSHA: "def", Surface: SurfaceCLI,
		Trigger: "decide", SpecID: "SPEC-002",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	n, _ := db.QueuePushCount(key)
	if n != 2 {
		t.Fatalf("expected 2 queued, got %d", n)
	}

	// Resolve one, mark the other needs-resolution.
	if err := db.QueuePushResolve(id1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueuePushMark(id2, QueueStatusNeedsResolution, "SPEC-002 §problem_statement"); err != nil {
		t.Fatal(err)
	}

	// Pending excludes needs-resolution and resolved entries.
	pending, _ := db.QueuePushPending(key)
	if len(pending) != 0 {
		t.Errorf("expected no flushable pending, got %d", len(pending))
	}
}
