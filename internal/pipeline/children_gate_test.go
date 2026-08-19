package pipeline

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline/expr"
)

func boolPtr(b bool) *bool { return &b }

// childrenGateStage wraps gates on a single stage so a table row can compose
// them the way a team's spec.config.yaml would.
func childrenGateStage(gates ...config.GateConfig) config.PipelineConfig {
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "build"},
			{Name: "pr-review", Gates: gates},
		},
	}
}

func TestChildrenCompleteGate(t *testing.T) {
	tests := []struct {
		name     string
		children expr.ChildrenContext
		want     bool
		reason   string
	}{
		{
			name:     "childless spec is false, never vacuously true",
			children: expr.ChildrenContext{},
			want:     false,
			reason:   "no deliverable slices",
		},
		{
			name:     "one open child",
			children: expr.ChildrenContext{Total: 3, Complete: 2, Open: 1},
			want:     false,
			reason:   "1 of 3 slices are still open",
		},
		{
			name:     "a blocked child is still open",
			children: expr.ChildrenContext{Total: 2, Complete: 1, Open: 1, Blocked: 1},
			want:     false,
		},
		{
			name:     "all children terminal",
			children: expr.ChildrenContext{Total: 3, Complete: 3},
			want:     true,
		},
		{
			name:     "a single terminal child is enough",
			children: expr.ChildrenContext{Total: 1, Complete: 1},
			want:     true,
		},
	}

	pl := childrenGateStage(config.GateConfig{ChildrenComplete: boolPtr(true)})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := EvaluateGates(pl, "pr-review", nil, false, false, nil, tt.children)
			if len(results) != 1 {
				t.Fatalf("got %d gate results, want 1", len(results))
			}
			if results[0].Passed != tt.want {
				t.Errorf("children_complete passed = %v, want %v (reason %q)", results[0].Passed, tt.want, results[0].Reason)
			}
			if results[0].Gate != "children_complete" {
				t.Errorf("gate name = %q, want children_complete", results[0].Gate)
			}
			if tt.reason != "" && !strings.Contains(results[0].Reason, tt.reason) {
				t.Errorf("reason %q should mention %q", results[0].Reason, tt.reason)
			}
		})
	}
}

// The whole safety argument for shipping children_complete before any team
// adopts it: composed under `any:` beside a delivery gate, a childless spec
// must still be held by the delivery gate. If children_complete were vacuously
// true for a childless spec, this composition would silently disable
// pr_stack_exists for every ordinary spec in the repo.
func TestChildrenCompleteGate_CannotWaiveDeliveryGateForOrdinarySpec(t *testing.T) {
	anyGate := config.GateConfig{Any: []config.GateConfig{
		{PRStackExists: boolPtr(true)},
		{ChildrenComplete: boolPtr(true)},
	}}
	reversed := config.GateConfig{Any: []config.GateConfig{
		{ChildrenComplete: boolPtr(true)},
		{PRStackExists: boolPtr(true)},
	}}

	for _, gate := range []config.GateConfig{anyGate, reversed} {
		pl := childrenGateStage(gate)

		results := EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{})
		if AllGatesPassed(results) {
			t.Fatalf("a childless spec with no PR stack must not pass any:[pr_stack_exists, children_complete]: %+v", results)
		}

		// An initiative with every slice done passes the same composed gate
		// without any PR stack of its own.
		results = EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{Total: 2, Complete: 2})
		if !AllGatesPassed(results) {
			t.Fatalf("a complete initiative must pass the composed gate: %+v", results)
		}
	}
}

func TestChildrenCompleteGate_UnderNot(t *testing.T) {
	pl := childrenGateStage(config.GateConfig{Not: &config.GateConfig{ChildrenComplete: boolPtr(true)}})

	results := EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{})
	if !AllGatesPassed(results) {
		t.Errorf("not:[children_complete] should pass for a childless spec: %+v", results)
	}

	results = EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{Total: 1, Complete: 1})
	if AllGatesPassed(results) {
		t.Errorf("not:[children_complete] should fail for a complete initiative: %+v", results)
	}
}

// The rollup is also reachable from expression gates and skip_when, which is
// what makes the tagged sub-context worth having.
func TestChildrenContext_IsVisibleToExpressions(t *testing.T) {
	pl := childrenGateStage(config.GateConfig{Expr: "children.total > 0 and children.open == 0"})

	results := EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{})
	if AllGatesPassed(results) {
		t.Error("childless spec should fail children.total > 0")
	}
	results = EvaluateGates(pl, "pr-review", nil, false, false, nil, expr.ChildrenContext{Total: 2, Complete: 2})
	if !AllGatesPassed(results) {
		t.Errorf("complete initiative should pass: %+v", results)
	}
}

func TestBuildExprContext_CarriesChildren(t *testing.T) {
	ctx := BuildExprContext(nil, false, false, &markdown.SpecMeta{ID: "SPEC-004"},
		expr.ChildrenContext{Total: 5, Complete: 3, Open: 2, Blocked: 1})
	want := expr.ChildrenContext{Total: 5, Complete: 3, Open: 2, Blocked: 1}
	if ctx.Children != want {
		t.Errorf("ctx.Children = %+v, want %+v", ctx.Children, want)
	}
}
