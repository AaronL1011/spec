package cmd

import (
	"context"
	"fmt"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/workflow"
	"github.com/spf13/cobra"
)

var revertCmd = &cobra.Command{
	Use:   "revert [id]",
	Short: "Send a spec back to a previous stage",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRevert,
}

func init() {
	revertCmd.Flags().String("to", "", "target stage to revert to (required)")
	revertCmd.Flags().String("reason", "", "reason for reversion (required)")
	rootCmd.AddCommand(revertCmd)
}

func runRevert(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	specID, err := resolveSpecIDArg(args, "spec revert <id>")
	if err != nil {
		return err
	}
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

	db, _ := openDB()
	if db != nil {
		defer func() { _ = db.Close() }()
	}
	deps := workflow.Deps{Config: rc, Registry: buildRegistry(rc), DB: db, Role: role}

	var res *workflow.RevertResult
	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		var rErr error
		res, rErr = workflow.Revert(context.Background(), deps, workflow.RevertInput{
			SpecID:      specID,
			SpecPath:    path,
			SpecDir:     specsDir(repoPath),
			TargetStage: targetStage,
			Reason:      reason,
		})
		if rErr != nil {
			return "", rErr
		}
		return res.CommitMsg, nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(res)
	}
	renderEffects(p, res.Effects)
	p.Line("✓ %s reverted: %s → %s", res.SpecID, res.PreviousStage, res.TargetStage)
	p.Line("  Reason: %s", res.Reason)
	return nil
}
