package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gh "github.com/google/go-github/v62/github"
	"github.com/aaronl1011/spec-cli/internal/adapter"
)

// DeployClient implements adapter.DeployAdapter using GitHub Actions workflow dispatch.
type DeployClient struct {
	client   *gh.Client
	owner    string
	workflow string // workflow file name, e.g. "deploy.yml"
}

// NewDeployClient creates a GitHub Actions DeployAdapter.
// workflow is the workflow file name (e.g., "deploy.yml") to dispatch.
func NewDeployClient(token, owner, workflow string) *DeployClient {
	var httpClient *http.Client
	if token != "" {
		httpClient = &http.Client{
			Transport: &tokenTransport{token: token},
			Timeout:   10 * time.Second,
		}
	} else {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	client := gh.NewClient(httpClient)
	if workflow == "" {
		workflow = "deploy.yml"
	}
	return &DeployClient{client: client, owner: owner, workflow: workflow}
}

// Trigger dispatches a workflow_dispatch event for each repo targeting the given environment.
// It creates a deployment run per repo and returns the first one (callers can extend for multi-repo).
func (d *DeployClient) Trigger(ctx context.Context, repos []string, env string) (*adapter.DeployRun, error) {
	if len(repos) == 0 {
		return nil, fmt.Errorf("no repos specified for deployment")
	}

	// Dispatch workflow for the first repo (primary). In practice, callers
	// may loop over repos; we dispatch all and return the first run reference.
	for _, repo := range repos {
		_, err := d.client.Actions.CreateWorkflowDispatchEventByFileName(
			ctx, d.owner, repo, d.workflow,
			gh.CreateWorkflowDispatchEventRequest{
				Ref: "main",
				Inputs: map[string]interface{}{
					"environment": env,
				},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("dispatching deploy workflow for %s/%s: %w", d.owner, repo, err)
		}
	}

	// GitHub doesn't return a run ID from dispatch. Return a pending stub
	// immediately — callers use Status() to poll for the actual run.
	return &adapter.DeployRun{
		ID:     "pending",
		Repo:   repos[0],
		Env:    env,
		Status: "pending",
	}, nil
}

// Status polls a deployment run for its current state.
func (d *DeployClient) Status(ctx context.Context, run *adapter.DeployRun) (*adapter.DeployStatus, error) {
	if run == nil || run.ID == "pending" {
		return &adapter.DeployStatus{
			Status:  "pending",
			Message: "Deployment was dispatched but no run ID is available yet.",
		}, nil
	}

	var runID int64
	if _, err := fmt.Sscanf(run.ID, "%d", &runID); err != nil {
		return nil, fmt.Errorf("parsing run ID %q: %w", run.ID, err)
	}

	workflowRun, _, err := d.client.Actions.GetWorkflowRunByID(ctx, d.owner, run.Repo, runID)
	if err != nil {
		return nil, fmt.Errorf("getting workflow run %d: %w", runID, err)
	}

	return &adapter.DeployStatus{
		RunID:   run.ID,
		Status:  mapRunStatus(workflowRun.GetStatus(), workflowRun.GetConclusion()),
		URL:     workflowRun.GetHTMLURL(),
		Message: fmt.Sprintf("Workflow: %s", workflowRun.GetName()),
	}, nil
}

func mapRunStatus(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return "success"
		case "failure", "cancelled", "timed_out":
			return "failure"
		default:
			return conclusion
		}
	case "in_progress", "queued":
		return "running"
	default:
		return "pending"
	}
}
