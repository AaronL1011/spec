package cmd

import (
	"bufio"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/spf13/cobra"
)

var nukeCmd = &cobra.Command{
	Use:   "nuke",
	Short: "Hard-reset the local specs-repo clone (last resort for corrupt state)",
	Long: `Delete the local specs-repo clone and re-clone it from origin.

Most corruption (e.g. a truncated object store after a mid-fetch network drop)
is auto-repaired transparently — you should rarely need this. Use 'spec nuke'
only when the local clone is beyond repair and you just need a clean slate.

This DISCARDS all local state in the clone: uncommitted edits AND any local
commits that have not been pushed. It does not touch the remote. Anything
already pushed is safe and comes back with the fresh clone.`,
	Example: "  spec nuke\n  spec nuke --yes",
	Args:    cobra.NoArgs,
	RunE:    runNuke,
}

func init() {
	nukeCmd.Flags().Bool("yes", false, "skip the confirmation prompt")
	rootCmd.AddCommand(nukeCmd)
}

func runNuke(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}
	cfg := &rc.Team.SpecsRepo
	p := newPrinter(cmd)

	state := gitpkg.InspectLocalState(ctx(), cfg)
	queued := clearableQueuedCount(cfg)

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes && !confirmNuke(cmd, state, queued) {
		p.Line("Aborted — nothing was changed.")
		return nil
	}

	if err := gitpkg.NukeSpecsRepo(ctx(), cfg); err != nil {
		return err
	}
	cleared := clearQueuedPushes(cfg)

	if p.JSONEnabled() {
		return p.JSON(map[string]any{
			"repo":           gitpkg.RepoKey(cfg),
			"reset":          true,
			"queued_cleared": cleared,
		})
	}
	p.Line("✓ Re-cloned %s from origin — local clone is clean.", gitpkg.RepoKey(cfg))
	if cleared > 0 {
		p.Line("  Cleared %d queued push(es) that referenced the discarded clone.", cleared)
	}
	return nil
}

// confirmNuke warns about at-risk local work and returns whether the user
// confirmed the destructive reset. A non-interactive shell without --yes
// declines (nuke never silently destroys work in a script).
func confirmNuke(cmd *cobra.Command, state gitpkg.LocalState, queued int) bool {
	if !state.Exists {
		cmd.Println("No local clone found — 'spec nuke' will create a fresh one.")
	}
	if state.UncommittedChanges {
		cmd.Println("⚠  You have UNCOMMITTED local edits in the specs repo — they will be lost.")
	}
	if state.UnpushedCommits || queued > 0 {
		cmd.Println("⚠  You have local commits that have NOT reached the remote — they will be lost.")
	}
	if !isInteractiveStdin() {
		cmd.Println("Refusing to hard-reset non-interactively without --yes.")
		return false
	}
	cmd.Print("Delete and re-clone the local specs repo? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

// isInteractiveStdin reports whether stdin is a terminal, so nuke can refuse to
// destroy work unattended.
func isInteractiveStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// clearableQueuedCount reports how many queued pushes reference this clone, for
// the pre-nuke warning. Best-effort: 0 if the store is unavailable.
func clearableQueuedCount(cfg *config.SpecsRepoConfig) int {
	db, err := openDB()
	if err != nil {
		return 0
	}
	defer func() { _ = db.Close() }()
	n, err := db.QueuePushCount(gitpkg.RepoKey(cfg))
	if err != nil {
		return 0
	}
	return n
}

// clearQueuedPushes removes every queued push referencing the discarded clone.
// Best-effort: returns the number cleared, or 0 if the store is unavailable.
func clearQueuedPushes(cfg *config.SpecsRepoConfig) int {
	db, err := openDB()
	if err != nil {
		return 0
	}
	defer func() { _ = db.Close() }()
	n, err := db.QueuePushClear(gitpkg.RepoKey(cfg))
	if err != nil {
		return 0
	}
	return n
}
