package teams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aaronl1011/spec-cli/internal/adapter"
)

func TestNotify_Success(t *testing.T) {
	var received webhookPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "", "", "")
	err := client.Notify(context.Background(), adapter.Notification{
		SpecID:  "SPEC-042",
		Title:   "Auth refactor",
		Message: "Stage → tl-review",
		Mention: "@alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Type != "message" {
		t.Errorf("expected type 'message', got %q", received.Type)
	}
	if len(received.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(received.Attachments))
	}
	card := received.Attachments[0].Content
	if card.Version != "1.4" {
		t.Errorf("expected card version 1.4, got %q", card.Version)
	}
	if len(card.Body) < 2 {
		t.Errorf("expected at least 2 body elements, got %d", len(card.Body))
	}
}

func TestNotify_WebhookError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "", "", "")
	err := client.Notify(context.Background(), adapter.Notification{
		SpecID: "SPEC-001",
		Title:  "Test",
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestPostStandup_Success(t *testing.T) {
	var received webhookPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("", server.URL, "", "", "")
	err := client.PostStandup(context.Background(), adapter.StandupReport{
		UserName:  "Aaron",
		Date:      "2026-04-21",
		Yesterday: []string{"Completed PR #415"},
		Today:     []string{"PR 3/4 api-gateway"},
		Blockers:  []string{"SPEC-037 blocked 5d"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	card := received.Attachments[0].Content
	// Should have header + separator + yesterday + today + blockers = 5 elements
	if len(card.Body) < 4 {
		t.Errorf("expected at least 4 body elements, got %d", len(card.Body))
	}
}

func TestFetchMentions_NoGraphConfig_ReturnsNil(t *testing.T) {
	client := NewClient("https://webhook", "", "", "", "")
	mentions, err := client.FetchMentions(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mentions != nil {
		t.Errorf("expected nil mentions without Graph config, got %v", mentions)
	}
}

func TestExtractSpecID(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"Check SPEC-042 details", "SPEC-042"},
		{"SPEC-1 ready", "SPEC-1"},
		{"No spec", ""},
		{"SPEC- no digits", ""},
	}
	for _, tt := range tests {
		got := extractSpecID(tt.text)
		if got != tt.want {
			t.Errorf("extractSpecID(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<p>Hello <b>world</b></p>", "Hello world"},
		{"no tags", "no tags"},
		{"<div>SPEC-042</div> is ready", "SPEC-042 is ready"},
	}
	for _, tt := range tests {
		got := stripHTML(tt.input)
		if got != tt.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildNotificationCard(t *testing.T) {
	card := buildNotificationCard(adapter.Notification{
		SpecID:  "SPEC-042",
		Title:   "Auth refactor",
		Message: "Advanced to build",
		Mention: "@bob",
	})
	// Header + message + mention = 3 elements
	if len(card.Body) != 3 {
		t.Errorf("expected 3 body elements, got %d", len(card.Body))
	}
	if card.Body[0].Weight != "Bolder" {
		t.Errorf("expected header to be Bolder")
	}
}
