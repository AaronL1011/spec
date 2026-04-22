package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexl/spec-cli/internal/adapter"
	gitpkg "github.com/nexl/spec-cli/internal/git"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var intakeCmd = &cobra.Command{
	Use:   "intake <title>",
	Short: "Create a lightweight triage item",
	Args:  cobra.ExactArgs(1),
	RunE:  runIntake,
}

func init() {
	intakeCmd.Flags().String("source", "", "source of the item (support | alert | stakeholder | engineer | comms)")
	intakeCmd.Flags().String("priority", "medium", "priority (low | medium | high | critical)")
	intakeCmd.Flags().String("source-ref", "", "reference (ticket #, alert ID, Slack permalink)")
	rootCmd.AddCommand(intakeCmd)
}

func runIntake(cmd *cobra.Command, args []string) error {
	title := args[0]
	source, _ := cmd.Flags().GetString("source")
	priority, _ := cmd.Flags().GetString("priority")
	sourceRef, _ := cmd.Flags().GetString("source-ref")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	reg := buildRegistry(rc)

	// Ensure specs repo is up to date
	_, err = gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo)
	if err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Compute next triage ID
	triageFiles, _ := gitpkg.ListTriageFiles(&rc.Team.SpecsRepo)
	triageID := markdown.NextTriageID(triageFiles)

	reportedBy := rc.UserName()
	content := markdown.ScaffoldTriage(triageID, title, priority, source, sourceRef, reportedBy)

	// Write via WithSpecsRepo
	err = gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		triageDir := filepath.Join(repoPath, "triage")
		if err := os.MkdirAll(triageDir, 0o755); err != nil {
			return "", err
		}

		triagePath := filepath.Join(triageDir, triageID+".md")
		if err := os.WriteFile(triagePath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing triage item: %w", err)
		}

		return fmt.Sprintf("feat: intake %s — %s", triageID, title), nil
	})
	if err != nil {
		return err
	}

	// Notify — non-fatal, warn on failure
	if rc.HasIntegration("comms") {
		if err := reg.Comms().Notify(ctx(), adapter.Notification{
			SpecID:  triageID,
			Title:   title,
			Message: fmt.Sprintf("New triage item: %s — %s [%s]", triageID, title, priority),
		}); err != nil {
			warnf("could not send notification: %v", err)
		}
	}

	fmt.Printf("✓ Created %s — %s\n", triageID, title)
	fmt.Printf("  Priority: %s\n", priority)
	if source != "" {
		fmt.Printf("  Source: %s\n", source)
	}
	fmt.Printf("  Promote to spec: spec promote %s\n", triageID)

	return nil
}
