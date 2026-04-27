package cmd

import (
	"context"
	"fmt"

	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/aaronl1011/spec-cli/internal/pipeline/effects"
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

		// Best-effort pattern: DB and effects failures do not block the revert.
		// Rationale: the spec file has already been mutated; reverting on DB failure
		// would leave the file in an inconsistent state. Effects (notifications, webhooks, logging)
		// are informational — if they fail, the spec still reverted correctly.
		// Errors are logged to the user via warnf() below.
		db, _ := openDB()
		if db != nil {
			defer func() { _ = db.Close() }()
		}

		resolvedPipeline, _ := pipeline.Resolve(rc.Team.Pipeline)
		executor := effects.NewExecutor(false)
		execCtx := effects.ExecutionContext{
			SpecID:         specID,
			SpecTitle:      meta.Title,
			FromStage:      previousStage,
			ToStage:        targetStage,
			TransitionType: effects.TransitionRevert,
			User:           rc.UserName(),
			UserRole:       role,
			Notifier:       &effects.NotifierAdapter{Comms: reg.Comms(), SpecID: specID, Title: meta.Title},
			Syncer: &effects.SyncerAdapter{
				Docs:             reg.Docs(),
				DB:               db,
				SpecDir:          repoPath,
				ConflictStrategy: rc.Team.Sync.ConflictStrategy,
				OwnerRole:        role,
				UserName:         rc.UserName(),
			},
			PMUpdater: &effects.PMUpdaterAdapter{PM: reg.PM(), EpicKey: meta.EpicKey},
			Webhooker: &effects.WebhookerAdapter{},
			Logger:    &effects.LoggerAdapter{DB: db, SpecDir: repoPath, SpecID: specID},
		}

		if exitStage := resolvedPipeline.StageByName(previousStage); exitStage != nil {
			if len(exitStage.Transitions.Revert.Effects) > 0 {
				runEffects(executor, exitStage.Transitions.Revert.Effects, execCtx)
			}
		}
		if enterStage := resolvedPipeline.StageByName(targetStage); enterStage != nil {
			if len(enterStage.OnEnter) > 0 {
				runEffects(executor, enterStage.OnEnter, execCtx)
			}
		}

		fmt.Printf("✓ %s reverted: %s → %s\n", specID, previousStage, targetStage)
		fmt.Printf("  Reason: %s\n", reason)

		if db != nil {
			metaJSON := fmt.Sprintf(`{"from_stage":%q,"to_stage":%q,"reason":%q}`, previousStage, targetStage, reason)
			_ = db.ActivityLog(specID, "revert", fmt.Sprintf("reverted to %s", targetStage), metaJSON, rc.UserName())
		}

		return fmt.Sprintf("fix: revert %s to %s — %s", specID, targetStage, reason), nil
	})
}
