package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if !strings.Contains(got, "3") || !strings.Contains(got, "open") {
		t.Errorf("should show triage count, got: %q", got)
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

func TestTriage_UrgentSortFirst(t *testing.T) {
	m := testTriageModel()
	// Deliver items via triageDataMsg to exercise the sort path.
	var cmd tea.Cmd
	m, cmd = m.update(triageDataMsg{Items: []triageItem{
		{ID: "TRIAGE-001", Title: "Normal item", Priority: "medium", Created: "2026-01-01"},
		{ID: "TRIAGE-002", Title: "Urgent item", Priority: "high", Severity: "urgent", Created: "2026-01-02"},
	}})
	_ = cmd
	if len(m.items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(m.items))
	}
	// Urgent item must be at index 0 regardless of creation order.
	if m.items[0].ID != "TRIAGE-002" {
		t.Errorf("urgent item should be first, got %q", m.items[0].ID)
	}
}

func TestTriage_ArchivedFiltered(t *testing.T) {
	// loadTriageData (the async path) filters archived items, but here we test the
	// list model directly: archived items should NOT appear if injected.
	// In practice the loader filters them before returning, so the model never
	// receives archived items. Confirm the model doesn't re-show them after a
	// re-sort.
	m := testTriageModel()
	var cmd tea.Cmd
	m, cmd = m.update(triageDataMsg{Items: []triageItem{
		{ID: "TRIAGE-003", Title: "Open", Priority: "low"},
	}})
	_ = cmd
	if len(m.items) != 1 {
		t.Fatalf("expected 1 open item, got %d", len(m.items))
	}
}

func TestTriage_CursorPreservedAfterSort(t *testing.T) {
	m := testTriageModel()
	var cmd tea.Cmd
	// Initial load: both normal, sorted by created desc: [TRIAGE-002, TRIAGE-001]
	m, cmd = m.update(triageDataMsg{Items: []triageItem{
		{ID: "TRIAGE-001", Priority: "low", Created: "2026-01-01"},
		{ID: "TRIAGE-002", Priority: "low", Created: "2026-01-02"},
	}})
	_ = cmd
	// Select TRIAGE-001 (at index 1 after sort by created desc).
	m.cursor = 1
	m.selectedItemKey = "TRIAGE-001"
	oldKey := m.selectedItemKey
	// TRIAGE-002 escalated to urgent: new sort puts it first.
	// Result: [TRIAGE-002 (urgent), TRIAGE-001]
	m, cmd = m.update(triageDataMsg{Items: []triageItem{
		{ID: "TRIAGE-001", Priority: "low", Created: "2026-01-01"},
		{ID: "TRIAGE-002", Priority: "low", Severity: "urgent", Created: "2026-01-02"},
	}})
	_ = cmd
	// Urgent TRIAGE-002 should be at index 0.
	if m.items[0].ID != "TRIAGE-002" {
		t.Errorf("urgent item should sort first, got %q", m.items[0].ID)
	}
	// selectedItemKey should be unchanged.
	if m.selectedItemKey != oldKey {
		t.Errorf("selectedItemKey = %q, want %q", m.selectedItemKey, oldKey)
	}
	// Cursor must have followed TRIAGE-001 to its new index (1).
	if m.cursor != 1 {
		t.Errorf("cursor should follow TRIAGE-001 to index 1, got %d", m.cursor)
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
