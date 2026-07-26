package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Documented config must parse, or the docs teach a syntax that does not work.
// These are the examples from docs/CONFIGURATION.md#coding-agent verbatim.

func TestDocumentedAgentExamples_Parse(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(*testing.T, *UserConfig)
	}{
		{
			name: "harness",
			yaml: `agent:
  provider: claude-code
  command: claude
`,
			check: func(t *testing.T, c *UserConfig) {
				if c.Agent == nil || c.Agent.Provider != "claude-code" {
					t.Fatalf("provider not parsed: %+v", c.Agent)
				}
				if got := c.Agent.Get("command"); got != "claude" {
					t.Errorf("command = %q, want claude", got)
				}
			},
		},
		{
			name: "local completion endpoint",
			yaml: `agent:
  provider: openai-compatible
  generate:
    base_url: http://localhost:11434/v1
    model: qwen2.5-coder:14b
`,
			check: func(t *testing.T, c *UserConfig) {
				if c.Agent.Generate.BaseURL != "http://localhost:11434/v1" {
					t.Errorf("base_url = %q", c.Agent.Generate.BaseURL)
				}
				if c.Agent.Generate.Model != "qwen2.5-coder:14b" {
					t.Errorf("model = %q", c.Agent.Generate.Model)
				}
			},
		},
		{
			name: "every generate setting",
			yaml: `agent:
  provider: openai-compatible
  generate:
    base_url: http://localhost:1234/v1
    model: qwen2.5-coder:14b
    max_tokens: 4096
    timeout: 120s
`,
			check: func(t *testing.T, c *UserConfig) {
				if c.Agent.Generate.MaxTokens != 4096 {
					t.Errorf("max_tokens = %d", c.Agent.Generate.MaxTokens)
				}
				if c.Agent.Generate.Timeout != "120s" {
					t.Errorf("timeout = %q", c.Agent.Generate.Timeout)
				}
			},
		},
		{
			name: "drafting disabled",
			yaml: `preferences:
  agent_drafts: false
`,
			check: func(t *testing.T, c *UserConfig) {
				if c.Preferences.AgentDraftsEnabled() {
					t.Error("agent_drafts: false should disable drafting")
				}
			},
		},
		{
			name: "transitions granted",
			yaml: `preferences:
  agent_authoring:
    transitions: true
`,
			check: func(t *testing.T, c *UserConfig) {
				if !c.Preferences.AgentAuthoring.Transitions {
					t.Error("transitions: true should be parsed")
				}
			},
		},
		{
			name: "personal agent with both planes",
			yaml: `agent:
  provider: pi
  command: pi
  generate:
    model: qwen2.5-coder:14b
`,
			check: func(t *testing.T, c *UserConfig) {
				if c.Agent.Provider != "pi" {
					t.Errorf("provider = %q", c.Agent.Provider)
				}
				if c.Agent.Generate.Model == "" {
					t.Error("generate.model should coexist with a harness provider")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg UserConfig
			if err := yaml.Unmarshal([]byte(tt.yaml), &cfg); err != nil {
				t.Fatalf("documented example does not parse: %v\n%s", err, tt.yaml)
			}
			tt.check(t, &cfg)
		})
	}
}

// A token must survive as its ${VAR} spelling, since the docs promise a literal
// is refused and only the reference form is safe to write back.
func TestDocumentedTokenExample_StaysAReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `user:
  name: Ada
  owner_role: engineer
agent:
  provider: openai
  generate:
    model: gpt-4o
    token: ${SPEC_LLM_TOKEN}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPEC_LLM_TOKEN", "secret-value")

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.Agent.Generate.Token != "secret-value" {
		t.Errorf("token should interpolate for use, got %q", cfg.Agent.Generate.Token)
	}

	// Round-tripping must not leak the secret into the file.
	gen, err := cfg.Agent.Generate.forMarshal()
	if err != nil {
		t.Fatalf("forMarshal: %v", err)
	}
	out, err := yaml.Marshal(gen)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "secret-value") {
		t.Errorf("marshalling leaked the resolved token:\n%s", out)
	}
	if !strings.Contains(string(out), "${SPEC_LLM_TOKEN}") {
		t.Errorf("the env reference should be preserved:\n%s", out)
	}
}

// The migration example in the docs must lint with the guidance the docs claim.
func TestDocumentedMigration_LintsWithGuidance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	body := `version: "1"
integrations:
  agent:
    provider: pi
  ai:
    provider: ollama
    model: llama3
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadTeamConfig(path); err != nil {
		t.Fatalf("a stale team config must still load, not hard-fail: %v", err)
	}

	var doc yaml.Node
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	// The lint walks the root mapping, not the document wrapper.
	diags := lintRemovedIntegrationKeys(path, documentRoot(&doc))
	if len(diags) != 2 {
		t.Fatalf("both removed keys should be reported, got %d: %+v", len(diags), diags)
	}

	var joined strings.Builder
	for _, d := range diags {
		joined.WriteString(d.Field + " " + d.Message + " " + d.Suggestion + "\n")
		// A diagnostic without a line number cannot be acted on quickly.
		if d.Line == 0 {
			t.Errorf("diagnostic for %q should carry a line number", d.Field)
		}
		// The docs promise removed keys warn rather than break older binaries.
		if d.Severity != SeverityError {
			t.Errorf("lint should report removed keys as errors, got %v", d.Severity)
		}
	}
	for _, want := range []string{"agent", "ai", "~/.spec/config.yaml"} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("lint output should mention %q:\n%s", want, joined.String())
		}
	}
}
