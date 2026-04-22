// Package pipeline implements the spec pipeline stage machine.
package pipeline

import (
	"fmt"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/pipeline/expr"
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

	// VariantName is the variant that was selected (empty if default).
	VariantName string

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

// StageOwner returns the owner role for a stage.
func (r *ResolvedPipeline) StageOwner(name string) string {
	stage := r.StageByName(name)
	if stage == nil {
		return ""
	}
	return stage.GetOwner()
}

// NextStage returns the next stage after the given one.
func (r *ResolvedPipeline) NextStage(current string) (string, bool) {
	idx, ok := r.StageIndex[current]
	if !ok || idx >= len(r.Stages)-1 {
		return "", false
	}
	return r.Stages[idx+1].Name, true
}

// PrevStage returns the previous stage before the given one.
func (r *ResolvedPipeline) PrevStage(current string) (string, bool) {
	idx, ok := r.StageIndex[current]
	if !ok || idx <= 0 {
		return "", false
	}
	return r.Stages[idx-1].Name, true
}

// IsValidTransition checks if advancing from 'from' to 'to' is valid.
func (r *ResolvedPipeline) IsValidTransition(from, to string) bool {
	fromIdx, fromOk := r.StageIndex[from]
	toIdx, toOk := r.StageIndex[to]
	return fromOk && toOk && toIdx > fromIdx
}

// IsValidReversion checks if reverting from 'from' to 'to' is valid.
func (r *ResolvedPipeline) IsValidReversion(from, to string) bool {
	fromIdx, fromOk := r.StageIndex[from]
	toIdx, toOk := r.StageIndex[to]
	return fromOk && toOk && toIdx < fromIdx
}

// Resolve takes a pipeline config and returns a fully resolved pipeline.
// It handles:
// 1. Loading the preset (if specified)
// 2. Removing skipped stages
// 3. Merging/overriding stages from config
func Resolve(cfg config.PipelineConfig) (*ResolvedPipeline, error) {
	var stages []config.StageConfig
	var presetName string
	var skipped []string

	// Step 1: Start with preset or explicit stages
	if cfg.Preset != "" {
		preset, err := LoadPreset(cfg.Preset)
		if err != nil {
			return nil, fmt.Errorf("loading preset %q: %w", cfg.Preset, err)
		}
		stages = preset.Stages
		presetName = cfg.Preset
	} else if len(cfg.Stages) > 0 {
		stages = cfg.Stages
	} else {
		// No preset, no stages - use default
		defaultPipeline := config.DefaultPipeline()
		stages = defaultPipeline.Stages
	}

	// Step 2: Remove skipped stages
	if len(cfg.Skip) > 0 {
		skipSet := make(map[string]bool)
		for _, s := range cfg.Skip {
			skipSet[s] = true
		}

		var filtered []config.StageConfig
		for _, stage := range stages {
			if skipSet[stage.Name] {
				skipped = append(skipped, stage.Name)
			} else {
				filtered = append(filtered, stage)
			}
		}
		stages = filtered
	}

	// Step 3: Apply stage overrides from config
	// Only if preset was used (otherwise stages ARE the config)
	if cfg.Preset != "" && len(cfg.Stages) > 0 {
		stages = applyStageOverrides(stages, cfg.Stages)
	}

	// Build index
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

// ResolveForSpec returns the pipeline variant for a specific spec based on its labels.
func ResolveForSpec(cfg config.PipelineConfig, labels []string) (*ResolvedPipeline, error) {
	// Check if spec matches a variant based on labels
	variantName := ""
	if len(cfg.VariantFromLabels) > 0 {
		labelSet := make(map[string]bool)
		for _, l := range labels {
			labelSet[l] = true
		}

		for _, mapping := range cfg.VariantFromLabels {
			if mapping.Default && variantName == "" {
				variantName = mapping.Variant
			}
			if mapping.Label != "" && labelSet[mapping.Label] {
				variantName = mapping.Variant
				break
			}
		}
	}

	// Use default variant if set and no match
	if variantName == "" && cfg.Default != "" {
		variantName = cfg.Default
	}

	// If we have a variant, resolve it
	if variantName != "" && cfg.Variants != nil {
		variant, ok := cfg.Variants[variantName]
		if ok {
			// Create a pipeline config from the variant
			variantCfg := config.PipelineConfig{
				Preset: variant.Preset,
				Skip:   variant.Skip,
				Stages: variant.Stages,
			}
			resolved, err := Resolve(variantCfg)
			if err != nil {
				return nil, fmt.Errorf("resolving variant %q: %w", variantName, err)
			}
			resolved.VariantName = variantName
			return resolved, nil
		}
	}

	// No variant - resolve the base config
	return Resolve(cfg)
}

// applyStageOverrides merges override stages into base stages.
// - If an override stage name matches a base stage, the override fields are merged.
// - If an override stage name doesn't match, it's appended (new stage).
func applyStageOverrides(base, overrides []config.StageConfig) []config.StageConfig {
	// Create a map of base stages for quick lookup
	baseMap := make(map[string]int)
	for i, s := range base {
		baseMap[s.Name] = i
	}

	result := make([]config.StageConfig, len(base))
	copy(result, base)

	for _, override := range overrides {
		if idx, ok := baseMap[override.Name]; ok {
			// Merge override into existing stage
			result[idx] = mergeStage(result[idx], override)
		} else {
			// New stage - append
			result = append(result, override)
		}
	}

	return result
}

// mergeStage merges override fields into base stage.
// Only non-zero override values replace base values.
func mergeStage(base, override config.StageConfig) config.StageConfig {
	result := base

	// Override simple fields if set
	if override.Owner != "" {
		result.Owner = override.Owner
	}
	if override.OwnerRole != "" {
		result.OwnerRole = override.OwnerRole
	}
	if override.Icon != "" {
		result.Icon = override.Icon
	}
	if override.SkipWhen != "" {
		result.SkipWhen = override.SkipWhen
	}

	// For slices, override replaces entirely if non-empty
	if len(override.Gates) > 0 {
		result.Gates = override.Gates
	}
	if len(override.Warnings) > 0 {
		result.Warnings = override.Warnings
	}
	if len(override.OnEnter) > 0 {
		result.OnEnter = override.OnEnter
	}
	if len(override.OnExit) > 0 {
		result.OnExit = override.OnExit
	}

	// Transitions - merge at the field level
	if override.Transitions.Advance.To != nil || override.Transitions.Advance.Gates != nil ||
		override.Transitions.Advance.Effects != nil || override.Transitions.Advance.Require != nil {
		result.Transitions.Advance = override.Transitions.Advance
	}
	if override.Transitions.Revert.To != nil || override.Transitions.Revert.Gates != nil ||
		override.Transitions.Revert.Effects != nil || override.Transitions.Revert.Require != nil {
		result.Transitions.Revert = override.Transitions.Revert
	}

	// Booleans - only override if explicitly set (tricky with bool default)
	// For now, override always wins if the override stage has these set
	if override.Optional {
		result.Optional = true
	}
	if override.AutoArchive {
		result.AutoArchive = true
	}

	return result
}

// PresetConfig holds a preset pipeline definition.
type PresetConfig struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Features    []string             `yaml:"features"`
	Stages      []config.StageConfig `yaml:"stages"`
}

// presets holds the built-in pipeline presets.
// These will be moved to embedded YAML files in PR #3.
var presets = map[string]PresetConfig{
	"minimal": {
		Name:        "minimal",
		Description: "Solo or tiny team. triage → draft → build → done",
		Features: []string{
			"Lightweight, no ceremony",
			"No dedicated review stages",
			"Good for solo developers",
		},
		Stages: []config.StageConfig{
			{Name: "triage", Owner: "anyone", Icon: "📥"},
			{Name: "draft", Owner: "author", Icon: "📝"},
			{Name: "build", Owner: "engineer", Icon: "🏗️"},
			{Name: "review", Owner: "engineer", Icon: "👁️"},
			{Name: "done", Owner: "author", Icon: "🎉"},
		},
	},
	"startup": {
		Name:        "startup",
		Description: "Fast-moving product team. PM specs, eng builds, ship quick.",
		Features: []string{
			"Lightweight process for speed",
			"No dedicated design or QA stages",
			"TL review before build",
		},
		Stages: []config.StageConfig{
			{Name: "triage", Owner: "pm", Icon: "📥"},
			{Name: "draft", Owner: "pm", Icon: "📝"},
			{Name: "review", Owner: "tl", Icon: "👀", Gates: []config.GateConfig{
				{SectionNotEmpty: "problem_statement"},
			}},
			{Name: "build", Owner: "engineer", Icon: "🏗️", Gates: []config.GateConfig{
				{SectionNotEmpty: "acceptance_criteria"},
			}},
			{Name: "pr_review", Owner: "engineer", Icon: "👁️"},
			{Name: "done", Owner: "tl", Icon: "🎉"},
		},
	},
	"product": {
		Name:        "product",
		Description: "Full lifecycle. PM → Design → Eng → QA → Deploy",
		Features: []string{
			"Full lifecycle with QA validation",
			"Design stage for UX review",
			"Optional deployment stages",
		},
		Stages: func() []config.StageConfig {
			t := true
			return []config.StageConfig{
				{Name: "triage", Owner: "pm", Icon: "📥"},
				{Name: "draft", Owner: "pm", Icon: "📝"},
				{Name: "tl_review", Owner: "tl", Icon: "👀", Gates: []config.GateConfig{
					{SectionNotEmpty: "problem_statement"},
				}},
				{Name: "design", Owner: "designer", Icon: "🎨", Gates: []config.GateConfig{
					{SectionNotEmpty: "user_stories"},
				}},
				{Name: "qa_expectations", Owner: "qa", Icon: "📋", Gates: []config.GateConfig{
					{SectionNotEmpty: "design_inputs"},
				}},
				{Name: "engineering", Owner: "engineer", Icon: "🔧", Gates: []config.GateConfig{
					{SectionNotEmpty: "acceptance_criteria"},
				}},
				{Name: "build", Owner: "engineer", Icon: "🏗️"},
				{Name: "pr_review", Owner: "engineer", Icon: "👁️", Gates: []config.GateConfig{
					{PRStackExists: &t},
				}},
				{Name: "qa_validation", Owner: "qa", Icon: "✅", Gates: []config.GateConfig{
					{PRsApproved: &t},
				}},
				{Name: "done", Owner: "tl", Icon: "🎉"},
				{Name: "deploying", Owner: "engineer", Icon: "🚀", Optional: true},
				{Name: "monitoring", Owner: "engineer", Icon: "📊", Optional: true},
				{Name: "closed", Owner: "tl", Icon: "📦", Optional: true, AutoArchive: true},
			}
		}(),
	},
	"platform": {
		Name:        "platform",
		Description: "RFC-driven. Propose → Review → Approve → Implement",
		Features: []string{
			"RFC/ADR-style proposal process",
			"Multiple review stages",
			"Good for infrastructure teams",
		},
		Stages: []config.StageConfig{
			{Name: "draft", Owner: "engineer", Icon: "📝"},
			{Name: "review", Owner: "tl", Icon: "👀", Gates: []config.GateConfig{
				{SectionNotEmpty: "problem_statement"},
				{SectionNotEmpty: "proposed_solution"},
			}},
			{Name: "discussion", Owner: "engineer", Icon: "💬"},
			{Name: "approved", Owner: "tl", Icon: "✅", Gates: []config.GateConfig{
				{Expr: "decisions.unresolved == 0", Message: "All decisions must be resolved"},
			}},
			{Name: "implementing", Owner: "engineer", Icon: "🏗️"},
			{Name: "done", Owner: "tl", Icon: "🎉"},
		},
	},
	"kanban": {
		Name:        "kanban",
		Description: "Continuous flow. backlog → doing → done",
		Features: []string{
			"Minimal stages for continuous flow",
			"No gates or approvals",
			"Good for maintenance work",
		},
		Stages: []config.StageConfig{
			{Name: "backlog", Owner: "anyone", Icon: "📥"},
			{Name: "doing", Owner: "engineer", Icon: "🏗️"},
			{Name: "done", Owner: "engineer", Icon: "✅"},
		},
	},
}

// LoadPreset returns the preset configuration for the given name.
func LoadPreset(name string) (*PresetConfig, error) {
	preset, ok := presets[name]
	if !ok {
		return nil, fmt.Errorf("unknown preset: %q (available: minimal, startup, product, platform, kanban)", name)
	}
	return &preset, nil
}

// PresetNames returns the names of all available presets.
func PresetNames() []string {
	return []string{"minimal", "startup", "product", "platform", "kanban"}
}

// PresetInfo returns metadata about a preset for display.
func PresetInfo(name string) (description string, features []string, stageNames []string, err error) {
	preset, err := LoadPreset(name)
	if err != nil {
		return "", nil, nil, err
	}
	names := make([]string, len(preset.Stages))
	for i, s := range preset.Stages {
		names[i] = s.Name
	}
	return preset.Description, preset.Features, names, nil
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

// ShouldSkipStage checks if a specific stage should be skipped for a spec.
func ShouldSkipStage(resolved *ResolvedPipeline, stageName string, ctx expr.Context) (bool, string) {
	stage := resolved.StageByName(stageName)
	if stage == nil || stage.SkipWhen == "" {
		return false, ""
	}

	shouldSkip, err := expr.Evaluate(stage.SkipWhen, ctx)
	if err != nil {
		return false, fmt.Sprintf("error: %v", err)
	}

	if shouldSkip {
		return true, stage.SkipWhen
	}
	return false, ""
}
