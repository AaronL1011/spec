package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/agentcheck"
	"github.com/aaronl1011/spec/internal/config"
)

func TestSettings_RendersIdentity(t *testing.T) {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSettings(rc, styles, keys)
	m.setSize(80, 30)

	got := m.view()
	if !strings.Contains(got, "Identity") {
		t.Error("should contain Identity section")
	}
	if !strings.Contains(got, "Test") {
		t.Error("should contain user name")
	}
	if !strings.Contains(got, "engineer") {
		t.Error("should contain role")
	}
}

func TestSettings_RendersIntegrations(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team.Integrations.Repo.Provider = "github"
	rc.Team.Integrations.Comms.Provider = "slack"

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSettings(rc, styles, keys)
	m.setSize(80, 30)

	got := m.view()
	if !strings.Contains(got, "Integrations") {
		t.Error("should contain Integrations section")
	}
	if !strings.Contains(got, "github") {
		t.Error("should show github as repo provider")
	}
	if !strings.Contains(got, "slack") {
		t.Error("should show slack as comms provider")
	}
}

func TestSettings_RendersAppearance(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Preferences.Theme = "dracula"
	rc.User.Preferences.RefreshInterval = "45s"
	rc.User.Preferences.Editor = "nvim"

	styles := NewStyles(ResolveTheme("dracula"))
	keys := DefaultKeyMap()
	m := newSettings(rc, styles, keys)
	m.setSize(80, 30)

	got := m.view()
	if !strings.Contains(got, "dracula") {
		t.Error("should show theme name")
	}
	if !strings.Contains(got, "45s") {
		t.Error("should show refresh interval")
	}
	if !strings.Contains(got, "nvim") {
		t.Error("should show editor")
	}
	if strings.Contains(got, "t to cycle") {
		t.Error("should not show legacy t shortcut hint")
	}
}

func TestSettings_RendersConfigPaths(t *testing.T) {
	rc := testResolvedConfig()
	rc.UserConfigPath = "/home/user/.spec/config.yaml"
	rc.TeamConfigPath = "/path/to/spec.config.yaml"

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSettings(rc, styles, keys)
	m.setSize(80, 30)

	got := m.view()
	if !strings.Contains(got, "Config Paths") {
		t.Error("should contain Config Paths section")
	}
	if !strings.Contains(got, ".spec/config.yaml") {
		t.Error("should show user config path")
	}
}

func TestSettings_EditableRowsShowEnterHint(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.setSize(80, 30)

	got := m.view()
	if !strings.Contains(got, "(Enter)") {
		t.Error("editable rows should show (Enter) hint")
	}
}

func TestSettings_FocusWraps(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())

	m.focused = fieldAgentCheck
	m, _ = m.update(keyMsg("j"))
	if m.focused != fieldName {
		t.Errorf("focus after j from last = %v, want fieldName", m.focused)
	}

	m, _ = m.update(keyMsg("k"))
	if m.focused != fieldAgentCheck {
		t.Errorf("focus after k from first = %v, want fieldAgentCheck", m.focused)
	}
}

func TestSettings_TKeyDoesNothing(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Preferences.Theme = ""
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())

	m, cmd := m.update(keyMsg("t"))
	if cmd != nil {
		t.Error("t key should not dispatch a command")
	}
	if m.rc.User.Preferences.Theme != "" {
		t.Error("t key should not change theme")
	}
}

func TestSettings_EscCancelsEdit(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldName
	m.draft = "changed"

	m, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Error("cancel should not persist")
	}
	if m.mode != settingsBrowse {
		t.Error("should return to browse mode")
	}
	if m.draft != "" {
		t.Error("draft should be cleared on cancel")
	}
}

func TestSettings_InvalidRefreshRejected(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldRefresh
	m.draft = "abc"

	m, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("invalid refresh should not persist")
	}
	if m.fieldErr == "" {
		t.Error("should set field error for invalid refresh")
	}
}

func TestSettings_ConfirmNameDispatchesPersist(t *testing.T) {
	rc := testResolvedConfig()
	rc.UserConfigPath = "/tmp/test-config.yaml"
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldName
	m.draft = "New Name"
	m.snapshots[fieldName] = "Test"

	m, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirm should schedule persist")
	}
	if m.rc.User.User.Name != "New Name" {
		t.Errorf("name = %q, want New Name", m.rc.User.User.Name)
	}
}

func TestSettings_TypeHLIntoTextField(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.User.Name = ""
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.focused = fieldName
	m, _ = m.beginEdit()

	for _, r := range []rune{'h', 'e', 'l', 'l', 'o'} {
		m, _ = m.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.draft != "hello" {
		t.Errorf("draft = %q, want hello (l/h must be typable in text fields)", m.draft)
	}
}

func TestSettings_SpaceIntoTextField(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.User.Name = "John"
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.focused = fieldName
	m, _ = m.beginEdit()

	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m, _ = m.update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if m.draft != "John D" {
		t.Errorf("draft = %q, want %q", m.draft, "John D")
	}
}

func TestSettings_CycleThemeWithHL(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldTheme
	m.enumIdx = 0
	m.draft = ThemeNames()[0]

	m, _ = m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.draft != ThemeNames()[1] {
		t.Errorf("theme after l = %q, want %q", m.draft, ThemeNames()[1])
	}
	m, _ = m.update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.draft != ThemeNames()[0] {
		t.Errorf("theme after h = %q, want %q", m.draft, ThemeNames()[0])
	}
}

func TestSettings_ThemeCyclePreviews(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldTheme
	m.enumIdx = 0
	m.draft = ThemeNames()[0]

	_, cmd := m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd == nil {
		t.Fatal("cycling theme in edit mode should emit a preview command")
	}
	msg, ok := cmd().(settingsThemePreviewMsg)
	if !ok {
		t.Fatalf("expected settingsThemePreviewMsg, got %T", cmd())
	}
	if msg.Theme != ThemeNames()[1] {
		t.Errorf("preview theme = %q, want %q", msg.Theme, ThemeNames()[1])
	}
}

func TestSettings_RoleCycleDoesNotPreview(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldRole
	m.enumIdx = 0

	_, cmd := m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd != nil {
		t.Error("cycling a non-theme enum field should not emit a preview command")
	}
}

func TestSettings_ThemeCancelRevertsPreview(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Preferences.Theme = "dracula"
	m := newSettings(rc, NewStyles(ResolveTheme("dracula")), DefaultKeyMap())
	m.focused = fieldTheme

	// Begin edit captures the original theme as the revert target.
	m, _ = m.beginEdit()
	// Preview a different theme by cycling.
	m, _ = m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})

	// Cancel should request a preview back to the original value.
	m, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("cancelling a theme edit should emit a revert preview command")
	}
	msg, ok := cmd().(settingsThemePreviewMsg)
	if !ok {
		t.Fatalf("expected settingsThemePreviewMsg, got %T", cmd())
	}
	if msg.Theme != "dracula" {
		t.Errorf("revert preview theme = %q, want dracula", msg.Theme)
	}
	if m.mode != settingsBrowse {
		t.Error("cancel should return to browse mode")
	}
}

func TestSettings_CycleRoleInEditMode(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.mode = settingsEditing
	m.focused = fieldRole
	m.draft = "engineer"
	m.enumIdx = 4

	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeySpace})
	if m.draft != "pm" {
		t.Errorf("role after cycle = %q, want pm", m.draft)
	}
}

func TestSettings_ViewFollowsFocusDown(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.setSize(80, 5)

	// Focus Name (top) — should be visible.
	got := m.view()
	if !strings.Contains(got, "Name") {
		t.Error("focused Name field should be visible")
	}

	// Navigate down to Editor — should scroll to show it.
	for m.focused != fieldEditor {
		m, _ = m.update(keyMsg("j"))
	}
	got = m.view()
	if !strings.Contains(got, "Editor") {
		t.Error("focused Editor field should be visible after scrolling")
	}
}

func TestSettings_PageDownScrollsToReadOnly(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team.Integrations.Repo.Provider = "github"
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.setSize(80, 5)

	// Page down repeatedly until Integrations section is visible.
	var found bool
	for range 20 {
		m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		got := m.view()
		if strings.Contains(got, "Integrations") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should be able to page down to Integrations section")
	}
}

func TestSettings_ScrollDownReachesConfigPaths(t *testing.T) {
	rc := testResolvedConfig()
	rc.UserConfigPath = "/home/user/.spec/config.yaml"
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.setSize(80, 5)

	// Scroll down line-by-line (shift+↓ = ScrollDown) until Config Paths is visible.
	var found bool
	for range 50 {
		m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
		got := m.view()
		if strings.Contains(got, "Config Paths") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should be able to scroll down to Config Paths section")
	}
}

func TestSettings_FocusChangeScrollsToField(t *testing.T) {
	rc := testResolvedConfig()
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.setSize(80, 5)

	// Navigate down to Editor.
	for m.focused != fieldEditor {
		m, _ = m.update(keyMsg("j"))
	}
	got := m.view()
	if !strings.Contains(got, "Editor") {
		t.Error("Editor should be visible when focused")
	}

	// Move focus back to Name at top — viewport should scroll up.
	for m.focused != fieldName {
		m, _ = m.update(keyMsg("k"))
	}
	got = m.view()
	if !strings.Contains(got, "Name") {
		t.Error("Name should be visible after scrolling up")
	}
}

func TestParseRefreshPref(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"45s", false},
		{"2m", false},
		{"1s", true},
		{"abc", true},
		{"", true},
	}
	for _, tt := range tests {
		_, err := parseRefreshPref(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseRefreshPref(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
		}
	}
}

func TestSettings_MouseToggleCyclesAndPersists(t *testing.T) {
	rc := testResolvedConfig()
	rc.UserConfigPath = "/tmp/test-config.yaml"
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.focused = fieldMouse

	// Default is off.
	if got := m.savedValue(fieldMouse); got != "off" {
		t.Fatalf("default mouse = %q, want off", got)
	}

	// Enter edit, cycle with space (off → on), confirm.
	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeySpace})
	_, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Error("confirming mouse toggle should dispatch a persist command")
	}
	if !rc.User.Preferences.Mouse {
		t.Error("mouse preference should be enabled after toggle")
	}
}

func TestSettings_AgentCheckRemainsLast(t *testing.T) {
	// The wrap-around invariant the navigation tests rely on depends on knowing
	// which row is final. Adding a field means updating this and TestSettings_
	// FocusWraps together, which is the point of asserting it separately.
	if fieldAgentCheck != fieldCount-1 {
		t.Errorf("fieldAgentCheck (%d) should be the last field before fieldCount (%d)", fieldAgentCheck, fieldCount)
	}
}

// The agent row is an action, not a value: enter must run the preflight rather
// than putting the panel into an edit mode with no field to edit.
func TestSettings_AgentRowEnterRunsPreflightNotEdit(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Agent = &config.ProviderConfig{Provider: "pi"}
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.focused = fieldAgentCheck

	m, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.isEditing() {
		t.Error("enter on the agent row should not open an editor")
	}
	if !m.agentChecking {
		t.Error("enter on the agent row should start the preflight")
	}
	if cmd == nil {
		t.Fatal("enter on the agent row should dispatch the preflight command")
	}
}

// A failed preflight must name the step that failed, and must not render a
// provider's multi-line remediation prose inline where it would push the panel
// off screen.
func TestSettings_AgentCheckFailureNamesStep(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Agent = &config.ProviderConfig{Provider: "pi"}
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.focused = fieldAgentCheck

	m, _ = m.update(agentCheckResultMsg{
		Report: agentcheck.Report{Provider: "pi", FailedStep: "completion"},
		Err:    errors.New("no API key found\nUse /login to log into a provider"),
	})

	if m.agentChecking {
		t.Error("a finished check should clear the in-flight flag")
	}
	detail := stripANSI(m.renderAgentCheckDetail())
	if !strings.Contains(detail, "completion") {
		t.Errorf("detail = %q, want the failing step named", detail)
	}
	if strings.Contains(detail, "/login") {
		t.Errorf("detail = %q, want only the first line of a multi-line error", detail)
	}
}

// A successful preflight reports the round-trip evidence, and flags settings
// that cannot take effect for the resolved provider.
func TestSettings_AgentCheckSuccessShowsEvidenceAndInertKeys(t *testing.T) {
	rc := testResolvedConfig()
	rc.User.Agent = &config.ProviderConfig{Provider: "pi"}
	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())

	m, _ = m.update(agentCheckResultMsg{Report: agentcheck.Report{
		Provider:      "pi",
		LatencyMS:     3000,
		Model:         "claude-sonnet-4-5",
		Tokens:        18,
		InertSettings: []string{"generate.max_tokens (the harness CLI exposes no token cap)"},
	}})

	detail := stripANSI(m.renderAgentCheckDetail())
	for _, want := range []string{"3.0s", "claude-sonnet-4-5", "18 tokens", "generate.max_tokens"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail = %q, want it to contain %q", detail, want)
		}
	}
}
