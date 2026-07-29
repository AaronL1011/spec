// Package pipeline implements the spec pipeline stage machine.
package pipeline

import (
	"fmt"
	"slices"

	"github.com/aaronl1011/spec/internal/config"
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

// Reasons a stage qualifies as terminal, in the order the rules are applied.
// They are user-facing: `spec pipeline` and `spec config test` print them so a
// team can see why a stage settles a spec without reading this file.
const (
	// TerminalReasonAutoArchive marks a stage with auto_archive: true.
	TerminalReasonAutoArchive = "auto_archive"

	// TerminalReasonLastRequired marks the final non-optional stage.
	TerminalReasonLastRequired = "last required stage"

	// TerminalReasonNameFallback marks a stage matched only by its name,
	// because no other rule produced a terminal stage.
	TerminalReasonNameFallback = "name fallback"
)

// fallbackTerminalNames are treated as completion stages when no structural
// rule matches — only reachable when every stage is optional.
var fallbackTerminalNames = []string{"done", "closed"}

// TerminalStage is a completion stage together with the rule that made it one.
type TerminalStage struct {
	// Name is the stage name.
	Name string

	// Reason is the rule that qualified it, one of the TerminalReason values.
	Reason string
}

// TerminalStages returns the names of stages that represent completion.
// The last required stage and any auto-archive stages are considered terminal.
// Falls back to "done" and "closed" if nothing else matches.
func TerminalStages(pipeline config.PipelineConfig) []string {
	withReasons := TerminalStagesWithReasons(pipeline)
	if len(withReasons) == 0 {
		return nil
	}
	names := make([]string, 0, len(withReasons))
	for _, t := range withReasons {
		names = append(names, t.Name)
	}
	return names
}

// TerminalStagesWithReasons is TerminalStages plus the rule that qualified each
// stage. Both read the same derivation, so an explanation can never drift from
// the behaviour it explains.
func TerminalStagesWithReasons(pipeline config.PipelineConfig) []TerminalStage {
	var terminal []TerminalStage
	seen := make(map[string]bool)

	// Any auto-archive stage is terminal
	for _, s := range pipeline.Stages {
		if s.AutoArchive {
			terminal = append(terminal, TerminalStage{Name: s.Name, Reason: TerminalReasonAutoArchive})
			seen[s.Name] = true
		}
	}

	// The last required stage is terminal (typically "done")
	required := pipeline.RequiredStages()
	if len(required) > 0 {
		last := required[len(required)-1].Name
		if !seen[last] {
			terminal = append(terminal, TerminalStage{Name: last, Reason: TerminalReasonLastRequired})
		}
	}

	if len(terminal) > 0 {
		return terminal
	}

	// Fallback: if no terminal stages found, treat "done" and "closed" as terminal
	for _, s := range pipeline.Stages {
		if slices.Contains(fallbackTerminalNames, s.Name) {
			terminal = append(terminal, TerminalStage{Name: s.Name, Reason: TerminalReasonNameFallback})
		}
	}

	return terminal
}

// IsTerminalStage reports whether stage represents completion for this
// pipeline. It is the single question callers ask when a transition should
// settle something permanently (an earned bounty, an archive).
func IsTerminalStage(pipeline config.PipelineConfig, stage string) bool {
	for _, s := range TerminalStages(pipeline) {
		if s == stage {
			return true
		}
	}
	return false
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
