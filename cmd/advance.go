package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/aaronl1011/spec-cli/internal/pipeline/effects"
	"github.com/spf13/cobra"
)

var advanceCmd = &cobra.Command{
	Use:   "advance [id]",
	Short: "Advance a spec to the next pipeline stage",
	Long: `Move a spec forward in the pipeline after validating role and gates.

By default the command advances to the immediate next stage. Tech leads can
optionally fast-track to a later stage with --to, and --dry-run previews
gate checks and transition effects without persisting changes.`,
	Example: "  spec advance SPEC-042\n  spec advance SPEC-042 --dry-run\n  spec advance SPEC-042 --to done",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runAdvance,
}

func init() {
	advanceCmd.Flags().String("to", "", "skip to a specific stage (TL fast-track only)")
	advanceCmd.Flags().Bool("dry-run", false, "show what would happen without making changes")
	rootCmd.AddCommand(advanceCmd)
}

func runAdvance(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec advance <id>")
	if err != nil {
		return err
	}
	targetStage, _ := cmd.Flags().GetString("to")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

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

	// Work within specs repo
	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		meta, err := readSpecMeta(path)
		if err != nil {
			return "", err
		}

		// Validate the advance
		if err := pipeline.ValidateAdvance(pl, meta.Status, targetStage, role); err != nil {
			return "", err
		}

		// Determine target stage
		target := targetStage
		if target == "" {
			next, err := pipeline.NextStage(pl, meta.Status, true)
			if err != nil {
				return "", fmt.Errorf("cannot advance from %q: %w", meta.Status, err)
			}
			target = next
		}

		// Evaluate gates on the target stage
		sections, err := markdown.ExtractSectionsFromFile(path)
		if err != nil {
			return "", err
		}

		hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
		gateResults := pipeline.EvaluateGates(pl, target, sections, hasPRStack, false, meta)
		if !pipeline.AllGatesPassed(gateResults) {
			fmt.Printf("Gate checks failed for %s → %s:\n", meta.Status, target)
			for _, r := range pipeline.FailedGates(gateResults) {
				fmt.Printf("  ✗ %s\n    %s\n", r.Gate, r.Reason)
			}
			return "", fmt.Errorf("gate conditions not met — resolve the issues above before advancing")
		}

		// Record skipped stages for fast-track
		var skipped []string
		if targetStage != "" {
			skipped = pipeline.SkippedStages(pl, meta.Status, target)
		}

		previousStage := meta.Status

		// Dry-run: show what would happen
		if dryRun {
			fmt.Printf("Dry-run: %s would advance %s → %s\n", specID, previousStage, target)
			if len(skipped) > 0 {
				fmt.Printf("  Skipped stages: %s\n", strings.Join(skipped, ", "))
			}

			// Show effects that would run
			resolvedPipeline, _ := pipeline.Resolve(rc.Team.Pipeline)
			if stage := resolvedPipeline.StageByName(previousStage); stage != nil {
				if len(stage.Transitions.Advance.Effects) > 0 {
					fmt.Println("  Effects:")
					executor := effects.NewExecutor(true)
					execCtx := effects.ExecutionContext{
						SpecID:         specID,
						SpecTitle:      meta.Title,
						FromStage:      previousStage,
						ToStage:        target,
						TransitionType: effects.TransitionAdvance,
						User:           rc.UserName(),
						UserRole:       role,
						DryRun:         true,
					}
					results := executor.Execute(context.Background(), stage.Transitions.Advance.Effects, execCtx)
					for _, r := range results {
						fmt.Printf("    → %s\n", r.Message)
					}
				}
			}
			return "", nil
		}

		// Advance
		_, err = pipeline.Advance(path, meta, target)
		if err != nil {
			return "", err
		}

		// Log skipped stages to decision log for fast-track
		if len(skipped) > 0 {
			msg := fmt.Sprintf("FAST-TRACK: %s → %s. Skipped: %s", previousStage, target, strings.Join(skipped, ", "))
			_, _ = markdown.AppendDecision(path, msg, rc.UserName()) // Best-effort logging
		}

		// Best-effort pattern: DB and effects failures do not block the advance.
		// Rationale: the spec file has already been mutated (line 139); reverting on DB failure
		// would leave the file in an inconsistent state. Effects (notifications, webhooks, logging)
		// are informational — if they fail, the spec still advanced correctly.
		// Errors are logged to the user via warnf() below.
		db, _ := openDB()
		if db != nil {
			defer func() { _ = db.Close() }()
		}

		executor := effects.NewExecutor(false)
		execCtx := effects.ExecutionContext{
			SpecID:         specID,
			SpecTitle:      meta.Title,
			FromStage:      previousStage,
			ToStage:        target,
			TransitionType: effects.TransitionAdvance,
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

		resolvedPipeline, _ := pipeline.Resolve(rc.Team.Pipeline)

		// Execute on_exit effects for the departed stage
		if exitStage := resolvedPipeline.StageByName(previousStage); exitStage != nil {
			if len(exitStage.OnExit) > 0 {
				runEffects(executor, exitStage.OnExit, execCtx)
			}
			if len(exitStage.Transitions.Advance.Effects) > 0 {
				results := runEffects(executor, exitStage.Transitions.Advance.Effects, execCtx)
				if effects.ShouldArchive(results) {
					fmt.Printf("  → spec marked for archiving\n")
				}
			}
		}

		// Execute on_enter effects for the entered stage
		if enterStage := resolvedPipeline.StageByName(target); enterStage != nil {
			if len(enterStage.OnEnter) > 0 {
				runEffects(executor, enterStage.OnEnter, execCtx)
			}
		}
		if rc.Team.Sync.OutboundOnAdvance && execCtx.Syncer != nil {
			if err := execCtx.Syncer.Sync(context.Background(), "out", specID); err != nil {
				warnf("outbound sync failed: %v", err)
			} else {
				fmt.Printf("  → synced out\n")
			}
		}

		fmt.Printf("✓ %s advanced: %s → %s\n", specID, previousStage, target)
		if len(skipped) > 0 {
			fmt.Printf("  Skipped stages: %s\n", strings.Join(skipped, ", "))
		}

		if db != nil {
			metaJSON := fmt.Sprintf(`{"from_stage":%q,"to_stage":%q}`, previousStage, target)
			_ = db.ActivityLog(specID, "advance", fmt.Sprintf("advanced to %s", target), metaJSON, rc.UserName())
		}

		return fmt.Sprintf("feat: advance %s to %s", specID, target), nil
	})
}

func runEffects(executor *effects.Executor, effs []config.EffectConfig, execCtx effects.ExecutionContext) []effects.Result {
	results := executor.Execute(context.Background(), effs, execCtx)
	for _, r := range results {
		if r.Error != nil {
			warnf("effect failed: %v", r.Error)
		} else if !r.Skipped && r.Message != "" {
			fmt.Printf("  → %s\n", r.Message)
		}
	}
	return results
}
