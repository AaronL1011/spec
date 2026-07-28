package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/openaicompat"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
)

// The assist path is advisory: it must reach the provider with the plan as the
// subject and must never carry a verdict. This runs the real seams — task
// assembly, the service, a completion-only adapter, an HTTP endpoint — with only
// the model stubbed, so a break in the chain fails here rather than in front of
// a reviewer.
func TestPlanAssist_SendsPlanAndReturnsAdvisoryNotes(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "stub-model-1",
			"choices": [{"message": {"role": "assistant", "content": "## Risks\n- Step 2 owns no migration."}}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}
		}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1", Model: "stub-model"})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}

	task, err := tasks.Get(tasks.ReviewPlan)
	if err != nil {
		t.Fatal(err)
	}

	in := llm.Input{
		SpecID: "SPEC-999",
		Repos:  []string{"spec-cli"},
		Sections: map[string]string{
			"pr_stack_plan": "1. [spec-cli] Add the limiter\n2. [spec-cli] Wire middleware (after: 1)",
		},
	}

	res, err := llm.NewService(client, true).Run(context.Background(), task, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "Risks") {
		t.Errorf("Text = %q, want advisory notes", res.Text)
	}
	// The plan must reach the model — an assist that reviews everything except
	// the plan is worse than none, because it reads authoritative.
	if !strings.Contains(gotBody, "Wire middleware") {
		t.Error("request body should carry the PR stack plan as the subject")
	}
	// The task is advisory by construction: nothing in the prompt should ask for
	// a verdict, since the approve/request-changes decision is human-only.
	for _, forbidden := range []string{"approve", "request-changes"} {
		if strings.Contains(strings.ToLower(gotBody), forbidden) {
			t.Errorf("prompt should not solicit a verdict, found %q", forbidden)
		}
	}
}

// A plan-less spec must be told what to do rather than sending an empty plan to
// a model, which would produce confident notes about nothing.
func TestPlanAssist_RequiresAPlan(t *testing.T) {
	in := llm.Input{SpecID: "SPEC-999", Sections: map[string]string{}}
	if in.Sections["pr_stack_plan"] != "" {
		t.Fatal("fixture should have no plan")
	}
	// runPlanAssist's guard is the contract; assert the message names the fix so
	// the check cannot be weakened to a bare failure.
	const want = "spec draft"
	if !strings.Contains(planAssistMissingPlanHint("SPEC-999"), want) {
		t.Errorf("hint should point at %q", want)
	}
}
