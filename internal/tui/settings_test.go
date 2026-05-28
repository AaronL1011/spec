package tui

import (
	"strings"
	"testing"
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

func TestSettings_Scrolls(t *testing.T) {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSettings(rc, styles, keys)
	m.setSize(80, 5) // very small

	before := m.view()
	m, _ = m.update(keyMsg("j"))
	after := m.view()

	if m.scroll != 1 {
		t.Errorf("scroll = %d, want 1", m.scroll)
	}
	if before == after {
		t.Error("view should change after scroll")
	}
}
