package tui

import (
	"context"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestNewRenderer_NoColorReturnsPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	r := newRenderer(Theme{Base: lipgloss.Color("#1e1e2e")})
	if _, ok := r.(PlainRenderer); !ok {
		t.Fatalf("newRenderer with NO_COLOR = %T, want PlainRenderer", r)
	}
}

func TestPlainRenderer_StripsHTMLComments(t *testing.T) {
	out, err := PlainRenderer{}.Render(context.Background(), "# Title\n<!-- owner: pm -->\nBody", 80)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := out; got == "" {
		t.Fatal("expected non-empty plain output")
	}
	if containsComment(out) {
		t.Errorf("plain output retained HTML comment: %q", out)
	}
}

func containsComment(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i] == '<' && s[i+1] == '!' && s[i+2] == '-' && s[i+3] == '-' {
			return true
		}
	}
	return false
}
