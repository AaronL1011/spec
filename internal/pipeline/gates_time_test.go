package pipeline

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline/expr"
)

func TestEvaluateGatesDurationGate(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02")
	twoDaysAgo := now.Add(-48 * time.Hour).Format("2006-01-02")

	tests := []struct {
		name        string
		meta        *markdown.SpecMeta
		gateStr     string
		wantPassed  bool
		description string
	}{
		{
			name: "spec 2 days old passes 1d gate",
			meta: &markdown.SpecMeta{
				Updated: twoDaysAgo,
			},
			gateStr:     "24h",
			wantPassed:  true, // 2 days old, passes 1 day requirement
			description: "old spec passes short duration gate",
		},
		{
			name: "spec 1 day old fails 2d gate",
			meta: &markdown.SpecMeta{
				Updated: yesterday,
			},
			gateStr:     "48h",
			wantPassed:  false, // only 1 day old, needs 2 days
			description: "spec fails if not in stage long enough",
		},
		{
			name:        "malformed date defaults to 0 dwell",
			meta:        &markdown.SpecMeta{Updated: "invalid-date"},
			gateStr:     "1h",
			wantPassed:  false, // 0 dwell fails the gate (safety measure)
			description: "malformed date causes gate to fail",
		},
		{
			name:        "empty date defaults to 0 dwell",
			meta:        &markdown.SpecMeta{Updated: ""},
			gateStr:     "1h",
			wantPassed:  false,
			description: "empty date causes gate to fail",
		},
		{
			name:        "no meta provided",
			meta:        nil,
			gateStr:     "1h",
			wantPassed:  false, // timeInStage is 0, fails the gate
			description: "nil meta causes gate to fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := config.PipelineConfig{
				Stages: []config.StageConfig{
					{
						Name: "review",
						Gates: []config.GateConfig{
							{Duration: tt.gateStr},
						},
					},
				},
			}

			results := EvaluateGates(pipeline, "review", nil, false, false, tt.meta)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}

			if results[0].Passed != tt.wantPassed {
				t.Errorf("%s: expected Passed=%v, got %v. Reason: %s",
					tt.description, tt.wantPassed, results[0].Passed, results[0].Reason)
			}
		})
	}
}

// TestEvaluateGatesAndEvaluateGatesWithContextConsistency verifies that both
// gate evaluation paths (EvaluateGates and EvaluateGatesWithContext) produce
// identical results. This ensures the consolidated evaluation logic is correct.
func TestEvaluateGatesAndEvaluateGatesWithContextConsistency(t *testing.T) {
	now := time.Now()
	pipeline := config.PipelineConfig{
		Stages: []config.StageConfig{
			{
				Name: "review",
				Gates: []config.GateConfig{
					{Duration: "1h"},
					{SectionNotEmpty: "design_doc"},
				},
			},
		},
	}

	meta := &markdown.SpecMeta{
		Updated: now.Add(-2 * time.Hour).Format("2006-01-02"),
	}

	sections := []markdown.Section{
		{Slug: "design_doc", Content: "Some content here"},
	}

	// Evaluate using EvaluateGates (raw parameters)
	resultsA := EvaluateGates(pipeline, "review", sections, false, false, meta)

	// Evaluate using EvaluateGatesWithContext (pre-built context)
	// We need to construct the same context
	ctx := buildContextFromRaw(pipeline, "review", sections, false, false, meta)
	resultsB := EvaluateGatesWithContext(pipeline, "review", ctx)

	if len(resultsA) != len(resultsB) {
		t.Fatalf("result count mismatch: EvaluateGates=%d, EvaluateGatesWithContext=%d",
			len(resultsA), len(resultsB))
	}

	for i := range resultsA {
		if resultsA[i].Passed != resultsB[i].Passed {
			t.Errorf("gate %d mismatch: EvaluateGates.Passed=%v, EvaluateGatesWithContext.Passed=%v",
				i, resultsA[i].Passed, resultsB[i].Passed)
		}
		if resultsA[i].Gate != resultsB[i].Gate {
			t.Errorf("gate %d name mismatch: %q vs %q", i, resultsA[i].Gate, resultsB[i].Gate)
		}
	}
}

// buildContextFromRaw replicates the context building logic from EvaluateGates
// for testing purposes.
func buildContextFromRaw(pipeline config.PipelineConfig, currentStage string, sections []markdown.Section, hasPRStack bool, prsApproved bool, meta *markdown.SpecMeta) expr.Context {
	var timeInStage time.Duration
	var revertCount int
	var specID, specTitle, specStatus string
	if meta != nil {
		if meta.Updated != "" {
			if updated, err := time.Parse("2006-01-02", meta.Updated); err == nil {
				timeInStage = time.Since(updated)
			}
		}
		revertCount = meta.RevertCount
		specID = meta.ID
		specTitle = meta.Title
		specStatus = meta.Status
	}

	ctx := expr.NewContextBuilder().
		WithSpec(specID, specTitle, specStatus, nil, 0, timeInStage, revertCount).
		WithPRStack(hasPRStack, 0, 0, false, false).
		WithPRs(0, 0, prsApproved).
		Build()

	for _, sec := range sections {
		ctx.Sections[sec.Slug] = expr.SectionContext{
			Empty:     len(sec.Content) == 0,
			WordCount: len(sec.Content),
		}
	}

	return ctx
}
