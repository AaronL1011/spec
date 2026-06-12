package git

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

// fakePush records each push invocation so tests can assert how many pushes a
// publisher coalesced a burst of notifications into.
type fakePush struct {
	mu    sync.Mutex
	msgs  []string
	specs []string
}

func (f *fakePush) fn(_ context.Context, _ *config.SpecsRepoConfig, msg string, opts SyncOptions) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	f.specs = append(f.specs, opts.SpecID)
	return true, nil
}

func (f *fakePush) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func newTestPublisher(debounce time.Duration) (*Publisher, *fakePush) {
	f := &fakePush{}
	p := NewPublisher(&config.SpecsRepoConfig{Owner: "o", Repo: "r"}, SyncOptions{}, debounce)
	p.push = f.fn
	return p, f
}

func TestPublisher_NilSafe(t *testing.T) {
	var p *Publisher
	// Must not panic on a nil receiver — surfaces with auto-push disabled hold
	// a nil publisher and call these unconditionally.
	p.Notify("SPEC-001")
	p.Close()
}

func TestPublisher_CoalescesBurstIntoOnePush(t *testing.T) {
	p, f := newTestPublisher(40 * time.Millisecond)
	defer p.Close()

	for i := 0; i < 5; i++ {
		p.Notify("SPEC-001")
		time.Sleep(5 * time.Millisecond) // within the debounce window
	}

	time.Sleep(120 * time.Millisecond) // let the debounce fire
	if got := f.calls(); got != 1 {
		t.Fatalf("expected 1 coalesced push, got %d", got)
	}
}

func TestPublisher_SingleSpecAttributed(t *testing.T) {
	p, f := newTestPublisher(20 * time.Millisecond)
	defer p.Close()

	p.Notify("SPEC-042")
	time.Sleep(80 * time.Millisecond)

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.msgs) != 1 || f.msgs[0] != "feat: update SPEC-042" {
		t.Fatalf("unexpected messages: %v", f.msgs)
	}
	if f.specs[0] != "SPEC-042" {
		t.Fatalf("expected SpecID attribution SPEC-042, got %q", f.specs[0])
	}
}

func TestPublisher_CloseFlushesPending(t *testing.T) {
	// A long debounce that would not fire on its own within the test window;
	// Close must still flush the pending edit so nothing is left unpublished.
	p, f := newTestPublisher(10 * time.Second)
	p.Notify("SPEC-007")
	p.Close()

	if got := f.calls(); got != 1 {
		t.Fatalf("expected Close to flush 1 pending push, got %d", got)
	}
}

func TestPublisher_NotifyAfterCloseIgnored(t *testing.T) {
	p, f := newTestPublisher(10 * time.Millisecond)
	p.Close() // nothing pending

	p.Notify("SPEC-009")
	time.Sleep(50 * time.Millisecond)
	if got := f.calls(); got != 0 {
		t.Fatalf("expected no push after Close, got %d", got)
	}
}

func TestCommitMessageFor(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{"none", nil, "feat: update specs"},
		{"single", []string{"SPEC-001"}, "feat: update SPEC-001"},
		{"multiple", []string{"SPEC-001", "SPEC-002"}, "feat: update specs (SPEC-001, SPEC-002)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commitMessageFor(tt.ids); got != tt.want {
				t.Fatalf("commitMessageFor(%v) = %q, want %q", tt.ids, got, tt.want)
			}
		})
	}
}
