package config

import "testing"

// TestBountyConfig_Defaults covers the accessors every caller reads through:
// an absent block behaves as "off", and an enabled block with no tuning gets
// the documented defaults.
func TestBountyConfig_Defaults(t *testing.T) {
	var absent *BountyConfig
	if absent.IsEnabled() {
		t.Error("absent bounty config must read as disabled")
	}
	if got := absent.Cap(); got != DefaultBountyMaxActive {
		t.Errorf("Cap() = %d, want %d", got, DefaultBountyMaxActive)
	}
	if !absent.ReasonRequired() {
		t.Error("reasons must be required by default — the reason is the message")
	}
	if !absent.ShimmerEnabled() {
		t.Error("shimmer must be on by default")
	}
	if got := absent.GrantableRoles(); len(got) != 2 || got[0] != "tl" || got[1] != "pm" {
		t.Errorf("GrantableRoles() = %v, want [tl pm]", got)
	}
}

// TestBountyConfig_ExplicitFalseIsHonoured is why RequireReason and Shimmer are
// pointers: an explicit false must not read as an unset default of true.
func TestBountyConfig_ExplicitFalseIsHonoured(t *testing.T) {
	no := false
	cfg := &BountyConfig{Enabled: true, RequireReason: &no, Shimmer: &no}
	if cfg.ReasonRequired() {
		t.Error("require_reason: false must be honoured")
	}
	if cfg.ShimmerEnabled() {
		t.Error("shimmer: false must be honoured")
	}
}

func TestBountyConfig_CapAndRoles(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *BountyConfig
		wantCap int
		role    string
		allowed bool
	}{
		{name: "defaults", cfg: &BountyConfig{Enabled: true}, wantCap: 3, role: "tl", allowed: true},
		{name: "zero cap falls back", cfg: &BountyConfig{MaxActive: 0}, wantCap: 3, role: "pm", allowed: true},
		{name: "negative cap falls back", cfg: &BountyConfig{MaxActive: -4}, wantCap: 3, role: "engineer", allowed: false},
		{name: "explicit cap", cfg: &BountyConfig{MaxActive: 1}, wantCap: 1, role: "qa", allowed: false},
		{
			name:    "narrowed roles",
			cfg:     &BountyConfig{GrantableBy: []string{"tl"}},
			wantCap: 3, role: "pm", allowed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Cap(); got != tt.wantCap {
				t.Errorf("Cap() = %d, want %d", got, tt.wantCap)
			}
			if got := tt.cfg.IsRoleAllowed(tt.role); got != tt.allowed {
				t.Errorf("IsRoleAllowed(%q) = %v, want %v", tt.role, got, tt.allowed)
			}
		})
	}
}

// TestResolvedConfig_BountyEnabled keeps the read paths (dashboard, TUI, list)
// safe to call without presence checks.
func TestResolvedConfig_BountyEnabled(t *testing.T) {
	var nilRC *ResolvedConfig
	if nilRC.BountyEnabled() {
		t.Error("nil resolved config must read as disabled")
	}
	if nilRC.Bounties() == nil {
		t.Fatal("Bounties() must never return nil")
	}
	rc := &ResolvedConfig{Team: &TeamConfig{}}
	if rc.BountyEnabled() {
		t.Error("team config without a bounty block must read as disabled")
	}
	rc.Team.Bounty = &BountyConfig{Enabled: true}
	if !rc.BountyEnabled() {
		t.Error("enabled bounty block must read as enabled")
	}
}

// TestLint_BountyBlock is AC-3/AC-2 at the config layer: a cap that cannot
// enforce scarcity and a role that owns no stage are both errors with a
// suggestion.
func TestLint_BountyBlock(t *testing.T) {
	path := writeLintConfig(t, `version: "1"
pipeline:
  stages:
    - name: draft
      owner_role: pm
    - name: build
      owner_role: engineer
bounty:
  enabled: true
  grantable_by: [pm, tel]
  max_active: 0
`)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("LintTeamConfigFile: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected errors for an unknown role and a zero cap")
	}

	roleDiag := findDiag(res.Diagnostics, "bounty.grantable_by")
	if roleDiag == nil {
		t.Fatal("expected a bounty.grantable_by diagnostic")
	}
	if !stringContains(roleDiag.Message, "tel") {
		t.Errorf("diagnostic should name the offending role: %q", roleDiag.Message)
	}
	if !stringContains(roleDiag.Suggestion, "pm") {
		t.Errorf("diagnostic should suggest a real role: %q", roleDiag.Suggestion)
	}
	if roleDiag.Line == 0 {
		t.Error("diagnostic must carry a line number")
	}

	capDiag := findDiag(res.Diagnostics, "bounty.max_active")
	if capDiag == nil {
		t.Fatal("expected a bounty.max_active diagnostic")
	}
	if capDiag.Suggestion == "" {
		t.Error("cap diagnostic must suggest a fix")
	}
}

// TestLint_BountyBlock_Valid asserts a well-formed block is silent, and that a
// preset-based pipeline (no stage list) does not produce false role errors.
func TestLint_BountyBlock_Valid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "explicit stages",
			body: `version: "1"
pipeline:
  stages:
    - name: draft
      owner_role: pm
    - name: build
      owner_role: engineer
bounty:
  enabled: true
  grantable_by: [pm]
  max_active: 2
  require_reason: true
  shimmer: false
`,
		},
		{
			name: "preset pipeline skips role checking",
			body: `version: "1"
pipeline:
  preset: default
bounty:
  enabled: true
  grantable_by: [tl, pm]
`,
		},
		{
			name: "absent block",
			body: `version: "1"
pipeline:
  preset: default
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := LintTeamConfigFile(writeLintConfig(t, tt.body))
			if err != nil {
				t.Fatalf("LintTeamConfigFile: %v", err)
			}
			if d := findDiag(res.Diagnostics, "bounty"); d != nil {
				t.Errorf("unexpected bounty diagnostic: %+v", *d)
			}
		})
	}
}

// TestLint_BountyGrantableByWrongShape catches `grantable_by: tl` (a scalar
// where a list belongs), which YAML would otherwise accept silently.
func TestLint_BountyGrantableByWrongShape(t *testing.T) {
	res, err := LintTeamConfigFile(writeLintConfig(t, `version: "1"
pipeline:
  preset: default
bounty:
  enabled: true
  grantable_by: tl
`))
	if err != nil {
		t.Fatalf("LintTeamConfigFile: %v", err)
	}
	d := findDiag(res.Diagnostics, "bounty.grantable_by")
	if d == nil {
		t.Fatal("expected a shape diagnostic for a scalar grantable_by")
	}
	if d.Severity != SeverityError {
		t.Errorf("severity = %s, want error", d.Severity)
	}
}

func TestParseBountyFinish(t *testing.T) {
	tests := []struct {
		in    string
		want  BountyFinish
		valid bool
	}{
		{in: "", want: BountyFinishGold, valid: true},
		{in: "gold", want: BountyFinishGold, valid: true},
		{in: "GOLD", want: BountyFinishGold, valid: true},
		{in: "  platinum  ", want: BountyFinishPlatinum, valid: true},
		{in: "prismatic", want: BountyFinishPrismatic, valid: true},
		{in: "titanium", want: BountyFinishGold, valid: false},
	}
	for _, tt := range tests {
		got, ok := ParseBountyFinish(tt.in)
		if got != tt.want || ok != tt.valid {
			t.Errorf("ParseBountyFinish(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.valid)
		}
	}
}

// TestBountyConfig_MetalFinish keeps rendering safe: an unrecognised finish
// falls back to gold rather than failing, because lint already reports it and a
// bad config must never blank the marker.
func TestBountyConfig_MetalFinish(t *testing.T) {
	var absent *BountyConfig
	if got := absent.MetalFinish(); got != BountyFinishGold {
		t.Errorf("absent config finish = %v, want gold", got)
	}
	if got := (&BountyConfig{Finish: "prismatic"}).MetalFinish(); got != BountyFinishPrismatic {
		t.Errorf("finish = %v, want prismatic", got)
	}
	if got := (&BountyConfig{Finish: "unobtainium"}).MetalFinish(); got != BountyFinishGold {
		t.Errorf("unknown finish = %v, want a gold fallback", got)
	}
}

// TestLint_BountyFinish reports an unknown finish with the valid list, since
// there is no close spelling to suggest for an invented metal.
func TestLint_BountyFinish(t *testing.T) {
	res, err := LintTeamConfigFile(writeLintConfig(t, `version: "1"
pipeline:
  preset: default
bounty:
  enabled: true
  finish: titanium
`))
	if err != nil {
		t.Fatalf("LintTeamConfigFile: %v", err)
	}
	d := findDiag(res.Diagnostics, "bounty.finish")
	if d == nil {
		t.Fatal("expected a bounty.finish diagnostic")
	}
	if !stringContains(d.Suggestion, "prismatic") {
		t.Errorf("suggestion = %q, want the valid finishes listed", d.Suggestion)
	}
	for _, valid := range []string{"gold", "platinum", "prismatic"} {
		clean, err := LintTeamConfigFile(writeLintConfig(t, `version: "1"
pipeline:
  preset: default
bounty:
  enabled: true
  finish: `+valid+"\n"))
		if err != nil {
			t.Fatalf("LintTeamConfigFile(%s): %v", valid, err)
		}
		if d := findDiag(clean.Diagnostics, "bounty.finish"); d != nil {
			t.Errorf("finish %q reported: %+v", valid, *d)
		}
	}
}
