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

func TestPipeline_DownWrapsToNextStage(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
		{Name: "review", Specs: nil}, // empty — should be skipped
		{Name: "build", Specs: []pipelineSpec{{ID: "SPEC-002"}, {ID: "SPEC-003"}}},
	}
	m.stageIdx = 0
	m.specIdx = 0

	// Already at last (only) spec in draft — down should jump to build.
	m, _ = m.update(keyMsg("j"))
	if m.stageIdx != 2 {
		t.Errorf("stageIdx = %d, want 2 (build, skipping empty review)", m.stageIdx)
	}
	if m.specIdx != 0 {
		t.Errorf("specIdx = %d, want 0 (first spec in build)", m.specIdx)
	}
	if got := m.selectedSpecID(); got != "SPEC-002" {
		t.Errorf("selectedSpecID = %q, want SPEC-002", got)
	}
}

func TestPipeline_UpWrapsToePrevStage(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}, {ID: "SPEC-002"}}},
		{Name: "review", Specs: nil}, // empty
		{Name: "build", Specs: []pipelineSpec{{ID: "SPEC-003"}}},
	}
	m.stageIdx = 2
	m.specIdx = 0

	// At first spec of build — up should jump to last spec of draft.
	m, _ = m.update(keyMsg("k"))
	if m.stageIdx != 0 {
		t.Errorf("stageIdx = %d, want 0 (draft)", m.stageIdx)
	}
	if m.specIdx != 1 {
		t.Errorf("specIdx = %d, want 1 (last spec in draft)", m.specIdx)
	}
	if got := m.selectedSpecID(); got != "SPEC-002" {
		t.Errorf("selectedSpecID = %q, want SPEC-002", got)
	}
}

func TestPipeline_DownAtEndStays(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
	}
	m.stageIdx = 0
	m.specIdx = 0

	// Only one spec in only one stage — down should stay.
	m, _ = m.update(keyMsg("j"))
	if m.stageIdx != 0 || m.specIdx != 0 {
		t.Errorf("should stay at (0,0), got (%d,%d)", m.stageIdx, m.specIdx)
	}
}

func TestPipeline_UpAtStartStays(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
	}
	m.stageIdx = 0
	m.specIdx = 0

	// At very top — up should stay.
	m, _ = m.update(keyMsg("k"))
	if m.stageIdx != 0 || m.specIdx != 0 {
		t.Errorf("should stay at (0,0), got (%d,%d)", m.stageIdx, m.specIdx)
	}
}

func TestPipeline_FullTraversal(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "A"}, {ID: "B"}}},
		{Name: "build", Specs: []pipelineSpec{{ID: "C"}}},
		{Name: "done", Specs: []pipelineSpec{{ID: "D"}, {ID: "E"}}},
	}

	// Walk down through all 5 specs: A → B → C → D → E
	want := []string{"A", "B", "C", "D", "E"}
	for i, w := range want {
		if got := m.selectedSpecID(); got != w {
			t.Errorf("step %d: selectedSpecID = %q, want %q", i, got, w)
		}
		if i < len(want)-1 {
			m, _ = m.update(keyMsg("j"))
		}
	}

	// Walk back up: E → D → C → B → A
	for i := len(want) - 1; i >= 0; i-- {
		if got := m.selectedSpecID(); got != want[i] {
			t.Errorf("back step %d: selectedSpecID = %q, want %q", i, got, want[i])
		}
		if i > 0 {
			m, _ = m.update(keyMsg("k"))
		}
	}
}
