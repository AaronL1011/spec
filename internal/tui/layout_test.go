package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestContentWidth(t *testing.T) {
	tests := []struct {
		total int
		want  int
	}{
		{total: 80, want: 80 - 2*Gutter},
		{total: 120, want: 120 - 2*Gutter},
		{total: 40, want: MinContent}, // below floor after gutter
		{total: 10, want: MinContent}, // tiny terminal clamps to floor
		{total: 0, want: MinContent},  // zero clamps to floor
		{total: MinContent + 2*Gutter, want: MinContent},
	}
	for _, tt := range tests {
		if got := ContentWidth(tt.total); got != tt.want {
			t.Errorf("ContentWidth(%d) = %d, want %d", tt.total, got, tt.want)
		}
	}
}

func TestIndent(t *testing.T) {
	if got := Indent(0); got != "" {
		t.Errorf("Indent(0) = %q, want empty", got)
	}
	if got := Indent(-1); got != "" {
		t.Errorf("Indent(-1) = %q, want empty", got)
	}
	if got := Indent(1); got != strings.Repeat(" ", IndentUnit) {
		t.Errorf("Indent(1) = %q, want %d spaces", got, IndentUnit)
	}
	if got := Indent(2); len(got) != 2*IndentUnit {
		t.Errorf("Indent(2) len = %d, want %d", len(got), 2*IndentUnit)
	}
}

func TestRuleLine(t *testing.T) {
	if got := RuleLine(0); got != "" {
		t.Errorf("RuleLine(0) = %q, want empty", got)
	}
	if got := RuleLine(-3); got != "" {
		t.Errorf("RuleLine(-3) = %q, want empty", got)
	}
	if got := RuleLine(5); lipgloss.Width(got) != 5 {
		t.Errorf("RuleLine(5) width = %d, want 5", lipgloss.Width(got))
	}
	if got := RuleLine(5); got != strings.Repeat(GlyphHRule, 5) {
		t.Errorf("RuleLine(5) = %q, want 5 rule glyphs", got)
	}
}

func TestHintStrip(t *testing.T) {
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	got := HintStrip(styles, Hint("n", "next"), Hint("p", "prev"))
	// Should contain the keys, labels, and the standard separator.
	for _, want := range []string{"n", "next", "p", "prev", "·"} {
		if !strings.Contains(got, want) {
			t.Errorf("HintStrip output missing %q: %q", want, got)
		}
	}
	// Empty pairs still yields the indent prefix without panicking.
	if got := HintStrip(styles); !strings.HasPrefix(got, Indent(1)) {
		t.Errorf("HintStrip() = %q, want indent prefix", got)
	}
}
