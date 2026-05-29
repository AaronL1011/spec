package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// allGlyphs is every glyph constant the TUI may render. Keep in sync with
// icons.go / glyph.go — the width test below is the guard against any glyph
// that secretly occupies two cells (the failure mode emoji introduced).
func allGlyphs() map[string]string {
	g := map[string]string{
		"IconFocus":     IconFocus,
		"IconActive":    IconActive,
		"IconStale":     IconStale,
		"IconBlocked":   IconBlocked,
		"IconReview":    IconReview,
		"IconIncoming":  IconIncoming,
		"IconDone":      IconDone,
		"IconRejected":  IconRejected,
		"IconChanges":   IconChanges,
		"IconPending":   IconPending,
		"IconFilled":    IconFilled,
		"IconOpen":      IconOpen,
		"IconBullet":    IconBullet,
		"IconCursor":    IconCursor,
		"IconCaret":     IconCaret,
		"IconClock":     IconClock,
		"IconToastOK":   IconToastOK,
		"IconToastErr":  IconToastErr,
		"IconToastInfo": IconToastInfo,
		"GlyphVSep":     GlyphVSep,
		"GlyphHRule":    GlyphHRule,
		"GlyphSection":  GlyphSection,
	}
	for i, s := range StageIcons {
		g["StageIcons["+string(rune('0'+i%10))+"]"] = s
	}
	return g
}

// TestIcons_MonoWidth asserts every glyph is exactly one terminal cell wide
// (AC-3). Emoji and variation-selector sequences would fail here.
func TestIcons_MonoWidth(t *testing.T) {
	for name, g := range allGlyphs() {
		if w := lipgloss.Width(g); w != 1 {
			t.Errorf("glyph %s = %q has width %d, want 1", name, g, w)
		}
	}
	for i, f := range glyph.SpinnerFrames {
		if w := lipgloss.Width(f); w != 1 {
			t.Errorf("spinner frame %d = %q has width %d, want 1", i, f, w)
		}
	}
}

// TestIcons_NoEmoji asserts no glyph carries an emoji-range code point (AC-1).
func TestIcons_NoEmoji(t *testing.T) {
	for name, g := range allGlyphs() {
		for _, r := range g {
			if isEmojiRune(r) {
				t.Errorf("glyph %s = %q contains emoji rune %U", name, g, r)
			}
		}
	}
}

// isEmojiRune flags code points that render as full-colour, double-width
// emoji. It deliberately does NOT flag text-presentation symbols (★ ✔ ✘ …)
// from the dingbats block, which render as single-cell glyphs unless followed
// by VS16 (U+FE0F) — that selector is flagged so any emoji-presentation
// sequence is still caught.
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // supplementary pictographs / emoji
		return true
	case r == 0xFE0F: // variation selector-16 (emoji presentation)
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators
		return true
	default:
		return false
	}
}

// TestIcons_StatusShapesDistinct asserts core statuses use distinct shapes so
// they remain differentiable without colour (AC-8 / US-6).
func TestIcons_StatusShapesDistinct(t *testing.T) {
	statuses := map[string]string{
		"active":  IconActive,
		"blocked": IconBlocked,
		"done":    IconDone,
		"failed":  IconRejected,
		"open":    IconOpen,
	}
	seen := map[string]string{}
	for name, g := range statuses {
		if prev, ok := seen[g]; ok {
			t.Errorf("status %q and %q share glyph %q — not distinguishable without colour", prev, name, g)
		}
		seen[g] = name
	}
}

// TestStageIconAt_Fallback covers out-of-range positions.
func TestStageIconAt_Fallback(t *testing.T) {
	if got := StageIconAt(-1); got != IconOpen {
		t.Errorf("StageIconAt(-1) = %q, want IconOpen", got)
	}
	if got := StageIconAt(len(StageIcons) + 5); got != IconOpen {
		t.Errorf("StageIconAt(oob) = %q, want IconOpen", got)
	}
	if got := StageIconAt(0); got != StageIcons[0] {
		t.Errorf("StageIconAt(0) = %q, want %q", got, StageIcons[0])
	}
}
