package dashboard

import (
	"math"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/urgency"
)

func testPipeline() config.PipelineConfig {
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "build", StaleAfter: "48h"},
			{Name: "done"}, // no stale_after → never stale
		},
	}
}

func TestStageUrgency(t *testing.T) {
	pl := testPipeline()
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	rfc := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	tests := []struct {
		name      string
		stage     string
		enteredAt string
		updated   string
		curve     urgency.Curve
		want      float64
	}{
		{"fresh", "build", rfc(0), "", urgency.EaseIn, 0},
		{"half window ease-in", "build", rfc(24 * time.Hour), "", urgency.EaseIn, 0.25},
		{"half window linear", "build", rfc(24 * time.Hour), "", urgency.Linear, 0.5},
		{"full window", "build", rfc(48 * time.Hour), "", urgency.EaseIn, 1},
		{"over window clamps", "build", rfc(96 * time.Hour), "", urgency.EaseIn, 1},
		{"stage without window is never stale", "done", rfc(500 * time.Hour), "", urgency.EaseIn, 0},
		{"unknown stage is never stale", "ghost", rfc(500 * time.Hour), "", urgency.EaseIn, 0},
		{"no entry time is cold", "build", "", "", urgency.EaseIn, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StageUrgency(pl, tt.curve, tt.stage, tt.enteredAt, tt.updated, now)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("StageUrgency = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStageUrgencyFallsBackToUpdated(t *testing.T) {
	pl := testPipeline()
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	// Legacy spec with no stage_entered_at: dwell is measured from the updated
	// date (two days earlier → full 48h window).
	updated := now.Add(-48 * time.Hour).Format("2006-01-02")
	got := StageUrgency(pl, urgency.Linear, "build", "", updated, now)
	if got <= 0 {
		t.Errorf("expected non-zero urgency from updated fallback, got %v", got)
	}
}

func TestUrgencyLabel(t *testing.T) {
	if urgencyLabel(0.99) != "" {
		t.Error("fraction below 1 should not be labelled stale")
	}
	if urgencyLabel(1) != "stale" {
		t.Error("fraction at/over 1 should be labelled stale")
	}
}
