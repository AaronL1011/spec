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
