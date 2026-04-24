// Package pipeline implements the spec pipeline stage machine.
package pipeline

import (
	"fmt"

	"github.com/aaronl1011/spec-cli/internal/config"
)

const (
	// StatusBlocked is the escape hatch status.
	StatusBlocked = "blocked"
)

// StageOwner returns the owner role for a stage as a display string.
func StageOwner(pipeline config.PipelineConfig, stageName string) string {
	s := pipeline.StageByName(stageName)
	if s == nil {
		return ""
	}
	return s.GetOwner()
}

// StageHasOwner returns true if the given role is an owner of the stage.
func StageHasOwner(pipeline config.PipelineConfig, stageName, role string) bool {
	s := pipeline.StageByName(stageName)
	if s == nil {
		return false
	}
	return s.HasOwner(role)
}

// NextStage returns the next non-optional stage, or the next stage if includeOptional.
func NextStage(pipeline config.PipelineConfig, current string, includeOptional bool) (string, error) {
	idx := pipeline.StageIndex(current)
	if idx < 0 {
		return "", fmt.Errorf("unknown stage %q", current)
	}

	for i := idx + 1; i < len(pipeline.Stages); i++ {
		stage := pipeline.Stages[i]
		if !stage.Optional || includeOptional {
			return stage.Name, nil
		}
	}

	return "", fmt.Errorf("no next stage after %q", current)
}

// ValidateAdvance checks if advancing from the current stage is permitted.
func ValidateAdvance(pipeline config.PipelineConfig, currentStage, targetStage, userRole string) error {
	if currentStage == StatusBlocked {
		return fmt.Errorf("spec is blocked — use 'spec resume' to unblock before advancing")
	}

	// Check user owns the current stage
	if !StageHasOwner(pipeline, currentStage, userRole) && userRole != "tl" {
		owner := StageOwner(pipeline, currentStage)
		return fmt.Errorf("stage %q is owned by %q — only the stage owner or a TL can advance", currentStage, owner)
	}

	// For TL fast-track (--to flag)
	if targetStage != "" {
		if userRole != "tl" {
			return fmt.Errorf("fast-track (--to) requires owner_role: tl — your role is %q", userRole)
		}
		if !pipeline.IsValidTransition(currentStage, targetStage) {
			return fmt.Errorf("cannot advance from %q to %q — target must be a later stage", currentStage, targetStage)
		}
		return nil
	}

	return nil
}

// ValidateRevert checks if reverting to a previous stage is permitted.
func ValidateRevert(pipeline config.PipelineConfig, currentStage, targetStage, userRole string) error {
	if currentStage == StatusBlocked {
		return fmt.Errorf("spec is blocked — use 'spec resume' to unblock, then revert if needed")
	}

	// Check user owns the current stage
	if !StageHasOwner(pipeline, currentStage, userRole) {
		owner := StageOwner(pipeline, currentStage)
		return fmt.Errorf("only the current stage owner (%s) can revert — your role is %q", owner, userRole)
	}

	if !pipeline.IsValidReversion(currentStage, targetStage) {
		return fmt.Errorf("cannot revert from %q to %q — target must be a previous stage", currentStage, targetStage)
	}

	return nil
}

// SkippedStages returns the stages that would be skipped in a fast-track.
func SkippedStages(pipeline config.PipelineConfig, from, to string) []string {
	fromIdx := pipeline.StageIndex(from)
	toIdx := pipeline.StageIndex(to)
	if fromIdx < 0 || toIdx <= fromIdx+1 {
		return nil
	}

	var skipped []string
	for i := fromIdx + 1; i < toIdx; i++ {
		skipped = append(skipped, pipeline.Stages[i].Name)
	}
	return skipped
}
