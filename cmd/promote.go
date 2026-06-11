package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var promoteCmd = &cobra.Command{
	Use:   "promote <triage-id>",
	Short: "Promote a triage item to a full spec",
	Args:  cobra.ExactArgs(1),
	RunE:  runPromote,
}

func init() {
	promoteCmd.Flags().String("title", "", "override the spec title (defaults to triage title)")
	rootCmd.AddCommand(promoteCmd)
}

func runPromote(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	triageID := strings.ToUpper(args[0])
	titleOverride, _ := cmd.Flags().GetString("title")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	reg := buildRegistry(rc)

	// Ensure specs repo
	_, err = gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo)
	if err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Read the triage item
	triagePath := gitpkg.TriageFilePath(&rc.Team.SpecsRepo, triageID+".md")
	triageMeta, err := markdown.ReadTriageMeta(triagePath)
	if err != nil {
		return fmt.Errorf("triage item %s not found — check the ID and try again", triageID)
	}

	title := triageMeta.Title
	if titleOverride != "" {
		title = titleOverride
	}

	// Claim an authoritative spec ID before writing (SPEC-018).
	specID, err := claimSpecID(ctx(), rc)
	if err != nil {
		return err
	}

	author := gitpkg.UserName(ctx())
	cycle := rc.CycleLabel()
	source := triageID

	content := markdown.ScaffoldSpec(specID, title, author, cycle, source)

	var newSpecID string

	err = gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		sd := specsDir(repoPath)

		// Write the new spec
		specPath := filepath.Join(sd, specID+".md")
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing spec: %w", err)
		}

		// Remove the triage item
		triageFile := filepath.Join(sd, "triage", triageID+".md")
		if err := os.Remove(triageFile); err != nil {
			// Non-fatal — the triage file might already be gone
			p.Warn("could not remove triage file: %v", err)
		}

		newSpecID = specID
		return fmt.Sprintf("feat: promote %s to %s — %s", triageID, specID, title), nil
	})
	if err != nil {
		return err
	}

	// Find-or-create the PM epic if configured (idempotent, crash-safe).
	var epicKey string
	if rc.HasIntegration("pm") {
		sm := pmSpecMeta(rc, newSpecID, title, &markdownMeta{Status: "draft"})
		epicKey = ensureEpic(rc, reg, newSpecID, sm)
	}

	// Notify — non-fatal, warn on failure
	if rc.HasIntegration("comms") {
		if err := reg.Comms().Notify(ctx(), adapter.Notification{
			SpecID:  newSpecID,
			Title:   title,
			Message: fmt.Sprintf("Promoted %s → %s — %s (status: draft)", triageID, newSpecID, title),
		}); err != nil {
			p.Warn("could not send notification: %v", err)
		}
	}

	if p.JSONEnabled() {
		return p.JSON(map[string]interface{}{
			"triage_id": triageID, "spec_id": newSpecID, "title": title,
			"status": "draft", "epic_key": epicKey,
		})
	}
	if epicKey != "" {
		p.Line("Created PM epic: %s", epicKey)
	}
	p.Line("✓ Promoted %s → %s — %s", triageID, newSpecID, title)
	p.Line("  Status: draft")
	p.Line("  Edit with: spec edit %s", newSpecID)
	return nil
}
