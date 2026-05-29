package tui

import "strings"

// This file is the single source of truth for spacing and layout metrics.
// No view may inline content-width offsets ("width - 4"), indent literals
// ("  "/"    "), or hand-rolled rule lines; they all derive from here so
// every view shares one spacing system and switching views never shifts the
// content gutter.

const (
	// Gutter is the left/right content padding in cells. The prevailing
	// convention was width-4 (two cells each side), which this formalises.
	Gutter = 2

	// IndentUnit is one indentation step in cells. Nesting is Indent(1),
	// Indent(2), etc.
	IndentUnit = 2

	// MinContent is the floor for derived content width, uniform across all
	// views (some previously floored at 30, others at 40).
	MinContent = 40
)

// ContentWidth derives the usable content width from the total terminal
// width: total minus the gutter on both sides, clamped to MinContent.
func ContentWidth(total int) int {
	w := total - 2*Gutter
	if w < MinContent {
		return MinContent
	}
	return w
}

// Indent returns n indentation units as spaces. Indent(0) is empty.
func Indent(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n*IndentUnit)
}

// RuleLine returns a horizontal rule of the given width using the standard
// rule glyph. A non-positive width yields an empty string.
func RuleLine(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(GlyphHRule, width)
}

// HintPair is a single footer hint: a key and its label.
type HintPair struct {
	Key   string
	Label string
}

// Hint constructs a HintPair.
func Hint(key, label string) HintPair {
	return HintPair{Key: key, Label: label}
}

// HintStrip formats footer key hints uniformly. The key is rendered in the
// accent style and the label in the muted style, with pairs joined by " · ".
// The result is prefixed by one indent unit so every footer aligns with the
// standard content gutter.
func HintStrip(styles Styles, pairs ...HintPair) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, styles.Accent.Render(p.Key)+" "+styles.Muted.Render(p.Label))
	}
	return Indent(1) + strings.Join(parts, styles.Muted.Render(" · "))
}
