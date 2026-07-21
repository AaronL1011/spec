package config

import "fmt"

// PresetConfig holds a preset pipeline definition.
type PresetConfig struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Features    []string      `yaml:"features"`
	Stages      []StageConfig `yaml:"stages"`
}

// presets holds the built-in pipeline presets.
//
// Preset data lives in the config package (not internal/pipeline) so that
// EffectivePipeline can expand a preset into concrete stages without importing
// internal/pipeline, which would create a cycle (pipeline depends on config).
// internal/pipeline.Resolve delegates to ResolveStages here so both the
// convenience accessor (ResolvedConfig.Pipeline) and the full resolver stay in
// sync with a single source of truth.
var presets = map[string]PresetConfig{
	"minimal": {
		Name:        "minimal",
		Description: "Solo or tiny team. triage → draft → build → done",
		Features: []string{
			"Lightweight, no ceremony",
			"No dedicated review stages",
			"Good for solo developers",
		},
		Stages: []StageConfig{
			{Name: "triage", Owner: Owners{"anyone"}, Icon: "📥"},
			{Name: "draft", Owner: Owners{"author"}, Icon: "📝"},
			{Name: "build", Owner: Owners{"engineer"}, Icon: "🏗️"},
			{Name: "review", Owner: Owners{"engineer"}, Icon: "👁️"},
			{Name: "done", Owner: Owners{"author"}, Icon: "🎉"},
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
		Stages: []StageConfig{
			{Name: "triage", Owner: Owners{"pm"}, Icon: "📥"},
			{Name: "draft", Owner: Owners{"pm"}, Icon: "📝"},
			{Name: "review", Owner: Owners{"tl"}, Icon: "👀", Gates: []GateConfig{
				{SectionNotEmpty: "problem_statement"},
			}},
			{Name: "build", Owner: Owners{"engineer"}, Icon: "🏗️", Gates: []GateConfig{
				{SectionNotEmpty: "acceptance_criteria"},
			}},
			{Name: "pr_review", Owner: Owners{"engineer"}, Icon: "👁️"},
			{Name: "done", Owner: Owners{"tl"}, Icon: "🎉"},
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
		Stages: func() []StageConfig {
			t := true
			return []StageConfig{
				{Name: "triage", Owner: Owners{"pm"}, Icon: "📥"},
				{Name: "draft", Owner: Owners{"pm"}, Icon: "📝"},
				{Name: "tl_review", Owner: Owners{"tl"}, Icon: "👀", Gates: []GateConfig{
					{SectionNotEmpty: "problem_statement"},
				}},
				{Name: "design", Owner: Owners{"designer"}, Icon: "🎨", Gates: []GateConfig{
					{SectionNotEmpty: "user_stories"},
				}},
				{Name: "qa_expectations", Owner: Owners{"qa"}, Icon: "📋", Gates: []GateConfig{
					{SectionNotEmpty: "design_inputs"},
				}},
				{Name: "engineering", Owner: Owners{"engineer"}, Icon: "🔧", Gates: []GateConfig{
					{SectionNotEmpty: "acceptance_criteria"},
				}},
				{Name: "build", Owner: Owners{"engineer"}, Icon: "🏗️"},
				{Name: "pr_review", Owner: Owners{"engineer"}, Icon: "👁️", Gates: []GateConfig{
					{PRStackExists: &t},
				}},
				{Name: "qa_validation", Owner: Owners{"qa"}, Icon: "✅", Gates: []GateConfig{
					{PRsApproved: &t},
				}},
				{Name: "done", Owner: Owners{"tl"}, Icon: "🎉"},
				{Name: "deploying", Owner: Owners{"engineer"}, Icon: "🚀", Optional: true},
				{Name: "monitoring", Owner: Owners{"engineer"}, Icon: "📊", Optional: true},
				{Name: "closed", Owner: Owners{"tl"}, Icon: "📦", Optional: true, AutoArchive: true},
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
		Stages: []StageConfig{
			{Name: "draft", Owner: Owners{"engineer"}, Icon: "📝"},
			{Name: "review", Owner: Owners{"tl"}, Icon: "👀", Gates: []GateConfig{
				{SectionNotEmpty: "problem_statement"},
				{SectionNotEmpty: "proposed_solution"},
			}},
			{Name: "discussion", Owner: Owners{"engineer"}, Icon: "💬"},
			{Name: "approved", Owner: Owners{"tl"}, Icon: "✅", Gates: []GateConfig{
				{Expr: "decisions.unresolved == 0", Message: "All decisions must be resolved"},
			}},
			{Name: "implementing", Owner: Owners{"engineer"}, Icon: "🏗️"},
			{Name: "done", Owner: Owners{"tl"}, Icon: "🎉"},
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
		Stages: []StageConfig{
			{Name: "backlog", Owner: Owners{"anyone"}, Icon: "📥"},
			{Name: "doing", Owner: Owners{"engineer"}, Icon: "🏗️"},
			{Name: "done", Owner: Owners{"engineer"}, Icon: "✅"},
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

// ResolveStages expands a PipelineConfig into its final ordered stage list,
// applying preset expansion, skip removal, and stage overrides. It returns the
// resolved stages, the preset name used (empty if none), the names of stages
// removed via Skip, and any error from preset lookup.
//
// This is the single source of truth for stage resolution: both
// EffectivePipeline (for convenience access via ResolvedConfig.Pipeline) and
// internal/pipeline.Resolve (for the richer ResolvedPipeline with index and
// skip-when evaluation) call it, so every consumer sees the same stages
// regardless of whether a team config uses a preset, explicit stages, or both.
func ResolveStages(cfg PipelineConfig) ([]StageConfig, string, []string, error) {
	var stages []StageConfig
	var presetName string
	var skipped []string

	// Step 1: Start with preset or explicit stages.
	switch {
	case cfg.Preset != "":
		preset, err := LoadPreset(cfg.Preset)
		if err != nil {
			return nil, "", nil, fmt.Errorf("loading preset %q: %w", cfg.Preset, err)
		}
		stages = preset.Stages
		presetName = cfg.Preset
	case len(cfg.Stages) > 0:
		stages = cfg.Stages
	default:
		// No preset, no stages - use default.
		stages = DefaultPipeline().Stages
	}

	// Step 2: Remove skipped stages.
	if len(cfg.Skip) > 0 {
		skipSet := make(map[string]bool)
		for _, s := range cfg.Skip {
			skipSet[s] = true
		}

		var filtered []StageConfig
		for _, stage := range stages {
			if skipSet[stage.Name] {
				skipped = append(skipped, stage.Name)
			} else {
				filtered = append(filtered, stage)
			}
		}
		stages = filtered
	}

	// Step 3: Apply stage overrides from config. Only when a preset was used;
	// without a preset the stages ARE the config.
	if cfg.Preset != "" && len(cfg.Stages) > 0 {
		stages = applyStageOverrides(stages, cfg.Stages)
	}

	return stages, presetName, skipped, nil
}

// applyStageOverrides merges override stages into base stages.
// - If an override stage name matches a base stage, the override fields are merged.
// - If an override stage name doesn't match, it's appended (new stage).
func applyStageOverrides(base, overrides []StageConfig) []StageConfig {
	// Create a map of base stages for quick lookup.
	baseMap := make(map[string]int)
	for i, s := range base {
		baseMap[s.Name] = i
	}

	result := make([]StageConfig, 0, len(base)+len(overrides))
	result = append(result, base...)

	for _, override := range overrides {
		if idx, ok := baseMap[override.Name]; ok {
			// Merge override into existing stage.
			result[idx] = mergeStage(result[idx], override)
		} else {
			// New stage - append.
			result = append(result, override)
		}
	}

	return result
}

// mergeStage merges override fields into base stage.
// Only non-zero override values replace base values.
func mergeStage(base, override StageConfig) StageConfig {
	result := base

	// Override simple fields if set.
	if !override.Owner.IsEmpty() {
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
	if override.Dashboard.DoScope != "" {
		result.Dashboard.DoScope = override.Dashboard.DoScope
	}
	if override.Dashboard.Claimable != nil {
		result.Dashboard.Claimable = override.Dashboard.Claimable
	}

	// For slices, override replaces entirely if non-empty.
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

	// Transitions - merge at the field level.
	if override.Transitions.Advance.To != nil || override.Transitions.Advance.Gates != nil ||
		override.Transitions.Advance.Effects != nil || override.Transitions.Advance.Require != nil {
		result.Transitions.Advance = override.Transitions.Advance
	}
	if override.Transitions.Revert.To != nil || override.Transitions.Revert.Gates != nil ||
		override.Transitions.Revert.Effects != nil || override.Transitions.Revert.Require != nil {
		result.Transitions.Revert = override.Transitions.Revert
	}

	// Booleans - only override if explicitly set (tricky with bool default).
	// For now, override always wins if the override stage has these set.
	if override.Optional {
		result.Optional = true
	}
	if override.AutoArchive {
		result.AutoArchive = true
	}

	return result
}
