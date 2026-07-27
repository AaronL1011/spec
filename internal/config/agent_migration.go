package config

import (
	"fmt"
	"sync"
)

// Cutover behaviour for the removed team-config integration keys.
//
// Resolve *ignores* the keys and warns; it never fails. Hard-failing was
// rejected: team config lives in the shared specs repo, so one teammate's edit
// would break every command for everyone still on the old binary — including
// `spec config lint`, the command that explains the fix. The real user-visible
// damage of the cutover is "my agent silently vanished", which a loud warning
// addresses directly, and lint is where a hard error belongs.

// warnOnce dedupes the migration warning per process. Adapter resolution runs
// more than once in some commands (`spec sync` builds a registry twice), so
// emitting on every call site would repeat the same line.
var warnOnce sync.Once

// AgentConfigWarnings returns the migration warnings for a team config, at most
// once per process. Subsequent calls return nil, so callers can emit
// unconditionally without repeating themselves.
//
// The returned strings are one line each and point at lint for the full
// replacement YAML rather than printing a wall of config.
func AgentConfigWarnings(cfg *TeamConfig) []string {
	if cfg == nil {
		return nil
	}
	present := removedKeysPresent(cfg)
	if len(present) == 0 {
		return nil
	}

	var out []string
	warnOnce.Do(func() {
		for _, key := range present {
			out = append(out, fmt.Sprintf(
				"integrations.%s in team config is ignored (removed in this release) — configure 'agent:' in ~/.spec/config.yaml; run 'spec config lint' for details",
				key))
		}
	})
	return out
}

// HasRemovedAgentKeys reports whether a team config still carries a removed
// integration key. The TUI uses this to surface the migration notice on a
// surface the user actually sees: the stderr warning is emitted before the alt
// screen opens, so it would flash past a TUI-primary user.
func HasRemovedAgentKeys(cfg *TeamConfig) bool {
	return len(removedKeysPresent(cfg)) > 0
}

// RemovedAgentKeys returns the removed integration keys a team config still
// sets, in stable order, so a renderer can name them.
//
// Unlike AgentConfigWarnings this is not once-per-process: a TUI notice renders
// from state rather than as a side effect, and it must survive a redraw.
func RemovedAgentKeys(cfg *TeamConfig) []string {
	return removedKeysPresent(cfg)
}

// AgentMigrationYAML is the replacement block for a user cutting over. It is
// exported so the TUI notice and `spec config lint` show the same literal YAML:
// two renderings of one migration must not disagree about the fix.
func AgentMigrationYAML() string { return replacementAgentYAML }

// removedKeysPresent returns the removed integration keys a team config still
// sets, in stable order.
//
// Detection reads the retained raw mapping rather than a struct field, because
// the fields are gone: an ignored key has to be recognised from the source YAML.
func removedKeysPresent(cfg *TeamConfig) []string {
	if cfg == nil || cfg.removedIntegrationKeys == nil {
		return nil
	}
	var out []string
	for _, removed := range removedTeamKeys {
		if cfg.removedIntegrationKeys[removed.key] {
			out = append(out, removed.key)
		}
	}
	return out
}
