package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/store"
)

func TestComputeForSpecEmpty(t *testing.T) {
	m := ComputeForSpec("SPEC-001", nil)
	if m.SpecID != "SPEC-001" {
		t.Errorf("expected SPEC-001, got %s", m.SpecID)
	}
	if m.Reversions != 0 || m.Ejections != 0 || m.TotalTime != 0 {
		t.Errorf("expected zero metrics, got %+v", m)
	}
}

func TestComputeForSpecNoMatch(t *testing.T) {
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-999", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`},
	}
	m := ComputeForSpec("SPEC-001", entries)
	if len(m.StagesVisited) != 0 {
		t.Errorf("expected no stages, got %v", m.StagesVisited)
	}
}

func TestComputeForSpecJourney(t *testing.T) {
	now := time.Now()
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-6 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-5 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "revert", Metadata: `{"from_stage":"review","to_stage":"draft"}`, CreatedAt: now.Add(-4 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-2 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"build","to_stage":"done"}`, CreatedAt: now},
	}

	m := ComputeForSpec("SPEC-001", entries)

	if m.Reversions != 1 {
		t.Errorf("expected 1 reversion, got %d", m.Reversions)
	}

	// 3 advance events: draft→review at -6h, review→build at -2h, build→done at 0h
	// draft dwell: 4h (from -6h to -2h), review dwell: 2h (from -2h to 0h)
	if m.TimePerStage["draft"] != 4*time.Hour {
		t.Errorf("expected draft dwell 4h, got %v", m.TimePerStage["draft"])
	}
	if m.TimePerStage["review"] != 2*time.Hour {
		t.Errorf("expected review dwell 2h, got %v", m.TimePerStage["review"])
	}
	if m.TotalTime != 6*time.Hour {
		t.Errorf("expected total 6h, got %v", m.TotalTime)
	}

	// Stages visited in order
	if len(m.StagesVisited) < 3 {
		t.Fatalf("expected at least 3 stages visited, got %v", m.StagesVisited)
	}
	if m.StagesVisited[0] != "draft" {
		t.Errorf("expected first stage draft, got %s", m.StagesVisited[0])
	}
}

func TestComputeForSpecEjections(t *testing.T) {
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: time.Now().Add(-3 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "eject", Metadata: `{"from_stage":"review"}`, CreatedAt: time.Now().Add(-2 * time.Hour)},
	}

	m := ComputeForSpec("SPEC-001", entries)
	if m.Ejections != 1 {
		t.Errorf("expected 1 ejection, got %d", m.Ejections)
	}
}

func TestFormatSpecSummary(t *testing.T) {
	sm := &SpecMetrics{
		SpecID:        "SPEC-001",
		StagesVisited: []string{"draft", "review", "build", "done"},
		TimePerStage: map[string]time.Duration{
			"draft":  2 * time.Hour,
			"review": 24 * time.Hour,
			"build":  48 * time.Hour,
		},
		TotalTime:  74 * time.Hour,
		Reversions: 1,
	}

	out := FormatSpecSummary(sm)

	if !strings.Contains(out, "draft → review → build → done") {
		t.Errorf("expected stage journey, got %s", out)
	}
	if !strings.Contains(out, "Reversions**: 1") {
		t.Errorf("expected reversion count, got %s", out)
	}
	if !strings.Contains(out, "Total time") {
		t.Errorf("expected total time, got %s", out)
	}
}
