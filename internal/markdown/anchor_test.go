package markdown

import "testing"

func TestResolveAnchor(t *testing.T) {
	body := "The gate can require review.\n\nRetries are capped at three.\nThe gate can require review.\n\n- item one\n- item two with **bold** text\n"

	tests := []struct {
		name     string
		quote    string
		prefix   string
		found    bool
		wantLine int
	}{
		{
			name:     "exact match",
			quote:    "Retries are capped at three.",
			found:    true,
			wantLine: 2,
		},
		{
			name:     "whitespace reflowed match",
			quote:    "Retries   are\ncapped at three.",
			found:    true,
			wantLine: 2,
		},
		{
			name:     "markdown emphasis tolerated",
			quote:    "item two with bold text",
			found:    true,
			wantLine: 6,
		},
		{
			name:     "duplicate disambiguated by prefix",
			quote:    "The gate can require review.",
			prefix:   "capped at three.",
			found:    true,
			wantLine: 3,
		},
		{
			name:  "duplicate without prefix is ambiguous",
			quote: "The gate can require review.",
			found: false,
		},
		{
			name:  "absent quote is a graceful miss",
			quote: "This text does not exist anywhere.",
			found: false,
		},
		{
			name:  "empty quote is a miss",
			quote: "   ",
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAnchor(body, tt.quote, tt.prefix)
			if got.Found != tt.found {
				t.Fatalf("Found = %v, want %v", got.Found, tt.found)
			}
			if got.Found && got.Line != tt.wantLine {
				t.Errorf("Line = %d, want %d", got.Line, tt.wantLine)
			}
		})
	}
}

func TestResolveAnchor_PrefixMissIsAmbiguous(t *testing.T) {
	body := "alpha beta\ngamma\nalpha beta\n"
	got := ResolveAnchor(body, "alpha beta", "nonexistent prefix")
	if got.Found || !got.Ambiguous {
		t.Errorf("expected visible ambiguous miss, got %+v", got)
	}
}
