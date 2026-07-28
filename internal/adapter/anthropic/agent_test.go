package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

func TestInvoke_NotSupported(t *testing.T) {
	c := NewClient("sk-ant-test", "")
	_, err := c.Invoke(context.Background(), adapter.InvokeRequest{})
	if !errors.Is(err, adapter.ErrNotSupported) {
		t.Errorf("Invoke err = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), "does not support sessions") {
		t.Errorf("error should explain the missing plane, got %q", err)
	}
}

func TestCapabilities_CompletionOnly(t *testing.T) {
	caps := NewClient("sk-ant-test", "").Capabilities()
	if !caps.Generate {
		t.Error("Generate capability should be true")
	}
	if caps.MCP || caps.Headless || caps.Skills || caps.SystemPrompt {
		t.Errorf("session-plane capabilities should all be false, got %+v", caps)
	}
}

func TestNewClient_DefaultsModel(t *testing.T) {
	if got := NewClient("k", "").model; got != DefaultModel {
		t.Errorf("model = %q, want %q", got, DefaultModel)
	}
	if got := NewClient("k", "claude-opus-4").model; got != "claude-opus-4" {
		t.Errorf("explicit model should win, got %q", got)
	}
}

func TestGenerate_SendsSystemAndReturnsUsage(t *testing.T) {
	var gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Drafted body."}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer srv.Close()

	c := NewClient("sk-ant-test", "")
	c.baseURL = srv.URL

	res, err := c.Generate(context.Background(), adapter.GenerateRequest{
		Task:   "draft-section",
		System: "You are a technical writer.",
		Prompt: "Draft the problem statement.",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotKey != "sk-ant-test" {
		t.Errorf("X-API-Key = %q", gotKey)
	}
	if gotVersion == "" {
		t.Error("Anthropic-Version header should be set")
	}
	if res.Text != "Drafted body." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Tokens.Input != 10 || res.Tokens.Output != 5 || res.Tokens.Total != 15 {
		t.Errorf("Tokens = %+v, want 10/5/15", res.Tokens)
	}
	if res.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want the responding model", res.Model)
	}
}

func TestGenerate_APIError_Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := NewClient("bad", "")
	c.baseURL = srv.URL

	_, err := c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error on HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should name the status, got %q", err)
	}
}

func TestGenerate_UnparseableOutput_DegradesWithRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClient("k", "")
	c.baseURL = srv.URL

	res, err := c.Generate(context.Background(), adapter.GenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("unparseable output should not error, got %v", err)
	}
	if res.Text != "" || res.Raw == "" {
		t.Errorf("want empty text with a bounded Raw tail, got %+v", res)
	}
}

// Compile-time assertion that the adapter satisfies the unified interface.
var _ adapter.AgentAdapter = (*Client)(nil)
