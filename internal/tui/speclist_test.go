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

func TestSpecList_FuzzyFilter(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth service"},
		{ID: "SPEC-002", Title: "Payments v2"},
		{ID: "SPEC-003", Title: "Auth middleware"},
	}

	m.searchQuery = "auth"
	m.applyFilter()

	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 filtered results for 'auth', got %d", len(m.filtered))
	}
	if m.filtered[0].ID != "SPEC-001" {
		t.Errorf("first result = %s, want SPEC-001", m.filtered[0].ID)
	}
}

func TestSpecList_FilterByStatus(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "A", Status: "build"},
		{ID: "SPEC-002", Title: "B", Status: "draft"},
		{ID: "SPEC-003", Title: "C", Status: "build"},
	}

	m.searchQuery = "build"
	m.applyFilter()

	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 filtered results for 'build', got %d", len(m.filtered))
	}
}

func TestSpecList_ClearFilter(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "A"},
		{ID: "SPEC-002", Title: "B"},
	}

	m.searchQuery = "zzz"
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Fatalf("expected 0 results for 'zzz', got %d", len(m.filtered))
	}

	m.searchQuery = ""
	m.applyFilter()
	if len(m.filtered) != 2 {
		t.Fatalf("clearing filter should show all %d specs, got %d", len(m.allSpecs), len(m.filtered))
	}
}

func TestSpecList_CursorClampOnFilter(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "A"},
		{ID: "SPEC-002", Title: "B"},
		{ID: "SPEC-003", Title: "C"},
	}
	m.applyFilter()
	m.cursor = 2 // last item

	m.searchQuery = "A"
	m.applyFilter()
	if m.cursor != 0 {
		t.Errorf("cursor should clamp to 0 after filter reduces list, got %d", m.cursor)
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
