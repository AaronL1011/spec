// Package noop provides no-op adapter implementations for unconfigured integrations.
// These adapters return empty results and nil errors — they never panic or block.
package noop

import (
	"context"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Comms is a no-op CommsAdapter.
type Comms struct{}

func (Comms) Notify(ctx context.Context, msg adapter.Notification) error           { return nil }
func (Comms) PostStandup(ctx context.Context, standup adapter.StandupReport) error  { return nil }
func (Comms) FetchMentions(ctx context.Context, since time.Time) ([]adapter.Mention, error) {
	return nil, nil
}

// PM is a no-op PMAdapter.
type PM struct{}

func (PM) CreateEpic(ctx context.Context, spec adapter.SpecMeta) (string, error) { return "", nil }
func (PM) UpdateStatus(ctx context.Context, epicKey string, status string) error  { return nil }
func (PM) FetchUpdates(ctx context.Context, epicKey string) (*adapter.PMUpdate, error) {
	return nil, nil
}

// Docs is a no-op DocsAdapter.
type Docs struct{}

func (Docs) FetchSections(ctx context.Context, specID string) (map[string]string, error) {
	return nil, nil
}
func (Docs) PushFull(ctx context.Context, specID string, content string) error { return nil }
func (Docs) PageURL(ctx context.Context, specID string) (string, error)        { return "", nil }

// Repo is a no-op RepoAdapter.
type Repo struct{}

func (Repo) ListPRs(ctx context.Context, repos []string, specID string) ([]adapter.PullRequest, error) {
	return nil, nil
}
func (Repo) PRStatus(ctx context.Context, repo string, prNumber int) (*adapter.PRDetail, error) {
	return nil, nil
}
func (Repo) SetPRDescription(ctx context.Context, repo string, prNumber int, body string) error {
	return nil
}
func (Repo) RequestedReviews(ctx context.Context, user string) ([]adapter.PullRequest, error) {
	return nil, nil
}

// Agent is a no-op AgentAdapter.
type Agent struct{}

func (Agent) Invoke(ctx context.Context, contextFile string, workDir string) error { return nil }
func (Agent) SupportsMCP() bool                                                    { return false }

// Deploy is a no-op DeployAdapter.
type Deploy struct{}

func (Deploy) Trigger(ctx context.Context, repos []string, env string) (*adapter.DeployRun, error) {
	return nil, nil
}
func (Deploy) Status(ctx context.Context, run *adapter.DeployRun) (*adapter.DeployStatus, error) {
	return nil, nil
}

// AI is a no-op AIAdapter.
type AI struct{}

func (AI) Complete(ctx context.Context, prompt string, system string) (string, error) {
	return "", nil
}
func (AI) Embed(ctx context.Context, text string) ([]float32, error) { return nil, nil }

// Intake is a no-op IntakeAdapter.
type Intake struct{}

func (Intake) FetchItems(ctx context.Context) ([]adapter.IntakeItem, error) { return nil, nil }
