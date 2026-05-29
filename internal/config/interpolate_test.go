package config

import (
	"testing"
)

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
		t.Setenv("SPEC_GITHUB_TOKEN", "spec-token")
		t.Setenv("GITHUB_TOKEN", "legacy-token")
		got, ok := lookupEnvWithAlias("SPEC_GITHUB_TOKEN")
		if !ok || got != "spec-token" {
			t.Errorf("lookupEnvWithAlias = (%q, %v), want (spec-token, true)", got, ok)
		}
	})

	t.Run("falls back to legacy when spec unset", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "legacy-token")
		got, ok := lookupEnvWithAlias("SPEC_GITHUB_TOKEN")
		if !ok || got != "legacy-token" {
			t.Errorf("lookupEnvWithAlias = (%q, %v), want (legacy-token, true)", got, ok)
		}
	})

	t.Run("legacy config resolves from spec var", func(t *testing.T) {
		t.Setenv("SPEC_GITHUB_TOKEN", "spec-token")
		got, ok := lookupEnvWithAlias("GITHUB_TOKEN")
		if !ok || got != "spec-token" {
			t.Errorf("lookupEnvWithAlias = (%q, %v), want (spec-token, true)", got, ok)
		}
	})

	t.Run("unset with no alias match returns false", func(t *testing.T) {
		got, ok := lookupEnvWithAlias("AI_API_KEY")
		if ok || got != "" {
			t.Errorf("lookupEnvWithAlias = (%q, %v), want (\"\", false)", got, ok)
		}
	})
}

func TestInterpolateEnvVars_TokenAlias(t *testing.T) {
	t.Run("legacy config resolves from spec env var", func(t *testing.T) {
		t.Setenv("SPEC_GITHUB_TOKEN", "spec-token")
		got := string(interpolateEnvVars([]byte("token: ${GITHUB_TOKEN}")))
		if got != "token: spec-token" {
			t.Errorf("interpolateEnvVars = %q, want %q", got, "token: spec-token")
		}
	})

	t.Run("unresolved var left literal", func(t *testing.T) {
		got := string(interpolateEnvVars([]byte("token: ${NOPE_TOKEN}")))
		if got != "token: ${NOPE_TOKEN}" {
			t.Errorf("interpolateEnvVars = %q, want literal passthrough", got)
		}
	})
}
