package tui

import "testing"

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
