package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/metrics"
)

func TestBuildRetroSection_PerSpec(t *testing.T) {
	spec := &metrics.SpecMetrics{
		SpecID:        "SPEC-042",
		StagesVisited: []string{"draft", "review", "build", "done"},
		TimePerStage: map[string]time.Duration{
			"draft":  2 * time.Hour,
			"review": 24 * time.Hour,
		},
		TotalTime:  26 * time.Hour,
		Reversions: 1,
	}

	cycle := &metrics.PipelineMetrics{
		SpecsCompleted:  5,
		TotalAdvances:   20,
		TotalReversions: 3,
		ReversionRate:   0.15,
		BottleneckStage: "review",
		AvgTimePerStage: map[string]time.Duration{
			"review": 48 * time.Hour,
		},
	}

	stageNames := []string{"draft", "review", "build", "done"}
	out := buildRetroSection(spec, cycle, stageNames, "Sprint 42")

	// Per-spec content
	if !strings.Contains(out, "This spec") {
		t.Errorf("expected per-spec header, got:\n%s", out)
	}
	if !strings.Contains(out, "draft → review → build → done") {
		t.Errorf("expected stage journey, got:\n%s", out)
	}
	if !strings.Contains(out, "Reversions**: 1") {
		t.Errorf("expected reversion count, got:\n%s", out)
	}

	// Cycle context
	if !strings.Contains(out, "Cycle context") {
		t.Errorf("expected cycle context header, got:\n%s", out)
	}
	if !strings.Contains(out, "Specs completed**: 5") {
		t.Errorf("expected cycle specs completed, got:\n%s", out)
	}
	if !strings.Contains(out, "Bottleneck**: review") {
		t.Errorf("expected bottleneck, got:\n%s", out)
	}
}

func TestBuildRetroSection_EmptySpec(t *testing.T) {
	spec := &metrics.SpecMetrics{
		SpecID:       "SPEC-001",
		TimePerStage: make(map[string]time.Duration),
	}
	cycle := &metrics.PipelineMetrics{
		AvgTimePerStage: make(map[string]time.Duration),
	}

	out := buildRetroSection(spec, cycle, nil, "Sprint 1")

	if !strings.Contains(out, "Sprint 1") {
		t.Errorf("expected label in output, got:\n%s", out)
	}
	// Should still produce valid output without panicking
	if !strings.Contains(out, "This spec") {
		t.Errorf("expected per-spec section, got:\n%s", out)
	}
}
