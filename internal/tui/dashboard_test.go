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

func TestDashboard_PriorityOrdering_BlockedFirst(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 100
	m.height = 30
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Active work", Stage: "build"},
		},
		Review: []dashboard.DashboardItem{
			{SpecID: "PR #42", Title: "Review this"},
		},
		Incoming: []dashboard.DashboardItem{
			{SpecID: "SPEC-010", Title: "New intake", Stage: "triage"},
		},
		Blocked: []dashboard.DashboardItem{
			{SpecID: "SPEC-005", Title: "Stuck"},
		},
	}
	m.items = m.buildRows()

	if len(m.items) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(m.items))
	}

	// Blocked should appear first, then DO, then REVIEW, then INCOMING.
	wantOrder := []string{"BLOCKED", "DO", "REVIEW", "INCOMING"}
	for i, want := range wantOrder {
		if m.items[i].section != want {
			t.Errorf("item[%d].section = %q, want %q", i, m.items[i].section, want)
		}
	}
}

func TestDashboard_UrgencySortWithinSection(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Normal", Urgency: "normal"},
			{SpecID: "SPEC-002", Title: "Stale", Urgency: "stale"},
			{SpecID: "SPEC-003", Title: "Critical", Urgency: "critical"},
		},
	}
	m.items = m.buildRows()

	if len(m.items) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(m.items))
	}
	if m.items[0].urgency != "critical" {
		t.Errorf("item[0].urgency = %q, want critical", m.items[0].urgency)
	}
	if m.items[1].urgency != "stale" {
		t.Errorf("item[1].urgency = %q, want stale", m.items[1].urgency)
	}
	if m.items[2].urgency != "normal" {
		t.Errorf("item[2].urgency = %q, want normal", m.items[2].urgency)
	}
}

func TestDashboard_CompactRenderNarrowWidth(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 45 // narrow
	m.height = 20
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "A really long spec title that should truncate", Stage: "build"},
		},
	}
	m.items = m.buildRows()

	got := m.view()
	// Should render without panic and contain the spec ID.
	if !strings.Contains(got, "SPEC-001") {
		t.Errorf("narrow view should contain SPEC-001, got: %q", got)
	}
}

func TestDashboard_SectionCount(t *testing.T) {
	m := testDashboard()
	m.items = []dashboardRow{
		{section: "DO"},
		{section: "DO"},
		{section: "REVIEW"},
	}
	if got := m.sectionCount("DO"); got != 2 {
		t.Errorf("sectionCount(DO) = %d, want 2", got)
	}
	if got := m.sectionCount("REVIEW"); got != 1 {
		t.Errorf("sectionCount(REVIEW) = %d, want 1", got)
	}
	if got := m.sectionCount("BLOCKED"); got != 0 {
		t.Errorf("sectionCount(BLOCKED) = %d, want 0", got)
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
