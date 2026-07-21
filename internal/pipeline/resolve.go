// Package pipeline implements the spec pipeline stage machine.
package pipeline

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/pipeline/expr"
)

// ResolvedPipeline is a fully resolved pipeline configuration with all
// presets expanded, stages skipped, and overrides applied.
type ResolvedPipeline struct {
	// Stages is the final list of stages in order.
	Stages []config.StageConfig

	// StageIndex maps stage name to index for fast lookup.
	StageIndex map[string]int

	// PresetName is the preset that was used (empty if none).
	PresetName string

	// SkippedStages lists stages that were removed via Skip config.
	SkippedStages []string
}

// StageByName returns the stage with the given name, or nil if not found.
func (r *ResolvedPipeline) StageByName(name string) *config.StageConfig {
	idx, ok := r.StageIndex[name]
	if !ok {
		return nil
	}
	return &r.Stages[idx]
}

// Resolve takes a pipeline config and returns a fully resolved pipeline.
// It delegates stage expansion (preset, skip, overrides) to
// config.ResolveStages — the single source of truth — so this resolver and
// config.EffectivePipeline (used by ResolvedConfig.Pipeline) never disagree
// on which stages apply.
func Resolve(cfg config.PipelineConfig) (*ResolvedPipeline, error) {
	stages, presetName, skipped, err := config.ResolveStages(cfg)
	if err != nil {
		return nil, err
	}

	// Build index.
	index := make(map[string]int)
	for i, s := range stages {
		index[s.Name] = i
	}

	return &ResolvedPipeline{
		Stages:        stages,
		StageIndex:    index,
		PresetName:    presetName,
		SkippedStages: skipped,
	}, nil
}

// SkipWhenResult holds the result of evaluating skip_when for a stage.
type SkipWhenResult struct {
	StageName string
	Skipped   bool
	Reason    string // The skip_when expression that matched
}

// EvaluateSkipWhen evaluates skip_when expressions for all stages given a context.
// Returns the stages that should be skipped for this spec.
func EvaluateSkipWhen(resolved *ResolvedPipeline, ctx expr.Context) []SkipWhenResult {
	var results []SkipWhenResult

	for _, stage := range resolved.Stages {
		if stage.SkipWhen == "" {
			continue
		}

		// Evaluate the skip_when expression
		shouldSkip, err := expr.Evaluate(stage.SkipWhen, ctx)
		if err != nil {
			// On error, don't skip (fail open)
			results = append(results, SkipWhenResult{
				StageName: stage.Name,
				Skipped:   false,
				Reason:    fmt.Sprintf("error evaluating skip_when: %v", err),
			})
			continue
		}

		if shouldSkip {
			results = append(results, SkipWhenResult{
				StageName: stage.Name,
				Skipped:   true,
				Reason:    stage.SkipWhen,
			})
		}
	}

	return results
}

// EffectiveStages returns the stages that apply to a spec after evaluating skip_when.
// This is used for determining the actual pipeline path for a spec.
func EffectiveStages(resolved *ResolvedPipeline, ctx expr.Context) []config.StageConfig {
	skipResults := EvaluateSkipWhen(resolved, ctx)

	// Build skip set
	skipSet := make(map[string]bool)
	for _, r := range skipResults {
		if r.Skipped {
			skipSet[r.StageName] = true
		}
	}

	// Filter stages
	var effective []config.StageConfig
	for _, stage := range resolved.Stages {
		if !skipSet[stage.Name] {
			effective = append(effective, stage)
		}
	}

	return effective
}

// NextEffectiveStage returns the next stage that isn't skipped.
func NextEffectiveStage(resolved *ResolvedPipeline, current string, ctx expr.Context) (string, bool) {
	effective := EffectiveStages(resolved, ctx)

	// Build index of effective stages
	effectiveIndex := make(map[string]int)
	for i, s := range effective {
		effectiveIndex[s.Name] = i
	}

	// Find current in effective stages
	currentIdx, ok := effectiveIndex[current]
	if !ok {
		// Current stage is skipped - find next from original index
		originalIdx, origOk := resolved.StageIndex[current]
		if !origOk {
			return "", false
		}
		// Find first effective stage after current
		for i := originalIdx + 1; i < len(resolved.Stages); i++ {
			if _, isEffective := effectiveIndex[resolved.Stages[i].Name]; isEffective {
				return resolved.Stages[i].Name, true
			}
		}
		return "", false
	}

	// Return next in effective stages
	if currentIdx >= len(effective)-1 {
		return "", false
	}
	return effective[currentIdx+1].Name, true
}
