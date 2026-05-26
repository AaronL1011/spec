package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/dashboard"
)

func testApp() App {
	return New(testResolvedConfig(), testRegistry(), "engineer")
}

func TestApp_InitReturnsCmd(t *testing.T) {
	app := testApp()
	cmd := app.Init()
	if cmd == nil {
		t.Error("Init() should return a command (batch of fetch + tick)")
	}
}

func TestApp_ViewBeforeWindowSize(t *testing.T) {
	app := testApp()
	got := app.View()
	if !strings.Contains(got, "Initialising") {
		t.Errorf("View() before WindowSizeMsg should show initialising, got: %q", got)
	}
}

func TestApp_ViewAfterWindowSize(t *testing.T) {
	app := testApp()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := model.(App).View()
	if strings.Contains(got, "Initialising") {
		t.Error("View() after WindowSizeMsg should not show initialising")
	}
}

func TestApp_TabSwitching(t *testing.T) {
	app := testApp()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	tests := []struct {
		key  string
		want View
	}{
		{"2", ViewPipeline},
		{"3", ViewSpecs},
		{"4", ViewTriage},
		{"5", ViewReviews},
		{"6", ViewSettings},
		{"1", ViewDashboard},
	}

	var model tea.Model = app
	for _, tt := range tests {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		a := model.(App)
		if a.activeView != tt.want {
			t.Errorf("after key %q: activeView = %d, want %d", tt.key, a.activeView, tt.want)
		}
	}
}

func TestApp_QuitOnQ(t *testing.T) {
	app := testApp()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("pressing 'q' should return a quit command")
	}
}

func TestApp_DashboardDataMsg(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	data := &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Test spec", Stage: "build"},
		},
	}
	model, _ := app.Update(dashboardDataMsg{Data: data})
	a := model.(App)

	if a.dashboard.pendingCount() != 1 {
		t.Errorf("pendingCount = %d, want 1", a.dashboard.pendingCount())
	}
}

func TestApp_DrillDownAndBack(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Open detail
	cmd := app.openDetail("SPEC-042")
	if cmd == nil {
		t.Error("openDetail should return a fetch command")
	}
	if !app.showDetail {
		t.Error("showDetail should be true")
	}
	if app.detail.specID != "SPEC-042" {
		t.Errorf("detail.specID = %q, want SPEC-042", app.detail.specID)
	}

	// Close detail
	app.closeDetail()
	if app.showDetail {
		t.Error("showDetail should be false after closeDetail")
	}
}

func TestApp_ViewSwitchClosesDetail(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.openDetail("SPEC-001")
	if !app.showDetail {
		t.Fatal("detail should be open")
	}

	app.switchView(ViewPipeline)
	if app.showDetail {
		t.Error("switching view should close detail")
	}
	if app.activeView != ViewPipeline {
		t.Errorf("activeView = %d, want ViewPipeline", app.activeView)
	}
}

func TestApp_SelectedSpecID(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Dashboard with data
	app.dashboard.loading = false
	app.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-007", Title: "Bond"},
		},
	}
	app.dashboard.items = app.dashboard.buildRows()

	if got := app.selectedSpecID(); got != "SPEC-007" {
		t.Errorf("selectedSpecID = %q, want SPEC-007", got)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"one", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
		{"one\ntwo\nthree", 3},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(got), tt.want)
		}
	}
}
