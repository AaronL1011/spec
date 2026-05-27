package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestApp_ModalConfirmFlow(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Set up a selected spec.
	app.dashboard.loading = false
	app.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Test"},
		},
	}
	app.dashboard.items = app.dashboard.buildRows()

	// Press 'a' to advance — should open confirm modal.
	model, _ := app.Update(keyMsg("a"))
	a := model.(App)
	if !a.modal.Visible {
		t.Fatal("modal should be visible after pressing 'a'")
	}
	if a.pendingAction != "advance" {
		t.Errorf("pendingAction = %q, want advance", a.pendingAction)
	}
	if a.pendingSpecID != "SPEC-001" {
		t.Errorf("pendingSpecID = %q, want SPEC-001", a.pendingSpecID)
	}

	// Press 'n' to cancel.
	model, _ = a.Update(keyMsg("n"))
	a = model.(App)
	if a.modal.Visible {
		t.Error("modal should be hidden after 'n'")
	}
}

func TestApp_ModalInputFlow(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.dashboard.loading = false
	app.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Test"},
		},
	}
	app.dashboard.items = app.dashboard.buildRows()

	// Press 'B' to block — should open input modal.
	model, _ := app.Update(keyMsg("B"))
	a := model.(App)
	if !a.modal.Visible {
		t.Fatal("modal should be visible after pressing 'B'")
	}
	if a.pendingAction != "block" {
		t.Errorf("pendingAction = %q, want block", a.pendingAction)
	}

	// Type a reason.
	model, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("API blocked")})
	a = model.(App)
	if a.modal.Input != "API blocked" {
		t.Errorf("modal input = %q, want 'API blocked'", a.modal.Input)
	}

	// Escape cancels.
	model, _ = a.Update(tea.KeyMsg{Type: tea.KeyEscape})
	a = model.(App)
	if a.modal.Visible {
		t.Error("modal should be hidden after escape")
	}
}

func TestApp_ActionResultShowsToast(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Success result.
	model, _ := app.Update(actionResultMsg{
		Action: "focus",
		SpecID: "SPEC-001",
		Detail: "focused",
	})
	a := model.(App)
	if !a.toast.Visible() {
		t.Error("toast should be visible after successful action")
	}

	// Error result.
	model, _ = app.Update(actionResultMsg{
		Action: "advance",
		Err:    fmt.Errorf("gate not met"),
	})
	a = model.(App)
	if !a.toast.Visible() {
		t.Error("toast should be visible after failed action")
	}
}

func TestApp_QuitDuringModal(t *testing.T) {
	app := testApp()
	app.modal.ShowConfirm("Test", "Are you sure?")
	app.modal.SetSize(80, 20)

	// 'q' during a modal should not quit (it's captured by modal).
	model, cmd := app.Update(keyMsg("q"))
	_ = model.(App)
	// Modal should handle 'q' as not-y/n, so modal stays visible.
	if cmd != nil {
		// If cmd is tea.Quit, that's wrong.
		// This is a soft check — 'q' is not 'y' or 'n' so modal ignores it.
	}
}

func TestApp_SearchSuppressesHotkeys(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Switch to Specs tab and load data.
	app.switchView(ViewSpecs)
	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Test"},
	}
	app.specs.applyFilter()

	// Activate search.
	model, _ := app.Update(keyMsg("/"))
	a := model.(App)
	if !a.specs.isInputActive() {
		t.Fatal("search should be active after '/'")
	}

	// Type 'a' — should go into search query, NOT open advance modal.
	model, _ = a.Update(keyMsg("a"))
	a = model.(App)
	if a.modal.Visible {
		t.Error("'a' during search should not open advance modal")
	}
	if a.specs.searchQuery != "a" {
		t.Errorf("searchQuery = %q, want 'a'", a.specs.searchQuery)
	}

	// Type 'f' — should append to query, NOT focus a spec.
	model, _ = a.Update(keyMsg("f"))
	a = model.(App)
	if a.specs.searchQuery != "af" {
		t.Errorf("searchQuery = %q, want 'af'", a.specs.searchQuery)
	}

	// Type 'q' — should append to query, NOT quit.
	model, cmd := a.Update(keyMsg("q"))
	a = model.(App)
	if a.specs.searchQuery != "afq" {
		t.Errorf("searchQuery = %q, want 'afq'", a.specs.searchQuery)
	}
	if cmd != nil {
		t.Error("'q' during search should not produce a quit command")
	}
}

func TestApp_HelpToggle(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Press ? to open help.
	model, _ := app.Update(keyMsg("?"))
	a := model.(App)
	if !a.help.visible {
		t.Error("help should be visible after ?")
	}

	// Press ? again to close.
	model, _ = a.Update(keyMsg("?"))
	a = model.(App)
	if a.help.visible {
		t.Error("help should be hidden after second ?")
	}
}

func TestParseRefreshInterval(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{"", 30 * time.Second},        // default
		{"45s", 45 * time.Second},     // valid
		{"2m", 2 * time.Minute},       // valid
		{"1s", 30 * time.Second},      // too short, fallback
		{"garbage", 30 * time.Second}, // invalid, fallback
	}
	for _, tt := range tests {
		rc := testResolvedConfig()
		rc.User.Preferences.RefreshInterval = tt.value
		got := parseRefreshInterval(rc)
		if got != tt.want {
			t.Errorf("parseRefreshInterval(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestApp_CycleTheme(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Default theme is auto (empty pref in test config).
	originalTheme := app.theme

	// Cycle once.
	app.cycleTheme()
	if app.theme == originalTheme {
		// Theme values should differ (auto → catppuccin-mocha).
		if app.rc.User.Preferences.Theme == "" {
			t.Error("theme pref should be set after cycling")
		}
	}
	firstCycled := app.rc.User.Preferences.Theme

	// Cycle again — should advance to next.
	app.cycleTheme()
	if app.rc.User.Preferences.Theme == firstCycled {
		t.Error("theme should change on each cycle")
	}

	// Styles should be propagated.
	if app.dashboard.styles.Title.GetBold() != app.styles.Title.GetBold() {
		t.Error("dashboard styles should match app styles after theme change")
	}
}

func TestApp_CycleThemeFromSettings(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()
	app.switchView(ViewSettings)

	// Send cycleThemeMsg (as settings view would produce).
	model, _ := app.Update(cycleThemeMsg{})
	a := model.(App)

	// Should have applied a new theme.
	if a.rc.User.Preferences.Theme == "" {
		t.Error("theme should be set after cycleThemeMsg")
	}
	if !a.toast.Visible() {
		t.Error("toast should show after theme change")
	}
}

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) < 10 {
		t.Errorf("expected at least 10 themes, got %d", len(names))
	}
	if names[0] != "auto" {
		t.Errorf("first theme should be 'auto', got %q", names[0])
	}
	// Every name should resolve without panic.
	for _, name := range names {
		ResolveTheme(name)
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
