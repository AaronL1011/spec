package resolve

import (
	"sort"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/openaicompat"
	"github.com/aaronl1011/spec/internal/config"
)

// completionPreset maps a provider name to the openaicompat adapter with a
// default endpoint. Vendor names are presets over one protocol adapter, not
// separate code paths: Ollama, llama.cpp, LM Studio, vLLM and every gateway
// speak OpenAI-compatible chat completions, so spec needs no per-vendor code
// and a server nobody anticipated works today via `openai-compatible` plus a
// base_url.
//
// An explicit generate.base_url always overrides the default, so a preset is a
// convenience rather than a constraint — someone running Ollama on a remote
// host sets base_url and the preset name stops mattering.
type completionPreset struct {
	// defaultBaseURL is used when the user sets no base_url. Empty means the
	// user must supply one (the generic `openai-compatible` name).
	defaultBaseURL string
}

// completionPresets is the single source of truth for vendor sugar. Adding a
// vendor is a table row, not a package.
var completionPresets = map[string]completionPreset{
	// The generic name: no default, base_url required.
	"openai-compatible": {defaultBaseURL: ""},
	// Ollama serves the OpenAI-compatible surface at /v1 alongside its native
	// /api/chat path. The legacy ollama provider targeted the native path, so
	// this default gains the /v1 suffix.
	"ollama": {defaultBaseURL: "http://localhost:11434/v1"},
	// llama.cpp's llama-server.
	"llama-server": {defaultBaseURL: "http://localhost:8080/v1"},
	// LM Studio's local server.
	"lmstudio": {defaultBaseURL: "http://localhost:1234/v1"},
	// Hosted OpenAI, which retires the long-standing "not yet implemented" stub.
	"openai": {defaultBaseURL: "https://api.openai.com/v1"},
}

// completionPresetNames returns the preset names in stable order, for error
// messages that need to enumerate the valid values.
func completionPresetNames() []string {
	names := make([]string, 0, len(completionPresets))
	for name := range completionPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// completionAgent builds the openaicompat adapter for a preset. An explicit
// base_url always wins over the preset default, so a stale default is a
// nuisance rather than a blocker.
//
// Completion settings are read from the typed Generate struct, falling back to
// flat Extra keys so a config written before `generate:` existed keeps working.
func completionAgent(preset completionPreset, agentCfg config.ProviderConfig) (adapter.AgentAdapter, string) {
	baseURL := firstNonEmpty(agentCfg.Generate.BaseURL, agentCfg.Get("base_url"), preset.defaultBaseURL)
	client, err := openaicompat.New(openaicompat.Options{
		BaseURL: baseURL,
		Model:   firstNonEmpty(agentCfg.Generate.Model, agentCfg.Get("model")),
		Token:   firstNonEmpty(agentCfg.Generate.Token, agentCfg.Get("token")),
		Timeout: generateTimeout(agentCfg),
	})
	if err != nil {
		return noop.Agent{}, err.Error()
	}
	return client, ""
}

// generateTimeout parses agent.generate.timeout, falling back to the adapter
// default when unset or unparseable. An invalid duration degrades to the default
// rather than failing resolution: a typo in a tuning knob should not disable
// drafting entirely.
func generateTimeout(agentCfg config.ProviderConfig) time.Duration {
	raw := firstNonEmpty(agentCfg.Generate.Timeout, agentCfg.Get("timeout"))
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// firstNonEmpty returns the first non-empty string, for layering typed config
// over legacy flat keys over a preset default.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
