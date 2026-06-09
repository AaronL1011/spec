package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTriageEditOverlay_CyclePriority(t *testing.T) {
	o := triageEditOverlay{priority: "low"}
	o.cyclePriority()
	if o.priority != "medium" {
		t.Errorf("after cycling low got %q, want medium", o.priority)
	}
	o.cyclePriority()
	if o.priority != "high" {
		t.Errorf("after cycling medium got %q, want high", o.priority)
	}
	o.cyclePriority()
	if o.priority != "critical" {
		t.Errorf("after cycling high got %q, want critical", o.priority)
	}
	o.cyclePriority()
	if o.priority != "low" {
		t.Errorf("after cycling critical (wrap) got %q, want low", o.priority)
	}
}

func TestTriageEditOverlay_Valid(t *testing.T) {
	var o triageEditOverlay
	o.openEdit(triageItem{ID: "TRIAGE-001"}, 80, 24)
	o.title.SetValue("")
	if o.valid() {
		t.Error("empty title should be invalid")
	}
	o.title.SetValue("  ")
	if o.valid() {
		t.Error("whitespace title should be invalid")
	}
	o.title.SetValue("Something")
	if !o.valid() {
		t.Error("non-empty title should be valid")
	}
}

func TestTriageEditOverlay_OpenPreFills(t *testing.T) {
	item := triageItem{
		ID:       "TRIAGE-005",
		Title:    "Pre-filled",
		Priority: "high",
		Source:   "Slack",
		Body:     "Some body",
	}
	var o triageEditOverlay
	o.openEdit(item, 80, 24)
	if !o.active {
		t.Error("overlay should be active after openEdit")
	}
	if o.title.Value() != item.Title {
		t.Errorf("title = %q, want %q", o.title.Value(), item.Title)
	}
	if o.priority != item.Priority {
		t.Errorf("priority = %q, want %q", o.priority, item.Priority)
	}
	if o.source.Value() != item.Source {
		t.Errorf("source = %q, want %q", o.source.Value(), item.Source)
	}
	if o.body.Value() != item.Body {
		t.Errorf("body = %q, want %q", o.body.Value(), item.Body)
	}
}

func TestTriageEditOverlay_BodySupportsCursorNavigation(t *testing.T) {
	var o triageEditOverlay
	o.openEdit(triageItem{ID: "TRIAGE-006", Body: "context line\nnotes line"}, 80, 24)
	o.field = triageEditFieldBody
	o.focusField()

	// Typing at the start (after moveBodyToStart) must prepend, not append, so
	// the user can edit the context section without deleting their notes.
	o.update(tea.KeyPressMsg{Text: "X"})
	if !strings.HasPrefix(o.body.Value(), "X") {
		t.Errorf("cursor should start at the top; got body %q", o.body.Value())
	}
	if !strings.Contains(o.body.Value(), "notes line") {
		t.Error("existing notes must be preserved when editing the top")
	}
}

func TestTriageEditOverlay_FieldNavigationFocus(t *testing.T) {
	var o triageEditOverlay
	o.openEdit(triageItem{ID: "TRIAGE-008"}, 80, 24)
	if !o.title.Focused() {
		t.Error("title should be focused initially")
	}
	o.nextField() // priority (no component)
	if o.title.Focused() {
		t.Error("title should blur when leaving the field")
	}
	o.nextField() // source
	if !o.source.Focused() {
		t.Error("source should be focused")
	}
	o.nextField() // body
	if !o.body.Focused() {
		t.Error("body should be focused")
	}
	o.prevField() // back to source
	if !o.source.Focused() {
		t.Error("source should be focused after prevField")
	}
}

func TestRenderTriageEdit_ContainsFields(t *testing.T) {
	var o triageEditOverlay
	o.openEdit(triageItem{ID: "TRIAGE-007", Title: "My Title", Priority: "medium"}, 80, 24)
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	out := renderTriageEdit(o, styles)
	if !strings.Contains(out, "TRIAGE-007") {
		t.Error("rendered form should contain triage ID")
	}
	if !strings.Contains(out, "My Title") {
		t.Error("rendered form should contain the title value")
	}
}
