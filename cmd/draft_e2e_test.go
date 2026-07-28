package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/openaicompat"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
)

// End-to-end through the real seams a draft crosses: task assembly, the service,
// a completion-only adapter, and an HTTP endpoint. Only the model is a stub, so a
// break anywhere in that chain fails here rather than in front of a user.

func TestDraftPath_AssemblesRunsAndReturnsContent(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "stub-model-1",
			"choices": [{"message": {"role": "assistant", "content": "Drafted TL;DR body."}}],
			"usage": {"prompt_tokens": 321, "completion_tokens": 42, "total_tokens": 363}
		}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1", Model: "stub-model"})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	svc := llm.NewService(client, true)
	if !svc.IsAvailable() {
		t.Fatal("a completion-only provider should be available for drafting")
	}

	task, err := tasks.Get(tasks.DraftSection)
	if err != nil {
		t.Fatal(err)
	}

	in := llm.Input{
		SpecID:   "SPEC-031",
		Section:  "tl_dr",
		Meta:     map[string]string{"title": "Agent Unification"},
		Sections: map[string]string{"problem_statement": "Two parallel LLM integrations duplicate each other."},
	}

	res, err := svc.Run(context.Background(), task, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "Drafted TL;DR body." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Model != "stub-model-1" {
		t.Errorf("Model = %q, want the responding model, not the requested one", res.Model)
	}
	if res.Tokens.Total != 363 {
		t.Errorf("Tokens.Total = %d, want 363", res.Tokens.Total)
	}
	if res.Duration <= 0 {
		t.Error("Duration should be measured, for the status line and telemetry")
	}

	// The assembled prompt must actually carry the spec's context, not just the
	// instruction: this is the wiring that a unit test of either side would miss.
	var sent struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent.Stream {
		t.Error("a draft is a one-shot completion; stream must be false")
	}
	if sent.Model != "stub-model" {
		t.Errorf("requested model = %q", sent.Model)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" {
		t.Fatalf("expected a system then user message, got %+v", sent.Messages)
	}
	user := sent.Messages[1].Content
	if !strings.Contains(user, "TL;DR") && !strings.Contains(user, "Tl Dr") {
		t.Errorf("prompt should name the target section:\n%s", user)
	}
	if !strings.Contains(user, "Two parallel LLM integrations") {
		t.Errorf("prompt should carry spec context:\n%s", user)
	}
}

// A provider that is reachable but failing must surface an actionable error
// rather than an empty draft, so a user can tell "broken" from "declined".
func TestDraftPath_ProviderErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := tasks.Get(tasks.DraftSection)

	_, err = llm.NewService(client, true).Run(context.Background(), task, llm.Input{Section: "tl_dr"})
	if err == nil {
		t.Fatal("expected an error from a failing provider")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should carry the provider's status, got %q", err)
	}
	// draftError must not mistake a real failure for an unconfigured agent.
	if msg := draftError(err).Error(); strings.Contains(msg, "no agent completion plane") {
		t.Errorf("a provider failure should not be reported as missing config: %q", msg)
	}
}

// A session-only harness has no completion plane, and the message must say so
// rather than implying nothing is configured.
func TestDraftPath_UnavailableIsDistinctFromFailure(t *testing.T) {
	svc := llm.NewService(nil, true)
	task, _ := tasks.Get(tasks.DraftSection)

	_, err := svc.Run(context.Background(), task, llm.Input{})
	if err == nil {
		t.Fatal("expected ErrUnavailable with no adapter")
	}
	msg := draftError(err).Error()
	if !strings.Contains(msg, "spec agent check") {
		t.Errorf("the message should point at the diagnostic, got %q", msg)
	}
}

// Retry must reach the provider with the reviewer's steer, end to end.
func TestDraftPath_RetryWithNoteReachesProvider(t *testing.T) {
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var sent struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(b, &sent)
		if len(sent.Messages) > 0 {
			prompts = append(prompts, sent.Messages[len(sent.Messages)-1].Content)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"draft"}}]}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	svc := llm.NewService(client, true)
	task, _ := tasks.Get(tasks.DraftSection)

	in := llm.Input{SpecID: "SPEC-031", Section: "tl_dr"}
	// First attempt, then a steered retry, mirroring what the gate does.
	if _, err := svc.Run(context.Background(), task, in); err != nil {
		t.Fatal(err)
	}
	in.SteerNotes = []string{"lead with the revenue impact"}
	in.PriorDraft = "draft"
	if _, err := svc.Run(context.Background(), task, in); err != nil {
		t.Fatal(err)
	}

	if len(prompts) != 2 {
		t.Fatalf("expected two provider calls, got %d", len(prompts))
	}
	if strings.Contains(prompts[0], "Reviewer feedback") {
		t.Error("the first attempt should carry no steer note")
	}
	if !strings.Contains(prompts[1], "Reviewer feedback") ||
		!strings.Contains(prompts[1], "lead with the revenue impact") {
		t.Errorf("the retry must carry the steer note:\n%s", prompts[1])
	}
	if !strings.Contains(prompts[1], "Previous attempt (rejected)") {
		t.Errorf("the retry should show the model its rejected draft:\n%s", prompts[1])
	}
}
