package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
)

// The cutover warning has two renderers of one message.
//
// Resolve emits a one-line warning to stderr, which is correct for CLI and
// non-TTY use but invisible in the TUI: it is written while adapters resolve,
// before the alt screen opens, so it scrolls away under the splash and the user
// never sees it. Since the TUI is the primary surface, a migration that is only
// announced on stderr is a migration most users discover as "my agent stopped
// working".
//
// So the same fact is also rendered here, plus the flagged Settings agent row.
// It is shown once and remembered: a notice that returns on every launch trains
// the user to dismiss notices unread, which would cost more than it buys.

// agentMigrationNoticeID keys the dismissal record. It carries the release the
// notice belongs to, so a future migration gets its own notice rather than being
// suppressed by this one's acknowledgement.
const agentMigrationNoticeID = "agent_migration_v1"

// showAgentMigrationNotice raises the migration notice when the team config
// still carries a removed integration key and the user has not dismissed it.
//
// Returns nil when there is nothing to say, which is the common case.
func (a *App) showAgentMigrationNotice() tea.Cmd {
	if a.rc == nil || !config.HasRemovedAgentKeys(a.rc.Team) {
		return nil
	}
	if a.db != nil && a.db.NoticeAcknowledged(agentMigrationNoticeID) {
		return nil
	}

	a.pendingAction = "ack-agent-migration"
	a.modal.ShowInfo("Agent config moved", agentMigrationNoticeBody(a.rc))
	a.modal.SetSize(a.width, a.contentHeight())
	return nil
}

// agentMigrationNoticeBody renders the notice text: what is ignored, what to do,
// and where the full detail lives.
func agentMigrationNoticeBody(rc *config.ResolvedConfig) string {
	keys := config.RemovedAgentKeys(rc.Team)
	labelled := make([]string, len(keys))
	for i, k := range keys {
		labelled[i] = "integrations." + k
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s in your team config %s ignored — removed in this release.\n\n",
		strings.Join(labelled, " and "), plural(len(keys), "is", "are"))

	// The agent is personal config now, so the fix is per-user and cannot be
	// made for them by editing the shared repo.
	b.WriteString("The agent is now personal config. Add this to ~/.spec/config.yaml:\n\n")
	b.WriteString(config.AgentMigrationYAML())
	b.WriteString("\n\nThen verify it with 'spec agent check'.\n")
	b.WriteString("Run 'spec config lint' for the full report, including the team-config keys to delete.")
	return b.String()
}

// plural picks a verb form for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// acknowledgeAgentMigration records the dismissal so the notice does not return.
//
// Best-effort: failing to persist an acknowledgement is not worth surfacing to
// the user, and the only cost is seeing the notice once more.
func acknowledgeAgentMigration(a *App) {
	if a.db == nil {
		return
	}
	_ = a.db.NoticeAcknowledge(agentMigrationNoticeID)
}
