package cmd

import (
	"context"
	"fmt"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/workflow"
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
	p := newPrinter(cmd)

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
	role, err := requireRole(rc)
	if err != nil {
		return err
	}

	db, _ := openDB()
	if db != nil {
		defer func() { _ = db.Close() }()
	}
	deps := workflow.Deps{Config: rc, Registry: buildRegistry(rc), DB: db, Role: role}

	var res *workflow.EjectResult
	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		var eErr error
		res, eErr = workflow.Eject(context.Background(), deps, workflow.EjectInput{
			SpecID:   specID,
			SpecPath: path,
			Reason:   reason,
		})
		if eErr != nil {
			return "", eErr
		}
		return res.CommitMsg, nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(res)
	}
	p.Line("🚫 %s blocked (was: %s)", res.SpecID, res.PreviousStage)
	p.Line("  Reason: %s", res.Reason)
	p.Line("  Resume with: spec resume %s", res.SpecID)
	return nil
}
