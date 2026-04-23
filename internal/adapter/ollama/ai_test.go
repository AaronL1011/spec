package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Model != "llama3.1" {
			t.Errorf("expected model llama3.1, got %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}
		// With system prompt, messages should be [system, user]
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages (system + user), got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("expected first message role=system, got %s", req.Messages[0].Role)
		}

		resp := chatResponse{
			Message: chatMessage{Role: "assistant", Content: "Draft output from Ollama."},
			Done:    true,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("", "", server.URL)
	result, err := client.Complete(context.Background(), "Draft a section", "You are helpful")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Draft output from Ollama." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestComplete_NoSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message (no system), got %d", len(req.Messages))
		}
		resp := chatResponse{
			Message: chatMessage{Role: "assistant", Content: "OK"},
			Done:    true,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("", "", server.URL)
	result, err := client.Complete(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "OK" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestEmbed_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("expected /api/embed, got %s", r.URL.Path)
		}

		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected embed model nomic-embed-text, got %s", req.Model)
		}

		resp := embedResponse{
			Embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("", "", server.URL)
	vec, err := client.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3-dim vector, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector: %v", vec)
	}
}

func TestEmbed_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{Embeddings: [][]float32{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("", "", server.URL)
	_, err := client.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty embeddings")
	}
}

func TestComplete_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("model not found"))
	}))
	defer server.Close()

	client := NewClient("", "", server.URL)
	_, err := client.Complete(context.Background(), "test", "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
