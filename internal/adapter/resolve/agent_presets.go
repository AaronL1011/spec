package resolve

import (
	"sort"

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
func completionAgent(preset completionPreset, agentCfg config.ProviderConfig) (adapter.AgentAdapter, string) {
	baseURL := agentCfg.Get("base_url")
	if baseURL == "" {
		baseURL = preset.defaultBaseURL
	}
	client, err := openaicompat.New(openaicompat.Options{
		BaseURL: baseURL,
		Model:   agentCfg.Get("model"),
		Token:   agentCfg.Get("token"),
	})
	if err != nil {
		return noop.Agent{}, err.Error()
	}
	return client, ""
}
