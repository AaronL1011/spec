package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key test-key, got %s", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Errorf("expected Anthropic-Version 2023-06-01")
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("expected model claude-sonnet-4-20250514, got %s", req.Model)
		}
		if req.System != "You are a spec assistant." {
			t.Errorf("expected system prompt, got %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != "Draft a problem statement" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		resp := messagesResponse{
			ID:   "msg_123",
			Type: "message",
			Content: []contentBlock{
				{Type: "text", Text: "Here is the drafted problem statement."},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", "")
	client.baseURL = server.URL

	result, err := client.Complete(context.Background(), "Draft a problem statement", "You are a spec assistant.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Here is the drafted problem statement." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestComplete_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer server.Close()

	client := NewClient("bad-key", "")
	client.baseURL = server.URL

	_, err := client.Complete(context.Background(), "test", "")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestEmbed_NotSupported(t *testing.T) {
	client := NewClient("test-key", "")
	result, err := client.Embed(context.Background(), "test text")
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
	if err != ErrEmbeddingsNotSupported {
		t.Errorf("expected ErrEmbeddingsNotSupported, got %v", err)
	}
}

func TestMessagesResponse_Text(t *testing.T) {
	resp := messagesResponse{
		Content: []contentBlock{
			{Type: "text", Text: "Hello "},
			{Type: "text", Text: "world"},
		},
	}
	if got := resp.Text(); got != "Hello world" {
		t.Errorf("Text() = %q, want %q", got, "Hello world")
	}
}
