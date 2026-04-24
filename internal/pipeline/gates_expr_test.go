package pipeline

import (
	"testing"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/pipeline/expr"
)

func TestEvaluateGatesWithExpressions(t *testing.T) {
	pipeline := config.PipelineConfig{
		Stages: []config.StageConfig{
			{
				Name:  "review",
				Owner: config.Owners{"tl"},
				Gates: []config.GateConfig{
					{Expr: "decisions.unresolved == 0", Message: "All decisions must be resolved"},
				},
			},
		},
	}

	tests := []struct {
		name       string
		ctx        expr.Context
		wantPassed bool
	}{
		{
			name: "expression passes",
			ctx: func() expr.Context {
				return expr.NewContextBuilder().
					WithDecisions(5, 5).
					Build()
			}(),
			wantPassed: true,
		},
		{
			name: "expression fails",
			ctx: func() expr.Context {
				return expr.NewContextBuilder().
					WithDecisions(5, 3).
					Build()
			}(),
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := EvaluateGatesWithContext(pipeline, "review", tt.ctx)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if results[0].Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v (reason: %s)", results[0].Passed, tt.wantPassed, results[0].Reason)
			}
		})
	}
}

func TestEvaluateComplexExpressionGate(t *testing.T) {
	pipeline := config.PipelineConfig{
		Stages: []config.StageConfig{
			{
				Name:  "build",
				Owner: config.Owners{"engineer"},
				Gates: []config.GateConfig{
					{Expr: "acceptance_criteria.items.count >= 3 && decisions.unresolved == 0"},
				},
			},
		},
	}

	ctx := expr.NewContextBuilder().
		WithAcceptanceCriteria(5, 2).
		WithDecisions(3, 3).
		Build()

	results := EvaluateGatesWithContext(pipeline, "build", ctx)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected gate to pass, got: %s", results[0].Reason)
	}
}

func TestEvaluateLabelsExpression(t *testing.T) {
	pipeline := config.PipelineConfig{
		Stages: []config.StageConfig{
			{
				Name:  "fast_track",
				Owner: config.Owners{"tl"},
				Gates: []config.GateConfig{
					{Expr: "'urgent' in spec.labels", Message: "Only urgent specs can fast-track"},
				},
			},
		},
	}

	tests := []struct {
		name       string
		labels     []string
		wantPassed bool
	}{
		{
			name:       "has urgent label",
			labels:     []string{"bug", "urgent"},
			wantPassed: true,
		},
		{
			name:       "no urgent label",
			labels:     []string{"feature", "low-priority"},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := expr.NewContextBuilder().
				WithSpec("SPEC-001", "Test", "build", tt.labels, 0, 0, 0).
				Build()

			results := EvaluateGatesWithContext(pipeline, "fast_track", ctx)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if results[0].Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", results[0].Passed, tt.wantPassed)
			}
		})
	}
}
