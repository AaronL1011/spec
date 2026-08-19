package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// createTaskRequest is a decode-only view of the create payload, extended with
// the parent field a task carries.
type createTaskRequest struct {
	Fields struct {
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		Summary   string `json:"summary"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Labels []string `json:"labels"`
		Parent struct {
			Key string `json:"key"`
		} `json:"parent"`
	} `json:"fields"`
}

func TestCreateTask_PlacesTaskUnderParentEpic(t *testing.T) {
	var got createTaskRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{ID: "10099", Key: "PLAT-99"})
	}))
	defer server.Close()

	key, err := testClient(server.URL).CreateTask(context.Background(), adapter.SpecMeta{
		ID:     "SPEC-009",
		Title:  "Token bucket limiter",
		Status: "draft",
	}, "PLAT-04")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if key != "PLAT-99" {
		t.Errorf("key = %q, want PLAT-99", key)
	}
	if got.Fields.IssueType.Name != "Task" {
		t.Errorf("issue type = %q, want Task", got.Fields.IssueType.Name)
	}
	if got.Fields.Parent.Key != "PLAT-04" {
		t.Errorf("parent = %q, want the initiative's epic PLAT-04", got.Fields.Parent.Key)
	}
	if got.Fields.Summary != "[SPEC-009] Token bucket limiter" {
		t.Errorf("unexpected summary %q", got.Fields.Summary)
	}
	// The spec-id marker keeps FindEpic the single idempotency guard, whatever
	// issue type the spec ended up with.
	if !containsFold(got.Fields.Labels, "spec-id-SPEC-009") {
		t.Errorf("task is missing the spec-id marker label: %v", got.Fields.Labels)
	}
}

// A company-managed project links children through the Epic Link custom field
// rather than `parent`. Tasks and stories must agree on which one is used.
func TestCreateTask_UsesEpicLinkFieldWhenConfigured(t *testing.T) {
	var raw map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fields map[string]interface{} `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		raw = body.Fields
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{ID: "1", Key: "PLAT-99"})
	}))
	defer server.Close()

	client := NewClient(Options{
		BaseURL:    server.URL,
		Email:      "user@example.com",
		Token:      "api-token",
		ProjectKey: "PLAT",
		Fields:     map[string]string{"epic_link": "customfield_10014"},
	})
	if _, err := client.CreateTask(context.Background(),
		adapter.SpecMeta{ID: "SPEC-009", Title: "Token bucket"}, "PLAT-04"); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if raw["customfield_10014"] != "PLAT-04" {
		t.Errorf("epic link field = %v, want PLAT-04", raw["customfield_10014"])
	}
	if _, ok := raw["parent"]; ok {
		t.Error("parent must not be set when the Epic Link field is configured")
	}
}

// A slice whose initiative has not synced cannot be placed. Creating a loose
// task would be worse than waiting: it would land outside the epic with no way
// to attach it later without a lossy Move.
func TestCreateTask_NoParentKeyCreatesNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("CreateTask must not issue a request without a parent key")
	}))
	defer server.Close()

	key, err := testClient(server.URL).CreateTask(context.Background(),
		adapter.SpecMeta{ID: "SPEC-009", Title: "Token bucket"}, "")
	if err != nil {
		t.Errorf("an unsynced initiative is a queued retry, not an error: %v", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func TestCreateTask_RespectsConfiguredTaskIssueType(t *testing.T) {
	var got createTaskRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{ID: "1", Key: "PLAT-99"})
	}))
	defer server.Close()

	client := NewClient(Options{
		BaseURL: server.URL, Email: "u", Token: "t",
		ProjectKey: "PLAT", TaskIssueType: "Sub-task",
	})
	if _, err := client.CreateTask(context.Background(),
		adapter.SpecMeta{ID: "SPEC-009", Title: "Token bucket"}, "PLAT-04"); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got.Fields.IssueType.Name != "Sub-task" {
		t.Errorf("issue type = %q, want the configured Sub-task", got.Fields.IssueType.Name)
	}
}

// Decision 8: a spec that already has a PM object is linked, never converted.
// The adapter offers no Move, so no caller can accidentally issue one — this
// asserts that absence, which is what makes a replayed pm_queue entry safe.
func TestClient_OffersNoIssueTypeConversion(t *testing.T) {
	var pm adapter.PMAdapter = NewClient(Options{BaseURL: "http://example.invalid"})
	if _, ok := pm.(interface {
		MoveIssueType(ctx context.Context, key, issueType string) error
	}); ok {
		t.Error("the Jira adapter must not expose an issue-type Move: it is lossy and pm_queue replays operations")
	}
}

func TestValidate_DoesNotRequireTaskIssueType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/project/PLAT") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issueTypes": []map[string]string{{"name": "Epic"}, {"name": "Story"}},
		})
	}))
	defer server.Close()

	// A team that has never linked two specs must not fail `spec config check`
	// over an issue type they may never create.
	if err := testClient(server.URL).Validate(context.Background()); err != nil {
		t.Errorf("Validate should not require the task issue type: %v", err)
	}
}
