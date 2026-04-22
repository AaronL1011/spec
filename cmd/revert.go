package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexl/spec-cli/internal/adapter"
	gitpkg "github.com/nexl/spec-cli/internal/git"
	"github.com/nexl/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var revertCmd = &cobra.Command{
	Use:   "revert <id>",
	Short: "Send a spec back to a previous stage",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevert,
}

func init() {
	revertCmd.Flags().String("to", "", "target stage to revert to (required)")
	revertCmd.Flags().String("reason", "", "reason for reversion (required)")
	rootCmd.AddCommand(revertCmd)
}

func runRevert(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])
	targetStage, _ := cmd.Flags().GetString("to")
	reason, _ := cmd.Flags().GetString("reason")

	if targetStage == "" {
		return fmt.Errorf("--to is required — specify the stage to revert to")
	}
	if reason == "" {
		return fmt.Errorf("--reason is required — explain why the spec is being reverted")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	role, err := requireRole(rc)
	if err != nil {
		return err
	}

	pl := rc.Pipeline()
	reg := buildRegistry(rc)

	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		meta, err := readSpecMeta(path)
		if err != nil {
			return "", err
		}

		if err := pipeline.ValidateRevert(pl, meta.Status, targetStage, role); err != nil {
			return "", err
		}

		previousStage := meta.Status
		if err := pipeline.Revert(path, meta, targetStage, reason, rc.UserName()); err != nil {
			return "", err
		}

		// Notify both owners — non-fatal, warn on failure
		if rc.HasIntegration("comms") {
			currentOwner := pipeline.StageOwner(pl, previousStage)
			targetOwner := pipeline.StageOwner(pl, targetStage)
			if err := reg.Comms().Notify(ctx(), adapter.Notification{
				SpecID:  specID,
				Title:   meta.Title,
				Message: fmt.Sprintf("[%s] Reverted: %s → %s | Reason: %s | From: %s, To: %s", specID, previousStage, targetStage, reason, currentOwner, targetOwner),
			}); err != nil {
				warnf("could not send notification: %v", err)
			}
		}

		fmt.Printf("✓ %s reverted: %s → %s\n", specID, previousStage, targetStage)
		fmt.Printf("  Reason: %s\n", reason)

		return fmt.Sprintf("fix: revert %s to %s — %s", specID, targetStage, reason), nil
	})
}
