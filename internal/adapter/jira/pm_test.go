package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aaronl1011/spec-cli/internal/adapter"
)

func TestCreateEpic_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("expected /rest/api/3/issue, got %s", r.URL.Path)
		}
		// Verify basic auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user@example.com" || pass != "api-token" {
			t.Errorf("unexpected auth: %q %q %v", user, pass, ok)
		}

		var req createIssueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Fields.Project.Key != "PLAT" {
			t.Errorf("expected project PLAT, got %s", req.Fields.Project.Key)
		}
		if req.Fields.IssueType.Name != "Epic" {
			t.Errorf("expected issue type Epic, got %s", req.Fields.IssueType.Name)
		}
		if req.Fields.Summary != "[SPEC-042] Auth refactor" {
			t.Errorf("unexpected summary: %s", req.Fields.Summary)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{
			ID:  "10042",
			Key: "PLAT-123",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "PLAT", "user@example.com", "api-token")
	key, err := client.CreateEpic(context.Background(), adapter.SpecMeta{
		ID:     "SPEC-042",
		Title:  "Auth refactor",
		Status: "draft",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "PLAT-123" {
		t.Errorf("expected key PLAT-123, got %s", key)
	}
}

func TestCreateEpic_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["permission denied"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "PLAT", "user@example.com", "bad-token")
	_, err := client.CreateEpic(context.Background(), adapter.SpecMeta{ID: "SPEC-001", Title: "Test"})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestUpdateStatus_FindsTransition(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/PLAT-123/transitions":
			_ = json.NewEncoder(w).Encode(transitionsResponse{
				Transitions: []transition{
					{ID: "11", Name: "Start Build", To: transitionTo{Name: "Build"}},
					{ID: "21", Name: "Done", To: transitionTo{Name: "Done"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/PLAT-123/transitions":
			var req transitionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Transition.ID != "11" {
				t.Errorf("expected transition ID '11', got %s", req.Transition.ID)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "PLAT", "user@example.com", "token")
	err := client.UpdateStatus(context.Background(), "PLAT-123", "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls (get transitions + post), got %d", calls)
	}
}

func TestUpdateStatus_EmptyEpicKey_Noop(t *testing.T) {
	client := NewClient("http://unused", "PLAT", "u", "t")
	err := client.UpdateStatus(context.Background(), "", "build")
	if err != nil {
		t.Fatalf("expected nil error for empty epic key, got %v", err)
	}
}

func TestFetchUpdates_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PLAT-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"key": "PLAT-123",
			"fields": {
				"status": {"name": "In Progress"},
				"assignee": {"displayName": "Alice"},
				"updated": "2026-04-21T10:30:00.000+0000"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "PLAT", "u", "t")
	update, err := client.FetchUpdates(context.Background(), "PLAT-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if update.Status != "In Progress" {
		t.Errorf("expected status 'In Progress', got %q", update.Status)
	}
	if update.Assignee != "Alice" {
		t.Errorf("expected assignee 'Alice', got %q", update.Assignee)
	}
}

func TestFetchUpdates_EmptyKey_ReturnsNil(t *testing.T) {
	client := NewClient("http://unused", "PLAT", "u", "t")
	update, err := client.FetchUpdates(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if update != nil {
		t.Errorf("expected nil update for empty key")
	}
}
