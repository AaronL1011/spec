package adapter

import "testing"

func TestNormalizeSeverity(t *testing.T) {
	cases := []struct {
		in   string
		want Severity
	}{
		{"critical", SeverityCritical},
		{"CRITICAL", SeverityCritical},
		{"high", SeverityHigh},
		{"medium", SeverityMedium},
		{"moderate", SeverityMedium}, // GitHub UI term for medium
		{"Moderate", SeverityMedium},
		{"low", SeverityLow},
		{" low ", SeverityLow},
		{"", SeverityUnknown},
		{"bogus", SeverityUnknown},
	}
	for _, c := range cases {
		if got := NormalizeSeverity(c.in); got != c.want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
