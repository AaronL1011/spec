package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec-cli/internal/adapter"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var ejectCmd = &cobra.Command{
	Use:   "eject [id]",
	Short: "Log a blocker and transition to blocked status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEject,
}

func init() {
	ejectCmd.Flags().String("reason", "", "reason for blocking (required)")
	rootCmd.AddCommand(ejectCmd)
}

func runEject(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec eject <id>")
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString("reason")

	if reason == "" {
		return fmt.Errorf("--reason is required — explain what's blocking the spec")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

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

		if meta.Status == pipeline.StatusBlocked {
			return "", fmt.Errorf("%s is already blocked — use 'spec resume %s' to unblock", specID, specID)
		}

		result, err := pipeline.Eject(path, meta, reason, rc.UserName())
		if err != nil {
			return "", err
		}

		// Notify TL — non-fatal, warn on failure
		if rc.HasIntegration("comms") {
			if err := reg.Comms().Notify(ctx(), adapter.Notification{
				SpecID:  specID,
				Title:   meta.Title,
				Message: fmt.Sprintf("🚫 [%s] BLOCKED from %s | Reason: %s | By: %s", specID, result.PreviousStage, reason, rc.UserName()),
			}); err != nil {
				warnf("could not send notification: %v", err)
			}
		}

		fmt.Printf("🚫 %s blocked (was: %s)\n", specID, result.PreviousStage)
		fmt.Printf("  Reason: %s\n", reason)
		fmt.Printf("  Resume with: spec resume %s\n", specID)

		if db, dbErr := openDB(); dbErr == nil {
			defer func() { _ = db.Close() }()
			metaJSON := fmt.Sprintf(`{"from_stage":%q,"reason":%q}`, result.PreviousStage, reason)
			_ = db.ActivityLog(specID, "eject", fmt.Sprintf("blocked from %s", result.PreviousStage), metaJSON, rc.UserName())
		}

		return fmt.Sprintf("fix: eject %s — %s", specID, reason), nil
	})
}
