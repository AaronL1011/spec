package store

import (
	"testing"
	"time"
)

func TestThreadSeen_RoundTrip(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()

	at := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := db.MarkThreadSeen("SPEC-1", "T-1", at); err != nil {
		t.Fatalf("MarkThreadSeen: %v", err)
	}

	seen, err := db.ThreadSeen("SPEC-1")
	if err != nil {
		t.Fatalf("ThreadSeen: %v", err)
	}
	if got, ok := seen["T-1"]; !ok || !got.Equal(at) {
		t.Errorf("seen[T-1] = %v (ok=%v), want %v", got, ok, at)
	}

	// Upsert advances the watermark.
	later := at.Add(time.Hour)
	if err := db.MarkThreadSeen("SPEC-1", "T-1", later); err != nil {
		t.Fatalf("MarkThreadSeen (upsert): %v", err)
	}
	seen, err = db.ThreadSeen("SPEC-1")
	if err != nil {
		t.Fatalf("ThreadSeen: %v", err)
	}
	if got := seen["T-1"]; !got.Equal(later) {
		t.Errorf("after upsert seen[T-1] = %v, want %v", got, later)
	}

	// Scoped per spec.
	other, err := db.ThreadSeen("SPEC-2")
	if err != nil {
		t.Fatalf("ThreadSeen(SPEC-2): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("SPEC-2 read-state should be empty, got %v", other)
	}
}

func TestThreadSeen_MarkUnseen(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.MarkThreadSeen("SPEC-1", "T-1", time.Now()); err != nil {
		t.Fatalf("MarkThreadSeen: %v", err)
	}
	if err := db.MarkThreadUnseen("SPEC-1", "T-1"); err != nil {
		t.Fatalf("MarkThreadUnseen: %v", err)
	}
	seen, err := db.ThreadSeen("SPEC-1")
	if err != nil {
		t.Fatalf("ThreadSeen: %v", err)
	}
	if _, ok := seen["T-1"]; ok {
		t.Error("T-1 should have no read-state row after MarkThreadUnseen")
	}

	// Unseen on a missing row is a no-op, not an error.
	if err := db.MarkThreadUnseen("SPEC-1", "T-404"); err != nil {
		t.Errorf("MarkThreadUnseen on missing row: %v", err)
	}
}
