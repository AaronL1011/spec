package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// This release removes the `ai` integration outright and moves `agent` from
// team config to personal config. Team config parsing is non-strict, so an
// unknown key is indistinguishable from a typo without an explicit rule — the
// rules below are what make the cutover legible instead of silent.
//
// Severity is deliberately split: resolve only *warns* (see AgentConfigWarnings)
// so no command breaks for a team still on the old binary, while lint reports an
// error, because lint is the command whose job is to fail on config defects.

// removedTeamKey describes a team-config key this release ignores.
type removedTeamKey struct {
	key    string
	reason string
}

// removedTeamKeys are the integration keys that no longer have any effect.
var removedTeamKeys = []removedTeamKey{
	{
		key:    "agent",
		reason: "a coding harness and its auth are personal tools, so the agent is configured per-user",
	},
	{
		key:    "ai",
		reason: "the separate 'ai' integration was merged into the agent's completion plane",
	},
}

// replacementAgentYAML is the literal block a user needs in personal config. It
// lives in lint output rather than the resolve warning, where it can be read
// without scrolling past command results.
const replacementAgentYAML = `# ~/.spec/config.yaml
agent:
  provider: claude-code   # or pi | anthropic | openai-compatible | ollama | none
  generate:
    model: claude-sonnet-4-5   # optional`

// lintRemovedIntegrationKeys reports team-config integration keys that this
// release ignores, naming the personal-config replacement.
func lintRemovedIntegrationKeys(path string, root *yaml.Node) []Diagnostic {
	integrations := mapValue(root, "integrations")
	if integrations == nil || integrations.Kind != yaml.MappingNode {
		return nil
	}

	var diags []Diagnostic
	for _, removed := range removedTeamKeys {
		node := mapValue(integrations, removed.key)
		if node == nil {
			continue
		}
		diags = append(diags, Diagnostic{
			File:     path,
			Line:     lineOf(node),
			Severity: SeverityError,
			Field:    "integrations." + removed.key,
			Message: fmt.Sprintf(
				"'integrations.%s' was removed in this release and is ignored — %s",
				removed.key, removed.reason),
			Suggestion: "delete this key from team config and configure the agent in ~/.spec/config.yaml:\n" + replacementAgentYAML,
		})
	}
	return diags
}

// LintUserConfigFile reports defects in a personal config file. It exists so the
// renamed preference key and inert completion settings surface in the command
// whose job is to report config problems.
func LintUserConfigFile(path string) (LintResult, error) {
	res := LintResult{File: path}

	data, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("reading user config %q: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			File: path, Line: yamlErrorLine(err), Severity: SeverityError,
			Message: "config is not valid YAML: " + err.Error(),
		})
		return res, nil
	}

	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return res, nil
	}

	res.Diagnostics = append(res.Diagnostics, lintRenamedPreferences(path, root)...)
	res.Diagnostics = append(res.Diagnostics, lintAgentGenerateNode(path, root)...)
	sortDiagnostics(res.Diagnostics)
	return res, nil
}

// lintRenamedPreferences reports personal-config keys renamed by this release.
func lintRenamedPreferences(path string, root *yaml.Node) []Diagnostic {
	prefs := mapValue(root, "preferences")
	if prefs == nil || prefs.Kind != yaml.MappingNode {
		return nil
	}
	node := mapValue(prefs, "ai_drafts")
	if node == nil {
		return nil
	}
	return []Diagnostic{{
		File:       path,
		Line:       lineOf(node),
		Severity:   SeverityError,
		Field:      "preferences.ai_drafts",
		Message:    "'preferences.ai_drafts' was renamed in this release and is ignored",
		Suggestion: "rename it to 'preferences.agent_drafts'",
	}}
}

// lintAgentGenerateNode reports completion settings that cannot take effect for
// the configured provider. A setting that looks effective but is not is worse
// than one that is absent, so these are surfaced rather than silently dropped.
func lintAgentGenerateNode(path string, root *yaml.Node) []Diagnostic {
	agent := mapValue(root, "agent")
	if agent == nil || agent.Kind != yaml.MappingNode {
		return nil
	}
	providerNode := mapValue(agent, "provider")
	generate := mapValue(agent, "generate")
	if generate == nil || generate.Kind != yaml.MappingNode {
		return nil
	}

	provider := ""
	if providerNode != nil {
		provider = providerNode.Value
	}

	var diags []Diagnostic

	// max_tokens is inert for harness providers: neither `claude -p` nor
	// `pi -p` exposes a token cap.
	if node := mapValue(generate, "max_tokens"); node != nil && isHarnessProvider(provider) {
		diags = append(diags, Diagnostic{
			File:       path,
			Line:       lineOf(node),
			Severity:   SeverityWarning,
			Field:      "agent.generate.max_tokens",
			Message:    fmt.Sprintf("'max_tokens' has no effect for provider %q — the harness CLI exposes no token cap", provider),
			Suggestion: "remove it, or use a completion-API provider (anthropic, openai-compatible) if you need a cap",
		})
	}

	// A literal token would be written back to disk on the next settings save.
	if node := mapValue(generate, "token"); node != nil && node.Kind == yaml.ScalarNode {
		if !envRefPattern.MatchString(node.Value) {
			diags = append(diags, Diagnostic{
				File:       path,
				Line:       lineOf(node),
				Severity:   SeverityWarning,
				Field:      "agent.generate.token",
				Message:    "'token' is set to a literal value, so it sits in plaintext in your config file",
				Suggestion: "use an environment reference such as ${SPEC_LLM_TOKEN} and export the variable",
			})
		}
	}

	// The generic provider needs an endpoint; presets supply their own.
	if provider == "openai-compatible" && mapValue(generate, "base_url") == nil {
		diags = append(diags, Diagnostic{
			File:       path,
			Line:       lineOf(generate),
			Severity:   SeverityError,
			Field:      "agent.generate.base_url",
			Message:    "provider 'openai-compatible' requires 'generate.base_url'",
			Suggestion: "set base_url (e.g. http://localhost:11434/v1), or use a vendor preset such as 'ollama'",
		})
	}

	return diags
}

// isHarnessProvider reports whether a provider is a coding harness driven as a
// subprocess, as opposed to a completion API reached over HTTP.
func isHarnessProvider(provider string) bool {
	switch provider {
	case "claude-code", "pi":
		return true
	default:
		return false
	}
}
