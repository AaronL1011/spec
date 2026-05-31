package git

import "github.com/aaronl1011/spec/internal/config"

// cfgForBranch builds a minimal SpecsRepoConfig for tests that only need a
// branch + owner/repo key. Token validation is bypassed by calling the
// internal helpers (autoRecover, sectionOverlap) directly rather than the
// public entry points.
type cfgForBranch struct {
	branch string
}

func (c cfgForBranch) cfg() *config.SpecsRepoConfig {
	return &config.SpecsRepoConfig{
		Provider: "github",
		Owner:    "test",
		Repo:     "specs",
		Branch:   c.branch,
		Token:    "x",
	}
}

// fakeRecorder is an in-memory git.Recorder for asserting audit/queue behaviour.
type fakeRecorder struct {
	events    []AuditEvent
	enqueued  int
	pending   []QueuedItem
	resolved  []int64
	marked    []int64
	lastFetch map[string]int64
}

func (r *fakeRecorder) Record(ev AuditEvent) { r.events = append(r.events, ev) }
func (r *fakeRecorder) Enqueue(repoKey, branch, commitSHA string, ev AuditEvent) {
	r.enqueued++
	r.pending = append(r.pending, QueuedItem{ID: int64(r.enqueued), Branch: branch, SpecID: ev.SpecID, Trigger: ev.Trigger, Surface: ev.Surface})
}
func (r *fakeRecorder) Pending(string) []QueuedItem { return r.pending }
func (r *fakeRecorder) ResolveQueued(id int64)      { r.resolved = append(r.resolved, id) }
func (r *fakeRecorder) MarkQueued(id int64, _, _ string) {
	r.marked = append(r.marked, id)
}
func (r *fakeRecorder) LastFetch(key string) (int64, bool) {
	if r.lastFetch == nil {
		return 0, false
	}
	v, ok := r.lastFetch[key]
	return v, ok
}
func (r *fakeRecorder) SetLastFetch(key string, secs int64) {
	if r.lastFetch == nil {
		r.lastFetch = map[string]int64{}
	}
	r.lastFetch[key] = secs
}
