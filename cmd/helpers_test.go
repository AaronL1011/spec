package cmd

import (
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/pi"
	"github.com/aaronl1011/spec/internal/config"
)

// The agent comes from personal config alone. A team config cannot supply one,
// so the presence of a team config must not change the resolved agent.
func TestBuildRegistry_AgentComesFromPersonalConfig(t *testing.T) {
	rc := &config.ResolvedConfig{
		Team: &config.TeamConfig{},
		User: &config.UserConfig{Agent: &config.ProviderConfig{Provider: "pi"}},
	}

	reg := buildRegistry(rc)
	if _, ok := reg.Agent().(*pi.Agent); !ok {
		t.Errorf("expected pi agent from personal config, got %T", reg.Agent())
	}
}

func TestBuildRegistry_TeamConfigAloneYieldsNoAgent(t *testing.T) {
	rc := &config.ResolvedConfig{Team: &config.TeamConfig{}}

	reg := buildRegistry(rc)
	if _, ok := reg.Agent().(noop.Agent); !ok {
		t.Errorf("team config cannot configure an agent; want noop, got %T", reg.Agent())
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
