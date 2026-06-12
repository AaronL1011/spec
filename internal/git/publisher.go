package git

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

// defaultPublishDebounce coalesces rapid successive edits (e.g. a burst of
// thread replies) into a single push, keeping background pushes infrequent
// without making published state feel stale.
const defaultPublishDebounce = 750 * time.Millisecond

// Publisher coalesces local-edit notifications and pushes them to the specs
// repo on a background timer, off the caller's critical path. It is the async
// counterpart to PushLocalEditsOpts for long-lived surfaces (the TUI, the MCP
// server) that must never block on the network while a user or agent is
// mid-flow.
//
// Notify is fire-and-forget: edits arriving within the debounce window coalesce
// into one push. The push reuses PushLocalEditsOpts, so it inherits the
// section-aware conflict check, rebase-retry, and durable offline queue — a
// background push that can't reach the remote leaves the work committed locally
// and recoverable by the next committing CLI operation.
//
// A nil *Publisher is valid: every method is a no-op, so surfaces that disable
// auto-push (AutoPushOff) simply hold a nil publisher and call its methods
// unconditionally.
type Publisher struct {
	cfg      *config.SpecsRepoConfig
	opts     SyncOptions
	debounce time.Duration
	// push performs the actual commit+push. It defaults to PushLocalEditsOpts
	// and is overridable in tests to verify debounce/coalesce behaviour without
	// a real repo.
	push func(ctx context.Context, cfg *config.SpecsRepoConfig, msg string, opts SyncOptions) (bool, error)

	mu      sync.Mutex
	pending map[string]struct{} // spec IDs touched since the last flush
	timer   *time.Timer
	closed  bool

	// flushing serializes pushes so at most one runs at a time, even when a
	// debounce timer fires while Close is performing its final flush.
	flushing sync.Mutex
}

// NewPublisher returns a Publisher that pushes working-tree edits for the given
// specs repo. opts carries surface/trigger attribution and an optional recorder
// for audit/queue bookkeeping. A debounce of <= 0 uses defaultPublishDebounce.
func NewPublisher(cfg *config.SpecsRepoConfig, opts SyncOptions, debounce time.Duration) *Publisher {
	if debounce <= 0 {
		debounce = defaultPublishDebounce
	}
	return &Publisher{
		cfg:      cfg,
		opts:     opts,
		debounce: debounce,
		push:     PushLocalEditsOpts,
		pending:  make(map[string]struct{}),
	}
}

// Notify schedules a debounced background push that will include specID in the
// commit message. Repeated calls within the debounce window coalesce into one
// push. It returns immediately and never blocks on git or the network.
func (p *Publisher) Notify(specID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if specID != "" {
		p.pending[specID] = struct{}{}
	}
	if p.timer == nil {
		p.timer = time.AfterFunc(p.debounce, p.flush)
	} else {
		p.timer.Reset(p.debounce)
	}
}

// Close cancels any pending debounce timer and performs a final synchronous
// flush so no edits are left unpushed when a surface shuts down. It is safe to
// call more than once.
func (p *Publisher) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	p.flush()
}

// flush pushes the current working tree and drains the pending set. Pushes are
// serialized via p.flushing. A push failure is intentionally swallowed: a
// same-section conflict leaves the work committed locally for the next CLI
// operation to surface, and a transient/offline failure is queued durably
// inside PushLocalEditsOpts — neither is actionable from a background timer.
func (p *Publisher) flush() {
	p.flushing.Lock()
	defer p.flushing.Unlock()

	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(p.pending))
	for id := range p.pending {
		ids = append(ids, id)
	}
	p.pending = make(map[string]struct{})
	p.mu.Unlock()

	sort.Strings(ids)
	opts := p.opts
	if len(ids) == 1 {
		opts.SpecID = ids[0]
	}
	_, _ = p.push(context.Background(), p.cfg, commitMessageFor(ids), opts)
}

// commitMessageFor builds a conventional-commit message describing the specs
// touched in a coalesced push.
func commitMessageFor(ids []string) string {
	switch len(ids) {
	case 0:
		return "feat: update specs"
	case 1:
		return fmt.Sprintf("feat: update %s", ids[0])
	default:
		return fmt.Sprintf("feat: update specs (%s)", strings.Join(ids, ", "))
	}
}
