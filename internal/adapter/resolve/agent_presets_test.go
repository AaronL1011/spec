package resolve

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/openaicompat"
	"github.com/aaronl1011/spec/internal/config"
)

func agentProvider(provider string, extra map[string]string) config.ProviderConfig {
	if extra == nil {
		extra = map[string]string{}
	}
	return config.ProviderConfig{Provider: provider, Extra: extra}
}

// Every vendor preset must resolve to the one openaicompat adapter with the
// documented default endpoint. This table is the guard against preset defaults
// drifting from what the spec and docs promise.
func TestAgent_VendorPresets_ResolveToOpenAICompat(t *testing.T) {
	tests := []struct {
		provider    string
		wantBaseURL string
	}{
		{"ollama", "http://localhost:11434/v1"},
		{"llama-server", "http://localhost:8080/v1"},
		{"lmstudio", "http://localhost:1234/v1"},
		{"openai", "https://api.openai.com/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			agent, warn := Agent(agentProvider(tt.provider, nil))
			if warn != "" {
				t.Fatalf("unexpected warning: %s", warn)
			}
			client, ok := agent.(*openaicompat.Client)
			if !ok {
				t.Fatalf("provider %q resolved to %T, want *openaicompat.Client", tt.provider, agent)
			}
			if got := client.BaseURL(); got != tt.wantBaseURL {
				t.Errorf("base_url = %q, want %q", got, tt.wantBaseURL)
			}
			if !client.Capabilities().Generate {
				t.Error("preset should advertise the completion plane")
			}
		})
	}
}

// An explicit base_url always wins, so a preset is never a constraint — someone
// running Ollama on another host is not stuck with the default port.
func TestAgent_ExplicitBaseURL_OverridesPresetDefault(t *testing.T) {
	agent, warn := Agent(agentProvider("ollama", map[string]string{
		"base_url": "http://gpu-box.internal:11434/v1",
	}))
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	client, ok := agent.(*openaicompat.Client)
	if !ok {
		t.Fatalf("resolved to %T, want *openaicompat.Client", agent)
	}
	if got := client.BaseURL(); got != "http://gpu-box.internal:11434/v1" {
		t.Errorf("base_url = %q, want the explicit value", got)
	}
}

// The generic name has no default endpoint, so a missing base_url must fail with
// an actionable message rather than silently pointing somewhere arbitrary.
func TestAgent_OpenAICompatible_RequiresBaseURL(t *testing.T) {
	agent, warn := Agent(agentProvider("openai-compatible", nil))
	if _, ok := agent.(noop.Agent); !ok {
		t.Errorf("resolved to %T, want noop when base_url is missing", agent)
	}
	if !strings.Contains(warn, "base_url") {
		t.Errorf("warning should name the missing field, got %q", warn)
	}
}

func TestAgent_OpenAICompatible_WithBaseURL_Resolves(t *testing.T) {
	agent, warn := Agent(agentProvider("openai-compatible", map[string]string{
		"base_url": "http://localhost:9999/v1",
		"model":    "some-model",
	}))
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	if _, ok := agent.(*openaicompat.Client); !ok {
		t.Fatalf("resolved to %T, want *openaicompat.Client", agent)
	}
}

func TestAgent_Anthropic_RequiresToken(t *testing.T) {
	agent, warn := Agent(agentProvider("anthropic", nil))
	if _, ok := agent.(noop.Agent); !ok {
		t.Errorf("resolved to %T, want noop without a token", agent)
	}
	if !strings.Contains(warn, "token") {
		t.Errorf("warning should name the missing token, got %q", warn)
	}
}

func TestAgent_Anthropic_WithToken_Resolves(t *testing.T) {
	agent, warn := Agent(agentProvider("anthropic", map[string]string{"token": "sk-ant-x"}))
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	if _, ok := agent.(noop.Agent); ok {
		t.Error("expected the anthropic adapter, got noop")
	}
	if !agent.Capabilities().Generate {
		t.Error("anthropic should advertise the completion plane")
	}
}

// An unknown name must teach the general mechanism, not just enumerate the
// table: the escape hatch is what makes an unanticipated server work.
func TestAgent_UnknownProvider_NamesEscapeHatch(t *testing.T) {
	agent, warn := Agent(agentProvider("some-new-server", nil))
	if _, ok := agent.(noop.Agent); !ok {
		t.Errorf("resolved to %T, want noop for an unknown provider", agent)
	}
	if !strings.Contains(warn, "openai-compatible") {
		t.Errorf("warning should name openai-compatible as the general option, got %q", warn)
	}
	for _, name := range []string{"ollama", "claude-code", "pi"} {
		if !strings.Contains(warn, name) {
			t.Errorf("warning should list %q among valid providers, got %q", name, warn)
		}
	}
}

func TestAgent_SessionProviders_Unchanged(t *testing.T) {
	for _, provider := range []string{"claude-code", "pi"} {
		agent, warn := Agent(agentProvider(provider, nil))
		if warn != "" {
			t.Errorf("%s: unexpected warning %q", provider, warn)
		}
		if !agent.Capabilities().MCP {
			t.Errorf("%s should still advertise MCP", provider)
		}
	}
}
