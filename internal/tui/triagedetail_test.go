package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

func testDetailPane() *triageDetailPane {
	item := triageItem{
		ID:         "TRIAGE-020",
		Title:      "Detail pane test",
		Priority:   "high",
		Severity:   "urgent",
		LinkedSpec: "SPEC-005",
		Source:     "slack",
		SourceRef:  "#channel-123",
		ReportedBy: "alice",
		Created:    "2026-06-01",
		Body:       "Something broke in production.\nSecond line.",
		Comments: []markdown.TriageComment{
			{Actor: "bob", Message: "confirmed on staging", At: "2026-06-01T10:00:00Z"},
		},
	}
	return newTriageDetailPane(item, 100, 30)
}

func TestTriageDetailPane_RendersAllMetadata(t *testing.T) {
	pane := testDetailPane()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	rc := rcWithRole("pm")

	out := pane.view(styles, rc)

	checks := []struct {
		label    string
		contains string
	}{
		{"title", "Detail pane test"},
		{"reporter", "@alice"},
		{"created", "2026-06-01"},
		{"priority", "priority: high"},
		{"severity", "severity: urgent"},
		{"source", "source: slack"},
		{"sourceRef", "ref: #channel-123"},
		{"linkedSpec", "linked: SPEC-005"},
		{"body", "Something broke"},
		{"history actor", "@bob"},
		{"history message", "confirmed on staging"},
	}
	for _, tc := range checks {
		if !strings.Contains(out, tc.contains) {
			t.Errorf("%s: output should contain %q", tc.label, tc.contains)
		}
	}
}

func TestTriageDetailPane_HintRowRoleFiltered(t *testing.T) {
	pane := testDetailPane()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))

	pmOut := pane.view(styles, rcWithRole("pm"))
	if !strings.Contains(pmOut, "promote") {
		t.Error("pm should see promote hint")
	}
	if !strings.Contains(pmOut, "edit") {
		t.Error("pm should see edit hint")
	}

	designerOut := pane.view(styles, rcWithRole("designer"))
	if strings.Contains(designerOut, "promote") {
		t.Error("designer should NOT see promote hint")
	}
	if strings.Contains(designerOut, "edit") {
		t.Error("designer should NOT see edit hint")
	}
	if !strings.Contains(designerOut, "note") {
		t.Error("designer should still see note hint")
	}
}

func TestTriageDetailPane_ContentAwareScroll(t *testing.T) {
	pane := testDetailPane()
	pane.height = 5 // small viewport

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	rc := rcWithRole("pm")

	_ = pane.view(styles, rc)

	pane.scrollDown()
	if pane.scroll < 1 {
		t.Error("scrollDown should increment scroll")
	}

	for i := 0; i < 100; i++ {
		pane.scrollDown()
	}
	mx := pane.maxScroll()
	if pane.scroll > mx {
		t.Errorf("scroll %d should not exceed maxScroll %d", pane.scroll, mx)
	}
}

func TestTriageDetailPane_HintAlwaysVisible(t *testing.T) {
	pane := testDetailPane()
	pane.item.Body = strings.Repeat("line\n", 50)
	pane.height = 10

	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	rc := rcWithRole("pm")

	out := pane.view(styles, rc)
	lines := strings.Split(out, "\n")

	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "note") && !strings.Contains(lastLine, "esc") {
		t.Error("hint row should be visible at the bottom regardless of scroll")
	}
}

func TestTriageDetailPane_UpdateItem(t *testing.T) {
	pane := testDetailPane()
	if pane.item.Title != "Detail pane test" {
		t.Fatal("initial title mismatch")
	}

	updated := pane.item
	updated.Title = "Updated title"
	updated.Severity = ""
	pane.updateItem(updated)

	if pane.item.Title != "Updated title" {
		t.Errorf("Title = %q after updateItem, want Updated title", pane.item.Title)
	}
	if pane.item.Severity != "" {
		t.Error("Severity should be cleared after updateItem")
	}
}

func TestTriageDetailPane_UrgentShowsDeescalateHint(t *testing.T) {
	pane := testDetailPane()
	pane.item.Severity = "urgent"
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	rc := rcWithRole("engineer")

	out := pane.view(styles, rc)
	if !strings.Contains(out, "de-escalate") {
		t.Error("urgent item should show de-escalate hint")
	}
}

func TestTriageDetailPane_NormalShowsEscalateHint(t *testing.T) {
	pane := testDetailPane()
	pane.item.Severity = ""
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	rc := rcWithRole("engineer")

	out := pane.view(styles, rc)
	if !strings.Contains(out, "escalate") {
		t.Error("normal item should show escalate hint")
	}
	if strings.Contains(out, "de-escalate") {
		t.Error("normal item should NOT show de-escalate hint")
	}
}

func TestTriageModel_FindItemByID(t *testing.T) {
	m := testTriageModel()
	m.items = []triageItem{
		{ID: "TRIAGE-001", Title: "First"},
		{ID: "TRIAGE-002", Title: "Second"},
	}

	found := m.findItemByID("TRIAGE-002")
	if found == nil {
		t.Fatal("should find TRIAGE-002")
		return
	}
	if found.Title != "Second" {
		t.Errorf("Title = %q, want Second", found.Title)
	}

	missing := m.findItemByID("TRIAGE-999")
	if missing != nil {
		t.Error("should return nil for missing ID")
	}
}
