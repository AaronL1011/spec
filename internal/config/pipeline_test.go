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
			wantType: "steps_exists", // legacy maps to new type
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
			stage:     StageConfig{Name: "build", Owner: Owners{"engineer"}},
			wantOwner: "engineer",
		},
		{
			name:      "falls back to OwnerRole",
			stage:     StageConfig{Name: "build", OwnerRole: "engineer"},
			wantOwner: "engineer",
		},
		{
			name:      "Owner takes precedence",
			stage:     StageConfig{Name: "build", Owner: Owners{"pm"}, OwnerRole: "engineer"},
			wantOwner: "pm",
		},
		{
			name:      "multiple owners",
			stage:     StageConfig{Name: "build", Owner: Owners{"pm", "tl"}},
			wantOwner: "pm, tl",
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

func TestStageConfigHasOwner(t *testing.T) {
	tests := []struct {
		name  string
		stage StageConfig
		role  string
		want  bool
	}{
		{
			name:  "single owner matches",
			stage: StageConfig{Name: "build", Owner: Owners{"engineer"}},
			role:  "engineer",
			want:  true,
		},
		{
			name:  "single owner no match",
			stage: StageConfig{Name: "build", Owner: Owners{"engineer"}},
			role:  "pm",
			want:  false,
		},
		{
			name:  "multiple owners first matches",
			stage: StageConfig{Name: "build", Owner: Owners{"pm", "tl"}},
			role:  "pm",
			want:  true,
		},
		{
			name:  "multiple owners second matches",
			stage: StageConfig{Name: "build", Owner: Owners{"pm", "tl"}},
			role:  "tl",
			want:  true,
		},
		{
			name:  "multiple owners no match",
			stage: StageConfig{Name: "build", Owner: Owners{"pm", "tl"}},
			role:  "engineer",
			want:  false,
		},
		{
			name:  "legacy OwnerRole matches",
			stage: StageConfig{Name: "build", OwnerRole: "engineer"},
			role:  "engineer",
			want:  true,
		},
		{
			name:  "no owner allows any role",
			stage: StageConfig{Name: "build"},
			role:  "pm",
			want:  true,
		},
		{
			name:  "empty role always allowed",
			stage: StageConfig{Name: "build", Owner: Owners{"engineer"}},
			role:  "",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stage.HasOwner(tt.role); got != tt.want {
				t.Errorf("HasOwner(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestOwnersUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		want     []string
		wantStr  string
	}{
		{
			name:    "single owner string",
			yaml:    `owner: pm`,
			want:    []string{"pm"},
			wantStr: "pm",
		},
		{
			name:    "multiple owners array",
			yaml:    `owner: [pm, tl]`,
			want:    []string{"pm", "tl"},
			wantStr: "pm, tl",
		},
		{
			name:    "multiple owners multiline",
			yaml:    "owner:\n  - pm\n  - tl\n  - designer",
			want:    []string{"pm", "tl", "designer"},
			wantStr: "pm, tl, designer",
		},
		{
			name:    "empty owner",
			yaml:    `name: build`,
			want:    nil,
			wantStr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stage StageConfig
			if err := yaml.Unmarshal([]byte(tt.yaml), &stage); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if len(stage.Owner) != len(tt.want) {
				t.Errorf("len(Owner) = %d, want %d", len(stage.Owner), len(tt.want))
			}
			for i, w := range tt.want {
				if i < len(stage.Owner) && stage.Owner[i] != w {
					t.Errorf("Owner[%d] = %q, want %q", i, stage.Owner[i], w)
				}
			}
			if got := stage.Owner.String(); got != tt.wantStr {
				t.Errorf("Owner.String() = %q, want %q", got, tt.wantStr)
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

func TestStageReviewConfig(t *testing.T) {
	yamlContent := `
name: engineering
owner: engineer
review:
  required: true
  reviewers: [tl, "@mike"]
  min_approvals: 2
`
	var stage StageConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &stage); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if stage.Review == nil {
		t.Fatal("Review config should not be nil")
	}

	if !stage.Review.IsRequired() {
		t.Error("IsRequired should be true")
	}

	if len(stage.Review.Reviewers) != 2 {
		t.Errorf("Reviewers count = %d, want 2", len(stage.Review.Reviewers))
	}

	if stage.Review.GetMinApprovals() != 2 {
		t.Errorf("MinApprovals = %d, want 2", stage.Review.GetMinApprovals())
	}
}

func TestStageReviewConfig_Defaults(t *testing.T) {
	// When Review is nil
	var cfg *StageReviewConfig
	if cfg.IsRequired() {
		t.Error("nil config should not be required")
	}
	if cfg.GetMinApprovals() != 1 {
		t.Errorf("nil config MinApprovals = %d, want 1", cfg.GetMinApprovals())
	}

	// When Review is present but Required not set
	cfg = &StageReviewConfig{}
	if !cfg.IsRequired() {
		t.Error("empty config should default to required")
	}
	if cfg.GetMinApprovals() != 1 {
		t.Errorf("empty config MinApprovals = %d, want 1", cfg.GetMinApprovals())
	}
}

func TestAutoAdvanceConfig(t *testing.T) {
	yamlContent := `
name: pr-review
owner: engineer
auto_advance:
  when: "prs.all_approved and prs.threads_resolved"
  notify: [author, next_owner]
  quiet_hours: "22:00-08:00"
  exclude_labels: [needs-discussion]
`
	var stage StageConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &stage); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	aa := stage.AutoAdvance
	if aa == nil {
		t.Fatal("AutoAdvance should not be nil")
	}

	if !aa.IsEnabled() {
		t.Error("IsEnabled should be true when When is set")
	}

	if aa.When != "prs.all_approved and prs.threads_resolved" {
		t.Errorf("When = %q", aa.When)
	}

	if len(aa.Notify) != 2 {
		t.Errorf("Notify count = %d, want 2", len(aa.Notify))
	}

	if aa.QuietHours != "22:00-08:00" {
		t.Errorf("QuietHours = %q", aa.QuietHours)
	}

	if len(aa.ExcludeLabels) != 1 || aa.ExcludeLabels[0] != "needs-discussion" {
		t.Errorf("ExcludeLabels = %v", aa.ExcludeLabels)
	}
}

func TestAutoAdvanceConfig_Disabled(t *testing.T) {
	f := false
	cfg := &AutoAdvanceConfig{
		When:    "prs.all_approved",
		Enabled: &f,
	}

	if cfg.IsEnabled() {
		t.Error("should not be enabled when Enabled is explicitly false")
	}

	// nil config
	var nilCfg *AutoAdvanceConfig
	if nilCfg.IsEnabled() {
		t.Error("nil config should not be enabled")
	}

	// empty When
	emptyCfg := &AutoAdvanceConfig{}
	if emptyCfg.IsEnabled() {
		t.Error("empty When should not be enabled")
	}
}

func TestGateConfig_StepsExists(t *testing.T) {
	tr := true

	// New style
	g1 := GateConfig{StepsExists: &tr}
	if !g1.HasStepsExists() {
		t.Error("HasStepsExists should be true for StepsExists")
	}
	if g1.Type() != "steps_exists" {
		t.Errorf("Type = %q, want steps_exists", g1.Type())
	}

	// Legacy style
	g2 := GateConfig{PRStackExists: &tr}
	if !g2.HasStepsExists() {
		t.Error("HasStepsExists should be true for PRStackExists (legacy)")
	}
	if g2.Type() != "steps_exists" {
		t.Errorf("Legacy Type = %q, want steps_exists", g2.Type())
	}
}

func TestGateConfig_ReviewApproved(t *testing.T) {
	tr := true
	g := GateConfig{ReviewApproved: &tr}

	if !g.HasReviewApproved() {
		t.Error("HasReviewApproved should be true")
	}
	if g.Type() != "review_approved" {
		t.Errorf("Type = %q, want review_approved", g.Type())
	}
}

func TestNewSimpleGate_NewTypes(t *testing.T) {
	tests := []struct {
		gateType string
		wantType string
	}{
		{"steps_exists", "steps_exists"},
		{"pr_stack_exists", "steps_exists"}, // legacy maps to new
		{"review_approved", "review_approved"},
	}

	for _, tt := range tests {
		g := NewSimpleGate(tt.gateType, "")
		if g.Type() != tt.wantType {
			t.Errorf("NewSimpleGate(%q).Type() = %q, want %q", tt.gateType, g.Type(), tt.wantType)
		}
	}
}
