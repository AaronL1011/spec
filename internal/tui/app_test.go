package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
)

func testApp() App {
	rc := &config.ResolvedConfig{
		User: &config.UserConfig{},
		Team: &config.TeamConfig{},
	}
	rc.User.User.Name = "Test"
	rc.User.User.OwnerRole = "engineer"

	reg := adapter.NewRegistry(nil)
	reg.WithComms(noop.Comms{}).
		WithPM(noop.PM{}).
		WithDocs(noop.Docs{}).
		WithRepo(noop.Repo{}).
		WithAgent(noop.Agent{}).
		WithDeploy(noop.Deploy{}).
		WithAI(noop.AI{})

	return New(rc, reg, "engineer")
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
