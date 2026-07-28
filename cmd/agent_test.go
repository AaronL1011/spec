package cmd

import "testing"

// formatLatency stays in cmd as a render helper; the preflight logic it once
// neighboured now lives in internal/agentcheck.

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{450, "450ms"},
		{1500, "1.5s"},
		{42000, "42.0s"},
	}
	for _, tt := range tests {
		if got := formatLatency(tt.ms); got != tt.want {
			t.Errorf("formatLatency(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
