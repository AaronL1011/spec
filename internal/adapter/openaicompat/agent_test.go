package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// The point of this adapter is that servers spec has never tested against work,
// so these tests pin the request/response *contract* rather than any vendor's
// behaviour. A stub /v1/chat/completions server stands in for the whole family.

func TestNew_RequiresBaseURL(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatal("expected an error when base_url is missing")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("error should name the missing field, got %q", err)
	}
}

func TestInvoke_NotSupported(t *testing.T) {
	c, err := New(Options{BaseURL: "http://localhost:1234/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Invoke(context.Background(), adapter.InvokeRequest{})
	if !errors.Is(err, adapter.ErrNotSupported) {
		t.Errorf("Invoke err = %v, want ErrNotSupported", err)
	}
}

func TestCapabilities_CompletionOnly(t *testing.T) {
	c, err := New(Options{BaseURL: "http://localhost:1234/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caps := c.Capabilities()
	if !caps.Generate {
		t.Error("Generate capability should be true")
	}
	if caps.MCP || caps.Headless || caps.Skills || caps.SystemPrompt {
		t.Errorf("session-plane capabilities should all be false, got %+v", caps)
	}
	if caps.StructuredOutput {
		t.Error("StructuredOutput should be false: response_format support varies by server")
	}
}

func TestGenerate_RequestShapeAndResponse(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "llama3.1",
			"choices": [{"message": {"role": "assistant", "content": "Drafted body."}}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19}
		}`))
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1", Model: "llama3.1", Token: "secret-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := c.Generate(context.Background(), adapter.GenerateRequest{
		Task:   "draft-section",
		System: "You are a technical writer.",
		Prompt: "Draft the problem statement.",
		Context: []adapter.ContextPart{
			{Label: "Existing sections", Content: "Some context."},
		},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}

	var sent chatRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if sent.Stream {
		t.Error("stream should be false: this is a one-shot completion")
	}
	if sent.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", sent.MaxTokens)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" {
		t.Fatalf("expected a system message then a user message, got %+v", sent.Messages)
	}
	if !strings.Contains(sent.Messages[1].Content, "Existing sections") {
		t.Error("labelled context parts should be rendered into the user message")
	}

	if res.Text != "Drafted body." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Model != "llama3.1" {
		t.Errorf("Model = %q, want the responding model", res.Model)
	}
	if res.Tokens.Total != 19 {
		t.Errorf("Tokens.Total = %d, want 19", res.Tokens.Total)
	}
}

// An air-gapped local server needs no credential, so the header must be absent
// rather than empty when no token is configured.
func TestGenerate_OmitsAuthHeaderWhenNoToken(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header should be absent when no token is set")
	}
}

// Output-format drift must degrade to an empty result with a bounded tail, not
// an error: a server that changes its envelope should not break drafting.
func TestGenerate_UnparseableOutput_DegradesWithRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("unparseable output should not error, got %v", err)
	}
	if res.Text != "" {
		t.Errorf("Text = %q, want empty", res.Text)
	}
	if res.Raw == "" {
		t.Error("Raw should carry a bounded tail for debugging")
	}
}

func TestGenerate_HTTPError_NamesEndpointAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error on HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should name the status, got %q", err)
	}
}

// A trailing slash on base_url must not produce a doubled path separator.
func TestNew_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/v1/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
}

// Compile-time assertion that the adapter satisfies the unified interface.
var _ adapter.AgentAdapter = (*Client)(nil)
