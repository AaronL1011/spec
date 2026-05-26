package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
)

func testDashboard() dashboardModel {
	rc := &config.ResolvedConfig{
		User: &config.UserConfig{},
		Team: &config.TeamConfig{},
	}
	rc.User.User.Name = "Test"
	rc.User.User.OwnerRole = "engineer"

	reg := adapter.NewRegistry(nil)
	reg.WithRepo(noop.Repo{})

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	return newDashboard(rc, reg, "engineer", styles, keys)
}

func TestDashboard_LoadingView(t *testing.T) {
	m := testDashboard()
	got := m.view()
	if !strings.Contains(got, "Loading") {
		t.Errorf("loading view should contain 'Loading', got: %q", got)
	}
}

func TestDashboard_EmptyData(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.data = &dashboard.DashboardData{}
	m.items = m.buildRows()

	got := m.view()
	if !strings.Contains(got, "All clear") {
		t.Errorf("empty dashboard should show 'All clear', got: %q", got)
	}
}

func TestDashboard_WithItems(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 80
	m.height = 24
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Auth service", Stage: "build", Urgency: "normal"},
			{SpecID: "SPEC-002", Title: "User onboarding", Stage: "build", Urgency: "stale"},
		},
		Blocked: []dashboard.DashboardItem{
			{SpecID: "SPEC-003", Title: "Payments v2", Detail: "waiting on API design"},
		},
	}
	m.items = m.buildRows()

	got := m.view()
	if !strings.Contains(got, "DO") {
		t.Error("view should contain DO section")
	}
	if !strings.Contains(got, "BLOCKED") {
		t.Error("view should contain BLOCKED section")
	}
	if !strings.Contains(got, "SPEC-001") {
		t.Error("view should contain SPEC-001")
	}
}

func TestDashboard_CursorBounds(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "First"},
			{SpecID: "SPEC-002", Title: "Second"},
		},
	}
	m.items = m.buildRows()

	// Move up when at top — should stay at 0
	m.cursor = 0
	m, _ = m.update(keyMsg("k"))
	if m.cursor != 0 {
		t.Errorf("cursor should stay at 0, got %d", m.cursor)
	}

	// Move down
	m, _ = m.update(keyMsg("j"))
	if m.cursor != 1 {
		t.Errorf("cursor should be 1, got %d", m.cursor)
	}

	// Move down past end — should stay
	m, _ = m.update(keyMsg("j"))
	if m.cursor != 1 {
		t.Errorf("cursor should stay at 1, got %d", m.cursor)
	}
}

func TestDashboard_PendingCount(t *testing.T) {
	m := testDashboard()
	m.data = &dashboard.DashboardData{
		Do:       []dashboard.DashboardItem{{}, {}},
		Review:   []dashboard.DashboardItem{{}},
		Incoming: []dashboard.DashboardItem{{}, {}, {}},
		Blocked:  []dashboard.DashboardItem{{}},
	}
	if got := m.pendingCount(); got != 7 {
		t.Errorf("pendingCount = %d, want 7", got)
	}
}

func TestDashboard_SelectedSpecID(t *testing.T) {
	m := testDashboard()
	m.items = []dashboardRow{
		{specID: "SPEC-001"},
		{specID: "SPEC-002"},
	}
	m.cursor = 1
	if got := m.selectedSpecID(); got != "SPEC-002" {
		t.Errorf("selectedSpecID = %q, want SPEC-002", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 1, "a"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
