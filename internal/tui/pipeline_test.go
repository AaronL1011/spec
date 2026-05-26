package tui

import (
	"strings"
	"testing"
)

func testPipelineModel() pipelineModel {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newPipeline(rc, styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestPipeline_EmptyStages(t *testing.T) {
	m := testPipelineModel()
	m.stages = nil

	got := m.view()
	if !strings.Contains(got, "No pipeline") {
		t.Errorf("empty pipeline should show message, got: %q", got)
	}
}

func TestPipeline_WithStages(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{
			Name:  "draft",
			Owner: "pm",
			Icon:  "📝",
			Specs: []pipelineSpec{
				{ID: "SPEC-001", Title: "Auth service"},
			},
		},
		{
			Name:  "build",
			Owner: "engineer",
			Icon:  "🏗️",
			Specs: []pipelineSpec{
				{ID: "SPEC-002", Title: "User onboarding"},
				{ID: "SPEC-003", Title: "Payments v2"},
			},
		},
		{
			Name:  "done",
			Owner: "",
			Specs: nil,
		},
	}

	got := m.view()
	if !strings.Contains(got, "draft") {
		t.Error("should contain 'draft' stage")
	}
	if !strings.Contains(got, "build") {
		t.Error("should contain 'build' stage")
	}
	if !strings.Contains(got, "SPEC-002") {
		t.Error("should contain SPEC-002")
	}
	if !strings.Contains(got, "done") {
		t.Error("should contain 'done' stage even if empty")
	}
}

func TestPipeline_NavigateStages(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
		{Name: "build", Specs: []pipelineSpec{{ID: "SPEC-002"}, {ID: "SPEC-003"}}},
	}

	// Start at stage 0
	if m.stageIdx != 0 {
		t.Fatalf("initial stageIdx = %d, want 0", m.stageIdx)
	}

	// Move right to stage 1
	m, _ = m.update(keyMsg("l"))
	if m.stageIdx != 1 {
		t.Errorf("after 'l': stageIdx = %d, want 1", m.stageIdx)
	}
	if m.specIdx != 0 {
		t.Errorf("after stage switch: specIdx should reset to 0, got %d", m.specIdx)
	}

	// Move down within stage 1
	m, _ = m.update(keyMsg("j"))
	if m.specIdx != 1 {
		t.Errorf("after 'j': specIdx = %d, want 1", m.specIdx)
	}

	// Selected spec should be SPEC-003
	if got := m.selectedSpecID(); got != "SPEC-003" {
		t.Errorf("selectedSpecID = %q, want SPEC-003", got)
	}

	// Move left back to stage 0 — specIdx resets
	m, _ = m.update(keyMsg("h"))
	if m.stageIdx != 0 {
		t.Errorf("after 'h': stageIdx = %d, want 0", m.stageIdx)
	}
	if m.specIdx != 0 {
		t.Errorf("after stage switch back: specIdx = %d, want 0", m.specIdx)
	}
}

func TestPipeline_BoundsCheck(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
	}

	// Move left at leftmost — should stay
	m, _ = m.update(keyMsg("h"))
	if m.stageIdx != 0 {
		t.Errorf("stageIdx = %d, want 0", m.stageIdx)
	}

	// Move right at rightmost — should stay
	m, _ = m.update(keyMsg("l"))
	if m.stageIdx != 0 {
		t.Errorf("stageIdx = %d, want 0 (only one stage)", m.stageIdx)
	}
}
