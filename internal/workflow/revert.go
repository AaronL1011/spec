package workflow

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/pipeline/effects"
)

// RevertInput describes a requested reversion.
type RevertInput struct {
	SpecID      string
	SpecPath    string
	SpecDir     string
	TargetStage string
	Reason      string
}

// RevertResult is the render-ready outcome of a reversion.
type RevertResult struct {
	SpecID        string          `json:"spec_id"`
	PreviousStage string          `json:"previous_stage"`
	TargetStage   string          `json:"target_stage"`
	Reason        string          `json:"reason"`
	Effects       []EffectOutcome `json:"effects,omitempty"`

	CommitMsg string `json:"-"`
}

// Revert sends a spec back to an earlier stage with a reason, runs revert/enter
// effects, and records activity.
func Revert(ctx context.Context, d Deps, in RevertInput) (*RevertResult, error) {
	ctx = ensureCtx(ctx)
	pl := d.Config.Pipeline()

	meta, err := readMeta(in.SpecPath)
	if err != nil {
		return nil, err
	}

	if err := pipeline.ValidateRevert(pl, meta.Status, in.TargetStage, d.Role); err != nil {
		return nil, err
	}

	res := &RevertResult{
		SpecID:        in.SpecID,
		PreviousStage: meta.Status,
		TargetStage:   in.TargetStage,
		Reason:        in.Reason,
	}

	if err := pipeline.Revert(in.SpecPath, meta, in.TargetStage, in.Reason, d.user()); err != nil {
		return nil, err
	}

	resolved, _ := d.resolvedPipeline()
	execCtx := d.execContext(in.SpecID, meta.Title, res.PreviousStage, in.TargetStage, in.SpecDir, meta.EpicKey, effects.TransitionRevert, false)

	if resolved != nil {
		executor := effects.NewExecutor(false)
		if exitStage := resolved.StageByName(res.PreviousStage); exitStage != nil && len(exitStage.Transitions.Revert.Effects) > 0 {
			res.Effects = append(res.Effects, outcomes(executor.Execute(ctx, exitStage.Transitions.Revert.Effects, execCtx))...)
		}
		if enterStage := resolved.StageByName(in.TargetStage); enterStage != nil && len(enterStage.OnEnter) > 0 {
			res.Effects = append(res.Effects, outcomes(executor.Execute(ctx, enterStage.OnEnter, execCtx))...)
		}
	}

	metaJSON := fmt.Sprintf(`{"from_stage":%q,"to_stage":%q,"reason":%q}`, res.PreviousStage, in.TargetStage, in.Reason)
	d.logActivity(in.SpecID, "revert", fmt.Sprintf("reverted to %s", in.TargetStage), metaJSON)

	res.CommitMsg = fmt.Sprintf("fix: revert %s to %s — %s", in.SpecID, in.TargetStage, in.Reason)
	return res, nil
}
