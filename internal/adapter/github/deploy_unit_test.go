package github

import (
	"context"
	"net/http"
	"testing"
)

func TestMapRunStatus(t *testing.T) {
	tests := []struct {
		status     string
		conclusion string
		want       string
	}{
		{"completed", "success", "success"},
		{"completed", "failure", "failure"},
		{"completed", "cancelled", "failure"},
		{"completed", "timed_out", "failure"},
		{"completed", "neutral", "neutral"},
		{"in_progress", "", "running"},
		{"queued", "", "running"},
		{"waiting", "", "pending"},
	}
	for _, tt := range tests {
		if got := mapRunStatus(tt.status, tt.conclusion); got != tt.want {
			t.Errorf("mapRunStatus(%q,%q) = %q, want %q", tt.status, tt.conclusion, got, tt.want)
		}
	}
}

// TestTrigger_NoRunYet_FallsBackToPending exercises the path where dispatch
// succeeds but no run is ever listed (cold workflow). latestDispatchedRun
// exhausts its retries and Trigger returns a pending placeholder rather than
// erroring. The retry backoff is bounded (sub-second total) so this stays fast.
func TestTrigger_NoRunYet_FallsBackToPending(t *testing.T) {
	_, dep := fixtureServer(t, []route{
		{method: http.MethodPost, contains: "/actions/workflows/deploy.yml/dispatches", status: http.StatusNoContent},
		{method: http.MethodGet, contains: "/actions/workflows/deploy.yml/runs", body: `{"total_count":0,"workflow_runs":[]}`},
	})

	run, err := dep.Trigger(context.Background(), []string{"auth-service"}, "staging")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run.ID != "pending" || run.Status != "pending" {
		t.Errorf("run = %+v, want a pending placeholder", run)
	}
}
