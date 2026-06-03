package tui

import (
	"strings"
	"testing"
)

// TestDashboard_ClickRow verifies the multi-line mapping: section headers and
// blank separators miss, item rows select/activate.
func TestDashboard_ClickRow(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 100
	m.height = 30
	m.items = []dashboardRow{
		{section: "BLOCKED", specID: "SPEC-005", title: "Stuck"},
		{section: "DO", specID: "SPEC-001", title: "Work"},
	}
	// Line model: 0 blank, 1 header, 2 item0, 3 blank, 4 header, 5 item1.
	// Each line maps to exactly one screen row (no MarginTop newline), so a
	// click lands on the row the user actually sees.
	for _, y := range []int{0, 1, 3, 4} {
		if got := m.clickRow(y); got != clickMissed {
			t.Errorf("click structural row %d = %v, want clickMissed", y, got)
		}
	}
	if got := m.clickRow(2); got != clickActivated { // cursor already 0
		t.Errorf("click selected row = %v, want clickActivated", got)
	}
	if got := m.clickRow(5); got != clickSelected {
		t.Errorf("click other item = %v, want clickSelected", got)
	}
	if m.cursor != 1 {
		t.Errorf("cursor after select = %d, want 1", m.cursor)
	}
}

// TestDashboard_LayoutLinesAreSingleRows guards the invariant that lets
// clickRow map screen rows to items: no rendered dashboard line may contain a
// newline, or it would occupy more screen rows than the model counts. This
// regresses the MarginTop(1) section-header bug that selected the row below.
func TestDashboard_LayoutLinesAreSingleRows(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 100
	m.height = 30
	m.items = []dashboardRow{
		{section: "BLOCKED", specID: "SPEC-005", title: "Stuck"},
		{section: "DO", specID: "SPEC-001", title: "Work"},
		{section: "DO", specID: "SPEC-002", title: "More"},
	}
	for i, l := range m.layoutLines(ContentWidth(m.width)) {
		if strings.Contains(l.text, "\n") {
			t.Errorf("dashLine %d contains a newline: %q", i, l.text)
		}
	}
}

// TestPipeline_ClickRow verifies the 2D mapping: a click resolves to the
// correct (stage, spec) pair, and structural lines miss.
func TestPipeline_ClickRow(t *testing.T) {
	m := testPipelineModel()
	m.width = 100
	m.height = 30
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001", Title: "A"}, {ID: "SPEC-002", Title: "B"}}},
		{Name: "build", Specs: []pipelineSpec{{ID: "SPEC-003", Title: "C"}}},
	}
	// Line model: 0 hdr, 1 (0,0), 2 (0,1), 3 blank, 4 hdr, 5 (1,0).
	if got := m.clickRow(0); got != clickMissed {
		t.Errorf("click stage header = %v, want clickMissed", got)
	}
	if got := m.clickRow(3); got != clickMissed {
		t.Errorf("click blank = %v, want clickMissed", got)
	}
	if got := m.clickRow(5); got != clickSelected {
		t.Errorf("click cross-stage spec = %v, want clickSelected", got)
	}
	if m.stageIdx != 1 || m.specIdx != 0 {
		t.Errorf("selection = (%d,%d), want (1,0)", m.stageIdx, m.specIdx)
	}
	if got := m.clickRow(5); got != clickActivated {
		t.Errorf("re-click selected spec = %v, want clickActivated", got)
	}
}

// TestPipeline_WheelMatchesKeys checks wheel stepping reuses the stage-aware
// navigation: one notch down from the last spec of a stage lands on the next
// stage's first spec.
func TestPipeline_WheelMatchesKeys(t *testing.T) {
	m := testPipelineModel()
	m.stages = []pipelineStage{
		{Name: "draft", Specs: []pipelineSpec{{ID: "SPEC-001"}}},
		{Name: "build", Specs: []pipelineSpec{{ID: "SPEC-002"}}},
	}
	m.wheelRows(1) // from (0,0), step down crosses the stage boundary
	if m.stageIdx != 1 || m.specIdx != 0 {
		t.Errorf("after wheel down selection = (%d,%d), want (1,0)", m.stageIdx, m.specIdx)
	}
	m.wheelRows(-1)
	if m.stageIdx != 0 || m.specIdx != 0 {
		t.Errorf("after wheel up selection = (%d,%d), want (0,0)", m.stageIdx, m.specIdx)
	}
}
