package tui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func TestMDRenderer_BasicContent(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	lines := r.render("Hello **world**")
	if len(lines) == 0 {
		t.Fatal("should produce output")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "world") {
		t.Errorf("should contain 'world', got: %q", joined)
	}
}

func TestMDRenderer_HeadingsRendered(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	lines := r.render("### Sub-heading\n\nSome text here.")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Sub-heading") {
		t.Error("should contain heading text")
	}
	if !strings.Contains(joined, "Some text") {
		t.Error("should contain paragraph text")
	}
}

func TestMDRenderer_CodeBlock(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	md := "```go\nfmt.Println(\"hello\")\n```"
	lines := r.render(md)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Println") {
		t.Error("should contain code content")
	}
}

func TestMDRenderer_List(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	md := "- Item one\n- Item two\n- Item three"
	lines := r.render(md)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Item one") {
		t.Errorf("should contain 'Item one', got: %q", joined)
	}
}

func TestMDRenderer_Table(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	md := "| Col A | Col B |\n|-------|-------|\n| 1     | 2     |"
	lines := r.render(md)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Col A") {
		t.Errorf("should contain 'Col A', got: %q", joined)
	}
}

func TestMDRenderer_EmptyContent(t *testing.T) {
	theme := ResolveTheme("catppuccin-mocha")
	r := newMDRenderer(theme, 80)

	lines := r.render("")
	if len(lines) != 0 {
		t.Errorf("empty content should produce no lines, got %d", len(lines))
	}
}

func TestStripHTMLComments(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<!-- owner: pm -->text", "text"},
		{"before<!-- comment -->after", "beforeafter"},
		{"no<!-- one --><!-- two -->end", "noend"},
		{"<!-- unterminated", ""},
	}
	for _, tt := range tests {
		got := stripHTMLComments(tt.input)
		if got != tt.want {
			t.Errorf("stripHTMLComments(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
