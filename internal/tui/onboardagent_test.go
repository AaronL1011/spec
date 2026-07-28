package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// The option order is the affordance: a user with a harness installed must be one
// keystroke from done, and skip must always be last so it is never the accidental
// default when a harness is present.
func TestAgentStepOptions_DetectedFirstSkipLast(t *testing.T) {
	opts := agentStepOptions([]string{"claude-code", "pi"})

	if len(opts) != 4 {
		t.Fatalf("got %d options, want 2 detected + endpoint + skip", len(opts))
	}
	if opts[0].Value != "claude-code" || opts[1].Value != "pi" {
		t.Errorf("detected harnesses should come first, got %q then %q", opts[0].Value, opts[1].Value)
	}
	if !strings.Contains(opts[0].Key, "found on PATH") {
		t.Errorf("a detected harness should say so; got %q", opts[0].Key)
	}
	if opts[2].Value != providerOpenAICompatible {
		t.Errorf("endpoint option should precede skip, got %q", opts[2].Value)
	}
	if opts[3].Value != skipAgentChoice {
		t.Errorf("skip should be last, got %q", opts[3].Value)
	}
}

// With no harness installed the step must still offer the endpoint route, or a
// user with a local model concludes spec needs a vendor they do not have.
func TestAgentStepOptions_NoHarnessStillOffersEndpoint(t *testing.T) {
	opts := agentStepOptions(nil)

	if len(opts) != 2 {
		t.Fatalf("got %d options, want endpoint + skip", len(opts))
	}
	if opts[0].Value != providerOpenAICompatible {
		t.Errorf("endpoint should be offered with no harness present, got %q", opts[0].Value)
	}
	if opts[1].Value != skipAgentChoice {
		t.Errorf("skip should remain available, got %q", opts[1].Value)
	}
}

// writeUserAgent must amend the identity step's config rather than replace it:
// the two steps write the same file in sequence.
func TestWriteUserAgent_PreservesIdentity(t *testing.T) {
	withTempUserConfig(t)

	if err := writeUserIdentity("Ada Lovelace", "engineer", "ada"); err != nil {
		t.Fatalf("writeUserIdentity: %v", err)
	}
	if err := writeUserAgent(&config.ProviderConfig{Provider: "pi"}); err != nil {
		t.Fatalf("writeUserAgent: %v", err)
	}

	cfg, err := config.LoadUserConfig(config.UserConfigPath())
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.User.Name != "Ada Lovelace" || cfg.User.Handle != "ada" {
		t.Errorf("identity lost: %+v", cfg.User)
	}
	if cfg.Agent == nil || cfg.Agent.Provider != "pi" {
		t.Errorf("agent = %+v, want provider pi", cfg.Agent)
	}
}

// An endpoint provider is only useful with a base_url, and it must round-trip
// through the typed Generate block rather than landing in Extra.
func TestWriteUserAgent_EndpointRoundTrips(t *testing.T) {
	withTempUserConfig(t)

	if err := writeUserIdentity("Ada", "engineer", "ada"); err != nil {
		t.Fatalf("writeUserIdentity: %v", err)
	}
	agentCfg := &config.ProviderConfig{Provider: providerOpenAICompatible}
	agentCfg.Generate.BaseURL = "http://localhost:11434/v1"
	agentCfg.Generate.Model = "llama3.1"
	if err := writeUserAgent(agentCfg); err != nil {
		t.Fatalf("writeUserAgent: %v", err)
	}

	cfg, err := config.LoadUserConfig(config.UserConfigPath())
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.Agent.Generate.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("base_url = %q, want it preserved", cfg.Agent.Generate.BaseURL)
	}
	if cfg.Agent.Generate.Model != "llama3.1" {
		t.Errorf("model = %q, want it preserved", cfg.Agent.Generate.Model)
	}
	if _, leaked := cfg.Agent.Extra["generate"]; leaked {
		t.Error("the generate block must not land in Extra")
	}
}

// Skipping must write nothing agent-related — no provider, and no agent_drafts
// preference pointing at an agent that does not exist.
func TestAgentStep_SkipWritesNothingAgentRelated(t *testing.T) {
	withTempUserConfig(t)

	if err := writeUserIdentity("Ada", "engineer", "ada"); err != nil {
		t.Fatalf("writeUserIdentity: %v", err)
	}

	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "agent:") {
		t.Errorf("identity step should write no agent key; got:\n%s", body)
	}
	if strings.Contains(body, "agent_drafts") {
		t.Errorf("no agent_drafts preference should be written; got:\n%s", body)
	}

	cfg, err := config.LoadUserConfig(config.UserConfigPath())
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.Agent != nil {
		t.Errorf("Agent = %+v, want nil after a skip", cfg.Agent)
	}
	// Drafting defaults on, so an agent configured later needs no second opt-in.
	if !cfg.Preferences.AgentDraftsEnabled() {
		t.Error("agent drafting should default to enabled")
	}
}

// withTempUserConfig points config.UserConfigPath at a temp HOME so a test never
// touches the developer's real ~/.spec/config.yaml.
func withTempUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".spec"), 0o755); err != nil {
		t.Fatalf("creating temp spec dir: %v", err)
	}
}
