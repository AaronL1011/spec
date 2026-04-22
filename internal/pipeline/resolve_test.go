package pipeline

import (
	"testing"

	"github.com/nexl/spec-cli/internal/config"
)

func TestResolveWithPreset(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset: "minimal",
	}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.PresetName != "minimal" {
		t.Errorf("PresetName = %q, want %q", resolved.PresetName, "minimal")
	}

	expectedStages := []string{"triage", "draft", "build", "review", "done"}
	if len(resolved.Stages) != len(expectedStages) {
		t.Fatalf("len(Stages) = %d, want %d", len(resolved.Stages), len(expectedStages))
	}

	for i, name := range expectedStages {
		if resolved.Stages[i].Name != name {
			t.Errorf("Stages[%d].Name = %q, want %q", i, resolved.Stages[i].Name, name)
		}
	}
}

func TestResolveWithSkip(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset: "product",
		Skip:   []string{"design", "qa_expectations"},
	}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Check skipped stages are recorded
	if len(resolved.SkippedStages) != 2 {
		t.Errorf("len(SkippedStages) = %d, want 2", len(resolved.SkippedStages))
	}

	// Check skipped stages are not in result
	for _, stage := range resolved.Stages {
		if stage.Name == "design" || stage.Name == "qa_expectations" {
			t.Errorf("Stage %q should have been skipped", stage.Name)
		}
	}

	// Check index is correct
	if _, ok := resolved.StageIndex["design"]; ok {
		t.Error("StageIndex should not contain 'design'")
	}
}

func TestResolveWithOverrides(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset: "minimal",
		Stages: []config.StageConfig{
			{
				Name: "build",
				Warnings: []config.WarningConfig{
					{After: "5d", Message: "Build exceeding 5 days", Notify: "tl"},
				},
			},
		},
	}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Find build stage
	buildStage := resolved.StageByName("build")
	if buildStage == nil {
		t.Fatal("build stage not found")
	}

	// Check warning was added
	if len(buildStage.Warnings) != 1 {
		t.Fatalf("len(Warnings) = %d, want 1", len(buildStage.Warnings))
	}
	if buildStage.Warnings[0].After != "5d" {
		t.Errorf("Warning.After = %q, want %q", buildStage.Warnings[0].After, "5d")
	}
}

func TestResolveWithExplicitStages(t *testing.T) {
	cfg := config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "todo", Owner: "anyone"},
			{Name: "doing", Owner: "engineer"},
			{Name: "done", Owner: "engineer"},
		},
	}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.PresetName != "" {
		t.Errorf("PresetName = %q, want empty", resolved.PresetName)
	}

	if len(resolved.Stages) != 3 {
		t.Fatalf("len(Stages) = %d, want 3", len(resolved.Stages))
	}
}

func TestResolveDefault(t *testing.T) {
	cfg := config.PipelineConfig{}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Should use DefaultPipeline
	if len(resolved.Stages) == 0 {
		t.Error("Stages should not be empty")
	}
}

func TestResolveForSpecWithVariant(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset: "product",
		Variants: map[string]config.VariantConfig{
			"bug": {
				Preset: "product",
				Skip:   []string{"design", "qa_expectations"},
			},
		},
		VariantFromLabels: []config.LabelVariantMapping{
			{Label: "bug", Variant: "bug"},
		},
	}

	// Test with bug label
	resolved, err := ResolveForSpec(cfg, []string{"bug", "urgent"})
	if err != nil {
		t.Fatalf("ResolveForSpec: %v", err)
	}

	if resolved.VariantName != "bug" {
		t.Errorf("VariantName = %q, want %q", resolved.VariantName, "bug")
	}

	// Check design was skipped
	if resolved.StageByName("design") != nil {
		t.Error("design stage should have been skipped")
	}
}

func TestResolveForSpecWithDefault(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset:  "product",
		Default: "standard",
		Variants: map[string]config.VariantConfig{
			"standard": {
				Preset: "product",
			},
			"bug": {
				Preset: "startup",
			},
		},
	}

	// Test with no matching labels
	resolved, err := ResolveForSpec(cfg, []string{"feature"})
	if err != nil {
		t.Fatalf("ResolveForSpec: %v", err)
	}

	if resolved.VariantName != "standard" {
		t.Errorf("VariantName = %q, want %q", resolved.VariantName, "standard")
	}
}

func TestResolvedPipelineMethods(t *testing.T) {
	cfg := config.PipelineConfig{
		Preset: "minimal",
	}

	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Test NextStage
	next, ok := resolved.NextStage("draft")
	if !ok {
		t.Error("NextStage(draft) should return true")
	}
	if next != "build" {
		t.Errorf("NextStage(draft) = %q, want %q", next, "build")
	}

	// Test PrevStage
	prev, ok := resolved.PrevStage("build")
	if !ok {
		t.Error("PrevStage(build) should return true")
	}
	if prev != "draft" {
		t.Errorf("PrevStage(build) = %q, want %q", prev, "draft")
	}

	// Test IsValidTransition
	if !resolved.IsValidTransition("draft", "build") {
		t.Error("IsValidTransition(draft, build) should be true")
	}
	if resolved.IsValidTransition("build", "draft") {
		t.Error("IsValidTransition(build, draft) should be false")
	}

	// Test IsValidReversion
	if !resolved.IsValidReversion("build", "draft") {
		t.Error("IsValidReversion(build, draft) should be true")
	}
	if resolved.IsValidReversion("draft", "build") {
		t.Error("IsValidReversion(draft, build) should be false")
	}

	// Test StageOwner
	owner := resolved.StageOwner("build")
	if owner != "engineer" {
		t.Errorf("StageOwner(build) = %q, want %q", owner, "engineer")
	}
}

func TestLoadPreset(t *testing.T) {
	for _, name := range PresetNames() {
		t.Run(name, func(t *testing.T) {
			preset, err := LoadPreset(name)
			if err != nil {
				t.Fatalf("LoadPreset(%q): %v", name, err)
			}
			if preset.Name != name {
				t.Errorf("Name = %q, want %q", preset.Name, name)
			}
			if preset.Description == "" {
				t.Error("Description should not be empty")
			}
			if len(preset.Stages) == 0 {
				t.Error("Stages should not be empty")
			}
		})
	}
}

func TestLoadPresetUnknown(t *testing.T) {
	_, err := LoadPreset("unknown")
	if err == nil {
		t.Error("LoadPreset(unknown) should return error")
	}
}

func TestPresetInfo(t *testing.T) {
	desc, features, stages, err := PresetInfo("product")
	if err != nil {
		t.Fatalf("PresetInfo: %v", err)
	}
	if desc == "" {
		t.Error("description should not be empty")
	}
	if len(features) == 0 {
		t.Error("features should not be empty")
	}
	if len(stages) == 0 {
		t.Error("stages should not be empty")
	}
}

func TestMergeStage(t *testing.T) {
	base := config.StageConfig{
		Name:  "build",
		Owner: "engineer",
		Icon:  "🏗️",
		Gates: []config.GateConfig{
			{SectionNotEmpty: "acceptance_criteria"},
		},
	}

	override := config.StageConfig{
		Name: "build",
		Warnings: []config.WarningConfig{
			{After: "5d", Message: "Build taking too long"},
		},
	}

	merged := mergeStage(base, override)

	// Original fields preserved
	if merged.Owner != "engineer" {
		t.Errorf("Owner = %q, want %q", merged.Owner, "engineer")
	}
	if merged.Icon != "🏗️" {
		t.Errorf("Icon = %q, want %q", merged.Icon, "🏗️")
	}

	// Gates preserved (override has none)
	if len(merged.Gates) != 1 {
		t.Errorf("len(Gates) = %d, want 1", len(merged.Gates))
	}

	// Warnings added from override
	if len(merged.Warnings) != 1 {
		t.Errorf("len(Warnings) = %d, want 1", len(merged.Warnings))
	}
}
