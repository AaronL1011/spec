package tui

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/tui/components"
)

// teamConfigWithRemovedKey parses a team config through the real unmarshaller,
// because removed-key detection reads the source YAML — the struct fields are
// gone, so a hand-built TeamConfig cannot represent an ignored key.
func teamConfigWithRemovedKey(t *testing.T, body string) *config.TeamConfig {
	t.Helper()
	var team config.TeamConfig
	if err := yaml.Unmarshal([]byte(body), &team); err != nil {
		t.Fatalf("parsing team config: %v", err)
	}
	return &team
}

// The stderr warning is emitted before the alt screen opens, so the TUI must
// carry the same message itself or a TUI-primary user never sees the migration.
func TestAgentMigrationNotice_ShownForRemovedKey(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team = teamConfigWithRemovedKey(t, "integrations:\n  agent:\n    provider: claude-code\n")

	a := App{rc: rc}
	a.modal = components.NewModal(modalStyles(ResolveTheme("auto"), NewStyles(ResolveTheme("auto"))))
	if cmd := a.showAgentMigrationNotice(); cmd != nil {
		t.Error("the notice renders synchronously; no command is expected")
	}

	if !a.modal.Visible {
		t.Fatal("a removed team-config key should raise the migration notice")
	}
	body := stripANSI(a.modal.Message)
	for _, want := range []string{
		"integrations.agent",  // names the dead key
		"~/.spec/config.yaml", // names where the replacement goes
		"agent:",              // carries the literal replacement YAML
		"spec agent check",    // names how to verify the fix
		"spec config lint",    // points at the full report
	} {
		if !strings.Contains(body, want) {
			t.Errorf("notice body should mention %q; got:\n%s", want, body)
		}
	}
	// Dismissal must be recorded, or the notice returns on every launch and
	// trains the user to close notices unread.
	if a.pendingAction != "ack-agent-migration" {
		t.Errorf("pendingAction = %q, want the dismissal to be tracked", a.pendingAction)
	}
}

// Both removed keys read naturally in one sentence rather than as two notices.
func TestAgentMigrationNotice_NamesBothRemovedKeys(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team = teamConfigWithRemovedKey(t,
		"integrations:\n  agent:\n    provider: pi\n  ai:\n    provider: anthropic\n")

	body := stripANSI(agentMigrationNoticeBody(rc))
	if !strings.Contains(body, "integrations.agent") || !strings.Contains(body, "integrations.ai") {
		t.Errorf("body should name both keys; got:\n%s", body)
	}
	if !strings.Contains(body, "are ignored") {
		t.Errorf("two keys should read as plural; got:\n%s", body)
	}
}

// The overwhelmingly common case is a clean config, which must stay silent.
func TestAgentMigrationNotice_SilentWithoutRemovedKeys(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team = teamConfigWithRemovedKey(t, "integrations:\n  repo:\n    provider: github\n")

	a := App{rc: rc}
	a.modal = components.NewModal(modalStyles(ResolveTheme("auto"), NewStyles(ResolveTheme("auto"))))
	a.showAgentMigrationNotice()
	if a.modal.Visible {
		t.Error("a config with no removed keys should raise no notice")
	}
}

// A nil team config (bare checkout, no config at all) must not panic.
func TestAgentMigrationNotice_ToleratesNoConfig(t *testing.T) {
	a := App{}
	a.modal = components.NewModal(modalStyles(ResolveTheme("auto"), NewStyles(ResolveTheme("auto"))))
	a.showAgentMigrationNotice()
	if a.modal.Visible {
		t.Error("no config means nothing to migrate")
	}
}

// The Settings row is the third renderer: a user who dismissed the notice and
// later wonders where their agent went looks here, and a bare dash would not
// explain it.
func TestSettings_AgentRowFlagsIgnoredTeamKey(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team = teamConfigWithRemovedKey(t, "integrations:\n  agent:\n    provider: claude-code\n")

	m := newSettings(rc, NewStyles(ResolveTheme("auto")), DefaultKeyMap())
	row := stripANSI(m.renderAgentRow())
	if !strings.Contains(row, "ignored") {
		t.Errorf("agent row should flag the ignored team key; got %q", row)
	}
	if !strings.Contains(row, "spec config lint") {
		t.Errorf("agent row should point at lint; got %q", row)
	}
}
