package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/pipeline/effects"
)

// ErrGatesNotMet is returned by Advance when the target stage's gate
// conditions fail. The returned AdvanceResult carries the specific failures
// so callers can render them.
var ErrGatesNotMet = errors.New("gate conditions not met")

// AdvanceInput describes a requested advance.
type AdvanceInput struct {
	SpecID      string
	SpecPath    string // resolved spec file inside the repo clone
	SpecDir     string // the specs/ directory inside the repo clone
	TargetStage string // "" advances to the immediate next stage
	DryRun      bool
}

// AdvanceResult is the render-ready outcome of an advance.
type AdvanceResult struct {
	SpecID        string                `json:"spec_id"`
	PreviousStage string                `json:"previous_stage"`
	NewStage      string                `json:"new_stage"`
	Skipped       []string              `json:"skipped,omitempty"`
	DryRun        bool                  `json:"dry_run,omitempty"`
	Archived      bool                  `json:"archived,omitempty"`
	SyncedOut     bool                  `json:"synced_out,omitempty"`
	GateFailures  []pipeline.GateResult `json:"gate_failures,omitempty"`
	Effects       []EffectOutcome       `json:"effects,omitempty"`

	// CommitMsg is the git commit message for the mutation, or "" when there
	// is nothing to commit (dry-run or gate failure).
	CommitMsg string `json:"-"`
}

// Advance validates and moves a spec to the next (or target) stage, evaluates
// gates, and runs transition effects. On gate failure it returns the populated
// result and ErrGatesNotMet without mutating the spec.
func Advance(ctx context.Context, d Deps, in AdvanceInput) (*AdvanceResult, error) {
	ctx = ensureCtx(ctx)
	pl := d.Config.Pipeline()

	meta, err := readMeta(in.SpecPath)
	if err != nil {
		return nil, err
	}

	if err := pipeline.ValidateAdvance(pl, meta.Status, in.TargetStage, d.Role); err != nil {
		return nil, err
	}

	target := in.TargetStage
	if target == "" {
		next, err := pipeline.NextStage(pl, meta.Status, true)
		if err != nil {
			return nil, fmt.Errorf("cannot advance from %q: %w", meta.Status, err)
		}
		target = next
	}

	res := &AdvanceResult{
		SpecID:        in.SpecID,
		PreviousStage: meta.Status,
		NewStage:      target,
		DryRun:        in.DryRun,
	}

	// Evaluate gates on the target stage.
	sections, err := markdown.ExtractSectionsFromFile(in.SpecPath)
	if err != nil {
		return nil, err
	}
	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	gateResults := pipeline.EvaluateGates(pl, target, sections, hasPRStack, false, meta)
	if !pipeline.AllGatesPassed(gateResults) {
		res.GateFailures = pipeline.FailedGates(gateResults)
		return res, ErrGatesNotMet
	}

	if in.TargetStage != "" {
		res.Skipped = pipeline.SkippedStages(pl, meta.Status, target)
	}

	resolved, _ := d.resolvedPipeline()
	execCtx := d.execContext(in.SpecID, meta.Title, res.PreviousStage, target, in.SpecDir, meta.EpicKey, effects.TransitionAdvance, in.DryRun)

	if in.DryRun {
		res.Effects = d.previewAdvanceEffects(ctx, resolved, res.PreviousStage, execCtx)
		return res, nil
	}

	if _, err := pipeline.Advance(in.SpecPath, meta, target); err != nil {
		return nil, err
	}

	// Fast-track: record skipped stages in the decision log (best-effort).
	if len(res.Skipped) > 0 {
		msg := fmt.Sprintf("FAST-TRACK: %s → %s. Skipped: %s", res.PreviousStage, target, joinComma(res.Skipped))
		_, _ = markdown.AppendDecision(in.SpecPath, msg, d.user())
	}

	res.Effects, res.Archived = d.runTransitionEffects(ctx, resolved, res.PreviousStage, target, execCtx)

	if d.Config.Team.Sync.OutboundOnAdvance && execCtx.Syncer != nil {
		if err := execCtx.Syncer.Sync(ctx, "out", in.SpecID); err != nil {
			res.Effects = append(res.Effects, EffectOutcome{Message: "outbound sync", Err: err.Error()})
		} else {
			res.SyncedOut = true
		}
	}

	metaJSON := fmt.Sprintf(`{"from_stage":%q,"to_stage":%q}`, res.PreviousStage, target)
	d.logActivity(in.SpecID, "advance", fmt.Sprintf("advanced to %s", target), metaJSON)

	res.CommitMsg = fmt.Sprintf("feat: advance %s to %s", in.SpecID, target)
	return res, nil
}

// runTransitionEffects executes on_exit + advance effects for the departed
// stage and on_enter effects for the entered stage, returning the outcomes
// and whether the spec was marked for archiving.
func (d Deps) runTransitionEffects(ctx context.Context, resolved *pipeline.ResolvedPipeline, from, to string, execCtx effects.ExecutionContext) ([]EffectOutcome, bool) {
	if resolved == nil {
		return nil, false
	}
	executor := effects.NewExecutor(false)
	var all []EffectOutcome
	archived := false

	if exitStage := resolved.StageByName(from); exitStage != nil {
		if len(exitStage.OnExit) > 0 {
			all = append(all, outcomes(executor.Execute(ctx, exitStage.OnExit, execCtx))...)
		}
		if len(exitStage.Transitions.Advance.Effects) > 0 {
			results := executor.Execute(ctx, exitStage.Transitions.Advance.Effects, execCtx)
			all = append(all, outcomes(results)...)
			if effects.ShouldArchive(results) {
				archived = true
			}
		}
	}
	if enterStage := resolved.StageByName(to); enterStage != nil && len(enterStage.OnEnter) > 0 {
		all = append(all, outcomes(executor.Execute(ctx, enterStage.OnEnter, execCtx))...)
	}
	return all, archived
}

// previewAdvanceEffects runs the departed stage's advance effects in dry-run
// mode to describe what would happen, without persisting anything.
func (d Deps) previewAdvanceEffects(ctx context.Context, resolved *pipeline.ResolvedPipeline, from string, execCtx effects.ExecutionContext) []EffectOutcome {
	if resolved == nil {
		return nil
	}
	stage := resolved.StageByName(from)
	if stage == nil || len(stage.Transitions.Advance.Effects) == 0 {
		return nil
	}
	executor := effects.NewExecutor(true)
	return outcomes(executor.Execute(ctx, stage.Transitions.Advance.Effects, execCtx))
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
