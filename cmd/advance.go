package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/adapter"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/aaronl1011/spec-cli/internal/pipeline/effects"
	"github.com/spf13/cobra"
)

var advanceCmd = &cobra.Command{
	Use:   "advance <id>",
	Short: "Advance a spec to the next pipeline stage",
	Long: `Move a spec forward in the pipeline after validating role and gates.

By default the command advances to the immediate next stage. Tech leads can
optionally fast-track to a later stage with --to, and --dry-run previews
gate checks and transition effects without persisting changes.`,
	Example: "  spec advance SPEC-042\n  spec advance SPEC-042 --dry-run\n  spec advance SPEC-042 --to done",
	Args:  cobra.ExactArgs(1),
	RunE:  runAdvance,
}

func init() {
	advanceCmd.Flags().String("to", "", "skip to a specific stage (TL fast-track only)")
	advanceCmd.Flags().Bool("dry-run", false, "show what would happen without making changes")
	rootCmd.AddCommand(advanceCmd)
}

func runAdvance(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])
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
		gateResults := pipeline.EvaluateGates(pl, target, sections, hasPRStack, false)
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

		// Execute transition effects from pipeline config
		resolvedPipeline, _ := pipeline.Resolve(rc.Team.Pipeline)
		if stage := resolvedPipeline.StageByName(previousStage); stage != nil {
			if len(stage.Transitions.Advance.Effects) > 0 {
				executor := effects.NewExecutor(false)
				execCtx := effects.ExecutionContext{
					SpecID:         specID,
					SpecTitle:      meta.Title,
					FromStage:      previousStage,
					ToStage:        target,
					TransitionType: effects.TransitionAdvance,
					User:           rc.UserName(),
					UserRole:       role,
				}

				results := executor.Execute(context.Background(), stage.Transitions.Advance.Effects, execCtx)
				for _, r := range results {
					if r.Error != nil {
						warnf("effect failed: %v", r.Error)
					} else if r.Skipped {
						// Silent skip
					} else if r.Message != "" {
						fmt.Printf("  → %s\n", r.Message)
					}
				}

				// Handle archive effect
				if effects.ShouldArchive(results) {
					// Archive will be handled by separate archive command
					fmt.Printf("  → spec marked for archiving\n")
				}
			}
		}

		// Legacy: Notify — non-fatal, warn on failure
		// TODO: migrate to effects system
		if rc.HasIntegration("comms") {
			nextOwner := pipeline.StageOwner(pl, target)
			if err := reg.Comms().Notify(ctx(), adapter.Notification{
				SpecID:  specID,
				Title:   meta.Title,
				Message: fmt.Sprintf("[%s] %s → %s | Owner: %s", specID, previousStage, target, nextOwner),
			}); err != nil {
				warnf("could not send notification: %v", err)
			}
		}

		// Legacy: Sync status to PM — non-fatal, warn on failure
		// TODO: migrate to effects system
		if rc.HasIntegration("pm") && meta.EpicKey != "" {
			if err := reg.PM().UpdateStatus(ctx(), meta.EpicKey, target); err != nil {
				warnf("could not sync status to PM: %v", err)
			}
		}

		fmt.Printf("✓ %s advanced: %s → %s\n", specID, previousStage, target)
		if len(skipped) > 0 {
			fmt.Printf("  Skipped stages: %s\n", strings.Join(skipped, ", "))
		}

		return fmt.Sprintf("feat: advance %s to %s", specID, target), nil
	})
}
