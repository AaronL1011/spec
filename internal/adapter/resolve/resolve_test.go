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
		{"Agent", isNoop[noop.Agent](reg.Agent())}, // agent is personal config; All() always installs noop
		{"Deploy", isNoop[noop.Deploy](reg.Deploy())},
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

func TestAll_UnknownProvider_Warning(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Comms = makeProvider("deepseek", nil)
	reg, warnings := All(cfg)

	if _, ok := reg.Comms().(noop.Comms); !ok {
		t.Error("expected noop comms for unknown provider")
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
		"base_url":       "https://myorg.atlassian.net/wiki",
		"space_key":      "ENG",
		"parent_page_id": "123456",
		"email":          "user@example.com",
		"token":          "api-token",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.Docs().(noop.Docs); ok {
		t.Error("expected Confluence Docs, got noop")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAll_Confluence_MissingParent_Disabled(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Docs = makeProvider("confluence", map[string]string{
		"base_url":  "https://myorg.atlassian.net/wiki",
		"space_key": "ENG",
		"email":     "user@example.com",
		"token":     "api-token",
	})
	reg, warnings := All(cfg)

	if _, ok := reg.Docs().(noop.Docs); !ok {
		t.Error("expected noop Docs when parent_page_id is missing")
	}
	if len(warnings) == 0 {
		t.Error("expected a warning about the missing parent_page_id")
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

func TestAgent_ResolvesProviders(t *testing.T) {
	tests := []struct {
		provider string
		wantNoop bool
		wantMCP  bool
	}{
		{"pi", false, true},
		{"claude-code", false, true},
		{"", true, false},
		{"none", true, false},
		{"cursor", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			agent, _ := Agent(makeProvider(tt.provider, nil))
			_, isNoop := agent.(noop.Agent)
			if isNoop != tt.wantNoop {
				t.Errorf("provider %q: isNoop = %v, want %v", tt.provider, isNoop, tt.wantNoop)
			}
			if agent.Capabilities().MCP != tt.wantMCP {
				t.Errorf("provider %q: MCP = %v, want %v", tt.provider, agent.Capabilities().MCP, tt.wantMCP)
			}
		})
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

// The agent is personal config, so All() must install the noop regardless of
// what a team config says. cmd.buildRegistry overlays the user's agent.
func TestAll_IgnoresTeamAgentConfig(t *testing.T) {
	cfg := &config.TeamConfig{}
	reg, warnings := All(cfg)

	if _, ok := reg.Agent().(noop.Agent); !ok {
		t.Errorf("All() agent = %T, want noop (agent is personal config)", reg.Agent())
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}
