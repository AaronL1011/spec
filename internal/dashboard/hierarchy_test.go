package dashboard

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
)

func urgencyTestPipeline() config.PipelineConfig {
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "engineering"},
			{Name: "build"},
			{Name: "done"},
		},
	}
}

// An initiative sitting still while its slices are in flight is correct
// behaviour, not staleness. Without suppression every initiative is
// permanently at maximum urgency and the gradient stops meaning anything
// anywhere else on the dashboard.
func TestInitiativeUrgency(t *testing.T) {
	tests := []struct {
		name     string
		refs     []hierarchy.SpecRef
		specID   string
		fraction float64
		want     float64
	}{
		{
			name: "initiative with open slices does not warm",
			refs: []hierarchy.SpecRef{
				{ID: "SPEC-004", Status: "engineering"},
				{ID: "SPEC-009", Status: "done", Parent: "SPEC-004"},
				{ID: "SPEC-010", Status: "build", Parent: "SPEC-004"},
			},
			specID:   "SPEC-004",
			fraction: 0.9,
			want:     0,
		},
		{
			name: "initiative whose slices are all done warms normally",
			refs: []hierarchy.SpecRef{
				{ID: "SPEC-004", Status: "engineering"},
				{ID: "SPEC-009", Status: "done", Parent: "SPEC-004"},
			},
			specID:   "SPEC-004",
			fraction: 0.9,
			want:     0.9,
		},
		{
			name: "a slice warms normally",
			refs: []hierarchy.SpecRef{
				{ID: "SPEC-004", Status: "engineering"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
			},
			specID:   "SPEC-009",
			fraction: 0.7,
			want:     0.7,
		},
		{
			name:     "a standalone spec warms normally",
			refs:     []hierarchy.SpecRef{{ID: "SPEC-014", Status: "build"}},
			specID:   "SPEC-014",
			fraction: 1,
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initiativeUrgency(hierarchy.Build(tt.refs), urgencyTestPipeline(), tt.specID, tt.fraction)
			if got != tt.want {
				t.Errorf("initiativeUrgency = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParentLabels(t *testing.T) {
	g := hierarchy.Build([]hierarchy.SpecRef{
		{ID: "SPEC-004", Title: "API rate limiting"},
		{ID: "SPEC-009", Parent: "SPEC-004"},
		{ID: "SPEC-011", Parent: "SPEC-999"},
	})

	id, title := parentLabels(g, "SPEC-009")
	if id != "SPEC-004" || title != "API rate limiting" {
		t.Errorf("parentLabels = %q/%q, want SPEC-004/API rate limiting", id, title)
	}
	if id, title := parentLabels(g, "SPEC-004"); id != "" || title != "" {
		t.Errorf("an initiative has no parent labels, got %q/%q", id, title)
	}
	if id, title := parentLabels(g, "SPEC-011"); id != "" || title != "" {
		t.Errorf("a dangling parent yields no labels, got %q/%q", id, title)
	}
}

func TestSliceMark(t *testing.T) {
	if got := SliceMark(""); got != "" {
		t.Errorf("a standalone spec gets no marker, got %q", got)
	}
	if SliceMark("SPEC-004") == "" {
		t.Error("a slice must be marked")
	}
	if len([]rune(SliceMark("SPEC-004"))) != 1 {
		t.Error("the marker must be one cell so the ID column never shifts")
	}
}

func TestTitleSuffix(t *testing.T) {
	if got := titleSuffix(""); got != "" {
		t.Errorf("no title yields no suffix, got %q", got)
	}
	if got := titleSuffix("API rate limiting"); got != " — API rate limiting" {
		t.Errorf("titleSuffix = %q", got)
	}
}
