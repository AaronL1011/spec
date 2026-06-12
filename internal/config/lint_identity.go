package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LintUserIdentitiesFile validates the per-provider identity map in a user
// config file against the providers a team config actually configures. It is
// advisory only — every finding is a warning, never an error — because a stale
// or mis-keyed identity degrades gracefully (the canonical handle stands in).
//
// teamCfg may be nil (no joined team), in which case there is nothing to
// cross-reference and the result is empty.
func LintUserIdentitiesFile(path string, teamCfg *TeamConfig) (LintResult, error) {
	res := LintResult{File: path}

	data, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("reading user config %q: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// The team linter already reports YAML errors for its file; for the
		// user file we surface a single warning and move on.
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			File: path, Line: yamlErrorLine(err), Severity: SeverityWarning,
			Message: "user config is not valid YAML: " + err.Error(),
		})
		return res, nil
	}

	res.Diagnostics = lintUserIdentitiesNode(path, &doc, teamCfg)
	sortDiagnostics(res.Diagnostics)
	return res, nil
}

// lintUserIdentitiesNode warns about identity provider keys that no configured
// integration uses — the typical cause is a typo ("gihub") or an entry left
// behind after a team dropped an integration.
func lintUserIdentitiesNode(path string, doc *yaml.Node, teamCfg *TeamConfig) []Diagnostic {
	root := documentRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	userNode := mapValue(root, "user")
	if userNode == nil {
		return nil
	}
	identitiesNode := mapValue(userNode, "identities")
	if identitiesNode == nil || identitiesNode.Kind != yaml.MappingNode {
		return nil
	}

	configured := configuredProviders(teamCfg)

	var diags []Diagnostic
	// Mapping content is [key, value, key, value, ...].
	for i := 0; i+1 < len(identitiesNode.Content); i += 2 {
		keyNode := identitiesNode.Content[i]
		provider := strings.ToLower(strings.TrimSpace(keyNode.Value))
		if provider == "" || configured[provider] {
			continue
		}
		diags = append(diags, Diagnostic{
			File: path, Line: lineOf(keyNode), Column: keyNode.Column,
			Severity: SeverityWarning, Field: "user.identities." + keyNode.Value,
			Message:    fmt.Sprintf("identity for provider %q is unused — no configured integration uses it", keyNode.Value),
			Suggestion: suggestProvider(provider, configured),
		})
	}
	return diags
}

// configuredProviders returns the set of provider names referenced by the
// team's integrations (lower-cased), excluding empty and "none".
func configuredProviders(teamCfg *TeamConfig) map[string]bool {
	set := make(map[string]bool)
	if teamCfg == nil {
		return set
	}
	in := teamCfg.Integrations
	for _, p := range []string{
		in.Comms.Provider, in.PM.Provider, in.Docs.Provider, in.Repo.Provider,
		in.Agent.Provider, in.AI.Provider, in.Design.Provider, in.Deploy.Provider,
	} {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && p != "none" {
			set[p] = true
		}
	}
	return set
}

// suggestProvider offers a did-you-mean hint pointing at a configured provider.
func suggestProvider(got string, configured map[string]bool) string {
	candidates := make([]string, 0, len(configured))
	for p := range configured {
		candidates = append(candidates, p)
	}
	if s := suggest(got, candidates); s != "" {
		return s
	}
	return "remove this entry or correct the provider name"
}
