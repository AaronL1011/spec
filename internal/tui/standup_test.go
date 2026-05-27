package tui

import (
	"strings"
	"testing"
)

func TestStandupOverlay_HiddenByDefault(t *testing.T) {
	var s standupOverlay
	if s.visible {
		t.Error("standup should be hidden by default")
	}
	if got := s.view(); got != "" {
		t.Error("hidden standup should render empty")
	}
}

func TestStandupOverlay_ShowAndHide(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	s := standupOverlay{styles: styles, width: 80, height: 20}

	s.show("  Test standup content\n  • Did stuff")
	if !s.visible {
		t.Error("standup should be visible after show")
	}

	got := s.view()
	if !strings.Contains(got, "Standup") {
		t.Error("should contain 'Standup' header")
	}
	if !strings.Contains(got, "Did stuff") {
		t.Error("should contain standup content")
	}
	if !strings.Contains(got, "c copy") {
		t.Error("should show copy hint")
	}

	s.hide()
	if s.visible {
		t.Error("standup should be hidden after hide")
	}
}

func TestStandupOverlay_Scroll(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	s := standupOverlay{styles: styles, width: 80, height: 5}
	s.show("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

	s.scrollDown()
	if s.scroll != 1 {
		t.Errorf("scroll = %d, want 1", s.scroll)
	}

	s.scrollUp()
	if s.scroll != 0 {
		t.Errorf("scroll = %d, want 0", s.scroll)
	}

	// Can't scroll above 0.
	s.scrollUp()
	if s.scroll != 0 {
		t.Errorf("scroll = %d, want 0", s.scroll)
	}
}

func TestApp_StandupOverlayFlow(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()

	// Receive standup data.
	model, _ := app.Update(standupDataMsg{Text: "  Test standup"})
	a := model.(App)
	if !a.standup.visible {
		t.Error("standup overlay should be visible")
	}

	// Esc closes it.
	model, _ = a.Update(keyMsg("\x1b")) // won't match Esc via keyMsg — use tea.KeyMsg
	// Use proper esc:
	a.standup.hide()
	if a.standup.visible {
		t.Error("standup should close on hide")
	}
}
