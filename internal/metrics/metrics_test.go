package metrics

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec-cli/internal/store"
)

func TestComputeEmpty(t *testing.T) {
	m := Compute(nil, nil, nil, nil)
	if m.SpecsCompleted != 0 || m.TotalAdvances != 0 || m.TotalReversions != 0 {
		t.Errorf("expected zero metrics, got %+v", m)
	}
	if m.ReversionRate != 0 {
		t.Errorf("expected 0 reversion rate, got %f", m.ReversionRate)
	}
}

func TestComputeAdvancesAndReversions(t *testing.T) {
	now := time.Now()
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-3 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-1 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "revert", Metadata: `{"from_stage":"build","to_stage":"review"}`, CreatedAt: now.Add(-30 * time.Minute)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-2 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"done"}`, CreatedAt: now},
	}

	stages := []string{"draft", "review", "build", "done"}
	m := Compute(entries, nil, stages, []string{"done"})

	if m.TotalAdvances != 4 {
		t.Errorf("expected 4 advances, got %d", m.TotalAdvances)
	}
	if m.TotalReversions != 1 {
		t.Errorf("expected 1 reversion, got %d", m.TotalReversions)
	}
	if m.ReversionRate != 0.25 {
		t.Errorf("expected 0.25 reversion rate, got %f", m.ReversionRate)
	}
	if m.SpecsCompleted != 1 {
		t.Errorf("expected 1 spec completed, got %d", m.SpecsCompleted)
	}
}

func TestComputeTimeInStage(t *testing.T) {
	now := time.Now()
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-4 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-2 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-6 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-4 * time.Hour)},
	}

	stages := []string{"draft", "review", "build"}
	m := Compute(entries, nil, stages, []string{"done"})

	// SPEC-001 draft dwell: 2h (from -4h to -2h between advance events)
	// But actually draft->review at -4h, then review->build at -2h => draft dwell is the gap
	// Wait: draft advance at -4h, but that's the advance FROM draft. We need the previous advance TO draft.
	// In our model: consecutive advances on same spec. advance[0] from_stage=draft at -4h, advance[1] from_stage=review at -2h.
	// So dwell for "draft" = advance[1].created_at - advance[0].created_at = 2h for SPEC-001
	// For SPEC-002: dwell for "draft" = advance[1].created_at - advance[0].created_at = 2h
	// Avg draft dwell = 2h

	// Review dwell: SPEC-001 has only 2 advances so review dwell not captured (no 3rd advance)
	// SPEC-002 also only 2 advances

	draftAvg := m.AvgTimePerStage["draft"]
	if draftAvg != 2*time.Hour {
		t.Errorf("expected avg draft dwell of 2h, got %v", draftAvg)
	}

	if m.BottleneckStage != "draft" {
		t.Errorf("expected bottleneck to be draft, got %q", m.BottleneckStage)
	}
}

func TestComputeBottleneck(t *testing.T) {
	now := time.Now()
	entries := []store.ActivityEntry{
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-10 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-2 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"build","to_stage":"done"}`, CreatedAt: now},
	}

	stages := []string{"draft", "review", "build", "done"}
	m := Compute(entries, nil, stages, []string{"done"})

	// draft dwell: 8h, review dwell: 2h
	if m.BottleneckStage != "draft" {
		t.Errorf("expected bottleneck to be draft (8h), got %q", m.BottleneckStage)
	}
}

func TestComputeMultipleVisitsToSameStage(t *testing.T) {
	now := time.Now()
	// Two specs both visit draft, but different durations.
	// SPEC-001: draft→review (2h in draft)
	// SPEC-002: draft→review→draft→build (4h in draft total: 3h + 1h)
	// Average draft dwell should be (2h + 3h + 1h) / 3 = 2h
	entries := []store.ActivityEntry{
		// SPEC-001
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-5 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"build"}`, CreatedAt: now.Add(-3 * time.Hour)},
		// SPEC-002
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-6 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"draft"}`, CreatedAt: now.Add(-3 * time.Hour)},
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"build"}`, CreatedAt: now.Add(-2 * time.Hour)},
	}

	stages := []string{"draft", "review", "build"}
	m := Compute(entries, nil, stages, []string{"build"})

	// SPEC-001 draft: advance at -5h to next at -3h = 2h dwell
	// SPEC-002 draft: advance at -6h to next at -3h = 3h dwell
	// Durations: [2h, 3h], average = 2.5h
	draftDwell := m.AvgTimePerStage["draft"]
	expectedDwell := 2*time.Hour + 30*time.Minute
	if draftDwell != expectedDwell {
		t.Errorf("expected draft dwell %v, got %v", expectedDwell, draftDwell)
	}

	// SPEC-001: no review dwell recorded (only 2 advances total, and review is from_stage of 2nd)
	// SPEC-002 review: advance at -3h to next at -2h = 1h dwell
	// Durations: [1h], average = 1h
	reviewDwell := m.AvgTimePerStage["review"]
	expectedReviewDwell := 1 * time.Hour
	if reviewDwell != expectedReviewDwell {
		t.Errorf("expected review dwell %v, got %v", expectedReviewDwell, reviewDwell)
	}
}

func TestComputeSpecsCompletedAccuracy(t *testing.T) {
	now := time.Now()
	// Verify that completed specs are counted accurately regardless of stage revisits
	entries := []store.ActivityEntry{
		// SPEC-001: draft→review→done (completed)
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-5 * time.Hour)},
		{SpecID: "SPEC-001", EventType: "advance", Metadata: `{"from_stage":"review","to_stage":"done"}`, CreatedAt: now.Add(-2 * time.Hour)},
		// SPEC-002: draft→review (not completed)
		{SpecID: "SPEC-002", EventType: "advance", Metadata: `{"from_stage":"draft","to_stage":"review"}`, CreatedAt: now.Add(-3 * time.Hour)},
	}

	stages := []string{"draft", "review", "done"}
	m := Compute(entries, nil, stages, []string{"done"})

	if m.SpecsCompleted != 1 {
		t.Errorf("expected 1 spec completed, got %d", m.SpecsCompleted)
	}
	if m.TotalAdvances != 3 {
		t.Errorf("expected 3 total advances, got %d", m.TotalAdvances)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
		{24 * time.Hour, "1d"},
		{26 * time.Hour, "1d 2h"},
		{72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.d)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
