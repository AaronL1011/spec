package git

import "context"

// Sync audit operation kinds recorded via the injected Recorder.
const (
	OpFetch      = "fetch"
	OpCommit     = "commit"
	OpPush       = "push"
	OpRecover    = "recover"
	OpQueueFlush = "queue-flush"
)

// Sync outcomes mirrored from the store layer so callers in git don't need to
// import internal/store (adapter-isolation: only internal/store touches SQLite).
const (
	OutcomeOK       = "ok"
	OutcomeQueued   = "queued"
	OutcomeConflict = "conflict"
	OutcomeError    = "error"
)

// AuditEvent is one sync action to record. It is surface/trigger attributed so
// the audit log can answer "what synced, when, by whom, via which surface".
type AuditEvent struct {
	Op      string
	Actor   string
	Surface string
	Trigger string
	SpecID  string
	Outcome string
	Detail  string
}

// QueuedItem is a committed-but-unpushed operation awaiting flush, surfaced to
// git's flush loop without exposing the store's row type.
type QueuedItem struct {
	ID      int64
	Branch  string
	SpecID  string
	Trigger string
	Surface string
}

// Recorder persists sync audit events and manages the queued-push backlog.
// It is injected into internal/git as an interface so git never imports
// internal/store directly (AGENTS.md: engines depend on interfaces; only
// internal/store touches SQLite).
type Recorder interface {
	// Record appends one audit event. Implementations must not fail the
	// surrounding sync operation on a recording error.
	Record(ev AuditEvent)
	// Enqueue records a committed-but-unpushed operation for later flush.
	Enqueue(repoKey, branch, commitSHA string, ev AuditEvent)
	// Pending returns flushable queued items for a repo, oldest first.
	Pending(repoKey string) []QueuedItem
	// ResolveQueued removes a successfully-flushed queued item.
	ResolveQueued(id int64)
	// MarkQueued flags a queued item (e.g. needs-resolution on conflict).
	MarkQueued(id int64, status, detail string)
	// LastFetch / SetLastFetch back the freshness TTL.
	LastFetch(repoKey string) (seconds int64, ok bool)
	SetLastFetch(repoKey string, seconds int64)
}

// noopRecorder is used when no recorder is injected (e.g. tests, or callers
// that don't need audit). Every method is a no-op.
type noopRecorder struct{}

func (noopRecorder) Record(AuditEvent)                          {}
func (noopRecorder) Enqueue(string, string, string, AuditEvent) {}
func (noopRecorder) Pending(string) []QueuedItem                { return nil }
func (noopRecorder) ResolveQueued(int64)                        {}
func (noopRecorder) MarkQueued(int64, string, string)           {}
func (noopRecorder) LastFetch(string) (int64, bool)             { return 0, false }
func (noopRecorder) SetLastFetch(string, int64)                 {}

// SyncOptions carries surface/trigger attribution and an optional recorder
// through a committing operation. A zero value is valid: it attributes to the
// CLI with a no-op recorder.
type SyncOptions struct {
	Surface  string
	Trigger  string
	SpecID   string
	Actor    string
	Recorder Recorder
}

func (o SyncOptions) recorder() Recorder {
	if o.Recorder == nil {
		return noopRecorder{}
	}
	return o.Recorder
}

func (o SyncOptions) record(op, outcome, detail string) {
	o.recorder().Record(AuditEvent{
		Op:      op,
		Actor:   o.Actor,
		Surface: o.Surface,
		Trigger: o.Trigger,
		SpecID:  o.SpecID,
		Outcome: outcome,
		Detail:  detail,
	})
}

// withAudit returns a context-free helper carrying defaults filled in.
func (o SyncOptions) normalized(ctx context.Context) SyncOptions {
	if o.Surface == "" {
		o.Surface = "cli"
	}
	if o.Actor == "" {
		o.Actor = UserName(ctx)
	}
	return o
}
