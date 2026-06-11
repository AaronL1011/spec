package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var assignCmd = &cobra.Command{
	Use:   "assign [id] [@user...]",
	Short: "Assign a spec to one or more people (defaults to you)",
	Long: `Assign a spec to the people responsible for moving it at its current stage.

Assignees drive personal dashboard scope: at an assignee-scoped stage, only a
spec's assignees see it in their DO section, while unassigned specs surface to
the whole owning role so anyone can claim them.

With no users, assigns the spec to you. Use --clear to remove all assignees.`,
	Example: "  spec assign SPEC-042\n  spec assign SPEC-042 @ana @ben\n  spec assign SPEC-042 --clear",
	Args:    cobra.MinimumNArgs(0),
	RunE:    runAssign,
}

func init() {
	assignCmd.Flags().Bool("clear", false, "remove all assignees")
	rootCmd.AddCommand(assignCmd)
}

func runAssign(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	var idArg []string
	var users []string
	if len(args) > 0 {
		idArg = args[:1]
		users = args[1:]
	}
	specID, err := resolveSpecIDArg(idArg, "spec assign <id> [@user...]")
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	clearAll, _ := cmd.Flags().GetBool("clear")
	if clearAll && len(users) > 0 {
		return fmt.Errorf("cannot combine --clear with explicit assignees")
	}

	assignees := normalizeAssignees(users)
	if !clearAll && len(assignees) == 0 {
		self := selfIdentity(rc)
		if self == "" {
			return fmt.Errorf("no user to assign — set user.name or user.handle in ~/.spec/config.yaml, or pass @user")
		}
		assignees = []string{self}
	}

	var commitMsg string
	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			return "", err
		}
		meta.Assignees = assignees
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}
		if clearAll {
			commitMsg = fmt.Sprintf("chore: unassign %s", specID)
		} else {
			commitMsg = fmt.Sprintf("chore: assign %s to %s", specID, strings.Join(assignees, ", "))
		}
		return commitMsg, nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(map[string]interface{}{"spec_id": specID, "assignees": assignees})
	}
	if clearAll {
		p.Line("✓ %s unassigned", specID)
	} else {
		p.Line("✓ %s assigned to %s", specID, strings.Join(assignees, ", "))
	}
	return nil
}

// shouldAutoClaim reports whether starting work on a spec should claim it: the
// spec is at an assignee-scoped stage and currently unassigned, and we have a
// self identity to assign. This is a cheap local check so callers can skip the
// specs-repo round-trip in the common (non-assignee-scoped) case.
func shouldAutoClaim(rc *config.ResolvedConfig, meta *markdown.SpecMeta) bool {
	if rc.Team == nil || len(meta.Assignees) > 0 || selfIdentity(rc) == "" {
		return false
	}
	stage := rc.Pipeline().StageByName(meta.Status)
	return stage != nil && stage.Dashboard.Scope() == config.DoScopeAssignee
}

// autoClaim assigns the current user to an unassigned, assignee-scoped spec so
// starting work claims it without ceremony. It re-checks the gating conditions
// authoritatively inside the specs-repo commit (the local copy may be stale)
// and is best-effort: the caller surfaces any error as a warning, never a
// build blocker.
func autoClaim(cmd *cobra.Command, rc *config.ResolvedConfig, specID string) (bool, error) {
	self := selfIdentity(rc)
	if self == "" {
		return false, nil
	}
	pl := rc.Pipeline()
	claimed := false
	err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			return "", err
		}
		stage := pl.StageByName(meta.Status)
		if stage == nil || stage.Dashboard.Scope() != config.DoScopeAssignee || len(meta.Assignees) > 0 {
			return "", nil // not claimable or already claimed — no-op, no commit
		}
		meta.Assignees = []string{self}
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}
		claimed = true
		return fmt.Sprintf("chore: claim %s for %s", specID, self), nil
	})
	return claimed, err
}

// maybeAutoClaim performs a best-effort auto-claim and prints the outcome,
// guarded by a cheap local pre-check. Never returns an error: claiming is a
// convenience, not a precondition for building.
func maybeAutoClaim(cmd *cobra.Command, rc *config.ResolvedConfig, specID string, meta *markdown.SpecMeta) {
	if !shouldAutoClaim(rc, meta) {
		return
	}
	claimed, err := autoClaim(cmd, rc, specID)
	switch {
	case err != nil:
		warnf("could not claim %s: %v", specID, err)
	case claimed:
		fmt.Printf("Claimed %s — you're now the assignee.\n", specID)
	}
}

// selfIdentity returns the current user's preferred assignment identity,
// favouring the comms handle over the display name.
func selfIdentity(rc *config.ResolvedConfig) string {
	if h := strings.TrimSpace(rc.UserHandle()); h != "" {
		return h
	}
	if n := strings.TrimSpace(rc.UserName()); n != "" && n != "unknown" {
		return n
	}
	return ""
}

// normalizeAssignees trims, drops blanks, and de-duplicates assignees
// (case-insensitively, ignoring a leading '@') while preserving input order.
func normalizeAssignees(users []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, u := range users {
		t := strings.TrimSpace(u)
		if t == "" {
			continue
		}
		key := strings.TrimPrefix(strings.ToLower(t), "@")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}
