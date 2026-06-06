package tui

import (
	"strings"
	"testing"
)

func TestTriageCloseOverlay_CycleReason(t *testing.T) {
	var o triageCloseOverlay
	o.openClose("TRIAGE-001")
	if o.selectedReason() != "resolved" {
		t.Errorf("initial reason = %q, want resolved", o.selectedReason())
	}
	o.cycleReason()
	if o.selectedReason() != "won't-fix" {
		t.Errorf("after cycle = %q, want won't-fix", o.selectedReason())
	}
}

func TestTriageNoteOverlay_Valid(t *testing.T) {
	var o triageNoteOverlay
	o.openNote("TRIAGE-002")
	if o.valid() {
		t.Error("empty note should be invalid")
	}
	o.append("some text")
	if !o.valid() {
		t.Error("non-empty note should be valid")
	}
}

func TestRenderTriageClose_ContainsFields(t *testing.T) {
	var o triageCloseOverlay
	o.openClose("TRIAGE-009")
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	out := renderTriageClose(o, styles)
	if !strings.Contains(out, "TRIAGE-009") {
		t.Error("rendered close dialog should contain triage ID")
	}
	if !strings.Contains(out, "resolved") {
		t.Error("rendered close dialog should show default reason")
	}
}

func TestRenderTriageNote(t *testing.T) {
	var o triageNoteOverlay
	o.openNote("TRIAGE-010")
	o.append("my note")
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	out := renderTriageNote(o, styles)
	if !strings.Contains(out, "TRIAGE-010") {
		t.Error("rendered note prompt should contain triage ID")
	}
	if !strings.Contains(out, "my note") {
		t.Error("rendered note prompt should show note text")
	}
}
