package glyph

import (
	"testing"
	"unicode/utf8"
)

// rowIcons are the glyphs that appear in a row of other icons, where relative
// visual weight matters. Spark is excluded deliberately: it is the standalone
// boot-splash mark and has no neighbours to be dwarfed by.
var rowIcons = map[string]string{
	"Focus": Focus, "Bounty": Bounty, "Active": Active, "Stale": Stale,
	"Blocked": Blocked, "Review": Review, "Discussion": Discussion,
	"Incoming": Incoming, "Pending": Pending, "Filled": Filled, "Open": Open,
	"Urgent": Urgent, "Bullet": Bullet, "Cursor": Cursor,
}

// TestRowIconsAreSingleRune keeps every row icon one rune, which is the
// precondition for it being one cell.
func TestRowIconsAreSingleRune(t *testing.T) {
	for name, g := range rowIcons {
		if got := utf8.RuneCountInString(g); got != 1 {
			t.Errorf("%s = %q is %d runes, want exactly 1", name, g, got)
		}
	}
}

// TestRowIconsAvoidDingbats guards the rule learned from the bounty marker: a
// Dingbat (U+2700–U+27BF) is drawn for a text run, not a cell, and mainstream
// programming fonts (JetBrains Mono, Caskaydia, Noto Sans Mono) do not cover
// the block at all. Both effects make a Dingbat render smaller — or from a
// different font entirely — than the Geometric Shapes it sits beside.
//
// ✓ (U+2713) and ✗ (U+2717) are grandfathered: they are read as text marks
// rather than status icons in a column, and no Geometric Shapes equivalent
// exists. Anything new belongs in U+25xx.
func TestRowIconsAvoidDingbats(t *testing.T) {
	grandfathered := map[rune]bool{'✓': true, '✗': true}
	for name, g := range rowIcons {
		r := []rune(g)[0]
		if r >= 0x2700 && r <= 0x27BF && !grandfathered[r] {
			t.Errorf("%s = %q is U+%04X, in the Dingbats block — prefer Geometric Shapes (U+25xx): "+
				"Dingbats are drawn at text size and are absent from common programming fonts", name, g, r)
		}
	}
}

// TestBountyIsGeometricShape pins the specific fix so a future refactor cannot
// quietly reintroduce a lightweight glyph for the bounty marker.
func TestBountyIsGeometricShape(t *testing.T) {
	r := []rune(Bounty)[0]
	if r < 0x25A0 || r > 0x25FF {
		t.Errorf("Bounty = %q (U+%04X), want a Geometric Shapes glyph so it matches the ink mass "+
			"and font coverage of the icons beside it", Bounty, r)
	}
	for name, g := range map[string]string{"Active": Active, "Review": Review, "Focus": Focus, "Open": Open} {
		if g == Bounty {
			t.Errorf("Bounty duplicates %s (%q) — status must stay distinguishable by shape alone", name, g)
		}
	}
}
