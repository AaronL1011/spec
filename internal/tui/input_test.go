package tui

import (
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func TestDropLastRune(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii", "abc", "ab"},
		{"single ascii", "a", ""},
		{"multibyte tail", "café", "caf"},
		{"emoji tail", "hi👋", "hi"},
		{"cjk tail", "日本語", "日本"},
		{"single multibyte", "é", ""},
		{"combining-as-separate", "a\u0301", "a"}, // drops the combining accent
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dropLastRune(tt.in)
			if got != tt.want {
				t.Errorf("dropLastRune(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("dropLastRune(%q) = %q is not valid UTF-8", tt.in, got)
			}
		})
	}
}

// TestDropLastRune_RepeatedNeverCorrupts ensures that repeatedly deleting from
// a multi-byte string always leaves valid UTF-8 and eventually empties cleanly.
func TestDropLastRune_RepeatedNeverCorrupts(t *testing.T) {
	s := "café — 日本語 👋 naïve"
	for s != "" {
		s = dropLastRune(s)
		if !utf8.ValidString(s) {
			t.Fatalf("intermediate value %q is not valid UTF-8", s)
		}
	}
}

func TestIntakeBackspace_RuneSafe(t *testing.T) {
	f := &intakeFormState{}
	f.open()
	f.field = intakeFieldTitle
	f.title = "café"
	f.backspaceField()
	if f.title != "caf" {
		t.Errorf("title backspace = %q, want %q", f.title, "caf")
	}
	if !utf8.ValidString(f.title) {
		t.Errorf("title %q is not valid UTF-8", f.title)
	}

	f.field = intakeFieldSource
	f.source = "日本"
	f.backspaceField()
	if f.source != "日" || !utf8.ValidString(f.source) {
		t.Errorf("source backspace = %q, want %q (valid=%v)", f.source, "日", utf8.ValidString(f.source))
	}

	// Backspacing an empty field is a no-op and must not panic.
	f.source = ""
	f.backspaceField()
	if f.source != "" {
		t.Errorf("empty source backspace = %q, want empty", f.source)
	}
}

func TestRevertBackspace_RuneSafe(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline(), 80, ResolveTheme("catppuccin-mocha"))
	r.nextField() // focus reason
	r.reason.SetValue("naïve")
	r.reason.CursorEnd()

	r.updateReason(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if r.reasonText() != "naïv" || !utf8.ValidString(r.reasonText()) {
		t.Errorf("reason backspace = %q, want %q (valid=%v)", r.reasonText(), "naïv", utf8.ValidString(r.reasonText()))
	}

	r.reason.SetValue("")
	r.updateReason(tea.KeyPressMsg{Code: tea.KeyBackspace}) // no-op, no panic
	if r.reasonText() != "" {
		t.Errorf("empty reason backspace = %q, want empty", r.reasonText())
	}
}

// TestSearchOverlayBackspace_RuneSafe verifies the overlay's text input drops
// a full multi-byte rune on backspace rather than a single byte, leaving
// valid UTF-8. The bubbles textinput handles this natively; this test pins
// that contract so a future swap never regresses it.
func TestSearchOverlayBackspace_RuneSafe(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.input.SetValue("café")
	m.input.Focus()

	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.input.Value(); got != "caf" || !utf8.ValidString(got) {
		t.Errorf("search backspace = %q, want %q (valid=%v)", got, "caf", utf8.ValidString(got))
	}
}
