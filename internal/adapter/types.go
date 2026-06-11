// Package adapter defines interfaces for all external integrations.
// Engines depend on these interfaces, never on concrete implementations.
package adapter

import "time"

// Notification represents a structured message to send via comms.
type Notification struct {
	SpecID  string
	Title   string
	Message string
	Channel string
	Mention string // e.g., "@alice"
}

// StandupReport represents a formatted standup.
type StandupReport struct {
	UserName  string
	Date      string
	Yesterday []string
	Today     []string
	Blockers  []string
}

// Mention represents a comms mention of a spec.
type Mention struct {
	SpecID    string
	Channel   string
	Author    string
	Preview   string
	Timestamp time.Time
}

// SpecMeta is a lightweight spec summary for adapter use.
type SpecMeta struct {
	ID      string
	Title   string
	Status  string
	EpicKey string
	Repos   []string

	// Problem is a short excerpt of the problem statement, used to give the
	// PM epic meaningful context instead of just an id.
	Problem string
	// Labels are applied to the created issue (in addition to config labels).
	Labels []string
	// Cycle is the team cycle/iteration label.
	Cycle string
	// URL is a back-link to the canonical spec document.
	URL string
}

// StorySpec describes a build step to reconcile into a PM story under an epic.
type StorySpec struct {
	// StepID is a stable identifier for the step, used as the idempotency key.
	StepID      string
	Repo        string
	Description string
	// Status is the spec step status: pending | in-progress | complete | blocked.
	Status string
}

// StoryLink is the result of reconciling a StorySpec: the PM story key and the
// status it was left in.
type StoryLink struct {
	StepID   string
	StoryKey string
	Status   string
}

// PMUpdate represents status changes from a PM tool.
type PMUpdate struct {
	Status    string
	Assignee  string
	UpdatedAt time.Time
}

// PullRequest represents a PR from a repo provider.
type PullRequest struct {
	Number    int
	Title     string
	Repo      string
	Branch    string
	Author    string
	URL       string
	Status    string // "open", "merged", "closed"
	Approved  bool
	CIStatus  string // "passing", "failing", "pending"
	CreatedAt time.Time
}

// PRDetail represents detailed PR information.
type PRDetail struct {
	PullRequest
	ReviewComments    int
	UnresolvedThreads int
}

// DeployRun represents a triggered deployment.
type DeployRun struct {
	ID     string
	Repo   string
	Env    string
	Status string
	URL    string
}

// DeployStatus represents the current state of a deployment.
type DeployStatus struct {
	RunID   string
	Status  string // "pending", "running", "success", "failure"
	URL     string
	Message string
}
