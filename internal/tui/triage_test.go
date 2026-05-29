package tui

import (
	"strings"
	"testing"
)

func testTriageModel() triageModel {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newTriage(rc, styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestTriage_EmptyQueue(t *testing.T) {
	m := testTriageModel()
	m.items = nil

	got := m.view()
	if !strings.Contains(got, "empty") {
		t.Errorf("empty triage should show 'empty', got: %q", got)
	}
}

func TestTriage_WithItems(t *testing.T) {
	m := testTriageModel()
	m.items = []triageItem{
		{ID: "TRIAGE-001", Title: "Login broken", Priority: "critical"},
		{ID: "TRIAGE-002", Title: "Slow dashboard", Priority: "medium"},
		{ID: "TRIAGE-003", Title: "Typo in docs", Priority: "low"},
	}

	got := m.view()
	if !strings.Contains(got, "TRIAGE-001") {
		t.Error("should contain TRIAGE-001")
	}
	if !strings.Contains(got, "3 items") {
		t.Error("should show '3 items in triage'")
	}
}

func TestTriage_CursorNavigation(t *testing.T) {
	m := testTriageModel()
	m.items = []triageItem{
		{ID: "TRIAGE-001"},
		{ID: "TRIAGE-002"},
		{ID: "TRIAGE-003"},
	}

	m, _ = m.update(keyMsg("j"))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	m, _ = m.update(keyMsg("j"))
	m, _ = m.update(keyMsg("j")) // past end
	if m.cursor != 2 {
		t.Errorf("cursor should clamp to 2, got %d", m.cursor)
	}

	if got := m.selectedItemID(); got != "TRIAGE-003" {
		t.Errorf("selectedItemID = %q, want TRIAGE-003", got)
	}
}

func TestPriorityIcon(t *testing.T) {
	tests := []struct {
		priority string
		want     string
	}{
		{"critical", IconActive},
		{"high", IconActive},
		{"medium", IconActive},
		{"low", IconActive},
		{"", IconOpen},
		{"unknown", IconOpen},
	}
	for _, tt := range tests {
		got := priorityIcon(tt.priority)
		if got != tt.want {
			t.Errorf("priorityIcon(%q) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}
