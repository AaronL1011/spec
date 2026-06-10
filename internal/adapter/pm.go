package adapter

import "context"

// PMAdapter manages project management tool integration.
//
// Implementations must be idempotent and degrade gracefully: an unconfigured
// or unreachable PM tool returns empty results and nil errors rather than
// blocking spec authoring (see docs/JIRA_HARDENING_PLAN.md).
type PMAdapter interface {
	// FindEpic returns the key of an existing epic linked to the spec, or ""
	// when none exists. It is the idempotency guard for CreateEpic.
	FindEpic(ctx context.Context, specID string) (epicKey string, err error)
	// CreateEpic creates a new epic/issue linked to a spec and returns its key.
	CreateEpic(ctx context.Context, spec SpecMeta) (epicKey string, err error)
	// LinkEpic records a back-link from the PM issue to the spec so board
	// consumers can navigate PM -> spec. specURL may be empty.
	LinkEpic(ctx context.Context, epicKey, specID, specURL string) error
	// UpdateStatus syncs the spec's pipeline stage to the PM tool's board
	// status. A stage with no configured mapping is a clean no-op.
	UpdateStatus(ctx context.Context, epicKey string, status string) error
	// FetchUpdates returns status changes from the PM tool since last sync.
	FetchUpdates(ctx context.Context, epicKey string) (*PMUpdate, error)
	// SyncStories reconciles per-step stories under an epic, returning the
	// resulting story links. A no-op when story sync is disabled.
	SyncStories(ctx context.Context, epicKey string, stories []StorySpec) ([]StoryLink, error)
	// Validate checks credentials and configuration against the live PM tool.
	Validate(ctx context.Context) error
}
