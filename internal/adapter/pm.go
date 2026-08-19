package adapter

import "context"

// PMAdapter manages project management tool integration.
//
// Implementations must be idempotent and degrade gracefully: an unconfigured
// or unreachable PM tool returns empty results and nil errors rather than
// blocking spec authoring (see docs/JIRA_HARDENING_PLAN.md).
//
// The interface deliberately does not name issue types beyond the two verbs
// spec needs. A PM tool's issue-type taxonomy is provider knowledge that lives
// in its adapter package; spec stores only a key (`pm_key`) and lets the
// adapter resolve the type from it.
type PMAdapter interface {
	// FindEpic returns the key of an existing PM object linked to the spec, or
	// "" when none exists. It is the idempotency guard for CreateEpic and
	// CreateTask alike: a spec that already has a PM object is linked, never
	// converted to another type.
	FindEpic(ctx context.Context, specID string) (pmKey string, err error)
	// CreateEpic creates a new epic/issue linked to a spec and returns its key.
	// Used for a standalone spec and for an initiative.
	CreateEpic(ctx context.Context, spec SpecMeta) (pmKey string, err error)
	// CreateTask creates a task under an existing epic, for a spec that is a
	// deliverable slice of an initiative. parentKey is the parent spec's PM
	// key. Implementations return ("", nil) when they cannot place the task,
	// so a PM shortfall never blocks the spec-side link.
	CreateTask(ctx context.Context, spec SpecMeta, parentKey string) (pmKey string, err error)
	// LinkEpic records a back-link from the PM issue to the spec so board
	// consumers can navigate PM -> spec. specURL may be empty.
	LinkEpic(ctx context.Context, pmKey, specID, specURL string) error
	// UpdateStatus syncs the spec's pipeline stage to the PM tool's board
	// status. A stage with no configured mapping is a clean no-op.
	UpdateStatus(ctx context.Context, pmKey string, status string) error
	// FetchUpdates returns status changes from the PM tool since last sync.
	FetchUpdates(ctx context.Context, pmKey string) (*PMUpdate, error)
	// SyncStories reconciles per-step children of a PM object, returning the
	// resulting story links. The adapter resolves the object's type from pmKey
	// and chooses the appropriate child issue type. A no-op when story sync is
	// disabled.
	SyncStories(ctx context.Context, pmKey string, stories []StorySpec) ([]StoryLink, error)
	// Validate checks credentials and configuration against the live PM tool.
	Validate(ctx context.Context) error
}
