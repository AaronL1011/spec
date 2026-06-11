package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// testClient builds a client pointed at a test server with a status map that
// covers the common stages used in these tests.
func testClient(baseURL string) *Client {
	return NewClient(Options{
		BaseURL:    baseURL,
		Email:      "user@example.com",
		Token:      "api-token",
		ProjectKey: "PLAT",
		StatusMap: map[string]string{
			"build": "Build",
			"done":  "Done",
		},
	})
}

func TestCreateEpic_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("expected /rest/api/3/issue, got %s", r.URL.Path)
		}
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
		if !containsFold(req.Fields.Labels, "spec-id-SPEC-042") {
			t.Errorf("expected spec-id marker label, got %v", req.Fields.Labels)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{ID: "10042", Key: "PLAT-123"})
	}))
	defer server.Close()

	client := testClient(server.URL)
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

func TestCreateEpic_SetsEpicNameField(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Fields map[string]interface{} `json:"fields"`
		}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		captured = raw.Fields
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{Key: "PLAT-9"})
	}))
	defer server.Close()

	client := NewClient(Options{
		BaseURL: server.URL, Email: "u", Token: "t", ProjectKey: "PLAT",
		Fields: map[string]string{"epic_name": "customfield_10011"},
	})
	if _, err := client.CreateEpic(context.Background(), adapter.SpecMeta{ID: "SPEC-1", Title: "Thing"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["customfield_10011"] != "Thing" {
		t.Errorf("expected Epic Name field set, got %v", captured["customfield_10011"])
	}
}

func TestCreateEpic_APIError_NamesField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"customfield_10011":"Epic Name is required."}}`))
	}))
	defer server.Close()

	client := testClient(server.URL)
	_, err := client.CreateEpic(context.Background(), adapter.SpecMeta{ID: "SPEC-001", Title: "Test"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !contains(err.Error(), "customfield_10011") {
		t.Errorf("expected error to name the field, got: %v", err)
	}
}

func TestFindEpic_ReturnsExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(searchResponse{Issues: []struct {
			Key string `json:"key"`
		}{{Key: "PLAT-55"}}})
	}))
	defer server.Close()

	client := testClient(server.URL)
	key, err := client.FindEpic(context.Background(), "SPEC-042")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "PLAT-55" {
		t.Errorf("expected PLAT-55, got %s", key)
	}
}

func TestFindEpic_NoneFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResponse{})
	}))
	defer server.Close()

	client := testClient(server.URL)
	key, err := client.FindEpic(context.Background(), "SPEC-042")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Errorf("expected empty key, got %s", key)
	}
}

func TestUpdateStatus_MapsAndTransitions(t *testing.T) {
	posted := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/PLAT-123":
			_, _ = w.Write([]byte(`{"key":"PLAT-123","fields":{"status":{"name":"To Do"},"updated":""}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/PLAT-123/transitions":
			_ = json.NewEncoder(w).Encode(transitionsResponse{Transitions: []transition{
				{ID: "11", Name: "Start Build", To: transitionTo{Name: "Build"}},
				{ID: "21", Name: "Done", To: transitionTo{Name: "Done"}},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/PLAT-123/transitions":
			var req transitionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			posted = req.Transition.ID
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	if err := client.UpdateStatus(context.Background(), "PLAT-123", "build"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if posted != "11" {
		t.Errorf("expected transition 11, got %q", posted)
	}
}

func TestUpdateStatus_UnmappedStage_NoOp(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := testClient(server.URL)
	// "triage" is not in the status map — must make no API call.
	if err := client.UpdateStatus(context.Background(), "PLAT-123", "triage"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected no API call for unmapped stage")
	}
}

func TestUpdateStatus_AlreadyInTarget_NoTransition(t *testing.T) {
	transitioned := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/PLAT-123" {
			_, _ = w.Write([]byte(`{"key":"PLAT-123","fields":{"status":{"name":"Build"},"updated":""}}`))
			return
		}
		if r.Method == http.MethodPost {
			transitioned = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(transitionsResponse{})
	}))
	defer server.Close()

	client := testClient(server.URL)
	if err := client.UpdateStatus(context.Background(), "PLAT-123", "build"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transitioned {
		t.Error("expected no transition when already in target status")
	}
}

func TestUpdateStatus_MissingTransition_ReturnsActionableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/PLAT-123" {
			_, _ = w.Write([]byte(`{"key":"PLAT-123","fields":{"status":{"name":"To Do"},"updated":""}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(transitionsResponse{Transitions: []transition{
			{ID: "21", Name: "Done", To: transitionTo{Name: "Done"}},
		}})
	}))
	defer server.Close()

	client := testClient(server.URL)
	err := client.UpdateStatus(context.Background(), "PLAT-123", "build")
	if err == nil {
		t.Fatal("expected missing transition error")
	}
	if !contains(err.Error(), "Build") {
		t.Errorf("expected error to mention target status, got: %v", err)
	}
}

func TestUpdateStatus_EmptyEpicKey_Noop(t *testing.T) {
	client := testClient("http://unused")
	if err := client.UpdateStatus(context.Background(), "", "build"); err != nil {
		t.Fatalf("expected nil error for empty epic key, got %v", err)
	}
}

func TestLinkEpic_PostsRemoteLink(t *testing.T) {
	linked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/PLAT-123/remotelink" && r.Method == http.MethodPost {
			linked = true
			w.WriteHeader(http.StatusCreated)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := testClient(server.URL)
	if err := client.LinkEpic(context.Background(), "PLAT-123", "SPEC-042", "https://example/spec"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !linked {
		t.Error("expected remote link POST")
	}
}

func TestFetchUpdates_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	client := testClient(server.URL)
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
	client := testClient("http://unused")
	update, err := client.FetchUpdates(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if update != nil {
		t.Errorf("expected nil update for empty key")
	}
}

func TestValidate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/project/PLAT" {
			_ = json.NewEncoder(w).Encode(projectResponse{Key: "PLAT", IssueTypes: []struct {
				Name string `json:"name"`
			}{{Name: "Epic"}, {Name: "Story"}}})
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer server.Close()

	client := testClient(server.URL)
	if err := client.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingIssueType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(projectResponse{Key: "PLAT", IssueTypes: []struct {
			Name string `json:"name"`
		}{{Name: "Task"}}})
	}))
	defer server.Close()

	client := testClient(server.URL)
	err := client.Validate(context.Background())
	if err == nil {
		t.Fatal("expected error for missing Epic issue type")
	}
}

func TestSyncStories_CreatesPerStep(t *testing.T) {
	created := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			_ = json.NewEncoder(w).Encode(searchResponse{}) // none exist yet
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			created++
			var raw struct {
				Fields map[string]interface{} `json:"fields"`
			}
			_ = json.NewDecoder(r.Body).Decode(&raw)
			if raw.Fields["parent"] == nil {
				t.Error("expected story to be parented to the epic")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(createIssueResponse{Key: "PLAT-200"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	links, err := client.SyncStories(context.Background(), "PLAT-1", []adapter.StorySpec{
		{StepID: "SPEC-1/0", Repo: "api", Description: "add endpoint", Status: "pending"},
		{StepID: "SPEC-1/1", Repo: "web", Description: "wire UI", Status: "pending"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created != 2 || len(links) != 2 {
		t.Errorf("expected 2 stories created, got created=%d links=%d", created, len(links))
	}
}

func TestSearch_FallsBackToLegacyEndpoint(t *testing.T) {
	var hitJQL, hitLegacy bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			hitJQL = true
			w.WriteHeader(http.StatusNotFound) // Server/DC lacks the enhanced endpoint
		case "/rest/api/3/search":
			hitLegacy = true
			_ = json.NewEncoder(w).Encode(searchResponse{Issues: []struct {
				Key string `json:"key"`
			}{{Key: "PLAT-9"}}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	key, err := client.FindEpic(context.Background(), "SPEC-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hitJQL || !hitLegacy {
		t.Errorf("expected fallback from /search/jql to /search, got jql=%v legacy=%v", hitJQL, hitLegacy)
	}
	if key != "PLAT-9" {
		t.Errorf("expected PLAT-9 via fallback, got %q", key)
	}
}

func TestDoRequest_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"key":"PLAT-1","fields":{"status":{"name":"To Do"},"updated":""}}`))
	}))
	defer server.Close()

	client := testClient(server.URL)
	update, err := client.FetchUpdates(context.Background(), "PLAT-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if update.Status != "To Do" {
		t.Errorf("unexpected status %q", update.Status)
	}
	if calls != 2 {
		t.Errorf("expected one retry after 429, got %d calls", calls)
	}
}

func TestCreateEpic_NotRetriedOn5xx(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := testClient(server.URL)
	// A create must not be auto-retried on 5xx — retrying a possibly-applied
	// create would duplicate the epic.
	_, err := client.CreateEpic(context.Background(), adapter.SpecMeta{ID: "SPEC-1", Title: "x"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 create attempt (no retry), got %d", calls)
	}
}

func TestSyncStories_UsesEpicLinkFieldWhenConfigured(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case "/rest/api/3/issue":
			var raw struct {
				Fields map[string]interface{} `json:"fields"`
			}
			_ = json.NewDecoder(r.Body).Decode(&raw)
			captured = raw.Fields
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(createIssueResponse{Key: "PLAT-200"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Options{
		BaseURL: server.URL, Email: "u", Token: "t", ProjectKey: "PLAT",
		Fields: map[string]string{"epic_link": "customfield_10014"},
	})
	_, err := client.SyncStories(context.Background(), "PLAT-1", []adapter.StorySpec{
		{StepID: "SPEC-1/0", Description: "step", Status: "pending"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["customfield_10014"] != "PLAT-1" {
		t.Errorf("expected Epic Link field set to epic key, got %v", captured["customfield_10014"])
	}
	if _, hasParent := captured["parent"]; hasParent {
		t.Error("expected no parent field when epic_link is configured")
	}
}

// createIssueRequest is a decode-only view of the create payload used by tests
// that assert on epic fields. The production code builds the payload as a map.
type createIssueRequest struct {
	Fields struct {
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		Summary   string `json:"summary"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Labels []string `json:"labels"`
	} `json:"fields"`
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
