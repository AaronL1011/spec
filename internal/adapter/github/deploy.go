package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	gh "github.com/google/go-github/v62/github"
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
			return nil, fmt.Errorf("dispatching deploy workflow for %s/%s: %w — token needs repo access and Actions workflow dispatch permission", d.owner, repo, err)
		}
	}

	run, err := d.latestDispatchedRun(ctx, repos[0])
	if err == nil && run != nil {
		return &adapter.DeployRun{
			ID:     strconv.FormatInt(run.GetID(), 10),
			Repo:   repos[0],
			Env:    env,
			Status: mapRunStatus(run.GetStatus(), run.GetConclusion()),
			URL:    run.GetHTMLURL(),
		}, nil
	}

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

func (d *DeployClient) latestDispatchedRun(ctx context.Context, repo string) (*gh.WorkflowRun, error) {
	for attempt := 0; attempt < 3; attempt++ {
		runs, _, err := d.client.Actions.ListWorkflowRunsByFileName(ctx, d.owner, repo, d.workflow, &gh.ListWorkflowRunsOptions{
			Event:       "workflow_dispatch",
			Branch:      "main",
			ListOptions: gh.ListOptions{PerPage: 1},
		})
		if err != nil {
			return nil, fmt.Errorf("listing workflow runs for %s/%s: %w", d.owner, repo, err)
		}
		if len(runs.WorkflowRuns) > 0 {
			return runs.WorkflowRuns[0], nil
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil
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
