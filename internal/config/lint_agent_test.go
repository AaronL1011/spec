package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lint is where the cutover fails loudly: resolve only warns, so a hard error
// belongs in the command whose job is to report config problems.
func TestLint_RemovedIntegrationKeys_AreErrors(t *testing.T) {
	path := writeLintConfig(t, `
version: "1"
specs_repo:
  provider: github
  owner: acme
  repo: specs
integrations:
  agent:
    provider: claude-code
  ai:
    provider: anthropic
`)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("LintTeamConfigFile: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("removed keys should be lint errors")
	}

	var sawAgent, sawAI bool
	for _, d := range res.Diagnostics {
		switch d.Field {
		case "integrations.agent":
			sawAgent = true
			if !strings.Contains(d.Suggestion, "~/.spec/config.yaml") {
				t.Error("agent diagnostic should point at personal config")
			}
			if !strings.Contains(d.Suggestion, "provider:") {
				t.Error("agent diagnostic should include the replacement YAML block")
			}
		case "integrations.ai":
			sawAI = true
		}
	}
	if !sawAgent || !sawAI {
		t.Errorf("expected diagnostics for both removed keys, got %+v", res.Diagnostics)
	}
}

// The command that explains the fix must keep working: a config carrying removed
// keys still lints (and reports) rather than failing to load.
func TestLint_StillRunsWithRemovedKeys(t *testing.T) {
	path := writeLintConfig(t, `
version: "1"
integrations:
  agent:
    provider: pi
`)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint must not fail on removed keys: %v", err)
	}
	if len(res.Diagnostics) == 0 {
		t.Error("expected the removed key to be reported")
	}
}

func TestLint_CleanConfigHasNoAgentDiagnostics(t *testing.T) {
	path := writeLintConfig(t, `
version: "1"
integrations:
  pm:
    provider: jira
`)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("LintTeamConfigFile: %v", err)
	}
	for _, d := range res.Diagnostics {
		if strings.HasPrefix(d.Field, "integrations.agent") || strings.HasPrefix(d.Field, "integrations.ai") {
			t.Errorf("unexpected agent diagnostic on a clean config: %+v", d)
		}
	}
}

func writeUserLintFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintUserConfig_RenamedPreference(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
preferences:
  ai_drafts: true
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("the renamed key should be an error")
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Field == "preferences.ai_drafts" {
			found = true
			if !strings.Contains(d.Suggestion, "agent_drafts") {
				t.Errorf("suggestion should name the new spelling, got %q", d.Suggestion)
			}
		}
	}
	if !found {
		t.Errorf("expected a diagnostic for ai_drafts, got %+v", res.Diagnostics)
	}
}

// A setting that looks effective but is not is worse than one that is absent.
func TestLintUserConfig_InertMaxTokensForHarness(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
agent:
  provider: pi
  generate:
    max_tokens: 2048
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Field == "agent.generate.max_tokens" {
			found = true
			if d.Severity != SeverityWarning {
				t.Errorf("inert setting should warn, not error; got %s", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected max_tokens to be reported inert for a harness, got %+v", res.Diagnostics)
	}
}

func TestLintUserConfig_MaxTokensValidForAPIProvider(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
agent:
  provider: anthropic
  generate:
    max_tokens: 2048
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Field == "agent.generate.max_tokens" {
			t.Errorf("max_tokens is honoured by completion-API providers, should not be flagged: %+v", d)
		}
	}
}

func TestLintUserConfig_OpenAICompatibleNeedsBaseURL(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
agent:
  provider: openai-compatible
  generate:
    model: some-model
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	if !res.HasErrors() {
		t.Error("a missing base_url should be an error for the generic provider")
	}
}

func TestLintUserConfig_LiteralTokenWarns(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
agent:
  provider: openai-compatible
  generate:
    base_url: https://gateway/v1
    token: sk-literal
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Field == "agent.generate.token" {
			found = true
		}
	}
	if !found {
		t.Errorf("a literal token should be flagged, got %+v", res.Diagnostics)
	}
}

func TestLintUserConfig_EnvRefTokenIsClean(t *testing.T) {
	path := writeUserLintFile(t, `
user:
  owner_role: engineer
agent:
  provider: openai-compatible
  generate:
    base_url: https://gateway/v1
    token: ${SPEC_LLM_TOKEN}
`)
	res, err := LintUserConfigFile(path)
	if err != nil {
		t.Fatalf("LintUserConfigFile: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Field == "agent.generate.token" {
			t.Errorf("an env reference is the recommended form, should not be flagged: %+v", d)
		}
	}
}
