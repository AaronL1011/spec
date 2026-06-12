package awareness

import (
	"testing"
)

func TestSummary_HasItems(t *testing.T) {
	tests := []struct {
		name    string
		summary Summary
		want    bool
	}{
		{"empty", Summary{}, false},
		{"reviews only", Summary{ReviewsNeeded: 1}, true},
		{"blocked only", Summary{SpecsBlocked: 2}, true},
		{"in progress only", Summary{SpecsInProgress: 3}, false}, // in progress isn't "needs attention"
		{"both", Summary{ReviewsNeeded: 1, SpecsBlocked: 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.HasItems(); got != tt.want {
				t.Errorf("HasItems() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummary_OneLiner(t *testing.T) {
	tests := []struct {
		name    string
		summary Summary
		want    string
	}{
		{"empty", Summary{}, ""},
		{"1 review", Summary{ReviewsNeeded: 1}, "📥 1 review pending (spec list --mine)"},
		{"2 reviews", Summary{ReviewsNeeded: 2}, "📥 2 reviews pending (spec list --mine)"},
		{"1 blocked", Summary{SpecsBlocked: 1}, "📥 1 spec blocked (spec list --mine)"},
		{"both", Summary{ReviewsNeeded: 1, SpecsBlocked: 2}, "📥 1 review pending, 2 specs blocked (spec list --mine)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.OneLiner(); got != tt.want {
				t.Errorf("OneLiner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanReview(t *testing.T) {
	tests := []struct {
		name       string
		reviewers  []string
		identities []string
		userRole   string
		want       bool
	}{
		{"direct name", []string{"alice"}, []string{"Alice"}, "", true},
		{"handle match", []string{"@bob"}, []string{"Bob"}, "", true},
		{"role match", []string{"tl"}, []string{"Charlie"}, "tl", true},
		{"no match", []string{"tl"}, []string{"Dave"}, "engineer", false},
		{"empty reviewers", []string{}, []string{"Eve"}, "tl", false},
		{"multiple reviewers", []string{"pm", "tl"}, []string{"Frank"}, "tl", true},
		{"per-provider identity", []string{"AaronL1011"}, []string{"aaron", "AaronL1011"}, "engineer", true},
		{"reviewer @-prefixed against identity", []string{"@aaron"}, []string{"aaron", "AaronL1011"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canReview(tt.reviewers, tt.identities, tt.userRole); got != tt.want {
				t.Errorf("canReview(%v, %v, %q) = %v, want %v",
					tt.reviewers, tt.identities, tt.userRole, got, tt.want)
			}
		})
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "" {
		t.Errorf("plural(1) = %q, want empty", plural(1))
	}
	if plural(0) != "s" {
		t.Errorf("plural(0) = %q, want 's'", plural(0))
	}
	if plural(2) != "s" {
		t.Errorf("plural(2) = %q, want 's'", plural(2))
	}
}
