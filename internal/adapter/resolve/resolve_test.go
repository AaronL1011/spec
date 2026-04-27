package resolve

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
)

func TestAll_EmptyConfig_AllNoop(t *testing.T) {
	cfg := &config.TeamConfig{}
	reg, warnings := All(cfg)

	checks := []struct {
		name string
		ok   bool
	}{
		{"Comms", isNoop[noop.Comms](reg.Comms())},
		{"PM", isNoop[noop.PM](reg.PM())},
		{"Docs", isNoop[noop.Docs](reg.Docs())},
		{"Repo", isNoop[noop.Repo](reg.Repo())},
		{"Agent", isNoop[noop.Agent](reg.Agent())},
		{"Deploy", isNoop[noop.Deploy](reg.Deploy())},
		{"AI", isNoop[noop.AI](reg.AI())},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("expected noop %s", c.name)
		}
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty config, got %v", warnings)
	}
}

func isNoop[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

func TestAll_SlackMissingToken_Warning(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Comms = makeProvider("slack", nil)
	_, warnings := All(cfg)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "token not configured") {
		t.Errorf("expected token warning, got %q", warnings[0])
	}
}

func TestAll_GitHubRepo_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Repo = makeProvider("github", map[string]string{
		"token": "ghp_test",
		"owner": "my-org",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.Repo().(noop.Repo); ok {
		t.Error("expected GitHub repo, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_AnthropicAI_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.AI = makeProvider("anthropic", map[string]string{
		"token": "sk-ant-test",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.AI().(noop.AI); ok {
		t.Error("expected Anthropic AI, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_OllamaAI_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.AI = makeProvider("ollama", nil)
	reg, warnings := All(cfg)

	if _, ok := reg.AI().(noop.AI); ok {
		t.Error("expected Ollama AI, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_UnknownProvider_Warning(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.AI = makeProvider("deepseek", nil)
	reg, warnings := All(cfg)

	if _, ok := reg.AI().(noop.AI); !ok {
		t.Error("expected noop AI for unknown provider")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "deepseek") {
		t.Errorf("expected warning about deepseek, got %q", warnings[0])
	}
}

func TestAll_Jira_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.PM = makeProvider("jira", map[string]string{
		"base_url":    "https://myorg.atlassian.net",
		"project_key": "PLAT",
		"email":       "user@example.com",
		"token":       "api-token",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.PM().(noop.PM); ok {
		t.Error("expected Jira PM, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_Jira_MissingToken_Warning(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.PM = makeProvider("jira", map[string]string{
		"base_url": "https://myorg.atlassian.net",
	})
	_, warnings := All(cfg)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "base_url, project_key, email, and token required") {
		t.Errorf("expected token warning, got %v", warnings)
	}
}

func TestAll_Confluence_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Docs = makeProvider("confluence", map[string]string{
		"base_url":  "https://myorg.atlassian.net/wiki",
		"space_key": "ENG",
		"email":     "user@example.com",
		"token":     "api-token",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.Docs().(noop.Docs); ok {
		t.Error("expected Confluence Docs, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_Teams_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Comms = makeProvider("teams", map[string]string{
		"webhook_url": "https://outlook.webhook.office.com/...",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.Comms().(noop.Comms); ok {
		t.Error("expected Teams Comms, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_ClaudeCode_Resolves(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Agent = makeProvider("claude-code", nil)
	reg, warnings := All(cfg)

	if _, ok := reg.Agent().(noop.Agent); ok {
		t.Error("expected Claude Agent, got noop")
	}
	if !reg.Agent().SupportsMCP() {
		t.Error("Claude agent should support MCP")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_GitHubRepoFallsBackToSpecsRepoToken(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.SpecsRepo.Token = "ghp_specs"
	cfg.SpecsRepo.Owner = "my-org"
	cfg.Integrations.Repo = makeProvider("github", nil)

	reg, warnings := All(cfg)

	if _, ok := reg.Repo().(noop.Repo); ok {
		t.Error("expected GitHub repo from specs_repo fallback, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

// makeProvider creates a ProviderConfig for testing.
func makeProvider(provider string, extra map[string]string) config.ProviderConfig {
	p := config.ProviderConfig{Provider: provider}
	p.Extra = make(map[string]string)
	for k, v := range extra {
		p.Extra[k] = v
	}
	return p
}
