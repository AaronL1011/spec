package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/store"
)

func TestBuildStatusLine(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// No session yet → empty line.
	if got := buildStatusLine(db, "SPEC-001", ""); got != "" {
		t.Errorf("expected empty line with no session, got %q", got)
	}

	steps := []build.PRStep{
		{Number: 1, Repo: "api-gateway", Description: "rate-limit middleware", Status: "pending"},
		{Number: 2, Repo: "frontend", Description: "error handling", Status: "pending"},
	}
	if _, err := build.CreateSession(db, "SPEC-001", steps, "/tmp"); err != nil {
		t.Fatal(err)
	}

	content := "# SPEC-001\n\n## 6. Acceptance Criteria\n\n- [x] one\n- [x] two\n- [ ] three\n"
	got := buildStatusLine(db, "SPEC-001", content)
	if !strings.Contains(got, "step 1/2") {
		t.Errorf("line %q missing step 1/2", got)
	}
	if !strings.Contains(got, "[api-gateway] rate-limit middleware") {
		t.Errorf("line %q missing step description", got)
	}
	if !strings.Contains(got, "ACs 2/3") {
		t.Errorf("line %q missing AC count", got)
	}
}

func TestBuildPreflightBlocksModal(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.dashboard.loading = false
	app.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{{SpecID: "SPEC-404", Title: "Missing"}},
	}
	app.dashboard.items = app.dashboard.buildRows()

	// 'b' on a spec that does not resolve must surface an inline error and
	// never open the confirm modal or set a pending build action.
	model, _ := app.Update(keyMsg("b"))
	a := model.(App)
	if a.modal.Visible {
		t.Error("modal should not be visible when preflight fails")
	}
	if a.pendingAction == "build" {
		t.Error("pendingAction should not be build when preflight fails")
	}
}

func TestAgentNameDefault(t *testing.T) {
	app := testApp()
	if name := app.agentName(); name == "" {
		t.Error("agentName should never be empty")
	}
}

func TestCmdResult(t *testing.T) {
	cmd := cmdResult("test", "SPEC-001", nil)
	if cmd == nil {
		t.Error("cmdResult should return a non-nil command")
	}
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.Action != "test" {
		t.Errorf("action = %q, want 'test'", result.Action)
	}
	if result.SpecID != "SPEC-001" {
		t.Errorf("specID = %q, want SPEC-001", result.SpecID)
	}
}
