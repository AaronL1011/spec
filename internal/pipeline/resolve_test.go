package pipeline

import (
	"testing"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/pipeline/expr"
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

func TestEvaluateSkipWhen(t *testing.T) {
	resolved := &ResolvedPipeline{
		Stages: []config.StageConfig{
			{Name: "triage", Owner: "pm"},
			{Name: "design", Owner: "designer", SkipWhen: "'no-design' in spec.labels"},
			{Name: "build", Owner: "engineer"},
			{Name: "qa", Owner: "qa", SkipWhen: "'skip-qa' in spec.labels"},
			{Name: "done", Owner: "tl"},
		},
		StageIndex: map[string]int{"triage": 0, "design": 1, "build": 2, "qa": 3, "done": 4},
	}

	tests := []struct {
		name          string
		labels        []string
		wantSkipped   []string
		wantEffective []string
	}{
		{
			name:          "no labels - nothing skipped",
			labels:        []string{},
			wantSkipped:   nil,
			wantEffective: []string{"triage", "design", "build", "qa", "done"},
		},
		{
			name:          "no-design label - design skipped",
			labels:        []string{"no-design"},
			wantSkipped:   []string{"design"},
			wantEffective: []string{"triage", "build", "qa", "done"},
		},
		{
			name:          "skip-qa label - qa skipped",
			labels:        []string{"skip-qa"},
			wantSkipped:   []string{"qa"},
			wantEffective: []string{"triage", "design", "build", "done"},
		},
		{
			name:          "both labels - both skipped",
			labels:        []string{"no-design", "skip-qa"},
			wantSkipped:   []string{"design", "qa"},
			wantEffective: []string{"triage", "build", "done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := expr.NewContextBuilder().
				WithSpec("SPEC-001", "Test", "triage", tt.labels, 0, 0, 0).
				Build()

			results := EvaluateSkipWhen(resolved, ctx)

			// Check skipped stages
			var skipped []string
			for _, r := range results {
				if r.Skipped {
					skipped = append(skipped, r.StageName)
				}
			}

			if len(skipped) != len(tt.wantSkipped) {
				t.Errorf("skipped = %v, want %v", skipped, tt.wantSkipped)
			}

			// Check effective stages
			effective := EffectiveStages(resolved, ctx)
			var effectiveNames []string
			for _, s := range effective {
				effectiveNames = append(effectiveNames, s.Name)
			}

			if len(effectiveNames) != len(tt.wantEffective) {
				t.Errorf("effective = %v, want %v", effectiveNames, tt.wantEffective)
			}
		})
	}
}

func TestNextEffectiveStage(t *testing.T) {
	resolved := &ResolvedPipeline{
		Stages: []config.StageConfig{
			{Name: "triage", Owner: "pm"},
			{Name: "design", Owner: "designer", SkipWhen: "'no-design' in spec.labels"},
			{Name: "build", Owner: "engineer"},
			{Name: "done", Owner: "tl"},
		},
		StageIndex: map[string]int{"triage": 0, "design": 1, "build": 2, "done": 3},
	}

	tests := []struct {
		name     string
		current  string
		labels   []string
		wantNext string
		wantOk   bool
	}{
		{
			name:     "normal flow",
			current:  "triage",
			labels:   []string{},
			wantNext: "design",
			wantOk:   true,
		},
		{
			name:     "skip design - jump to build",
			current:  "triage",
			labels:   []string{"no-design"},
			wantNext: "build",
			wantOk:   true,
		},
		{
			name:     "at end",
			current:  "done",
			labels:   []string{},
			wantNext: "",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := expr.NewContextBuilder().
				WithSpec("SPEC-001", "Test", tt.current, tt.labels, 0, 0, 0).
				Build()

			next, ok := NextEffectiveStage(resolved, tt.current, ctx)
			if next != tt.wantNext || ok != tt.wantOk {
				t.Errorf("NextEffectiveStage(%q) = (%q, %v), want (%q, %v)",
					tt.current, next, ok, tt.wantNext, tt.wantOk)
			}
		})
	}
}

func TestShouldSkipStage(t *testing.T) {
	resolved := &ResolvedPipeline{
		Stages: []config.StageConfig{
			{Name: "design", Owner: "designer", SkipWhen: "'urgent' in spec.labels"},
			{Name: "qa", Owner: "qa", SkipWhen: "spec.word_count < 100"},
		},
		StageIndex: map[string]int{"design": 0, "qa": 1},
	}

	tests := []struct {
		name       string
		stageName  string
		labels     []string
		wordCount  int
		wantSkip   bool
	}{
		{
			name:      "design not skipped",
			stageName: "design",
			labels:    []string{"feature"},
			wantSkip:  false,
		},
		{
			name:      "design skipped for urgent",
			stageName: "design",
			labels:    []string{"urgent"},
			wantSkip:  true,
		},
		{
			name:      "qa not skipped with enough words",
			stageName: "qa",
			wordCount: 200,
			wantSkip:  false,
		},
		{
			name:      "qa skipped for short specs",
			stageName: "qa",
			wordCount: 50,
			wantSkip:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := expr.NewContextBuilder().
				WithSpec("SPEC-001", "Test", "triage", tt.labels, tt.wordCount, 0, 0).
				Build()

			skipped, _ := ShouldSkipStage(resolved, tt.stageName, ctx)
			if skipped != tt.wantSkip {
				t.Errorf("ShouldSkipStage(%q) = %v, want %v", tt.stageName, skipped, tt.wantSkip)
			}
		})
	}
}
