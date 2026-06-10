package store

import "testing"

func TestPMQueue_EnqueuePendingResolve(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	id, err := db.PMQueueEnqueue(PMQueueItem{
		SpecID: "SPEC-1", EpicKey: "PLAT-1", Op: PMOpStatus, Payload: "build", Detail: "boom",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	pending, err := db.PMQueuePending("")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Op != PMOpStatus || pending[0].Payload != "build" {
		t.Fatalf("unexpected pending: %+v", pending)
	}

	if err := db.PMQueueResolve(id); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	n, err := db.PMQueueCount()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 after resolve, got %d", n)
	}
}

func TestPMQueue_DedupesSameOp(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	item := PMQueueItem{SpecID: "SPEC-1", EpicKey: "PLAT-1", Op: PMOpStatus, Payload: "build"}
	id1, _ := db.PMQueueEnqueue(item)
	id2, _ := db.PMQueueEnqueue(item)
	if id1 != id2 {
		t.Errorf("expected dedupe to same row, got %d and %d", id1, id2)
	}
	n, _ := db.PMQueueCount()
	if n != 1 {
		t.Errorf("expected 1 queued row, got %d", n)
	}

	pending, _ := db.PMQueuePending("SPEC-1")
	if len(pending) != 1 || pending[0].Attempts != 1 {
		t.Errorf("expected attempts incremented on dedupe, got %+v", pending)
	}
}

func TestPMQueue_PendingScopedBySpec(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, _ = db.PMQueueEnqueue(PMQueueItem{SpecID: "SPEC-1", Op: PMOpStatus, Payload: "build"})
	_, _ = db.PMQueueEnqueue(PMQueueItem{SpecID: "SPEC-2", Op: PMOpStatus, Payload: "done"})

	scoped, _ := db.PMQueuePending("SPEC-2")
	if len(scoped) != 1 || scoped[0].SpecID != "SPEC-2" {
		t.Errorf("expected only SPEC-2, got %+v", scoped)
	}
}
