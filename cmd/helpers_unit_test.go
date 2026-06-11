package cmd

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestSplitConfigList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "alpha", []string{"alpha"}},
		{"comma separated", "a, b ,c", []string{"a", "b", "c"}},
		{"newline separated", "a\nb\n c ", []string{"a", "b", "c"}},
		{"mixed with blanks", "a,,\n,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitConfigList(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitConfigList(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitConfigList(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildEngineOptions_Defaults(t *testing.T) {
	rc := &config.ResolvedConfig{}
	opts := buildEngineOptions(rc, true)
	if !opts.Headless {
		t.Error("expected Headless=true")
	}
	if len(opts.SkillRefs) != 0 {
		t.Errorf("expected no skill refs, got %v", opts.SkillRefs)
	}
}

func TestBuildEngineOptions_TeamAndUserOverrides(t *testing.T) {
	team := &config.TeamConfig{}
	team.Build.Router = "team-router"
	team.Build.Strategy = "team-strategy"
	team.Integrations.Agent = config.ProviderConfig{
		Provider: "pi",
		Extra: map[string]string{
			"skill":        "s1, s2",
			"router":       "user-router",
			"test_command": "go test ./...",
		},
	}
	rc := &config.ResolvedConfig{Team: team}

	opts := buildEngineOptions(rc, false)
	if opts.Headless {
		t.Error("expected Headless=false")
	}
	if len(opts.SkillRefs) != 2 {
		t.Errorf("expected 2 skill refs, got %v", opts.SkillRefs)
	}
	if opts.TestCommand != "go test ./..." {
		t.Errorf("TestCommand = %q, want 'go test ./...'", opts.TestCommand)
	}
	// Per-user/agent router overrides the team default.
	if opts.Router != "user-router" {
		t.Errorf("Router = %q, want 'user-router'", opts.Router)
	}
	if opts.Strategy != "team-strategy" {
		t.Errorf("Strategy = %q, want 'team-strategy'", opts.Strategy)
	}
}

func TestNormalizeSpecID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"spec-001", "SPEC-001"},
		{" SPEC-002 ", "SPEC-002"},
		{"Spec-003", "SPEC-003"},
	}
	for _, tt := range tests {
		if got := normalizeSpecID(tt.in); got != tt.want {
			t.Errorf("normalizeSpecID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
