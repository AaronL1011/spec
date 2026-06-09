package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHelp_HiddenByDefault(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)

	if h.visible {
		t.Error("help should be hidden by default")
	}
	if got := h.view(); got != "" {
		t.Error("hidden help should render empty")
	}
}

func TestHelp_ToggleVisibility(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)
	h.setSize(80, 24)

	h.toggle()
	if !h.visible {
		t.Error("help should be visible after toggle")
	}

	h.toggle()
	if h.visible {
		t.Error("help should be hidden after second toggle")
	}
}

func TestHelp_RendersBindings(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)
	h.setSize(80, 60)
	h.setContext("Dashboard")
	h.visible = true

	got := h.view()
	if !strings.Contains(got, "Navigation") {
		t.Error("should contain Navigation section")
	}
	if !strings.Contains(got, "Views") {
		t.Error("should contain Views section")
	}
	if !strings.Contains(got, "Actions") {
		t.Error("should contain Actions section")
	}
	if !strings.Contains(got, "Global") {
		t.Error("should contain Global section")
	}
	if !strings.Contains(got, "Dashboard") {
		t.Error("should show context label")
	}
}

func TestHelp_SettingsContext(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)
	h.setSize(80, 60)
	h.setContext("Settings")
	h.visible = true

	got := h.view()
	if !strings.Contains(got, "Settings") {
		t.Error("should contain Settings section")
	}
	if strings.Contains(got, "advance") {
		t.Error("Settings help should not list spec action bindings")
	}
}

func TestHelp_DismissOnEsc(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)
	h.setSize(80, 24)
	h.visible = true

	h, _ = h.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h.visible {
		t.Error("help should close on Esc")
	}
}

func TestHelp_DismissOnQuestionMark(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	h := newHelp(keys, styles)
	h.setSize(80, 24)
	h.visible = true

	h, _ = h.update(keyMsg("?"))
	if h.visible {
		t.Error("help should close on ?")
	}
}
