package pipeline

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
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
