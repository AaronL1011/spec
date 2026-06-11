package config

import (
	"testing"
)

// tokenAliasVars enumerates every provider token variable that
// lookupEnvWithAlias treats as an alias pair. Token-alias tests must pin
// *both* sides of every pair so that a developer's ambient environment (a real
// exported SPEC_GITHUB_TOKEN / GITHUB_TOKEN, etc.) can never influence the
// outcome — nor leak into a failure message (AC-1, AC-2).
var tokenAliasVars = []string{
	"SPEC_GITHUB_TOKEN", "GITHUB_TOKEN",
	"SPEC_GITLAB_TOKEN", "GITLAB_TOKEN",
	"SPEC_BITBUCKET_TOKEN", "BITBUCKET_TOKEN",
	// AI_API_KEY has no alias but is checked by the "no alias" cases; pinning
	// it keeps those subtests independent of the ambient environment.
	"AI_API_KEY",
}

// pinTokenEnv clears every token variable to the empty string (which
// lookupNonEmptyEnv treats as unset) and then applies the provided overrides.
// Because t.Setenv restores prior values on cleanup, this fully isolates a
// subtest from whatever tokens the developer has exported. An empty override
// value is the "unset-equivalent" for the side of an alias pair under test.
func pinTokenEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	for _, v := range tokenAliasVars {
		t.Setenv(v, "")
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
}

// assertLookup compares a lookup result against expectations without ever
// echoing the resolved token value (AC-2). On mismatch it reports only the
// variable name, the boolean presence flags, and whether the value matched —
// never the value itself, which could be a real exported secret if isolation
// ever regressed.
func assertLookup(t *testing.T, varName, got string, ok bool, want string, wantOK bool) {
	t.Helper()
	if ok != wantOK {
		t.Errorf("lookupEnvWithAlias(%q): ok = %v, want %v", varName, ok, wantOK)
	}
	if got != want {
		t.Errorf("lookupEnvWithAlias(%q): resolved value mismatch (values redacted)", varName)
	}
}

func TestTokenEnvAlias(t *testing.T) {
	tests := []struct {
		name      string
		varName   string
		wantAlias string
		wantOK    bool
	}{
		{"spec prefixed maps to legacy", "SPEC_GITHUB_TOKEN", "GITHUB_TOKEN", true},
		{"legacy maps to spec prefixed", "GITHUB_TOKEN", "SPEC_GITHUB_TOKEN", true},
		{"gitlab spec to legacy", "SPEC_GITLAB_TOKEN", "GITLAB_TOKEN", true},
		{"bitbucket legacy to spec", "BITBUCKET_TOKEN", "SPEC_BITBUCKET_TOKEN", true},
		{"non-token var has no alias", "AI_API_KEY", "", false},
		{"non-token var with spec prefix", "SPEC_HOME", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alias, ok := tokenEnvAlias(tt.varName)
			if ok != tt.wantOK || alias != tt.wantAlias {
				t.Errorf("tokenEnvAlias(%q) = (%q, %v), want (%q, %v)",
					tt.varName, alias, ok, tt.wantAlias, tt.wantOK)
			}
		})
	}
}

func TestLookupEnvWithAlias(t *testing.T) {
	t.Run("exact var wins over alias", func(t *testing.T) {
		pinTokenEnv(t, map[string]string{
			"SPEC_GITHUB_TOKEN": "spec-token",
			"GITHUB_TOKEN":      "legacy-token",
		})
		got, ok := lookupEnvWithAlias("SPEC_GITHUB_TOKEN")
		assertLookup(t, "SPEC_GITHUB_TOKEN", got, ok, "spec-token", true)
	})

	t.Run("falls back to legacy when spec unset", func(t *testing.T) {
		// Both sides of the alias pair are pinned: SPEC_GITHUB_TOKEN is
		// explicitly empty (unset-equivalent) so the legacy var must win
		// regardless of what the developer has exported.
		pinTokenEnv(t, map[string]string{
			"SPEC_GITHUB_TOKEN": "",
			"GITHUB_TOKEN":      "legacy-token",
		})
		got, ok := lookupEnvWithAlias("SPEC_GITHUB_TOKEN")
		assertLookup(t, "SPEC_GITHUB_TOKEN", got, ok, "legacy-token", true)
	})

	t.Run("legacy config resolves from spec var", func(t *testing.T) {
		pinTokenEnv(t, map[string]string{
			"SPEC_GITHUB_TOKEN": "spec-token",
			"GITHUB_TOKEN":      "",
		})
		got, ok := lookupEnvWithAlias("GITHUB_TOKEN")
		assertLookup(t, "GITHUB_TOKEN", got, ok, "spec-token", true)
	})

	t.Run("unset with no alias match returns false", func(t *testing.T) {
		pinTokenEnv(t, map[string]string{"AI_API_KEY": ""})
		got, ok := lookupEnvWithAlias("AI_API_KEY")
		assertLookup(t, "AI_API_KEY", got, ok, "", false)
	})
}

func TestInterpolateEnvVars_TokenAlias(t *testing.T) {
	t.Run("legacy config resolves from spec env var", func(t *testing.T) {
		pinTokenEnv(t, map[string]string{
			"SPEC_GITHUB_TOKEN": "spec-token",
			"GITHUB_TOKEN":      "",
		})
		got := string(interpolateEnvVars([]byte("token: ${GITHUB_TOKEN}")))
		if got != "token: spec-token" {
			t.Errorf("interpolateEnvVars = %q, want %q", got, "token: spec-token")
		}
	})

	t.Run("unresolved var left literal", func(t *testing.T) {
		// Pin both sides of the NOPE_TOKEN alias pair so an ambient
		// SPEC_NOPE_TOKEN can't resolve the placeholder under test.
		pinTokenEnv(t, nil)
		t.Setenv("NOPE_TOKEN", "")
		t.Setenv("SPEC_NOPE_TOKEN", "")
		got := string(interpolateEnvVars([]byte("token: ${NOPE_TOKEN}")))
		if got != "token: ${NOPE_TOKEN}" {
			t.Errorf("interpolateEnvVars = %q, want literal passthrough", got)
		}
	})
}
