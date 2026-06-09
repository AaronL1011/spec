package tui

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
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
		if theme.Text == nil {
			t.Errorf("ResolveTheme(%q): Text should not be empty", pref)
		}
	}
}

func TestHasDarkBackground_CachedAndStable(t *testing.T) {
	// The detection is cached behind sync.Once so cycling onto the "auto"
	// theme never re-queries the terminal (which would block once Bubble Tea
	// owns stdin). Repeated calls must return the same value.
	first := hasDarkBackground()
	for range 100 {
		if hasDarkBackground() != first {
			t.Fatal("hasDarkBackground should return a stable, cached value")
		}
	}

	// Resolving "auto" repeatedly must likewise stay consistent.
	want := ResolveTheme("auto")
	for range 100 {
		if ResolveTheme("auto").Base != want.Base {
			t.Fatal("ResolveTheme(\"auto\") should be deterministic across calls")
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

func assertNonEmpty(t *testing.T, field string, c color.Color) {
	t.Helper()
	// lipgloss v2 colours are color.Color values; an unset theme field is nil.
	if c == nil {
		t.Errorf("%s colour should not be empty", field)
	}
}
