package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func testPipeline() config.PipelineConfig {
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "draft"},
			{Name: "review"},
			{Name: "build"},
			{Name: "done"},
		},
	}
}

func TestRevertOverlay_OpenSetsTargets(t *testing.T) {
	var r revertOverlay
	err := r.openRevert("SPEC-001", "build", testPipeline())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.active {
		t.Error("overlay should be active after open")
	}
	if r.specID != "SPEC-001" {
		t.Errorf("specID = %q, want SPEC-001", r.specID)
	}
	// Stages before "build" are draft, review.
	if len(r.stages) != 2 {
		t.Fatalf("stages = %v, want 2 entries", r.stages)
	}
	if r.stages[0] != "draft" || r.stages[1] != "review" {
		t.Errorf("stages = %v, want [draft review]", r.stages)
	}
	// Default to immediately preceding stage.
	if r.selectedStage() != "review" {
		t.Errorf("selectedStage = %q, want review", r.selectedStage())
	}
}

func TestRevertOverlay_OpenFromFirstStage_ReturnsError(t *testing.T) {
	var r revertOverlay
	err := r.openRevert("SPEC-001", "draft", testPipeline())
	if err == nil {
		t.Error("opening revert from first stage should return an error")
	}
}

func TestRevertOverlay_OpenFromUnknownStage_ReturnsError(t *testing.T) {
	var r revertOverlay
	err := r.openRevert("SPEC-001", "nonexistent", testPipeline())
	if err == nil {
		t.Error("opening revert from unknown stage should return an error")
	}
}

func TestRevertOverlay_CycleStage(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "done", testPipeline())
	// Stages: draft, review, build. Default idx=2 (build).
	if r.selectedStage() != "build" {
		t.Fatalf("initial = %q, want build", r.selectedStage())
	}

	r.cycleStage()
	if r.selectedStage() != "draft" {
		t.Errorf("after cycle = %q, want draft (wraps)", r.selectedStage())
	}

	r.cycleStage()
	if r.selectedStage() != "review" {
		t.Errorf("after second cycle = %q, want review", r.selectedStage())
	}
}

func TestRevertOverlay_CycleStageReverse(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "done", testPipeline())
	// Default idx=2 (build).
	r.cycleStageReverse()
	if r.selectedStage() != "review" {
		t.Errorf("after reverse cycle = %q, want review", r.selectedStage())
	}
}

func TestRevertOverlay_FieldNavigation(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline())

	if r.field != revertFieldStage {
		t.Errorf("initial field = %d, want stage", r.field)
	}

	r.nextField()
	if r.field != revertFieldReason {
		t.Errorf("after nextField = %d, want reason", r.field)
	}

	// Can't go beyond last field.
	r.nextField()
	if r.field != revertFieldReason {
		t.Errorf("field should stay at reason, got %d", r.field)
	}

	r.prevField()
	if r.field != revertFieldStage {
		t.Errorf("after prevField = %d, want stage", r.field)
	}

	// Can't go before first field.
	r.prevField()
	if r.field != revertFieldStage {
		t.Errorf("field should stay at stage, got %d", r.field)
	}
}

func TestRevertOverlay_ReasonInput(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline())
	r.nextField()

	r.appendToReason("gate")
	r.appendToReason(" failed")
	if r.reason != "gate failed" {
		t.Errorf("reason = %q, want 'gate failed'", r.reason)
	}

	r.backspaceReason()
	if r.reason != "gate faile" {
		t.Errorf("after backspace = %q, want 'gate faile'", r.reason)
	}
}

func TestRevertOverlay_Valid(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline())

	// Stage is set but no reason.
	if r.valid() {
		t.Error("should be invalid without reason")
	}

	r.reason = "needs rework"
	if !r.valid() {
		t.Error("should be valid with stage and reason")
	}
}

func TestRevertOverlay_Close(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline())
	r.close()
	if r.active {
		t.Error("overlay should be inactive after close")
	}
}

func TestRenderRevert_ContainsFields(t *testing.T) {
	var r revertOverlay
	_ = r.openRevert("SPEC-001", "build", testPipeline())
	r.reason = "gate failed"

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	got := renderRevert(r, styles)

	if !strings.Contains(got, "SPEC-001") {
		t.Error("should contain spec ID")
	}
	if !strings.Contains(got, "Stage") {
		t.Error("should contain Stage label")
	}
	if !strings.Contains(got, "Reason") {
		t.Error("should contain Reason label")
	}
	// Default selection is the immediately preceding stage ("review").
	if !strings.Contains(got, "review") {
		t.Error("should contain selected stage name")
	}
}
