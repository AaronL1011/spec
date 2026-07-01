package identity

import "testing"

func TestMatchesIdentity(t *testing.T) {
	v := Viewer{
		Name:       "Aaron Lewis",
		Handle:     "aaron",
		Identities: []string{"aaron", "Aaron Lewis", "AaronL1011", "@aaron"},
	}

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"matches canonical handle", "aaron", true},
		{"matches canonical handle with leading @", "@aaron", true},
		{"matches display name", "Aaron Lewis", true},
		{"matches display name case-insensitively", "aaron lewis", true},
		{"matches per-provider identity", "AaronL1011", true},
		{"matches per-provider identity case-insensitively", "aaronl1011", true},
		{"does not match someone else", "someone-else", false},
		{"empty candidate never matches", "", false},
		{"whitespace-only candidate never matches", "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesIdentity(tt.candidate, v); got != tt.want {
				t.Errorf("MatchesIdentity(%q) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestMatchesIdentity_EmptyViewerNeverMatches(t *testing.T) {
	if MatchesIdentity("anything", Viewer{}) {
		t.Error("MatchesIdentity against a zero-value Viewer should never match")
	}
}

func TestAnyIdentity(t *testing.T) {
	v := Viewer{Name: "Ana", Handle: "@ana"}

	tests := []struct {
		name       string
		candidates []string
		want       bool
	}{
		{"nil candidates", nil, false},
		{"empty candidates", []string{}, false},
		{"single match", []string{"@ana"}, true},
		{"match among several", []string{"@ben", "Ana", "@carlos"}, true},
		{"no match", []string{"@ben", "@carlos"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyIdentity(tt.candidates, v); got != tt.want {
				t.Errorf("AnyIdentity(%v) = %v, want %v", tt.candidates, got, tt.want)
			}
		})
	}
}
