package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// waitForEvent blocks until a ChangeEvent arrives or the timeout elapses.
func waitForEvent(t *testing.T, c <-chan ChangeEvent, timeout time.Duration) (ChangeEvent, bool) {
	t.Helper()
	select {
	case ev, ok := <-c:
		return ev, ok
	case <-time.After(timeout):
		return ChangeEvent{}, false
	}
}

func TestWatcher_DetectsWrite(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	writeFile(t, spec, "initial")

	w, err := New(context.Background(), []string{spec}, 50*time.Millisecond)
	if err != nil {
		t.Logf("fsnotify unavailable, using polling fallback: %v", err)
	}
	defer func() { _ = w.Close() }()

	time.Sleep(20 * time.Millisecond)
	writeFile(t, spec, "changed")

	ev, ok := waitForEvent(t, w.C, 3*time.Second)
	if !ok {
		t.Fatal("expected a change event for the spec write, got none")
	}
	if len(ev.Paths) == 0 {
		t.Fatal("change event carried no paths")
	}
}

func TestWatcher_DetectsSidecarWrite(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	sidecar := filepath.Join(dir, "SPEC-007.thread.yml")
	writeFile(t, spec, "spec")
	writeFile(t, sidecar, "threads: []")

	w, _ := New(context.Background(), []string{spec, sidecar}, 50*time.Millisecond)
	defer func() { _ = w.Close() }()

	time.Sleep(20 * time.Millisecond)
	writeFile(t, sidecar, "threads:\n  - id: T-1")

	ev, ok := waitForEvent(t, w.C, 3*time.Second)
	if !ok {
		t.Fatal("expected a change event for the sidecar write, got none")
	}
	found := false
	for _, p := range ev.Paths {
		if filepath.Base(p) == filepath.Base(sidecar) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sidecar in changed paths, got %v", ev.Paths)
	}
}

func TestWatcher_IgnoresUnwatchedFiles(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	other := filepath.Join(dir, "SPEC-999.md")
	writeFile(t, spec, "spec")

	w, _ := New(context.Background(), []string{spec}, 50*time.Millisecond)
	defer func() { _ = w.Close() }()

	time.Sleep(20 * time.Millisecond)
	writeFile(t, other, "unrelated")

	if _, ok := waitForEvent(t, w.C, 500*time.Millisecond); ok {
		t.Fatal("expected no event for an unwatched file in the same dir")
	}
}

func TestWatcher_CoalescesBurst(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	writeFile(t, spec, "v0")

	w, _ := New(context.Background(), []string{spec}, 150*time.Millisecond)
	defer func() { _ = w.Close() }()

	time.Sleep(20 * time.Millisecond)
	// A tight burst of writes within the debounce window should coalesce.
	for i := 0; i < 5; i++ {
		writeFile(t, spec, "v"+string(rune('1'+i)))
		time.Sleep(10 * time.Millisecond)
	}

	if _, ok := waitForEvent(t, w.C, 2*time.Second); !ok {
		t.Fatal("expected one coalesced event for the burst")
	}
	// No second event should follow for the same burst.
	if _, ok := waitForEvent(t, w.C, 400*time.Millisecond); ok {
		t.Fatal("burst produced more than one event")
	}
}

func TestWatcher_Retarget(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	specA := filepath.Join(dirA, "SPEC-001.md")
	specB := filepath.Join(dirB, "SPEC-002.md")
	writeFile(t, specA, "a")
	writeFile(t, specB, "b")

	w, _ := New(context.Background(), []string{specA}, 50*time.Millisecond)
	defer func() { _ = w.Close() }()

	if err := w.Retarget([]string{specB}); err != nil {
		t.Fatalf("retarget: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Changing the old target must no longer fire.
	writeFile(t, specA, "a2")
	if _, ok := waitForEvent(t, w.C, 500*time.Millisecond); ok {
		t.Fatal("old target should no longer produce events after retarget")
	}

	// Changing the new target fires.
	writeFile(t, specB, "b2")
	if _, ok := waitForEvent(t, w.C, 3*time.Second); !ok {
		t.Fatal("new target should produce events after retarget")
	}
}

func TestWatcher_DetectsDelete(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	writeFile(t, spec, "spec")

	w, _ := New(context.Background(), []string{spec}, 50*time.Millisecond)
	defer func() { _ = w.Close() }()

	time.Sleep(20 * time.Millisecond)
	if err := os.Remove(spec); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, ok := waitForEvent(t, w.C, 3*time.Second); !ok {
		t.Fatal("expected a change event for the deletion")
	}
}

func TestWatcher_CloseClosesChannel(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	writeFile(t, spec, "spec")

	w, _ := New(context.Background(), []string{spec}, 50*time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Channel must be closed after Close returns.
	if _, ok := <-w.C; ok {
		t.Fatal("expected C to be closed after Close")
	}
	// Second close must not panic.
	_ = w.Close()
}

func TestWatcher_ContextCancelStops(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	writeFile(t, spec, "spec")

	ctx, cancel := context.WithCancel(context.Background())
	w, _ := New(ctx, []string{spec}, 50*time.Millisecond)
	cancel()

	// The goroutine should exit and close the channel.
	select {
	case <-w.C:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop on context cancel")
	}
}
