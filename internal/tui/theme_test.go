package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResolveTheme_NamedThemes(t *testing.T) {
	names := []string{
		"catppuccin-mocha", "catppuccin-latte", "catppuccin-macchiato", "catppuccin-frappe",
		"gruvbox-dark", "gruvbox", "dracula", "tokyo-night", "nord",
		"solarized-dark", "solarized-light", "rose-pine", "rosé-pine",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			theme := ResolveTheme(name)
			assertNonEmpty(t, "Base", theme.Base)
			assertNonEmpty(t, "Text", theme.Text)
			assertNonEmpty(t, "Accent", theme.Accent)
			assertNonEmpty(t, "Error", theme.Error)
		})
	}
}

func TestResolveTheme_AutoFallback(t *testing.T) {
	for _, pref := range []string{"", "auto", "unknown-theme"} {
		theme := ResolveTheme(pref)
		if theme.Text == "" {
			t.Errorf("ResolveTheme(%q): Text should not be empty", pref)
		}
	}
}

func TestNewStyles_AllFieldsSet(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	styles := NewStyles(theme)

	// Spot-check that key styles have foreground colour set.
	if styles.Title.GetForeground() == (lipgloss.NoColor{}) {
		t.Error("Title style should have foreground set")
	}
	if styles.Error.GetForeground() == (lipgloss.NoColor{}) {
		t.Error("Error style should have foreground set")
	}
}

func assertNonEmpty(t *testing.T, field string, c lipgloss.Color) {
	t.Helper()
	if string(c) == "" {
		t.Errorf("%s colour should not be empty", field)
	}
}
