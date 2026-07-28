package bounty

import (
	"errors"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

var testNow = time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

func enabledConfig() *config.BountyConfig {
	return &config.BountyConfig{Enabled: true}
}

// TestAuthorize_RoleGate is AC-2: only configured roles may grant, and a
// disabled feature is reported separately from a permission failure.
func TestAuthorize_RoleGate(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		cfg         *config.BountyConfig
		wantErr     error
		wantRoleErr bool
	}{
		{name: "disabled", role: "tl", cfg: &config.BountyConfig{}, wantErr: ErrDisabled},
		{name: "absent config", role: "tl", cfg: nil, wantErr: ErrDisabled},
		{name: "default allows tl", role: "tl", cfg: enabledConfig()},
		{name: "default allows pm", role: "pm", cfg: enabledConfig()},
		{name: "default rejects engineer", role: "engineer", cfg: enabledConfig(), wantRoleErr: true},
		{
			name:        "narrowed list rejects pm",
			role:        "pm",
			cfg:         &config.BountyConfig{Enabled: true, GrantableBy: []string{"tl"}},
			wantRoleErr: true,
		},
		{
			name: "widened list allows engineer",
			role: "engineer",
			cfg:  &config.BountyConfig{Enabled: true, GrantableBy: []string{"tl", "engineer"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Authorize(tt.role, tt.cfg)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Authorize(%q) = %v, want %v", tt.role, err, tt.wantErr)
				}
			case tt.wantRoleErr:
				var re *RoleError
				if !errors.As(err, &re) {
					t.Fatalf("Authorize(%q) = %v, want *RoleError", tt.role, err)
				}
				if len(re.Allowed) == 0 {
					t.Error("RoleError must name the allowed roles so the message is actionable")
				}
			default:
				if err != nil {
					t.Fatalf("Authorize(%q) = %v, want nil", tt.role, err)
				}
			}
		})
	}
}

// TestCheckCap is AC-3: the cap blocks at the limit and the error carries the
// specs the granter has to choose between.
func TestCheckCap(t *testing.T) {
	tests := []struct {
		name    string
		active  []string
		cfg     *config.BountyConfig
		wantErr bool
	}{
		{name: "under default cap", active: []string{"SPEC-1", "SPEC-2"}, cfg: enabledConfig()},
		{name: "at default cap", active: []string{"SPEC-1", "SPEC-2", "SPEC-3"}, cfg: enabledConfig(), wantErr: true},
		{name: "over default cap", active: []string{"SPEC-1", "SPEC-2", "SPEC-3", "SPEC-4"}, cfg: enabledConfig(), wantErr: true},
		{name: "none active", cfg: enabledConfig()},
		{
			name:    "explicit cap of one",
			active:  []string{"SPEC-1"},
			cfg:     &config.BountyConfig{Enabled: true, MaxActive: 1},
			wantErr: true,
		},
		{
			name:   "raised cap",
			active: []string{"SPEC-1", "SPEC-2", "SPEC-3"},
			cfg:    &config.BountyConfig{Enabled: true, MaxActive: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCap(tt.active, tt.cfg)
			if tt.wantErr != (err != nil) {
				t.Fatalf("CheckCap(%v) = %v, wantErr %v", tt.active, err, tt.wantErr)
			}
			if err == nil {
				return
			}
			var ce *CapError
			if !errors.As(err, &ce) {
				t.Fatalf("CheckCap error = %T, want *CapError", err)
			}
			if len(ce.Active) != len(tt.active) {
				t.Errorf("CapError.Active = %v, want %v", ce.Active, tt.active)
			}
		})
	}
}

// TestGrant_RequiresReasonAndStamps is AC-1: a grant records granter, time, and
// reason, and an empty reason is refused while reasons are required.
func TestGrant_RequiresReasonAndStamps(t *testing.T) {
	var meta markdown.SpecMeta
	if err := Grant(&meta, "aaron", "   ", true, testNow); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("Grant with blank reason = %v, want ErrReasonRequired", err)
	}
	if meta.HasBounty() {
		t.Fatal("a refused grant must not write a bounty")
	}

	if err := Grant(&meta, "aaron", "  unblocks billing  ", true, testNow); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if !meta.BountyActive() {
		t.Fatal("granted bounty should be active")
	}
	if meta.Bounty.GrantedBy != "aaron" {
		t.Errorf("GrantedBy = %q, want aaron", meta.Bounty.GrantedBy)
	}
	if meta.Bounty.Reason != "unblocks billing" {
		t.Errorf("Reason = %q, want trimmed reason", meta.Bounty.Reason)
	}
	if meta.Bounty.GrantedAt != "2026-07-28T09:00:00Z" {
		t.Errorf("GrantedAt = %q, want RFC3339 UTC stamp", meta.Bounty.GrantedAt)
	}
}

// TestGrant_ReasonOptionalWhenNotRequired covers require_reason: false.
func TestGrant_ReasonOptionalWhenNotRequired(t *testing.T) {
	var meta markdown.SpecMeta
	if err := Grant(&meta, "aaron", "", false, testNow); err != nil {
		t.Fatalf("Grant without reason = %v, want nil when reasons are optional", err)
	}
	if !meta.BountyActive() {
		t.Fatal("bounty should be active")
	}
}

// TestGrant_UpdateKeepsOriginalGrant is the re-grant path: only the reason
// changes, so the cap slot and the grant's provenance are untouched.
func TestGrant_UpdateKeepsOriginalGrant(t *testing.T) {
	var meta markdown.SpecMeta
	if err := Grant(&meta, "aaron", "first reason", true, testNow); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	later := testNow.Add(48 * time.Hour)
	if err := Grant(&meta, "priya", "sharper reason", true, later); err != nil {
		t.Fatalf("re-Grant: %v", err)
	}
	if meta.Bounty.Reason != "sharper reason" {
		t.Errorf("Reason = %q, want the updated reason", meta.Bounty.Reason)
	}
	if meta.Bounty.GrantedBy != "aaron" || meta.Bounty.GrantedAt != Stamp(testNow) {
		t.Errorf("re-grant must preserve the original grant: got %+v", meta.Bounty)
	}
}

// TestGrant_RefusedAfterEarn is AC-9: an earned award is immutable.
func TestGrant_RefusedAfterEarn(t *testing.T) {
	meta := earnedMeta(t)
	if err := Grant(meta, "aaron", "new reason", true, testNow); !errors.Is(err, ErrAlreadyEarned) {
		t.Fatalf("Grant on earned bounty = %v, want ErrAlreadyEarned", err)
	}
}

// TestClear covers every refusal path plus the happy one.
func TestClear(t *testing.T) {
	t.Run("no bounty", func(t *testing.T) {
		var meta markdown.SpecMeta
		if err := Clear("SPEC-1", &meta, false); !errors.Is(err, ErrNoBounty) {
			t.Fatalf("Clear = %v, want ErrNoBounty", err)
		}
	})

	t.Run("unclaimed clears", func(t *testing.T) {
		meta := grantedMeta(t)
		if err := Clear("SPEC-1", meta, false); err != nil {
			t.Fatalf("Clear: %v", err)
		}
		if meta.HasBounty() {
			t.Error("cleared bounty should be gone")
		}
	})

	t.Run("claimed needs force", func(t *testing.T) {
		meta := claimedMeta(t)
		err := Clear("SPEC-1", meta, false)
		var ce *ClaimedError
		if !errors.As(err, &ce) {
			t.Fatalf("Clear = %v, want *ClaimedError", err)
		}
		if ce.ClaimedBy != "priya" {
			t.Errorf("ClaimedError.ClaimedBy = %q, want priya", ce.ClaimedBy)
		}
		if !meta.HasBounty() {
			t.Error("a refused clear must leave the bounty in place")
		}
		if err := Clear("SPEC-1", meta, true); err != nil {
			t.Fatalf("forced Clear: %v", err)
		}
		if meta.HasBounty() {
			t.Error("forced clear should remove the bounty")
		}
	})

	t.Run("earned cannot be cleared", func(t *testing.T) {
		meta := earnedMeta(t)
		if err := Clear("SPEC-1", meta, true); !errors.Is(err, ErrAlreadyEarned) {
			t.Fatalf("Clear earned = %v, want ErrAlreadyEarned even with force", err)
		}
	})
}

// TestClaim covers AC-8 and the self-claim refusal.
func TestClaim(t *testing.T) {
	t.Run("no bounty records nothing", func(t *testing.T) {
		var meta markdown.SpecMeta
		if Claim(&meta, "priya", testNow) {
			t.Error("Claim on an unbountied spec should record nothing")
		}
	})

	t.Run("stamps claimant", func(t *testing.T) {
		meta := grantedMeta(t)
		if !Claim(meta, "priya", testNow) {
			t.Fatal("Claim should record the claimant")
		}
		if meta.Bounty.ClaimedBy != "priya" || meta.Bounty.ClaimedAt != Stamp(testNow) {
			t.Errorf("claim not stamped: %+v", meta.Bounty)
		}
	})

	t.Run("same claimant is a no-op", func(t *testing.T) {
		meta := claimedMeta(t)
		if Claim(meta, "@Priya", testNow.Add(time.Hour)) {
			t.Error("re-claiming by the same person (any spelling) should record nothing")
		}
	})

	t.Run("reassignment overwrites", func(t *testing.T) {
		meta := claimedMeta(t)
		later := testNow.Add(72 * time.Hour)
		if !Claim(meta, "sam", later) {
			t.Fatal("reassignment should record the new claimant")
		}
		if meta.Bounty.ClaimedBy != "sam" || meta.Bounty.ClaimedAt != Stamp(later) {
			t.Errorf("claimant not overwritten: %+v", meta.Bounty)
		}
	})

	t.Run("granter cannot claim own bounty", func(t *testing.T) {
		meta := grantedMeta(t)
		if !SelfClaim(meta, "@Aaron") {
			t.Error("SelfClaim should match the granter regardless of spelling")
		}
		if Claim(meta, "aaron", testNow) {
			t.Error("a granter claiming their own bounty must not be recorded")
		}
		if meta.Bounty.ClaimedBy != "" {
			t.Errorf("ClaimedBy = %q, want empty", meta.Bounty.ClaimedBy)
		}
	})

	t.Run("blank handle records nothing", func(t *testing.T) {
		meta := grantedMeta(t)
		if Claim(meta, "  ", testNow) {
			t.Error("a blank handle must not be recorded")
		}
	})
}

// TestEarn covers AC-9: only a claimed bounty is awarded, and only once.
func TestEarn(t *testing.T) {
	t.Run("unclaimed earns nothing", func(t *testing.T) {
		meta := grantedMeta(t)
		if Earn(meta, testNow) {
			t.Error("an unclaimed bounty has no one to award")
		}
		if meta.BountyEarned() {
			t.Error("no award should be recorded")
		}
	})

	t.Run("freezes claimant", func(t *testing.T) {
		meta := claimedMeta(t)
		at := testNow.Add(96 * time.Hour)
		if !Earn(meta, at) {
			t.Fatal("Earn should record the award")
		}
		if meta.Bounty.EarnedBy != "priya" || meta.Bounty.EarnedAt != Stamp(at) {
			t.Errorf("award not stamped: %+v", meta.Bounty)
		}
		if meta.BountyActive() {
			t.Error("an earned bounty is no longer active and must not hold a cap slot")
		}
	})

	t.Run("second earn is a no-op", func(t *testing.T) {
		meta := earnedMeta(t)
		first := meta.Bounty.EarnedAt
		if Earn(meta, testNow.Add(500*time.Hour)) {
			t.Error("an earned bounty must not be re-awarded")
		}
		if meta.Bounty.EarnedAt != first {
			t.Errorf("EarnedAt changed to %q, want immutable %q", meta.Bounty.EarnedAt, first)
		}
	})
}

// TestParseStamp treats a malformed stamp as absent rather than fatal.
func TestParseStamp(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "2026-07-28T09:00:00Z", want: true},
		{in: "  2026-07-28T09:00:00Z  ", want: true},
		{in: "2026-07-28", want: false},
		{in: "", want: false},
		{in: "yesterday", want: false},
	}
	for _, tt := range tests {
		if _, ok := ParseStamp(tt.in); ok != tt.want {
			t.Errorf("ParseStamp(%q) ok = %v, want %v", tt.in, ok, tt.want)
		}
	}
}

func grantedMeta(t *testing.T) *markdown.SpecMeta {
	t.Helper()
	meta := &markdown.SpecMeta{ID: "SPEC-1"}
	if err := Grant(meta, "aaron", "unblocks billing", true, testNow); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	return meta
}

func claimedMeta(t *testing.T) *markdown.SpecMeta {
	t.Helper()
	meta := grantedMeta(t)
	if !Claim(meta, "priya", testNow.Add(time.Hour)) {
		t.Fatal("Claim: expected the claim to be recorded")
	}
	return meta
}

func earnedMeta(t *testing.T) *markdown.SpecMeta {
	t.Helper()
	meta := claimedMeta(t)
	if !Earn(meta, testNow.Add(24*time.Hour)) {
		t.Fatal("Earn: expected the award to be recorded")
	}
	return meta
}

// earnedSpec builds a spec whose bounty was earned by who at earnedAt.
func earnedSpec(id, who, earnedAt string) markdown.SpecMeta {
	return markdown.SpecMeta{ID: id, Bounty: &markdown.BountyState{
		GrantedBy: "aaron", GrantedAt: "2026-07-01T09:00:00Z",
		ClaimedBy: who, EarnedBy: who, EarnedAt: earnedAt,
	}}
}

// TestTally_GroupsAndOrders is AC-10: the ledger groups by person, orders by
// count then handle, and carries the contributing spec IDs.
func TestTally_GroupsAndOrders(t *testing.T) {
	metas := []markdown.SpecMeta{
		earnedSpec("SPEC-003", "priya", "2026-08-04T03:15:00Z"),
		earnedSpec("SPEC-001", "@Priya", "2026-08-01T03:15:00Z"),
		earnedSpec("SPEC-002", "sam", "2026-08-02T03:15:00Z"),
		earnedSpec("SPEC-004", "ana", "2026-08-03T03:15:00Z"),
		{ID: "SPEC-005"}, // no bounty
		{ID: "SPEC-006", Bounty: &markdown.BountyState{ // granted, never earned
			GrantedBy: "aaron", GrantedAt: "2026-07-01T09:00:00Z", ClaimedBy: "sam",
		}},
	}

	awards := Tally(metas, time.Time{}, time.Time{})
	if len(awards) != 3 {
		t.Fatalf("awards = %+v, want three earners", awards)
	}
	if awards[0].Handle != "priya" || awards[0].Count != 2 {
		t.Errorf("top award = %+v, want priya with 2 (both spellings counted as one person)", awards[0])
	}
	if got := awards[0].SpecIDs; len(got) != 2 || got[0] != "SPEC-001" || got[1] != "SPEC-003" {
		t.Errorf("spec IDs = %v, want [SPEC-001 SPEC-003] in ID order", got)
	}
	// Ties break on handle, ascending.
	if awards[1].Handle != "ana" || awards[2].Handle != "sam" {
		t.Errorf("tie order = %q then %q, want ana then sam", awards[1].Handle, awards[2].Handle)
	}
}

// TestTally_WindowBounds covers the inclusive window and unbounded sides.
func TestTally_WindowBounds(t *testing.T) {
	metas := []markdown.SpecMeta{
		earnedSpec("SPEC-001", "ana", "2026-06-30T23:59:59Z"),
		earnedSpec("SPEC-002", "ana", "2026-07-01T00:00:00Z"),
		earnedSpec("SPEC-003", "ana", "2026-09-30T00:00:00Z"),
		earnedSpec("SPEC-004", "ana", "2026-10-01T00:00:01Z"),
	}
	q3start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	q3end := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name      string
		from, to  time.Time
		wantCount int
	}{
		{name: "all time", wantCount: 4},
		{name: "quarter", from: q3start, to: q3end, wantCount: 2},
		{name: "since only", from: q3start, wantCount: 3},
		{name: "until only", to: q3end, wantCount: 3},
		{name: "empty window", from: q3end, to: q3start, wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			awards := Tally(metas, tt.from, tt.to)
			got := 0
			if len(awards) > 0 {
				got = awards[0].Count
			}
			if got != tt.wantCount {
				t.Errorf("count = %d, want %d", got, tt.wantCount)
			}
		})
	}
}

// TestTally_SkipsUnusableStamps keeps a hand-edited spec from distorting the
// ledger instead of being counted at the zero time.
func TestTally_SkipsUnusableStamps(t *testing.T) {
	metas := []markdown.SpecMeta{
		earnedSpec("SPEC-001", "ana", "yesterday"),
		earnedSpec("SPEC-002", "ana", ""),
		{ID: "SPEC-003", Bounty: &markdown.BountyState{
			GrantedAt: "2026-07-01T09:00:00Z", EarnedAt: "2026-08-01T09:00:00Z", // earned_by missing
		}},
	}
	if awards := Tally(metas, time.Time{}, time.Time{}); len(awards) != 0 {
		t.Errorf("awards = %+v, want none from unusable records", awards)
	}
}

// TestTally_Empty is the "no awards yet" path every new team starts in.
func TestTally_Empty(t *testing.T) {
	if awards := Tally(nil, time.Time{}, time.Time{}); len(awards) != 0 {
		t.Errorf("Tally(nil) = %+v, want empty", awards)
	}
}
