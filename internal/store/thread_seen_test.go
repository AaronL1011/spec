package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestThreadSeen_RoundTripPreservesMilliseconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	at := time.Date(2026, 7, 9, 12, 0, 0, 567_000_000, time.UTC)
	if err := db.MarkThreadSeen("SPEC-1", "T-1", "@Aaron", at); err != nil {
		t.Fatalf("MarkThreadSeen: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()
	seen, err := db.ThreadSeen("SPEC-1", "aaron")
	if err != nil {
		t.Fatalf("ThreadSeen: %v", err)
	}
	if got := seen["T-1"]; !got.Equal(at) {
		t.Errorf("seen = %v, want %v", got, at)
	}
}

func TestThreadSeen_IsPerViewerAndMonotonic(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()

	later := time.Date(2026, 7, 9, 13, 0, 0, 123_000_000, time.UTC)
	earlier := later.Add(-time.Hour)
	if err := db.MarkThreadSeen("SPEC-1", "T-1", "aaron", later); err != nil {
		t.Fatalf("MarkThreadSeen: %v", err)
	}
	if err := db.MarkThreadSeen("SPEC-1", "T-1", "aaron", earlier); err != nil {
		t.Fatalf("older MarkThreadSeen: %v", err)
	}
	seen, err := db.ThreadSeen("SPEC-1", "aaron")
	if err != nil {
		t.Fatalf("ThreadSeen: %v", err)
	}
	if got := seen["T-1"]; !got.Equal(later) {
		t.Errorf("watermark regressed to %v, want %v", got, later)
	}
	other, err := db.ThreadSeen("SPEC-1", "bob")
	if err != nil {
		t.Fatalf("ThreadSeen(bob): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("read-state leaked to another viewer: %v", other)
	}
}

func TestThreadSeen_MarkUnseenIsViewerScoped(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()

	at := time.Now().UTC()
	for _, viewer := range []string{"aaron", "bob"} {
		if err := db.MarkThreadSeen("SPEC-1", "T-1", viewer, at); err != nil {
			t.Fatalf("MarkThreadSeen(%s): %v", viewer, err)
		}
	}
	if err := db.MarkThreadUnseen("SPEC-1", "T-1", "aaron"); err != nil {
		t.Fatalf("MarkThreadUnseen: %v", err)
	}
	for viewer, want := range map[string]int{"aaron": 0, "bob": 1} {
		seen, err := db.ThreadSeen("SPEC-1", viewer)
		if err != nil {
			t.Fatalf("ThreadSeen(%s): %v", viewer, err)
		}
		if len(seen) != want {
			t.Errorf("%s rows = %d, want %d", viewer, len(seen), want)
		}
	}
}
