package cmd

import (
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/pi"
	"github.com/aaronl1011/spec/internal/config"
)

func TestBuildRegistry_PerUserAgentOverride(t *testing.T) {
	team := &config.TeamConfig{}
	team.Integrations.Agent = config.ProviderConfig{Provider: "claude-code"}

	// User overrides the team's claude-code with pi.
	rc := &config.ResolvedConfig{
		Team: team,
		User: &config.UserConfig{Agent: &config.ProviderConfig{Provider: "pi"}},
	}

	reg := buildRegistry(rc)
	if _, ok := reg.Agent().(*pi.Agent); !ok {
		t.Errorf("expected pi agent from per-user override, got %T", reg.Agent())
	}
}

func TestBuildRegistry_TeamDefaultWhenNoUserOverride(t *testing.T) {
	team := &config.TeamConfig{}
	team.Integrations.Agent = config.ProviderConfig{Provider: "pi"}

	rc := &config.ResolvedConfig{Team: team}

	reg := buildRegistry(rc)
	if _, ok := reg.Agent().(*pi.Agent); !ok {
		t.Errorf("expected pi agent from team default, got %T", reg.Agent())
	}
}

func TestBuildRegistry_UserAgentWithoutTeamConfig(t *testing.T) {
	rc := &config.ResolvedConfig{
		User: &config.UserConfig{Agent: &config.ProviderConfig{Provider: "pi"}},
	}

	reg := buildRegistry(rc)
	if _, ok := reg.Agent().(*pi.Agent); !ok {
		t.Errorf("expected pi agent for user override without team config, got %T", reg.Agent())
	}
}

func TestBuildRegistry_NoAgentConfiguredIsNoop(t *testing.T) {
	reg := buildRegistry(&config.ResolvedConfig{})
	if _, ok := reg.Agent().(noop.Agent); !ok {
		t.Errorf("expected noop agent when nothing configured, got %T", reg.Agent())
	}
}
