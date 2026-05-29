package cmd

import (
	"context"
	"errors"
	"strings"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/workflow"
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
	p := newPrinter(cmd)

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

	db, _ := openDB()
	if db != nil {
		defer func() { _ = db.Close() }()
	}
	deps := workflow.Deps{Config: rc, Registry: buildRegistry(rc), DB: db, Role: role}

	var res *workflow.AdvanceResult
	gitErr := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		var aErr error
		res, aErr = workflow.Advance(context.Background(), deps, workflow.AdvanceInput{
			SpecID:      specID,
			SpecPath:    path,
			SpecDir:     specsDir(repoPath),
			TargetStage: targetStage,
			DryRun:      dryRun,
		})
		if aErr != nil {
			return "", aErr
		}
		return res.CommitMsg, nil
	})
	if gitErr != nil {
		// Gate failures carry structured detail — render them, then return a
		// clean terminal error without the wrapped git plumbing.
		if res != nil && errors.Is(gitErr, workflow.ErrGatesNotMet) {
			renderGateFailures(p, res)
			return errors.New("gate conditions not met — resolve the issues above before advancing")
		}
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(res)
	}
	renderAdvance(p, res)
	return nil
}

func renderGateFailures(p *printer, res *workflow.AdvanceResult) {
	if p.JSONEnabled() {
		_ = p.JSON(res)
		return
	}
	p.Line("Gate checks failed for %s → %s:", res.PreviousStage, res.NewStage)
	for _, g := range res.GateFailures {
		p.Line("  ✗ %s", g.Gate)
		p.Line("    %s", g.Reason)
	}
}

func renderAdvance(p *printer, res *workflow.AdvanceResult) {
	if res.DryRun {
		p.Line("Dry-run: %s would advance %s → %s", res.SpecID, res.PreviousStage, res.NewStage)
		if len(res.Skipped) > 0 {
			p.Line("  Skipped stages: %s", strings.Join(res.Skipped, ", "))
		}
		if len(res.Effects) > 0 {
			p.Line("  Effects:")
			for _, e := range res.Effects {
				p.Line("    → %s", e.Message)
			}
		}
		return
	}

	renderEffects(p, res.Effects)
	if res.Archived {
		p.Line("  → spec marked for archiving")
	}
	if res.SyncedOut {
		p.Line("  → synced out")
	}
	p.Line("✓ %s advanced: %s → %s", res.SpecID, res.PreviousStage, res.NewStage)
	if len(res.Skipped) > 0 {
		p.Line("  Skipped stages: %s", strings.Join(res.Skipped, ", "))
	}
}

// renderEffects prints effect outcomes: successes to stdout, errors as warnings.
func renderEffects(p *printer, effs []workflow.EffectOutcome) {
	for _, e := range effs {
		if e.Err != "" {
			p.Warn("effect failed: %s", e.Err)
			continue
		}
		if e.Message != "" {
			p.Line("  → %s", e.Message)
		}
	}
}
