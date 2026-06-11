package github

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

func TestTrigger_DispatchesAndReturnsRun(t *testing.T) {
	_, dep := fixtureServer(t, []route{
		{method: http.MethodPost, contains: "/actions/workflows/deploy.yml/dispatches", status: http.StatusNoContent},
		{method: http.MethodGet, contains: "/actions/workflows/deploy.yml/runs", file: "workflow_runs.json"},
	})

	run, err := dep.Trigger(context.Background(), []string{"auth-service"}, "production")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run.ID != "555001" {
		t.Errorf("run ID = %q, want 555001", run.ID)
	}
	if run.Env != "production" || run.Repo != "auth-service" {
		t.Errorf("run = %+v, want env=production repo=auth-service", run)
	}
	if run.Status != "running" {
		t.Errorf("run status = %q, want running (in_progress)", run.Status)
	}
}

func TestTrigger_NoRepos_Errors(t *testing.T) {
	_, dep := fixtureServer(t, nil)
	_, err := dep.Trigger(context.Background(), nil, "production")
	if err == nil {
		t.Fatal("expected an error when no repos are specified")
	}
}

func TestTrigger_DispatchForbidden_ReturnsActionableError(t *testing.T) {
	_, dep := fixtureServer(t, []route{
		{method: http.MethodPost, contains: "/actions/workflows/deploy.yml/dispatches",
			status: http.StatusForbidden, body: `{"message":"Resource not accessible by integration"}`},
	})

	_, err := dep.Trigger(context.Background(), []string{"auth-service"}, "production")
	if err == nil {
		t.Fatal("expected an error on forbidden dispatch")
	}
	if !strings.Contains(err.Error(), "Actions workflow dispatch permission") {
		t.Errorf("error = %q, want the actionable next-step suffix", err)
	}
}

func TestStatus_PollsRunByID(t *testing.T) {
	_, dep := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/actions/runs/555001", file: "workflow_run.json"},
	})

	st, err := dep.Status(context.Background(), &adapter.DeployRun{ID: "555001", Repo: "auth-service", Env: "production"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "success" {
		t.Errorf("status = %q, want success", st.Status)
	}
	if !strings.Contains(st.Message, "Deploy") {
		t.Errorf("message = %q, want it to name the workflow", st.Message)
	}
}

func TestStatus_PendingRun_NoNetwork(t *testing.T) {
	_, dep := fixtureServer(t, nil)
	st, err := dep.Status(context.Background(), &adapter.DeployRun{ID: "pending"})
	if err != nil {
		t.Fatalf("Status(pending): %v", err)
	}
	if st.Status != "pending" {
		t.Errorf("status = %q, want pending", st.Status)
	}
}

func TestStatus_ServerError_ReturnsError(t *testing.T) {
	_, dep := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/actions/runs/555001", status: http.StatusBadGateway, body: `{"message":"bad gateway"}`},
	})

	_, err := dep.Status(context.Background(), &adapter.DeployRun{ID: "555001", Repo: "auth-service"})
	if err == nil {
		t.Fatal("expected an error on 5xx status poll")
	}
}
