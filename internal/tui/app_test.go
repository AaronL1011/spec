package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/tui/components"
)

func testApp() App {
	db, err := store.OpenMemory()
	if err != nil {
		panic(err)
	}
	return newAppWithDB(testResolvedConfig(), testRegistry(), "engineer", db)
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
	got := app.View().Content
	if !strings.Contains(got, "Initialising") {
		t.Errorf("View() before WindowSizeMsg should show initialising, got: %q", got)
	}
}

func TestApp_ViewAfterWindowSize(t *testing.T) {
	app := testApp()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := model.(App).View().Content
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
		model, _ = model.Update(tea.KeyPressMsg{Text: tt.key})
		a := model.(App)
		if a.activeView != tt.want {
			t.Errorf("after key %q: activeView = %d, want %d", tt.key, a.activeView, tt.want)
		}
	}
}

// TestApp_QuitOnQ verifies that 'q' no longer quits the app (SPEC-010).
// Quit is now double-esc at the top level or ctrl+c.
func TestApp_QuitOnQ(t *testing.T) {
	app := testApp()
	_, cmd := app.Update(tea.KeyPressMsg{Text: "q"})
	// 'q' must NOT quit any more.
	if cmd != nil {
		t.Error("pressing 'q' should NOT return a quit command (retired by SPEC-010)")
	}
}

// TestApp_CtrlCQuits verifies that ctrl+c is the hard-quit key.
func TestApp_CtrlCQuits(t *testing.T) {
	app := testApp()
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("ctrl+c should return a quit command")
	}
}

// TestApp_DoubleEscQuits verifies the double-esc-to-quit escalation (SPEC-010).
func TestApp_DoubleEscQuits(t *testing.T) {
	app := testApp()

	// First esc at top level: arms exit, must NOT quit.
	model, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Error("first esc at top level should not quit")
	}
	a := model.(App)
	if !a.exitArmed {
		t.Error("exitArmed should be true after first esc")
	}

	// Second esc within the window: must quit.
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Error("second esc within arm window should quit")
	}
}

// TestApp_SingleEscAtTopDoesNotQuit verifies that a single esc at the top
// level only arms exit but does not exit (AC-3).
func TestApp_SingleEscAtTopDoesNotQuit(t *testing.T) {
	app := testApp()
	model, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Error("single esc should not quit; it only arms exit")
	}
	a := model.(App)
	if !a.exitArmed {
		t.Error("exitArmed should be true after first esc at top level")
	}
}

// TestApp_NonEscDisarmsExit verifies that pressing any non-esc key after
// arming exit disarms it without quitting.
func TestApp_NonEscDisarmsExit(t *testing.T) {
	app := testApp()
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // arm
	a := model.(App)
	if !a.exitArmed {
		t.Fatal("exitArmed should be true after first esc")
	}

	// Press '?' (help) — should disarm exit arm but not quit.
	model, _ = a.Update(keyMsg("?"))
	a = model.(App)
	if a.exitArmed {
		t.Error("exitArmed should be cleared after pressing a non-esc key")
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

// TestApp_TriageDetailEscClosesPane is a regression test for the bug where the
// global esc escalation swallowed esc before it could close the triage detail
// pane, trapping the user in the detail view.
func TestApp_TriageDetailEscClosesPane(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.activeView = ViewTriage
	app.triage.loading = false
	app.triage.items = []triageItem{{ID: "TRIAGE-001", Title: "Test"}}
	app.triage.cursor = 0

	app.openTriageDetailForSelected()
	if !app.showTriageDetail {
		t.Fatal("triage detail should be open")
	}

	// esc must close the pane through the real key path, not arm exit.
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a := model.(App)
	if a.showTriageDetail {
		t.Error("esc should close the triage detail pane")
	}
	if a.exitArmed {
		t.Error("esc inside triage detail must not arm exit")
	}
}

// TestApp_TriageDetailViewSwitchClosesPane verifies switching views closes the
// triage detail pane so it cannot linger as stale state over another view.
func TestApp_TriageDetailViewSwitchClosesPane(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.activeView = ViewTriage
	app.triage.loading = false
	app.triage.items = []triageItem{{ID: "TRIAGE-001", Title: "Test"}}
	app.triage.cursor = 0

	app.openTriageDetailForSelected()
	if !app.showTriageDetail {
		t.Fatal("triage detail should be open")
	}

	app.switchView(ViewSpecs)
	if app.showTriageDetail {
		t.Error("switching view should close the triage detail pane")
	}
}

// TestApp_TriageDetailScrollsWithoutClosing verifies navigation keys scroll the
// pane and are absorbed rather than leaking to the list or closing the pane.
func TestApp_TriageDetailScrollsWithoutClosing(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.activeView = ViewTriage
	app.triage.loading = false
	app.triage.items = []triageItem{{ID: "TRIAGE-001", Title: "Test", Body: strings.Repeat("line\n", 50)}}
	app.triage.cursor = 0
	app.openTriageDetailForSelected()

	model, _ := app.Update(keyMsg("j"))
	a := model.(App)
	if !a.showTriageDetail {
		t.Error("scrolling must not close the triage detail pane")
	}
}

// TestApp_DashboardDoesNotRefreshOnSwitchWhenLoaded verifies the dashboard
// fetches only on first load; switching back to an already-loaded dashboard
// schedules no refresh (updates come from the auto-timer or manual refresh).
func TestApp_DashboardDoesNotRefreshOnSwitchWhenLoaded(t *testing.T) {
	app := testApp()

	// Unloaded dashboard: a switch should schedule the initial fetch.
	app.activeView = ViewPipeline
	if cmd := app.switchView(ViewDashboard); cmd == nil {
		t.Error("unloaded dashboard should fetch on first open")
	}

	// Mark loaded and clear in-flight, then switch away and back.
	app.dashboard.loaded = true
	app.markRefreshDone(refreshKeyDashboard)
	app.switchView(ViewPipeline)
	if cmd := app.switchView(ViewDashboard); cmd != nil {
		t.Error("loaded dashboard should not refresh on switch — rely on timer/manual")
	}

	// The auto-timer / manual refresh path must STILL refresh the loaded
	// dashboard — only the tab-switch is suppressed.
	app.markRefreshDone(refreshKeyDashboard)
	if cmd := app.refreshActiveView(); cmd == nil {
		t.Error("timer/manual refresh should still refresh a loaded dashboard")
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

	// Press 'x' to toggle block — should open input modal.
	model, _ := app.Update(keyMsg("x"))
	a := model.(App)
	if !a.modal.Visible {
		t.Fatal("modal should be visible after pressing 'x'")
	}
	if a.pendingAction != "block" {
		t.Errorf("pendingAction = %q, want block", a.pendingAction)
	}

	// Type a reason.
	model, _ = a.Update(tea.KeyPressMsg{Text: "API blocked"})
	a = model.(App)
	if a.modal.Value() != "API blocked" {
		t.Errorf("modal input = %q, want 'API blocked'", a.modal.Value())
	}

	// Escape cancels.
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(App)
	if a.modal.Visible {
		t.Error("modal should be hidden after escape")
	}
}

func TestApp_ActionResultShowsStatus(t *testing.T) {
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
	if a.statusBar.StatusKind() != components.StatusSuccess {
		t.Errorf("status should be success after successful action, got %v", a.statusBar.StatusKind())
	}

	// Error result.
	model, _ = app.Update(actionResultMsg{
		Action: "advance",
		SpecID: "SPEC-001",
		Err:    fmt.Errorf("gate not met: QA validation incomplete"),
	})
	a = model.(App)
	if a.statusBar.StatusKind() != components.StatusError {
		t.Errorf("status should be error after failed action, got %v", a.statusBar.StatusKind())
	}
	// The slot shows a short summary; the full error is kept for expansion.
	if !strings.Contains(a.statusBar.StatusLabel(), "failed") {
		t.Errorf("error summary should be short, got %q", a.statusBar.StatusLabel())
	}
	if !strings.Contains(a.statusBar.ErrorDetail(), "gate not met") {
		t.Errorf("full error detail should be preserved, got %q", a.statusBar.ErrorDetail())
	}
}

// TestApp_ExpandErrorOpensModal verifies the E key opens the full error text in
// a read-only modal, and is a no-op when there is no error.
func TestApp_ExpandErrorOpensModal(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// No error yet: E must not open a modal.
	model, _ := app.Update(keyMsg("E"))
	a := model.(App)
	if a.modal.Visible {
		t.Fatal("E should be a no-op when there is no error")
	}

	// Produce a sticky error, then expand it.
	model, _ = a.Update(actionResultMsg{Action: "advance", SpecID: "SPEC-001",
		Err: fmt.Errorf("gate not met: QA validation incomplete for SPEC-001")})
	a = model.(App)
	model, _ = a.Update(keyMsg("E"))
	a = model.(App)
	if !a.modal.Visible || a.modal.Kind != components.ModalInfo {
		t.Fatalf("E should open a read-only info modal, visible=%v kind=%v", a.modal.Visible, a.modal.Kind)
	}
	if !strings.Contains(a.modal.Message, "gate not met") {
		t.Errorf("modal should show the full error, got %q", a.modal.Message)
	}
}

// TestApp_BackgroundRefreshKeepsStickyError verifies that a background refresh
// starting does NOT clear an unseen sticky error (only user-initiated work
// supersedes it).
func TestApp_BackgroundRefreshKeepsStickyError(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	model, _ := app.Update(actionResultMsg{Action: "push", SpecID: "SPEC-001",
		Err: fmt.Errorf("network unreachable")})
	a := model.(App)
	if !a.statusBar.HasError() {
		t.Fatal("precondition: sticky error should be showing")
	}

	// Kick off a background refresh and reconcile busy state.
	a.scheduleRefresh(refreshKeyDashboard, func() tea.Msg { return dashboardDataMsg{} })
	a.syncBusyState()

	if !a.statusBar.HasError() {
		t.Error("background refresh must not clear an unseen sticky error")
	}
}

func TestApp_SpinnerDuringAdvance(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.dashboard.loading = false
	app.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Test", Stage: "build"},
		},
	}
	app.dashboard.items = app.dashboard.buildRows()

	// Open advance modal and confirm.
	model, _ := app.Update(keyMsg("a"))
	model, _ = model.Update(keyMsg("y"))
	a := model.(App)

	// Spinner should be active after confirming advance.
	if !a.actionInFlight {
		t.Error("actionInFlight should be true after confirming advance")
	}
	if !a.spinnerOn {
		t.Error("spinnerOn should be true during advance")
	}
	if a.actionLabel != "advancing SPEC-001" {
		t.Errorf("actionLabel = %q, want 'advancing SPEC-001'", a.actionLabel)
	}

	// Verify spinner appears in the rendered view.
	view := a.View().Content
	if !strings.Contains(view, "advancing") {
		t.Error("view should show advancing label in status bar")
	}

	// After result arrives, the action is no longer in-flight.
	// A follow-up refresh is kicked off, so spinnerOn may still be true
	// (showing "refreshing…") — that is correct and expected behavior.
	model, _ = a.Update(actionResultMsg{Action: "advance", SpecID: "SPEC-001", Detail: "advanced"})
	a = model.(App)
	if a.actionInFlight {
		t.Error("actionInFlight should be false after result")
	}
}

func TestApp_SpinnerDuringBlock(t *testing.T) {
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

	// Open block modal with 'x', type reason, submit.
	model, _ := app.Update(keyMsg("x"))
	model, _ = model.Update(keyMsg("blocked"))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(App)

	if !a.actionInFlight {
		t.Error("actionInFlight should be true after block submit")
	}
	if a.actionLabel != "blocking SPEC-001" {
		t.Errorf("actionLabel = %q, want 'blocking SPEC-001'", a.actionLabel)
	}
}

func TestApp_PushHotkeyTriggersAction(t *testing.T) {
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

	model, _ := app.Update(keyMsg("p"))
	a := model.(App)

	if !a.actionInFlight {
		t.Error("actionInFlight should be true after pressing 'p'")
	}
	if a.actionLabel != "pushing SPEC-001" {
		t.Errorf("actionLabel = %q, want 'pushing SPEC-001'", a.actionLabel)
	}
}

func TestApp_SyncHotkeyTriggersAction(t *testing.T) {
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

	model, _ := app.Update(keyMsg("s"))
	a := model.(App)

	if !a.actionInFlight {
		t.Error("actionInFlight should be true after pressing 's'")
	}
	if a.actionLabel != "syncing SPEC-001" {
		t.Errorf("actionLabel = %q, want 'syncing SPEC-001'", a.actionLabel)
	}
}

func TestApp_SpinnerClearsOnError(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Simulate in-flight state.
	app.actionInFlight = true
	app.actionLabel = "advancing SPEC-001"
	app.syncBusyState()

	if !app.spinnerOn {
		t.Fatal("spinner should be on before result")
	}

	// Error result should clear the action-in-flight state.
	// A follow-up refresh is also kicked off so the spinner may remain on
	// showing "refreshing…" — that is correct and expected behavior.
	model, _ := app.Update(actionResultMsg{Action: "advance", Err: fmt.Errorf("gate not met")})
	a := model.(App)
	if a.actionInFlight {
		t.Error("actionInFlight should be false after error result")
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
	// cmd should not be tea.Quit — 'q' is not 'y' or 'n' so modal ignores it.
	_ = cmd
}

func TestApp_SearchSuppressesHotkeys(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// On the specs tab, `/` opens the global overlay — the input captures the
	// next keystroke and no spec hotkey should fire while it is open.
	app.switchView(ViewSpecs)
	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Test"},
	}
	app.specs.applyFilter()

	model, _ := app.Update(keyMsg("/"))
	a := model.(App)
	if !a.search.visible {
		t.Fatal("search overlay should be visible after '/'")
	}

	// Type 'a' — should land in the query, NOT open the advance modal.
	model, _ = a.Update(keyMsg("a"))
	a = model.(App)
	if a.modal.Visible {
		t.Error("'a' during search should not open advance modal")
	}
	if got := a.search.input.Value(); got != "a" {
		t.Errorf("query = %q, want 'a'", got)
	}

	// Type 'f' — appends to query, NOT focus a spec.
	model, _ = a.Update(keyMsg("f"))
	a = model.(App)
	if got := a.search.input.Value(); got != "af" {
		t.Errorf("query = %q, want 'af'", got)
	}

	// Type 'q' — appends to query, NOT quit (overlay stays open).
	model, _ = a.Update(keyMsg("q"))
	a = model.(App)
	if got := a.search.input.Value(); got != "afq" {
		t.Errorf("query = %q, want 'afq'", got)
	}
	if !a.search.visible {
		t.Error("'q' during search should not close the overlay")
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

func TestApp_ApplyTheme(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.applyTheme("dracula")
	if app.dashboard.styles.Title.GetBold() != app.styles.Title.GetBold() {
		t.Error("dashboard styles should match app styles after theme change")
	}
}

func TestApp_SettingsAppliedMsg_Theme(t *testing.T) {
	app := testApp()
	app.rc.User.Preferences.Theme = "dracula"

	model, _ := app.Update(settingsAppliedMsg{Field: fieldTheme})
	a := model.(App)

	want := ResolveTheme("dracula")
	if a.theme.Base != want.Base {
		t.Error("theme should update after settingsAppliedMsg for theme field")
	}
}

func TestApp_SettingsAppliedMsg_NameHeader(t *testing.T) {
	app := testApp()
	app.rc.User.User.Name = "Ada Lovelace"

	model, _ := app.Update(settingsAppliedMsg{Field: fieldName})
	a := model.(App)

	view := a.header.View()
	if !strings.Contains(view, "Ada Lovelace") {
		t.Errorf("header should show new name, got %q", view)
	}
}

func TestApp_SettingsAppliedMsg_Role(t *testing.T) {
	app := testApp()
	app.rc.User.User.OwnerRole = "pm"

	model, _ := app.Update(settingsAppliedMsg{Field: fieldRole})
	a := model.(App)

	if a.role != "pm" {
		t.Errorf("role = %q, want pm", a.role)
	}
}

func TestApp_SettingsThemePreview_AppliesWithoutPersisting(t *testing.T) {
	app := testApp()
	app.rc.User.Preferences.Theme = "" // unset; "auto" is the effective value

	model, _ := app.Update(settingsThemePreviewMsg{Theme: "dracula"})
	a := model.(App)

	if a.theme.Base != ResolveTheme("dracula").Base {
		t.Error("preview should apply the dracula theme to the live styles")
	}
	if a.rc.User.Preferences.Theme != "" {
		t.Errorf("preview must not persist the theme, got %q", a.rc.User.Preferences.Theme)
	}
}

func TestApp_SettingsPersistedMsg_SuccessStatus(t *testing.T) {
	app := testApp()
	model, _ := app.Update(settingsPersistedMsg{Field: fieldName, Err: nil})
	a := model.(App)
	if a.statusBar.StatusKind() != components.StatusSuccess {
		t.Errorf("status should be success after settings persist, got %v", a.statusBar.StatusKind())
	}
}

// TestApp_ViewFitsTerminalHeight guards against the chrome (header/status bar)
// rendering more rows than the layout budgets for them. When that happened,
// View() exceeded the terminal height and the alt-screen renderer left a stale
// full-width bar at the top of the screen — visible as a flash when saving a
// setting toggled a separate status surface (and thus the line count). The
// canonical status element now lives inside the single status-bar row, so the
// view must always be exactly a.height lines, idle or with a status showing.
func TestApp_ViewFitsTerminalHeight(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 40}, {100, 30}, {80, 24}, {60, 12}} {
		app := testApp()
		m, _ := app.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		app = m.(App)
		app.activeView = ViewSettings

		if lines := strings.Count(app.View().Content, "\n") + 1; lines != sz.h {
			t.Errorf("%dx%d: View() = %d lines, want %d (idle status)", sz.w, sz.h, lines, sz.h)
		}

		m, _ = app.Update(settingsPersistedMsg{Field: fieldTheme, Err: nil})
		app = m.(App)
		if app.statusBar.StatusKind() != components.StatusSuccess {
			t.Fatal("status should be success after persist")
		}
		if lines := strings.Count(app.View().Content, "\n") + 1; lines != sz.h {
			t.Errorf("%dx%d: View() = %d lines, want %d (status showing)", sz.w, sz.h, lines, sz.h)
		}
	}
}

func TestApp_ScheduleRefreshCoalescesInFlightWork(t *testing.T) {
	app := testApp()
	cmd := func() tea.Msg {
		return dashboardDataMsg{}
	}

	first := app.scheduleRefresh(refreshKeyDashboard, cmd)
	if first == nil {
		t.Fatal("first refresh should be scheduled")
	}
	second := app.scheduleRefresh(refreshKeyDashboard, cmd)
	if second != nil {
		t.Fatal("second refresh should be coalesced while in flight")
	}

	model, _ := app.Update(dashboardDataMsg{})
	app = model.(App)
	third := app.scheduleRefresh(refreshKeyDashboard, cmd)
	if third == nil {
		t.Fatal("refresh should be schedulable after data arrives")
	}
}

func TestApp_ReaderModeSkipsTickRefresh(t *testing.T) {
	app := testApp()
	app.showDetail = true
	app.detail = newSpecDetail(app.rc, "SPEC-001", app.styles, app.keys, app.theme)
	app.detail.readerMode = true

	if cmd := app.refreshActiveView(); cmd != nil {
		t.Fatal("reader mode should not schedule periodic detail refresh")
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

func TestNormalizeContentLines_FixedHeightWidth(t *testing.T) {
	lines := normalizeContentLines("abc\n", 6, 3)
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	for i, line := range lines {
		if got := xansi.StringWidth(line); got != 6 {
			t.Fatalf("line %d width = %d, want 6", i, got)
		}
	}
}

func TestNormalizeLineWidth_TruncatesANSIText(t *testing.T) {
	line := "\x1b[31mhello world\x1b[0m"
	norm := normalizeLineWidth(line, 5)
	if got := xansi.StringWidth(norm); got != 5 {
		t.Fatalf("normalized width = %d, want 5", got)
	}
	if !strings.Contains(norm, "\x1b[") {
		t.Fatal("normalized line should retain ANSI styling")
	}
}

func TestApp_ReaderPendingKeepsPreviousContent(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## Problem", Level: 2, Content: "Problem content."},
		{Slug: "solution", Heading: "## Solution", Level: 2, Content: "Solution content."},
	}

	m, cmd := m.update(keyMsg("o"))
	m, _ = m.update(cmd())
	before := m.view()
	if !strings.Contains(before, "Problem") {
		t.Fatalf("expected initial section content, got: %s", before)
	}

	m, _ = m.update(keyMsg("n"))
	during := m.view()
	if !strings.Contains(during, "Problem") {
		t.Fatalf("pending render should keep previous content visible, got: %s", during)
	}
	if strings.Contains(during, "Rendering §") {
		t.Fatalf("pending view should not show transient rendering label, got: %s", during)
	}
}

func TestApp_FirstReaderOpenShowsSpinnerNotNoContent(t *testing.T) {
	var model tea.Model = testApp()
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model, _ = model.Update(navigateToSpecMsg{SpecID: "SPEC-001"})
	model, _ = model.Update(specDetailDataMsg{
		Meta:     &markdown.SpecMeta{ID: "SPEC-001", Title: "Test Spec", Status: "build", Author: "alice", Updated: "2026-05-20"},
		Sections: []markdown.Section{{Slug: "problem", Heading: "## Problem Statement", Level: 2, Content: "Some problem."}},
	})

	model, _ = model.Update(tea.KeyPressMsg{Text: "o"})
	app := model.(App)
	// Simulate pending state — render in flight, no content yet.
	app.detail.readerContent = ""
	app.detail.renderInFlight = true
	app.syncBusyState()

	view := app.View().Content
	if strings.Contains(view, "(no content)") {
		t.Fatal("first open should not show no-content placeholder")
	}
	if !strings.Contains(view, "Rendering §") {
		t.Fatal("status bar should show rendering pending label")
	}
}

func TestApp_ReaderModeImmediateRender(t *testing.T) {
	// Simulate the exact Bubbletea runtime: store model as tea.Model
	// and drive all transitions through Update.
	var model tea.Model = testApp()

	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model, _ = model.Update(navigateToSpecMsg{SpecID: "SPEC-001"})
	model, _ = model.Update(specDetailDataMsg{
		Meta: &markdown.SpecMeta{
			ID: "SPEC-001", Title: "Test Spec", Status: "build",
			Author: "alice", Updated: "2026-05-20",
		},
		Sections: []markdown.Section{
			{Slug: "problem", Heading: "## Problem Statement", Level: 2, Content: "Some problem."},
			{Slug: "solution", Heading: "## Proposed Solution", Level: 2, Content: "Some solution."},
		},
	})

	overviewView := model.View().Content

	// Press 'o' to enter reader mode.
	var cmd tea.Cmd
	model, cmd = model.Update(tea.KeyPressMsg{Text: "o"})

	app := model.(App)
	if !app.detail.readerMode {
		t.Fatal("readerMode should be true after 'o'")
	}
	if cmd == nil {
		t.Fatal("entering reader mode should return a non-nil render cmd")
	}
	model, _ = model.Update(cmd())

	readerView := model.View().Content
	if overviewView == readerView {
		t.Fatal("overview and reader views must produce different output")
	}
	if !strings.Contains(readerView, "Problem Statement") {
		t.Error("reader view should contain the section heading")
	}
}

func TestApp_RevertOverlayOpensOnV(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Set up a selected spec with a non-first stage.
	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Test", Status: "build"},
	}
	app.specs.applyFilter()
	app.switchView(ViewSpecs)

	// Press 'v' to open revert overlay.
	model, _ := app.Update(keyMsg("v"))
	a := model.(App)
	if !a.revert.active {
		t.Fatal("revert overlay should be active after pressing 'v'")
	}
	if a.revert.specID != "SPEC-001" {
		t.Errorf("revert.specID = %q, want SPEC-001", a.revert.specID)
	}
	if len(a.revert.stages) == 0 {
		t.Fatal("revert should have target stages")
	}
}

func TestApp_RevertOverlayEscCancels(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Test", Status: "build"},
	}
	app.specs.applyFilter()
	app.switchView(ViewSpecs)

	model, _ := app.Update(keyMsg("v"))
	a := model.(App)
	if !a.revert.active {
		t.Fatal("revert should be active")
	}

	// Press Esc to cancel.
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(App)
	if a.revert.active {
		t.Error("revert overlay should close on Esc")
	}
}

func TestApp_RevertOverlayCapturesKeys(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Open revert overlay directly.
	_ = app.revert.openRevert("SPEC-001", "build", app.rc.Pipeline(), app.width, app.theme)
	app.revert.nextField() // move to reason

	// Type a reason — should go to the overlay, not trigger hotkeys.
	model, cmd := app.Update(keyMsg("q"))
	a := model.(App)
	if a.revert.reasonText() != "q" {
		t.Errorf("reason = %q, want 'q'", a.revert.reasonText())
	}
	// 'q' should be captured as text, not quit. The textinput may return a
	// cursor command; the only forbidden outcome is a quit.
	if cmd != nil && isQuit(cmd) {
		t.Error("'q' during revert overlay should not quit")
	}
}

func TestApp_RevertRendersInView(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	_ = app.revert.openRevert("SPEC-001", "build", app.rc.Pipeline(), app.width, app.theme)
	got := app.View().Content
	if !strings.Contains(got, "Revert") {
		t.Error("view should contain Revert overlay")
	}
	if !strings.Contains(got, "SPEC-001") {
		t.Error("view should contain spec ID in revert overlay")
	}
}

func TestApp_RevertFirstStageShowsStatusError(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Set up a spec in the first stage.
	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Test", Status: "triage"},
	}
	app.specs.applyFilter()
	app.switchView(ViewSpecs)

	// Press 'v' — should show error status, not open overlay.
	model, _ := app.Update(keyMsg("v"))
	a := model.(App)
	if a.revert.active {
		t.Error("revert overlay should not open for first stage")
	}
	if a.statusBar.StatusKind() != components.StatusError {
		t.Errorf("status should show error for first stage revert, got %v", a.statusBar.StatusKind())
	}
}

func TestApp_ModalInputAcceptsSpaces(t *testing.T) {
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

	// Open block modal with 'x'.
	model, _ := app.Update(keyMsg("x"))
	a := model.(App)
	if !a.modal.Visible {
		t.Fatal("modal should be visible")
	}

	// Type text with a space. A real space key press carries Text " ".
	model, _ = a.Update(keyMsg("API"))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model, _ = model.Update(keyMsg("v2"))
	a = model.(App)
	if a.modal.Value() != "API v2" {
		t.Errorf("modal input = %q, want 'API v2'", a.modal.Value())
	}
}

func TestApp_SearchAcceptsSpaces(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "My Spec"},
	}
	app.specs.applyFilter()
	app.switchView(ViewSpecs)

	// Activate the overlay and type a multi-word query with a space.
	model, _ := app.Update(keyMsg("/"))
	model, _ = model.Update(keyMsg("my"))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model, _ = model.Update(keyMsg("spec"))
	a := model.(App)
	if got := a.search.input.Value(); got != "my spec" {
		t.Errorf("query = %q, want 'my spec'", got)
	}
}

func TestApp_RevertReasonAcceptsSpaces(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	_ = app.revert.openRevert("SPEC-001", "build", app.rc.Pipeline(), app.width, app.theme)
	app.revert.nextField() // move to reason

	model, _ := app.Update(keyMsg("gate"))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model, _ = model.Update(keyMsg("failed"))
	a := model.(App)
	if a.revert.reasonText() != "gate failed" {
		t.Errorf("reason = %q, want 'gate failed'", a.revert.reasonText())
	}
}

func TestApp_IntakeAcceptsSpaces(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	app.intake.open()

	// Type a title with a space.
	model, _ := app.Update(keyMsg("new"))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model, _ = model.Update(keyMsg("item"))
	a := model.(App)
	if a.intake.title != "new item" {
		t.Errorf("title = %q, want 'new item'", a.intake.title)
	}
}

// TestApp_EscClearsSearchFilterBeforeArmingExit reproduces the bug where the
// double-esc exit guard hijacked esc and made clearing a committed search
// filter impossible. Sequence: / type → esc (leave bar) → esc (clear filter,
// not arm) → esc (arm exit).
func TestApp_EscClearsSearchOverlayBeforeArmingExit(t *testing.T) {
	app := testApp()
	app.width = 100
	app.height = 30
	app.propagateSize()
	app.activeView = ViewSpecs
	app.specs.loading = false
	app.specs.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth"},
		{ID: "SPEC-002", Title: "Payments"},
	}
	app.specs.applyFilter()

	// '/' opens the overlay; type a query.
	model, _ := app.Update(keyMsg("/"))
	a := model.(App)
	if !a.search.visible {
		t.Fatal("'/' should open the search overlay")
	}
	model, _ = a.Update(keyMsg("auth"))
	a = model.(App)
	if got := a.search.input.Value(); got == "" {
		t.Fatal("typing should build the search query")
	}

	// esc #1: close the overlay; must NOT arm exit.
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(App)
	if a.search.visible {
		t.Error("first esc should close the overlay")
	}
	if a.exitArmed {
		t.Error("first esc should not arm exit")
	}

	// esc #2: nothing left to pop → arm exit.
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(App)
	if !a.exitArmed {
		t.Error("second esc with nothing open should arm exit")
	}
}
