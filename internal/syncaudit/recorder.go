// Package syncaudit bridges the git sync layer to the SQLite store. It is the
// single place that implements git.Recorder, keeping internal/git free of any
// store import (AGENTS.md: engines depend on interfaces; only internal/store
// touches SQLite). git defines the Recorder interface; this package wires it to
// the store's sync_audit / sync_queue tables and freshness timestamps.
package syncaudit

import (
	"time"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
)

func timeFromUnix(seconds int64) time.Time { return time.Unix(seconds, 0) }

// Recorder implements git.Recorder against a *store.DB.
type Recorder struct {
	db *store.DB
}

// New returns a store-backed recorder, or nil if db is nil (callers treat a
// nil recorder as "no audit", matching git's noop default).
func New(db *store.DB) *Recorder {
	if db == nil {
		return nil
	}
	return &Recorder{db: db}
}

var _ gitpkg.Recorder = (*Recorder)(nil)

// Record appends one audit event. Recording failures are swallowed: auditing
// must never fail the surrounding sync operation.
func (r *Recorder) Record(ev gitpkg.AuditEvent) {
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.SyncAuditLog(store.SyncAuditEntry{
		Op:      ev.Op,
		Actor:   ev.Actor,
		Surface: ev.Surface,
		Trigger: ev.Trigger,
		SpecID:  ev.SpecID,
		Outcome: ev.Outcome,
		Detail:  ev.Detail,
	})
}

// Enqueue records a committed-but-unpushed operation and audits the deferral.
func (r *Recorder) Enqueue(repoKey, branch, commitSHA string, ev gitpkg.AuditEvent) {
	if r == nil || r.db == nil {
		return
	}
	_, _ = r.db.QueuePushEnqueue(store.QueuedPush{
		RepoKey:   repoKey,
		Branch:    branch,
		CommitSHA: commitSHA,
		Surface:   ev.Surface,
		Trigger:   ev.Trigger,
		SpecID:    ev.SpecID,
		Status:    store.QueueStatusQueued,
		Detail:    ev.Detail,
	})
}

// Pending returns flushable queued items, oldest first.
func (r *Recorder) Pending(repoKey string) []gitpkg.QueuedItem {
	if r == nil || r.db == nil {
		return nil
	}
	rows, err := r.db.QueuePushPending(repoKey)
	if err != nil {
		return nil
	}
	items := make([]gitpkg.QueuedItem, 0, len(rows))
	for _, q := range rows {
		items = append(items, gitpkg.QueuedItem{
			ID:      q.ID,
			Branch:  q.Branch,
			SpecID:  q.SpecID,
			Trigger: q.Trigger,
			Surface: q.Surface,
		})
	}
	return items
}

// ResolveQueued removes a flushed queued item.
func (r *Recorder) ResolveQueued(id int64) {
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.QueuePushResolve(id)
}

// MarkQueued flags a queued item (e.g. needs-resolution).
func (r *Recorder) MarkQueued(id int64, status, detail string) {
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.QueuePushMark(id, status, detail)
}

// LastFetch returns the last-fetch unix timestamp for a repo.
func (r *Recorder) LastFetch(repoKey string) (int64, bool) {
	if r == nil || r.db == nil {
		return 0, false
	}
	t, err := r.db.LastFetchGet(repoKey)
	if err != nil || t.IsZero() {
		return 0, false
	}
	return t.Unix(), true
}

// SetLastFetch records a successful fetch timestamp for a repo.
func (r *Recorder) SetLastFetch(repoKey string, seconds int64) {
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.LastFetchSet(repoKey, timeFromUnix(seconds))
}
