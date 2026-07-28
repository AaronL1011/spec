package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/tui/components"
)

// bountyApp is an app whose dashboard has one selectable spec and whose team
// has bounties enabled, granted by the given role.
func bountyApp(t *testing.T, role string, granters ...string) App {
	t.Helper()
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rc := testResolvedConfig()
	rc.Team.Bounty = &config.BountyConfig{Enabled: true, GrantableBy: granters}
	app := newAppWithDB(rc, testRegistry(), role, db)
	app.data(t)
	return app
}

// data seeds one DO row so selectedSpecID resolves.
func (a *App) data(t *testing.T) {
	t.Helper()
	a.dashboard.loading = false
	a.dashboard.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{{SpecID: "SPEC-001", Title: "Bountyable", Stage: "build"}},
	}
	a.dashboard.items = a.dashboard.buildRows()
}

// pressKeys sends a key sequence to the app and returns the resulting model.
func pressKeys(app App, keys ...string) App {
	model := tea.Model(app)
	for _, k := range keys {
		model, _ = model.Update(keyMsg(k))
	}
	return model.(App)
}

// TestBountyKey_OpensReasonPrompt is AC-16: g b on a selected spec opens an
// input modal for that spec.
func TestBountyKey_OpensReasonPrompt(t *testing.T) {
	app := bountyApp(t, "tl", "tl", "pm")
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	got := pressKeys(app, "g", "b")
	if got.modal.Kind != components.ModalInput {
		t.Fatalf("modal kind = %v, want an input prompt", got.modal.Kind)
	}
	if got.pendingAction != "bounty" || got.pendingSpecID != "SPEC-001" {
		t.Errorf("pending action = %q on %q, want bounty on SPEC-001", got.pendingAction, got.pendingSpecID)
	}
	if !strings.Contains(got.modal.Title, "SPEC-001") {
		t.Errorf("modal title = %q, want the spec named", got.modal.Title)
	}
	if !strings.Contains(got.modal.Title, IconSpark) {
		t.Errorf("modal title = %q, want the bounty spark", got.modal.Title)
	}
}

// TestBountyKey_RoleRefusedBeforePrompt is AC-16's refusal path: an
// unauthorised role is told immediately instead of typing a reason first.
func TestBountyKey_RoleRefusedBeforePrompt(t *testing.T) {
	app := bountyApp(t, "engineer", "tl")
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	got := pressKeys(app, "g", "b")
	if got.modal.Kind == components.ModalInput {
		t.Error("an unauthorised role must not be prompted for a reason")
	}
	if got.pendingAction == "bounty" {
		t.Error("no bounty action should be pending")
	}
	if !got.statusBar.ShowingOutcome() {
		t.Error("the refusal should surface in the status bar")
	}
}

// TestBountyKey_DisabledFeatureRefuses covers AC-13 in the TUI.
func TestBountyKey_DisabledFeatureRefuses(t *testing.T) {
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := newAppWithDB(testResolvedConfig(), testRegistry(), "tl", db)
	app.data(t)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	got := pressKeys(app, "g", "b")
	if got.modal.Kind == components.ModalInput {
		t.Error("g b must not prompt when bounties are disabled")
	}
}

// TestBountyKey_EmptySubmitIsNoOp keeps a reasonless grant impossible from the
// TUI, matching the CLI's require_reason default.
func TestBountyKey_EmptySubmitIsNoOp(t *testing.T) {
	app := bountyApp(t, "tl", "tl")
	app.pendingAction = "bounty"
	app.pendingSpecID = "SPEC-001"
	if cmd := app.executeActionWithInput("   "); cmd != nil {
		t.Error("an empty reason should do nothing, not grant a reasonless bounty")
	}
}

// TestBountyKey_InGPrefixHelp keeps the binding discoverable.
func TestBountyKey_InGPrefixHelp(t *testing.T) {
	var found bool
	for _, b := range DefaultKeyMap().ActionBindings() {
		if b.Help().Key == "g b" {
			found = true
			if b.Help().Desc == "" {
				t.Error("g b binding needs a description for the help overlay")
			}
		}
	}
	if !found {
		t.Error("g b should appear in the action bindings shown in help")
	}
}

// TestBountyMarkerFrameShared asserts every surface animates off one clock, so
// a spec shimmers in step wherever it appears.
func TestBountyMarkerFrameShared(t *testing.T) {
	app := bountyApp(t, "tl", "tl")
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.dashboard.data.Do[0].Bounty = &markdown.BountyState{GrantedAt: "2026-07-28T09:00:00Z"}
	app.dashboard.items = app.dashboard.buildRows()

	model := tea.Model(app)
	for range 3 {
		model, _ = model.Update(spinnerTickMsg{})
	}
	got := model.(App)
	if got.bountyFrame != 3 {
		t.Fatalf("bountyFrame = %d after 3 ticks, want 3", got.bountyFrame)
	}
	got.View() // propagates the clock to the views
}
