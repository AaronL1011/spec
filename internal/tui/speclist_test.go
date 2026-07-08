package tui

import (
	"strings"
	"testing"
)

func testSpecListModel() specListModel {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSpecList(rc, styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestSpecList_EmptyView(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = nil
	m.applyFilter()

	got := m.view()
	if !strings.Contains(got, "No specs") {
		t.Errorf("empty list should show 'No specs', got: %q", got)
	}
}

func TestSpecList_WithSpecs(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth", Status: "build", Author: "alice"},
		{ID: "SPEC-002", Title: "Payments", Status: "draft", Author: "bob"},
	}
	m.applyFilter()

	got := m.view()
	if !strings.Contains(got, "SPEC-001") {
		t.Error("should contain SPEC-001")
	}
	if !strings.Contains(got, "SPEC-002") {
		t.Error("should contain SPEC-002")
	}
	if !strings.Contains(got, "2 specs") {
		t.Error("should show '2 specs' count")
	}
}

// The in-list substring filter was removed when the global `/` search overlay
// took over spec search (SPEC-028). applyFilter now just exposes the full
// spec set (split by archiveMode, selected at fetch time), so the old
// FuzzyFilter / FilterByStatus / ClearFilter / CursorClampOnFilter cases no
// longer apply. This test pins the new contract: all specs are visible.
func TestSpecList_ApplyFilterShowsAll(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "A"},
		{ID: "SPEC-002", Title: "B"},
		{ID: "SPEC-003", Title: "C"},
	}
	m.cursor = 2
	m.applyFilter()

	if len(m.filtered) != len(m.allSpecs) {
		t.Fatalf("applyFilter should show all %d specs, got %d", len(m.allSpecs), len(m.filtered))
	}
	// Cursor clamps within the (now full) list.
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		t.Errorf("cursor = %d out of range [0,%d)", m.cursor, len(m.filtered))
	}
}

func TestSpecList_SelectedSpecID(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001"},
		{ID: "SPEC-002"},
	}
	m.applyFilter()
	m.cursor = 1

	if got := m.selectedSpecID(); got != "SPEC-002" {
		t.Errorf("selectedSpecID = %q, want SPEC-002", got)
	}
}

func TestSpecList_RowFitsWidth(t *testing.T) {
	m := testSpecListModel()

	widths := []int{50, 60, 70, 80, 100, 120}
	for _, w := range widths {
		row := m.formatRow(
			"SPEC-001",
			"A very long spec title that could overflow the row width boundary",
			"in_progress",
			"alice",
			"2026-05-26",
			w,
		)
		if len(row) > w {
			t.Errorf("width=%d: row length %d exceeds width, row=%q", w, len(row), row)
		}
	}
}

func TestSpecList_ArchiveToggle(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth"},
	}
	m.applyFilter()

	// Initial state: not in archive mode
	if m.archiveMode {
		t.Error("initial archiveMode should be false")
	}

	// Toggle with '`'
	m, _ = m.update(keyMsg("`"))
	if !m.archiveMode {
		t.Error("after '`', archiveMode should be true")
	}
	if m.cursor != 0 {
		t.Errorf("cursor should reset to 0 after toggle, got %d", m.cursor)
	}

	// Toggle back with '`'
	m, _ = m.update(keyMsg("`"))
	if m.archiveMode {
		t.Error("after second '`', archiveMode should be false")
	}
}

func TestSpecList_ArchiveView_Empty(t *testing.T) {
	m := testSpecListModel()
	m.archiveMode = true
	m.allSpecs = nil
	m.applyFilter()

	got := m.view()
	if !strings.Contains(got, "No archived specs") {
		t.Errorf("archive mode empty should show 'No archived specs', got: %q", got)
	}
}

func TestSpecList_ArchiveView_Hints(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth"},
	}
	m.applyFilter()

	// Active list shows "`  archive" hint
	got := m.view()
	if !strings.Contains(got, "`") || !strings.Contains(got, "archive") {
		t.Error("active list should show '` archive' hint")
	}

	// Archive list shows "` specs" hint
	m.archiveMode = true
	m.allSpecs = nil
	m.applyFilter()
	got = m.view()
	if !strings.Contains(got, "`") || !strings.Contains(got, "specs") {
		t.Error("archive list should show '` specs' hint")
	}
}

func TestScrollWindow(t *testing.T) {
	tests := []struct {
		cursor, total, visible int
		wantStart, wantEnd     int
	}{
		{0, 5, 10, 0, 5},     // all visible
		{0, 20, 10, 0, 10},   // at top
		{15, 20, 10, 10, 20}, // near bottom
		{10, 20, 10, 5, 15},  // middle
	}
	for _, tt := range tests {
		s, e := scrollWindow(tt.cursor, tt.total, tt.visible)
		if s != tt.wantStart || e != tt.wantEnd {
			t.Errorf("scrollWindow(%d,%d,%d) = (%d,%d), want (%d,%d)",
				tt.cursor, tt.total, tt.visible, s, e, tt.wantStart, tt.wantEnd)
		}
	}
}
