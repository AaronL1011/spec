package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexl/spec-cli/internal/adapter"
	"github.com/nexl/spec-cli/internal/config"
	gitpkg "github.com/nexl/spec-cli/internal/git"
	"github.com/nexl/spec-cli/internal/markdown"
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

	// Compute next spec ID
	specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
	allFiles := append(specFiles, archiveFiles...)
	specID := markdown.NextSpecID(allFiles)

	author := gitpkg.UserName(ctx())
	cycle := rc.CycleLabel()
	source := triageID

	content := markdown.ScaffoldSpec(specID, title, author, cycle, source)

	var newSpecID string

	err = gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		// Write the new spec
		specPath := filepath.Join(repoPath, specID+".md")
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing spec: %w", err)
		}

		// Remove the triage item
		triageFile := filepath.Join(repoPath, "triage", triageID+".md")
		if err := os.Remove(triageFile); err != nil {
			// Non-fatal — the triage file might already be gone
			warnf("could not remove triage file: %v", err)
		}

		newSpecID = specID
		return fmt.Sprintf("feat: promote %s to %s — %s", triageID, specID, title), nil
	})
	if err != nil {
		return err
	}

	// Create PM epic if configured
	if rc.HasIntegration("pm") {
		epicKey, pmErr := reg.PM().CreateEpic(ctx(), adapter.SpecMeta{
			ID:    newSpecID,
			Title: title,
		})
		if pmErr != nil {
			warnf("could not create PM epic: %v", pmErr)
		} else if epicKey != "" {
			fmt.Printf("Created PM epic: %s\n", epicKey)
		}
	}

	// Notify — non-fatal, warn on failure
	if rc.HasIntegration("comms") {
		if err := reg.Comms().Notify(ctx(), adapter.Notification{
			SpecID:  newSpecID,
			Title:   title,
			Message: fmt.Sprintf("Promoted %s → %s — %s (status: draft)", triageID, newSpecID, title),
		}); err != nil {
			warnf("could not send notification: %v", err)
		}
	}

	fmt.Printf("✓ Promoted %s → %s — %s\n", triageID, newSpecID, title)
	fmt.Printf("  Status: draft\n")
	fmt.Printf("  Edit with: spec edit %s\n", newSpecID)

	return nil
}
