package config

import (
	"testing"
	"time"
)

func TestSecurityConfig_SLAFor_Defaults(t *testing.T) {
	var sc SecurityConfig // all-empty ⇒ built-in defaults
	cases := []struct {
		severity string
		want     time.Duration
	}{
		{"critical", 24 * time.Hour},
		{"high", 7 * 24 * time.Hour},
		{"medium", 14 * 24 * time.Hour},
		{"low", 30 * 24 * time.Hour},
		{"CRITICAL", 24 * time.Hour}, // case-insensitive
		{" high ", 7 * 24 * time.Hour},
	}
	for _, c := range cases {
		got, ok := sc.SLAFor(c.severity)
		if !ok {
			t.Errorf("SLAFor(%q) ok = false, want true", c.severity)
			continue
		}
		if got != c.want {
			t.Errorf("SLAFor(%q) = %v, want %v", c.severity, got, c.want)
		}
	}

	if _, ok := sc.SLAFor("bogus"); ok {
		t.Error("SLAFor(bogus) ok = true, want false for unrecognised severity")
	}
}

func TestSecurityConfig_SLAFor_Override(t *testing.T) {
	sc := SecurityConfig{SLA: SecuritySLA{High: "3d", Low: "notaduration"}}

	if got, _ := sc.SLAFor("high"); got != 3*24*time.Hour {
		t.Errorf("SLAFor(high) = %v, want 72h (configured override)", got)
	}
	// Unparseable override falls back to the built-in default.
	if got, _ := sc.SLAFor("low"); got != 30*24*time.Hour {
		t.Errorf("SLAFor(low) = %v, want 720h (default after bad override)", got)
	}
	// Untouched tier still returns its default.
	if got, _ := sc.SLAFor("critical"); got != 24*time.Hour {
		t.Errorf("SLAFor(critical) = %v, want 24h default", got)
	}
}

func TestSecurityConfig_SurfaceWindow(t *testing.T) {
	if got := (SecurityConfig{}).SurfaceWindow(); got != 24*time.Hour {
		t.Errorf("default SurfaceWindow = %v, want 24h", got)
	}
	if got := (SecurityConfig{DashboardSurfaceWithin: "12h"}).SurfaceWindow(); got != 12*time.Hour {
		t.Errorf("SurfaceWindow(12h) = %v, want 12h", got)
	}
	if got := (SecurityConfig{DashboardSurfaceWithin: "nope"}).SurfaceWindow(); got != 24*time.Hour {
		t.Errorf("SurfaceWindow(bad) = %v, want 24h fallback", got)
	}
}

func TestSecurityScopeOrDefault(t *testing.T) {
	cases := map[string]string{
		"":      "org",
		"org":   "org",
		"repo":  "repo",
		"REPO":  "repo",
		" repo ": "repo",
		"weird": "org",
	}
	for in, want := range cases {
		if got := SecurityScopeOrDefault(in); got != want {
			t.Errorf("SecurityScopeOrDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecurity_CategoryRouting(t *testing.T) {
	rc := &ResolvedConfig{Team: &TeamConfig{}}
	rc.Team.Integrations.Security = ProviderConfig{Provider: "dependabot"}

	if got := rc.IntegrationProvider("security"); got != "dependabot" {
		t.Errorf("IntegrationProvider(security) = %q, want dependabot", got)
	}
	if !rc.HasIntegration("security") {
		t.Error("HasIntegration(security) = false, want true")
	}

	rc.Team.Integrations.Security = ProviderConfig{Provider: "none"}
	if rc.HasIntegration("security") {
		t.Error("HasIntegration(security) = true for 'none', want false")
	}

	nilTeam := &ResolvedConfig{}
	if nilTeam.HasIntegration("security") {
		t.Error("HasIntegration(security) = true with nil team, want false")
	}
}

func TestLint_SecurityConfig(t *testing.T) {
	body := `version: "1"
integrations:
  security:
    provider: dependabott
    scope: organization
security:
  sla:
    high: 3x
  dashboard_surface_within: soon
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected errors for bad security config")
	}
	for _, field := range []string{
		"integrations.security.provider",
		"integrations.security.scope",
		"security.sla.high",
		"security.dashboard_surface_within",
	} {
		if findDiag(res.Diagnostics, field) == nil {
			t.Errorf("expected a diagnostic for %s; got %+v", field, res.Diagnostics)
		}
	}
}

func TestLint_SecurityConfig_Valid(t *testing.T) {
	body := `version: "1"
integrations:
  security:
    provider: dependabot
    scope: org
security:
  sla:
    critical: 1d
    high: 1w
    medium: 2w
    low: 30d
  dashboard_surface_within: 12h
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	for _, field := range []string{"security", "integrations.security"} {
		if d := findDiag(res.Diagnostics, field); d != nil {
			t.Errorf("valid security config produced diagnostic for %s: %+v", field, *d)
		}
	}
}
