package tui

import (
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
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
	r := &revertOverlay{reason: "naïve"}
	r.backspaceReason()
	if r.reason != "naïv" || !utf8.ValidString(r.reason) {
		t.Errorf("reason backspace = %q, want %q (valid=%v)", r.reason, "naïv", utf8.ValidString(r.reason))
	}

	r.reason = ""
	r.backspaceReason() // no-op, no panic
	if r.reason != "" {
		t.Errorf("empty reason backspace = %q, want empty", r.reason)
	}
}

func TestSpecListSearchBackspace_RuneSafe(t *testing.T) {
	m := newSpecList(nil, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	m.searchActive = true
	m.searchQuery = "café"

	m, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.searchQuery != "caf" || !utf8.ValidString(m.searchQuery) {
		t.Errorf("search backspace = %q, want %q (valid=%v)", m.searchQuery, "caf", utf8.ValidString(m.searchQuery))
	}
}
