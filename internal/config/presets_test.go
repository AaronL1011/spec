package config

import (
	"testing"
)

func TestMergeStage(t *testing.T) {
	base := StageConfig{
		Name:  "build",
		Owner: Owners{"engineer"},
		Icon:  "🏗️",
		Gates: []GateConfig{
			{SectionNotEmpty: "acceptance_criteria"},
		},
	}

	override := StageConfig{
		Name: "build",
		Warnings: []WarningConfig{
			{After: "5d", Message: "Build taking too long"},
		},
	}

	merged := mergeStage(base, override)

	// Original fields preserved.
	if merged.GetOwner() != "engineer" {
		t.Errorf("Owner = %q, want %q", merged.GetOwner(), "engineer")
	}
	if merged.Icon != "🏗️" {
		t.Errorf("Icon = %q, want %q", merged.Icon, "🏗️")
	}

	// Gates preserved (override has none).
	if len(merged.Gates) != 1 {
		t.Errorf("len(Gates) = %d, want 1", len(merged.Gates))
	}

	// Warnings added from override.
	if len(merged.Warnings) != 1 {
		t.Errorf("len(Warnings) = %d, want 1", len(merged.Warnings))
	}
}

// TestResolveStages_PresetExpands asserts that a preset-only PipelineConfig
// expands into the preset's concrete stages. This is the resolution path that
// EffectivePipeline and pipeline.Resolve share.
func TestResolveStages_PresetExpands(t *testing.T) {
	stages, name, skipped, err := ResolveStages(PipelineConfig{Preset: "minimal"})
	if err != nil {
		t.Fatalf("ResolveStages: %v", err)
	}
	if name != "minimal" {
		t.Errorf("presetName = %q, want %q", name, "minimal")
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}
	want := []string{"triage", "draft", "build", "review", "done"}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stageNames(stages), want)
	}
	for i, n := range want {
		if stages[i].Name != n {
			t.Errorf("stages[%d].Name = %q, want %q", i, stages[i].Name, n)
		}
	}
}

// TestEffectivePipeline_HonorsPreset is the regression test for the bug where a
// team config setting only `preset: minimal` caused the TUI (and other
// ResolvedConfig.Pipeline callers) to fall back to the full default product
// pipeline — surfacing stages like tl-review and qa-expectations and making
// Advance fail on the stage mismatch. EffectivePipeline must now expand the
// preset so direct .Stages consumers see the minimal set.
func TestEffectivePipeline_HonorsPreset(t *testing.T) {
	tc := &TeamConfig{Pipeline: PipelineConfig{Preset: "minimal"}}
	pl := EffectivePipeline(tc)

	got := stageNames(pl.Stages)
	want := []string{"triage", "draft", "build", "review", "done"}
	if len(got) != len(want) {
		t.Fatalf("stages = %v, want %v (preset was ignored)", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("stages[%d] = %q, want %q", i, got[i], n)
		}
	}

	// Stages that exist only in the default/product pipeline must not leak in.
	for _, banned := range []string{"tl_review", "tl-review", "qa_expectations", "qa-expectations", "design"} {
		if pl.StageByName(banned) != nil {
			t.Errorf("preset pipeline must not contain stage %q", banned)
		}
	}
}

// TestEffectivePipeline_NilTeamFallsBackToDefault ensures the no-config path
// still returns the full default pipeline.
func TestEffectivePipeline_NilTeamFallsBackToDefault(t *testing.T) {
	pl := EffectivePipeline(nil)
	if len(pl.Stages) == 0 || pl.StageByName("done") == nil {
		t.Fatalf("default pipeline missing expected stages: %v", stageNames(pl.Stages))
	}
}

func stageNames(stages []StageConfig) []string {
	names := make([]string, len(stages))
	for i, s := range stages {
		names[i] = s.Name
	}
	return names
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
	if _, err := LoadPreset("unknown"); err == nil {
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
