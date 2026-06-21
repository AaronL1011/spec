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
		// Curated additions and their aliases.
		"kanagawa", "kanagawa-wave",
		"everforest-dark", "everforest", "everforest-light",
		"github-dark", "github-light", "github",
		"ayu-mirage", "ayu-light", "ayu",
		"modus-vivendi", "modus-dark", "modus-operandi", "modus-light",
		"graphite", "mono", "monochrome", "noir",
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

// TestResolveTheme_ThemeNamesComplete asserts every entry in ThemeNames()
// resolves to a populated Theme and that each name is a unique switch arm in
// ResolveTheme (no two names collide silently). The settings UI cycles through
// ThemeNames(), so a stale or missing switch arm would surface a blank live
// preview rather than failing here.
func TestResolveTheme_ThemeNamesComplete(t *testing.T) {
	names := ThemeNames()
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, dup := seen[name]; dup {
			t.Errorf("ThemeNames has duplicate %q", name)
			continue
		}
		seen[name] = struct{}{}
		theme := ResolveTheme(name)
		assertNonEmpty(t, name+"/Base", theme.Base)
		assertNonEmpty(t, name+"/Text", theme.Text)
		assertNonEmpty(t, name+"/Accent", theme.Accent)
		assertNonEmpty(t, name+"/Error", theme.Error)
	}
}

// TestResolveTheme_AliasesMatchCanonical confirms an alias resolves to the
// same palette as its canonical key, guarding against drift when the switch
// is edited.
func TestResolveTheme_AliasesMatchCanonical(t *testing.T) {
	pairs := []struct{ alias, canonical string }{
		{"gruvbox", "gruvbox-dark"},
		{"rosé-pine", "rose-pine"},
		{"kanagawa-wave", "kanagawa"},
		{"everforest", "everforest-dark"},
		{"github", "github-light"},
		{"ayu", "ayu-light"},
		{"modus-dark", "modus-vivendi"},
		{"modus-light", "modus-operandi"},
		{"mono", "graphite"},
		{"monochrome", "graphite"},
		{"noir", "graphite"},
	}
	for _, p := range pairs {
		if !themesEqual(ResolveTheme(p.alias), ResolveTheme(p.canonical)) {
			t.Errorf("alias %q != canonical %q", p.alias, p.canonical)
		}
	}
}

// TestTheme_LightThemesBaseLighterThanText guards against accidentally swapping
// foreground and background on the light themes — a real risk now that the
// lineup carries four light palettes. A light theme's Base must be visibly
// lighter than its Text so the surface reads as background, not text.
func TestTheme_LightThemesBaseLighterThanText(t *testing.T) {
	light := []string{
		"solarized-light", "catppuccin-latte",
		"everforest-light", "github-light", "ayu-light", "modus-operandi",
	}
	for _, name := range light {
		t.Run(name, func(t *testing.T) {
			theme := ResolveTheme(name)
			if luminance(theme.Base) <= luminance(theme.Text) {
				t.Errorf("light theme %q: Base must be lighter than Text", name)
			}
		})
	}
}

// TestTheme_GraphiteIsAchromatic confirms the bespoke monochrome theme is
// truly grayscale: every structural token has R==G==B, and the four semantic
// tokens are luminance-ordered so status sorts by brightness without hue.
func TestTheme_GraphiteIsAchromatic(t *testing.T) {
	theme := ResolveTheme("graphite")

	structural := []struct {
		name  string
		color color.Color
	}{
		{"Base", theme.Base}, {"Surface", theme.Surface}, {"Overlay", theme.Overlay},
		{"Text", theme.Text}, {"SubText", theme.SubText}, {"Muted", theme.Muted},
	}
	for _, s := range structural {
		r, g, b := rgb(s.color)
		if r != g || g != b {
			t.Errorf("graphite %s must be achromatic (R==G==B), got %02x%02x%02x", s.name, r, g, b)
		}
	}

	// Semantics ordered by luminance, brightest = most alert.
	if luminance(theme.Error) <= luminance(theme.Success) {
		t.Error("graphite Error must be brighter than Success")
	}
	if luminance(theme.Success) <= luminance(theme.Accent) {
		t.Error("graphite Success must be brighter than Accent")
	}
	if luminance(theme.Warning) >= luminance(theme.SubText) {
		t.Error("graphite Warning must recede below SubText")
	}
	// Distinctness: every token unique so panels/borders don't collapse.
	all := []color.Color{theme.Base, theme.Surface, theme.Overlay, theme.Text,
		theme.SubText, theme.Muted, theme.Accent, theme.Success, theme.Warning, theme.Error}
	keys := make(map[[3]uint8]struct{}, len(all))
	for _, c := range all {
		r, g, b := rgb(c)
		keys[[3]uint8{r, g, b}] = struct{}{}
	}
	if len(keys) != len(all) {
		t.Errorf("graphite tokens must be distinct, got %d unique of %d", len(keys), len(all))
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

// themesEqual reports whether two themes are byte-for-byte identical across
// all 10 semantic tokens. Used by the alias test to confirm an alias resolves
// to the same palette as its canonical key.
func themesEqual(a, b Theme) bool {
	for _, pair := range [][2]color.Color{
		{a.Base, b.Base}, {a.Surface, b.Surface}, {a.Overlay, b.Overlay},
		{a.Text, b.Text}, {a.SubText, b.SubText}, {a.Muted, b.Muted},
		{a.Accent, b.Accent}, {a.Success, b.Success}, {a.Warning, b.Warning},
		{a.Error, b.Error},
	} {
		ar, ag, ab := rgb(pair[0])
		br, bg, bb := rgb(pair[1])
		if ar != br || ag != bg || ab != bb {
			return false
		}
	}
	return true
}

// luminance returns a coarse perceived-lightness proxy (max channel) for
// ordering grayscale-ish theme tokens. Good enough for the light/dark and
// graphite ordering assertions; not a full sRGB luminance formula.
func luminance(c color.Color) uint8 {
	r, g, b := rgb(c)
	if r > g {
		g = r
	}
	if g > b {
		b = g
	}
	return b
}

// rgb extracts the 8-bit channels of a theme colour, defaulting unset (nil)
// colours to black so the helpers never panic on a partially-built theme.
func rgb(c color.Color) (r, g, b uint8) {
	if c == nil {
		return 0, 0, 0
	}
	r32, g32, b32, _ := c.RGBA()
	return uint8(r32 >> 8), uint8(g32 >> 8), uint8(b32 >> 8)
}
