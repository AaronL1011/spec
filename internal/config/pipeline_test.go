package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGateConfigTypes(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantType string
		wantVal  string
	}{
		{
			name:     "section_not_empty",
			yaml:     `section_not_empty: problem_statement`,
			wantType: "section_not_empty",
			wantVal:  "problem_statement",
		},
		{
			name:     "section_complete (legacy)",
			yaml:     `section_complete: acceptance_criteria`,
			wantType: "section_complete",
			wantVal:  "acceptance_criteria",
		},
		{
			name:     "pr_stack_exists",
			yaml:     `pr_stack_exists: true`,
			wantType: "pr_stack_exists",
			wantVal:  "true",
		},
		{
			name:     "prs_approved",
			yaml:     `prs_approved: true`,
			wantType: "prs_approved",
			wantVal:  "true",
		},
		{
			name:     "duration",
			yaml:     `duration: 24h`,
			wantType: "duration",
			wantVal:  "24h",
		},
		{
			name:     "expression",
			yaml:     "expr: \"decisions.unresolved == 0\"\nmessage: \"All decisions must be resolved\"",
			wantType: "expr",
			wantVal:  "decisions.unresolved == 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gate GateConfig
			if err := yaml.Unmarshal([]byte(tt.yaml), &gate); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := gate.Type(); got != tt.wantType {
				t.Errorf("Type() = %q, want %q", got, tt.wantType)
			}
			if got := gate.Value(); got != tt.wantVal {
				t.Errorf("Value() = %q, want %q", got, tt.wantVal)
			}
		})
	}
}

func TestGateConfigLogicalOperators(t *testing.T) {
	yamlContent := `
all:
  - section_not_empty: problem_statement
  - section_not_empty: goals_non_goals
`
	var gate GateConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &gate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gate.Type() != "all" {
		t.Errorf("Type() = %q, want %q", gate.Type(), "all")
	}
	if len(gate.All) != 2 {
		t.Errorf("len(All) = %d, want 2", len(gate.All))
	}
	if gate.All[0].SectionNotEmpty != "problem_statement" {
		t.Errorf("All[0].SectionNotEmpty = %q, want %q", gate.All[0].SectionNotEmpty, "problem_statement")
	}
}

func TestGateConfigAny(t *testing.T) {
	yamlContent := `
any:
  - section_not_empty: design_inputs
  - link_exists:
      section: design_inputs
      type: figma
`
	var gate GateConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &gate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gate.Type() != "any" {
		t.Errorf("Type() = %q, want %q", gate.Type(), "any")
	}
	if len(gate.Any) != 2 {
		t.Errorf("len(Any) = %d, want 2", len(gate.Any))
	}
	if gate.Any[1].LinkExists == nil {
		t.Fatal("Any[1].LinkExists is nil")
	}
	if gate.Any[1].LinkExists.Section != "design_inputs" {
		t.Errorf("LinkExists.Section = %q, want %q", gate.Any[1].LinkExists.Section, "design_inputs")
	}
}

func TestStageConfigGetOwner(t *testing.T) {
	tests := []struct {
		name      string
		stage     StageConfig
		wantOwner string
	}{
		{
			name:      "uses Owner field",
			stage:     StageConfig{Name: "build", Owner: "engineer"},
			wantOwner: "engineer",
		},
		{
			name:      "falls back to OwnerRole",
			stage:     StageConfig{Name: "build", OwnerRole: "engineer"},
			wantOwner: "engineer",
		},
		{
			name:      "Owner takes precedence",
			stage:     StageConfig{Name: "build", Owner: "pm", OwnerRole: "engineer"},
			wantOwner: "pm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stage.GetOwner(); got != tt.wantOwner {
				t.Errorf("GetOwner() = %q, want %q", got, tt.wantOwner)
			}
		})
	}
}

func TestPipelineConfigWithPreset(t *testing.T) {
	yamlContent := `
preset: product
skip:
  - design
stages:
  - name: build
    warnings:
      - after: 5d
        message: "Build exceeding 5 days"
        notify: tl
`
	var pipeline PipelineConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &pipeline); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pipeline.Preset != "product" {
		t.Errorf("Preset = %q, want %q", pipeline.Preset, "product")
	}
	if len(pipeline.Skip) != 1 || pipeline.Skip[0] != "design" {
		t.Errorf("Skip = %v, want [design]", pipeline.Skip)
	}
	if len(pipeline.Stages) != 1 {
		t.Fatalf("len(Stages) = %d, want 1", len(pipeline.Stages))
	}
	if len(pipeline.Stages[0].Warnings) != 1 {
		t.Fatalf("len(Warnings) = %d, want 1", len(pipeline.Stages[0].Warnings))
	}
	if pipeline.Stages[0].Warnings[0].After != "5d" {
		t.Errorf("Warning.After = %q, want %q", pipeline.Stages[0].Warnings[0].After, "5d")
	}
}

func TestEffectConfig(t *testing.T) {
	yamlContent := `
transitions:
  advance:
    effects:
      - notify:
          target: next_owner
      - sync: outbound
      - webhook:
          url: https://example.com/hook
          method: POST
  revert:
    require:
      - reason
    effects:
      - notify:
          targets:
            - pr_author
            - tl
      - increment: revert_count
`
	var stage StageConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &stage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(stage.Transitions.Advance.Effects) != 3 {
		t.Errorf("len(Advance.Effects) = %d, want 3", len(stage.Transitions.Advance.Effects))
	}
	if stage.Transitions.Advance.Effects[1].Sync != "outbound" {
		t.Errorf("Effect[1].Sync = %q, want %q", stage.Transitions.Advance.Effects[1].Sync, "outbound")
	}
	if stage.Transitions.Advance.Effects[2].Webhook == nil {
		t.Fatal("Effect[2].Webhook is nil")
	}
	if stage.Transitions.Advance.Effects[2].Webhook.URL != "https://example.com/hook" {
		t.Errorf("Webhook.URL = %q, want %q", stage.Transitions.Advance.Effects[2].Webhook.URL, "https://example.com/hook")
	}

	if len(stage.Transitions.Revert.Require) != 1 || stage.Transitions.Revert.Require[0] != "reason" {
		t.Errorf("Revert.Require = %v, want [reason]", stage.Transitions.Revert.Require)
	}
	if stage.Transitions.Revert.Effects[1].Increment != "revert_count" {
		t.Errorf("Effect[1].Increment = %q, want %q", stage.Transitions.Revert.Effects[1].Increment, "revert_count")
	}
}

func TestVariantConfig(t *testing.T) {
	yamlContent := `
default: standard
variants:
  standard:
    preset: product
  bug:
    preset: product
    skip:
      - design
      - tl_review
  hotfix:
    stages:
      - name: triage
        owner: engineer
      - name: build
        owner: engineer
      - name: done
        owner: tl
variant_from_labels:
  - label: bug
    variant: bug
  - label: hotfix
    variant: hotfix
  - variant: standard
    default: true
`
	var pipeline PipelineConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &pipeline); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pipeline.Default != "standard" {
		t.Errorf("Default = %q, want %q", pipeline.Default, "standard")
	}
	if len(pipeline.Variants) != 3 {
		t.Errorf("len(Variants) = %d, want 3", len(pipeline.Variants))
	}
	if pipeline.Variants["bug"].Preset != "product" {
		t.Errorf("Variants[bug].Preset = %q, want %q", pipeline.Variants["bug"].Preset, "product")
	}
	if len(pipeline.Variants["bug"].Skip) != 2 {
		t.Errorf("len(Variants[bug].Skip) = %d, want 2", len(pipeline.Variants["bug"].Skip))
	}
	if len(pipeline.VariantFromLabels) != 3 {
		t.Errorf("len(VariantFromLabels) = %d, want 3", len(pipeline.VariantFromLabels))
	}
	if pipeline.VariantFromLabels[0].Label != "bug" {
		t.Errorf("VariantFromLabels[0].Label = %q, want %q", pipeline.VariantFromLabels[0].Label, "bug")
	}
}

func TestNewSimpleGate(t *testing.T) {
	gate := NewSimpleGate("section_not_empty", "problem_statement")
	if gate.SectionNotEmpty != "problem_statement" {
		t.Errorf("SectionNotEmpty = %q, want %q", gate.SectionNotEmpty, "problem_statement")
	}
	if gate.Type() != "section_not_empty" {
		t.Errorf("Type() = %q, want %q", gate.Type(), "section_not_empty")
	}
}

func TestNewExprGate(t *testing.T) {
	gate := NewExprGate("decisions.unresolved == 0", "All decisions must be resolved")
	if gate.Expr != "decisions.unresolved == 0" {
		t.Errorf("Expr = %q, want %q", gate.Expr, "decisions.unresolved == 0")
	}
	if gate.Message != "All decisions must be resolved" {
		t.Errorf("Message = %q, want %q", gate.Message, "All decisions must be resolved")
	}
}
