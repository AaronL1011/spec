package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/spf13/cobra"
)

// publishEdits commits and pushes any local working-tree edits for specID to
// the specs repo, governed by the configured auto-push policy. It is the single
// seam the editor-based commands (edit, plan edit) call after the user finishes
// editing, so published state never depends on the user remembering to run
// 'spec push'.
//
// The --no-push flag (when registered on cmd) forces the manual path for this
// invocation regardless of policy, letting a user batch several edits before
// publishing. A push failure that is merely queued/offline is non-fatal — the
// commit is durable and drains on the next online operation.
func publishEdits(cmd *cobra.Command, rc *config.ResolvedConfig, specID string) error {
	if err := requireTeamConfig(rc); err != nil {
		// No specs repo to publish to — nothing to do, not an error here.
		return nil //nolint:nilerr // edit still succeeded; publishing is best-effort.
	}

	noPush := false
	if cmd != nil && cmd.Flags().Lookup("no-push") != nil {
		noPush, _ = cmd.Flags().GetBool("no-push")
	}

	policy := rc.AutoPushPolicy()
	switch {
	case noPush || policy == config.AutoPushOff:
		printManualPushHint(specID)
		return nil
	case policy == config.AutoPushPrompt && !confirmPublish(cmd, specID):
		printManualPushHint(specID)
		return nil
	}

	pushed, err := gitpkg.PushLocalEditsOpts(
		context.Background(),
		&rc.Team.SpecsRepo,
		fmt.Sprintf("feat: update %s", specID),
		syncOpts(cmd, specID),
	)
	if err != nil {
		return fmt.Errorf("publishing %s: %w", specID, err)
	}
	if pushed {
		fmt.Printf("✓ %s published to specs repo\n", specID)
	}
	return nil
}

// printManualPushHint tells the user how to publish later when auto-push is
// disabled, declined, or unavailable.
func printManualPushHint(specID string) {
	fmt.Printf("Local changes saved — run 'spec push %s' to publish to the team.\n", specID)
}

// confirmPublish asks the user whether to publish now. It returns true (publish)
// when the surface is non-interactive (scripted/--quiet/--json or no TTY), since
// such surfaces cannot answer a prompt and the configured intent is to publish.
func confirmPublish(cmd *cobra.Command, specID string) bool {
	if !awarenessAllowed(cmd) {
		return true
	}
	fmt.Printf("Publish %s to the specs repo now? [Y/n] ", specID)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return true
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes"
}
