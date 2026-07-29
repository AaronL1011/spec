package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scaffoldedSpec(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-001.md")
	content := ScaffoldSpec("SPEC-001", "Test", "Ana", "C1", "direct")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBountyRoundTrip is AC-1: the full bounty record survives a write/read
// cycle through frontmatter, which is what makes the award durable in git.
func TestBountyRoundTrip(t *testing.T) {
	path := scaffoldedSpec(t)
	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.Bounty = &BountyState{
		GrantedBy: "aaron",
		GrantedAt: "2026-07-28T09:00:00Z",
		Reason:    "unblocks the billing migration",
		ClaimedBy: "priya",
		ClaimedAt: "2026-07-29T22:40:00Z",
		EarnedBy:  "priya",
		EarnedAt:  "2026-08-04T03:15:00Z",
	}
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounty == nil {
		t.Fatal("bounty did not survive the round trip")
	}
	if *got.Bounty != *meta.Bounty {
		t.Errorf("bounty = %+v, want %+v", *got.Bounty, *meta.Bounty)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"granted_by", "granted_at", "reason", "claimed_by", "earned_by", "earned_at"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("frontmatter missing %q:\n%s", key, data)
		}
	}
}

// TestBountyOmittedWhenAbsent is AC-13: specs written before the feature
// existed round-trip without gaining a bounty key.
func TestBountyOmittedWhenAbsent(t *testing.T) {
	path := scaffoldedSpec(t)
	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.HasBounty() {
		t.Error("a freshly scaffolded spec must not carry a bounty")
	}
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "bounty") {
		t.Errorf("expected no bounty key when absent, got:\n%s", data)
	}
}

// TestBountyPredicates covers the state questions every render and rule path
// asks, including the nil-receiver and nil-pointer cases.
func TestBountyPredicates(t *testing.T) {
	tests := []struct {
		name                             string
		meta                             *SpecMeta
		has, active, earned, claimedFlag bool
	}{
		{name: "nil meta", meta: nil},
		{name: "no bounty", meta: &SpecMeta{}},
		{name: "empty state is not a bounty", meta: &SpecMeta{Bounty: &BountyState{}}},
		{
			name:   "granted",
			meta:   &SpecMeta{Bounty: &BountyState{GrantedBy: "aaron", GrantedAt: "2026-07-28T09:00:00Z"}},
			has:    true,
			active: true,
		},
		{
			name:        "claimed",
			meta:        &SpecMeta{Bounty: &BountyState{GrantedAt: "2026-07-28T09:00:00Z", ClaimedBy: "priya"}},
			has:         true,
			active:      true,
			claimedFlag: true,
		},
		{
			name: "earned",
			meta: &SpecMeta{Bounty: &BountyState{
				GrantedAt: "2026-07-28T09:00:00Z", ClaimedBy: "priya",
				EarnedBy: "priya", EarnedAt: "2026-08-04T03:15:00Z",
			}},
			has:         true,
			earned:      true,
			claimedFlag: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.meta.HasBounty(); got != tt.has {
				t.Errorf("HasBounty() = %v, want %v", got, tt.has)
			}
			if got := tt.meta.BountyActive(); got != tt.active {
				t.Errorf("BountyActive() = %v, want %v", got, tt.active)
			}
			if got := tt.meta.BountyEarned(); got != tt.earned {
				t.Errorf("BountyEarned() = %v, want %v", got, tt.earned)
			}
			if got := tt.meta.BountyClaimed(); got != tt.claimedFlag {
				t.Errorf("BountyClaimed() = %v, want %v", got, tt.claimedFlag)
			}
		})
	}
}

func TestNormalizeHandle(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "@Ana", want: "ana"},
		{in: "  ana  ", want: "ana"},
		{in: "ANA", want: "ana"},
		{in: "", want: ""},
		{in: "@", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeHandle(tt.in); got != tt.want {
			t.Errorf("NormalizeHandle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
