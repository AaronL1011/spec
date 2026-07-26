package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeAgentUserCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ProviderConfig flattens unknown keys into Extra by stringifying them, which
// would turn a nested generate mapping into "map[model:x]". These assert the
// typed decode instead.
func TestGenerateConfig_DecodesAsTypedStruct(t *testing.T) {
	path := writeAgentUserCfg(t, `
user:
  owner_role: engineer
agent:
  provider: openai-compatible
  command: ignored-but-kept
  generate:
    model: llama3.1
    max_tokens: 2048
    base_url: http://localhost:11434/v1
    timeout: 90s
`)

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.Agent == nil {
		t.Fatal("agent config not loaded")
	}
	gen := cfg.Agent.Generate
	if gen.Model != "llama3.1" {
		t.Errorf("model = %q", gen.Model)
	}
	if gen.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048 (int, not a stringified map)", gen.MaxTokens)
	}
	if gen.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("base_url = %q", gen.BaseURL)
	}
	if gen.Timeout != "90s" {
		t.Errorf("timeout = %q", gen.Timeout)
	}
	// The nested mapping must not also land in Extra as a stringified map.
	if got := cfg.Agent.Get("generate"); got != "" {
		t.Errorf("Extra[\"generate\"] = %q, want empty (generate is typed)", got)
	}
	// Other flat keys still flow through Extra as before.
	if got := cfg.Agent.Get("command"); got != "ignored-but-kept" {
		t.Errorf("Extra[\"command\"] = %q, want preserved", got)
	}
}

// A settings save must not drop the generate block or flatten it.
func TestGenerateConfig_RoundTripsThroughMarshal(t *testing.T) {
	path := writeAgentUserCfg(t, `
user:
  owner_role: engineer
agent:
  provider: ollama
  generate:
    model: llama3.1
    max_tokens: 1024
`)
	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	out := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteUserConfig(out, cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}

	reloaded, err := LoadUserConfig(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Agent == nil {
		t.Fatal("agent lost on round-trip")
	}
	if reloaded.Agent.Generate.Model != "llama3.1" {
		t.Errorf("model = %q, want llama3.1", reloaded.Agent.Generate.Model)
	}
	if reloaded.Agent.Generate.MaxTokens != 1024 {
		t.Errorf("max_tokens = %d, want 1024", reloaded.Agent.Generate.MaxTokens)
	}

	// Assert the on-disk shape is a nested mapping, not a flattened string.
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "map[") {
		t.Errorf("generate was stringified into the file:\n%s", raw)
	}
}

// The token is the first secret in personal config, and config loading resolves
// ${VAR} before unmarshal. Writing back the resolved value would leak the key
// into a file that often lives in a dotfiles repo.
func TestGenerateToken_EnvRefIsNeverResolvedOnDisk(t *testing.T) {
	t.Setenv("SPEC_LLM_TOKEN", "super-secret-value")

	path := writeAgentUserCfg(t, `
user:
  owner_role: engineer
agent:
  provider: openai-compatible
  generate:
    base_url: https://gateway.internal/v1
    token: ${SPEC_LLM_TOKEN}
`)
	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	// In memory the token is resolved, so adapters can use it.
	if cfg.Agent.Generate.Token != "super-secret-value" {
		t.Errorf("in-memory token = %q, want the resolved value", cfg.Agent.Generate.Token)
	}
	if cfg.Agent.Generate.TokenSource() != "${SPEC_LLM_TOKEN}" {
		t.Errorf("token source = %q, want the literal reference", cfg.Agent.Generate.TokenSource())
	}

	out := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteUserConfig(out, cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("resolved credential was written to disk:\n%s", raw)
	}
	if !strings.Contains(string(raw), "${SPEC_LLM_TOKEN}") {
		t.Errorf("env reference was not preserved:\n%s", raw)
	}
}

// A literal token cannot be re-emitted safely, so the write is refused with the
// env-var form named rather than silently persisting the secret.
func TestGenerateToken_LiteralValueRefusesWrite(t *testing.T) {
	cfg := &UserConfig{Agent: &ProviderConfig{
		Provider: "openai-compatible",
		Generate: GenerateConfig{BaseURL: "https://x/v1", Token: "sk-literal-key"},
	}}

	err := WriteUserConfig(filepath.Join(t.TempDir(), "config.yaml"), cfg)
	if err == nil {
		t.Fatal("expected the write to be refused")
	}
	if !strings.Contains(err.Error(), "SPEC_LLM_TOKEN") {
		t.Errorf("error should name the env-var form, got %q", err)
	}
}

func TestGenerateConfig_OmittedWhenEmpty(t *testing.T) {
	cfg := &UserConfig{Agent: &ProviderConfig{Provider: "pi"}}
	out := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteUserConfig(out, cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "generate") {
		t.Errorf("empty generate block should be omitted:\n%s", raw)
	}
}

// --- cutover behaviour ---

func TestTeamConfig_RemovedKeysAreIgnoredNotFatal(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "agent only",
			body: "version: \"1\"\nintegrations:\n  agent:\n    provider: claude-code\n",
			want: []string{"agent"},
		},
		{
			name: "ai only",
			body: "version: \"1\"\nintegrations:\n  ai:\n    provider: anthropic\n",
			want: []string{"ai"},
		},
		{
			name: "both",
			body: "version: \"1\"\nintegrations:\n  agent:\n    provider: pi\n  ai:\n    provider: ollama\n",
			want: []string{"agent", "ai"},
		},
		{
			name: "neither",
			body: "version: \"1\"\nintegrations:\n  pm:\n    provider: jira\n",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg TeamConfig
			// Parsing must succeed in every case: a removed key is ignored,
			// never a parse error, so no command breaks on upgrade.
			if err := yaml.Unmarshal([]byte(tc.body), &cfg); err != nil {
				t.Fatalf("removed keys must not fail parsing: %v", err)
			}
			got := removedKeysPresent(&cfg)
			if len(got) != len(tc.want) {
				t.Fatalf("removedKeysPresent = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("removedKeysPresent[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
			if HasRemovedAgentKeys(&cfg) != (len(tc.want) > 0) {
				t.Errorf("HasRemovedAgentKeys = %v, want %v", HasRemovedAgentKeys(&cfg), len(tc.want) > 0)
			}
		})
	}
}

func TestAgentConfigWarnings_NilConfigIsSafe(t *testing.T) {
	if warns := AgentConfigWarnings(nil); warns != nil {
		t.Errorf("nil team config should produce no warnings, got %v", warns)
	}
}

func TestHasAgent_ReadsPersonalConfigOnly(t *testing.T) {
	tests := []struct {
		name string
		rc   ResolvedConfig
		want bool
	}{
		{"no config", ResolvedConfig{}, false},
		{"team config alone", ResolvedConfig{Team: &TeamConfig{}}, false},
		{"personal agent", ResolvedConfig{User: &UserConfig{Agent: &ProviderConfig{Provider: "pi"}}}, true},
		{"explicit none", ResolvedConfig{User: &UserConfig{Agent: &ProviderConfig{Provider: "none"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.HasAgent(); got != tt.want {
				t.Errorf("HasAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Transitions are opt-in: an agent must not move specs or fire team-visible
// pipeline effects unless the user says so.
func TestAgentTransitionsAllowed_DefaultsOff(t *testing.T) {
	rc := ResolvedConfig{User: &UserConfig{Agent: &ProviderConfig{Provider: "pi"}}}
	if rc.AgentTransitionsAllowed() {
		t.Error("transitions should be off by default")
	}

	path := writeAgentUserCfg(t, `
user:
  owner_role: engineer
preferences:
  agent_authoring:
    transitions: true
`)
	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if !(&ResolvedConfig{User: cfg}).AgentTransitionsAllowed() {
		t.Error("transitions should be enabled when opted in")
	}
}
