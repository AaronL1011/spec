package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/dashboard"
)

// hierarchyFixture is one initiative with three slices in mixed states, plus a
// standalone spec that must keep its own position in the list.
func hierarchyFixture() []specListItem {
	return []specListItem{
		{ID: "SPEC-004", Title: "API rate limiting", Status: "engineering", Author: "aaron", Updated: "2026-01-04"},
		{ID: "SPEC-009", Title: "Token bucket limiter", Status: "done", Author: "aaron", Updated: "2026-01-09", Parent: "SPEC-004", complete: true},
		{ID: "SPEC-010", Title: "Redis backend", Status: "build", Author: "sam", Updated: "2026-01-10", Parent: "SPEC-004"},
		{ID: "SPEC-013", Title: "Admin overrides", Status: "draft", Author: "kim", Updated: "2026-01-13", Parent: "SPEC-004"},
		{ID: "SPEC-014", Title: "Unrelated standalone spec", Status: "tl-review", Author: "sam", Updated: "2026-01-14"},
	}
}

func TestNestSpecs(t *testing.T) {
	got := nestSpecs(hierarchyFixture())

	wantOrder := []string{"SPEC-004", "SPEC-009", "SPEC-010", "SPEC-013", "SPEC-014"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d rows, want %d", len(got), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Fatalf("row %d = %s, want %s", i, got[i].ID, want)
		}
	}
	if !got[0].initiative || got[0].depth != 0 {
		t.Errorf("SPEC-004 should be an initiative at depth 0: %+v", got[0])
	}
	if got[0].rollup.Complete != 1 || got[0].rollup.Total != 3 {
		t.Errorf("rollup = %+v, want 1/3", got[0].rollup)
	}
	for _, i := range []int{1, 2, 3} {
		if got[i].depth != 1 {
			t.Errorf("%s should be nested at depth 1", got[i].ID)
		}
	}
	if !got[3].lastChild {
		t.Error("SPEC-013 is the last slice and should render an elbow")
	}
	if got[1].lastChild || got[2].lastChild {
		t.Error("only the final slice is the last child")
	}
	if got[4].depth != 0 || got[4].initiative {
		t.Errorf("standalone spec should be untouched: %+v", got[4])
	}
}

func TestNestSpecs_NoHierarchyIsIdentity(t *testing.T) {
	flat := []specListItem{{ID: "SPEC-001"}, {ID: "SPEC-002"}}
	got := nestSpecs(flat)
	if len(got) != 2 || got[0].ID != "SPEC-001" || got[1].ID != "SPEC-002" {
		t.Errorf("a list with no parents must be returned unchanged: %+v", got)
	}
	if hasHierarchy(got) {
		t.Error("hasHierarchy should be false, so the tree gutter stays off")
	}
}

// An archived initiative's live slices appear in the active list. They must
// stay at top level there rather than vanishing because their parent is not in
// the same list.
func TestNestSpecs_OrphanedSliceStaysVisible(t *testing.T) {
	got := nestSpecs([]specListItem{
		{ID: "SPEC-009", Parent: "SPEC-004"},
		{ID: "SPEC-014"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0].ID != "SPEC-009" || got[0].depth != 0 {
		t.Errorf("slice with an absent parent should stay at top level: %+v", got[0])
	}
}

func TestNestSpecs_SelfParentIsNotNested(t *testing.T) {
	got := nestSpecs([]specListItem{{ID: "SPEC-009", Parent: "SPEC-009"}})
	if len(got) != 1 || got[0].depth != 0 || got[0].initiative {
		t.Errorf("a self-parenting spec must not nest under itself: %+v", got)
	}
}

func TestCollapseSlices(t *testing.T) {
	nested := nestSpecs(hierarchyFixture())

	got := collapseSlices(nested, map[string]bool{"SPEC-004": true})
	if len(got) != 2 {
		t.Fatalf("collapsed list = %d rows, want 2 (initiative + standalone)", len(got))
	}
	if !got[0].folded {
		t.Error("the collapsed initiative should be marked folded")
	}
	if got[0].rollup.Total != 3 {
		t.Error("a folded initiative still reports its rollup")
	}

	if len(collapseSlices(nested, nil)) != len(nested) {
		t.Error("an empty collapse set must not change the list")
	}
}

func TestTreeGutter(t *testing.T) {
	tests := []struct {
		name         string
		item         specListItem
		anyHierarchy bool
		want         string
	}{
		{name: "off when the list has no hierarchy", item: specListItem{depth: 1}, want: ""},
		{name: "standalone reserves the cell", item: specListItem{}, anyHierarchy: true, want: "   "},
		{name: "expanded initiative", item: specListItem{initiative: true}, anyHierarchy: true, want: IconExpanded + "  "},
		{name: "folded initiative", item: specListItem{initiative: true, folded: true}, anyHierarchy: true, want: IconCollapsed + "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := treeGutter(tt.item, tt.anyHierarchy); got != tt.want {
				t.Errorf("treeGutter = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every connector, box-drawing or ASCII, must be the same display width or the
// columns to its right shift depending on which pair the terminal gets.
func TestTreeConnector_WidthIsStable(t *testing.T) {
	for _, c := range []string{connectorTee, connectorElbow, connectorTeeASCII, connectorElbowASCII} {
		if got := lipgloss.Width(c); got != connectorWidth {
			t.Errorf("connector %q is %d cells, want %d", c, got, connectorWidth)
		}
	}
}

// A test binary's stdout is never a TTY, so colour is disabled and the ASCII
// pair is what the renderer selects — the same fallback a piped or NO_COLOR
// terminal gets.
func TestTreeConnector_ASCIIFallback(t *testing.T) {
	if !colourDisabled() {
		t.Skip("colour is enabled; the box-drawing pair is in use")
	}
	if got := treeConnector(false); got != connectorTeeASCII {
		t.Errorf("tee = %q, want the ASCII fallback %q", got, connectorTeeASCII)
	}
	if got := treeConnector(true); got != connectorElbowASCII {
		t.Errorf("elbow = %q, want the ASCII fallback %q", got, connectorElbowASCII)
	}
}

// The nested view must not break the row-width budget the list already
// guarantees, at any width, with or without a bounty gutter.
func TestSpecList_NestedRowsFitWidth(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = hierarchyFixture()
	m.applyFilter()

	for _, w := range []int{50, 60, 70, 80, 100, 120} {
		for _, item := range m.filtered {
			mark, rest := m.formatRow(item, m.rowGutter(item), w)
			if got := lipgloss.Width(mark + rest); got > w {
				t.Errorf("width=%d: row %s is %d cells", w, item.ID, got)
			}
		}
	}
}

func TestSpecList_NestedView(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = hierarchyFixture()
	m.applyFilter()

	got := m.view()
	if !strings.Contains(got, treeConnector(false)) || !strings.Contains(got, treeConnector(true)) {
		t.Errorf("nested view should draw both connectors:\n%s", got)
	}
	if !strings.Contains(got, "1/3") {
		t.Errorf("the initiative row should carry its rollup:\n%s", got)
	}
	if !strings.Contains(got, IconExpanded) {
		t.Errorf("the initiative row should show its fold state:\n%s", got)
	}
}

func TestSpecList_CollapseToggle(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = hierarchyFixture()
	m.applyFilter()
	if len(m.filtered) != 5 {
		t.Fatalf("expected 5 rows before collapsing, got %d", len(m.filtered))
	}

	space := tea.KeyPressMsg{Code: ' ', Text: " "}
	if !key.Matches(space, m.keys.Collapse) {
		t.Skip("space is not bound to collapse in this keymap")
	}

	m.cursor = 0
	m, _ = m.update(space)
	if len(m.filtered) != 2 {
		t.Fatalf("collapsing the initiative should hide its 3 slices, got %d rows", len(m.filtered))
	}
	if m.selectedSpecID() != "SPEC-004" {
		t.Errorf("selection should stay on the initiative, got %s", m.selectedSpecID())
	}

	m, _ = m.update(space)
	if len(m.filtered) != 5 {
		t.Fatalf("expanding should restore the slices, got %d rows", len(m.filtered))
	}
}

// Pressing collapse on a slice folds its initiative and moves the selection
// there, so the row under the cursor never disappears.
func TestSpecList_CollapseFromSliceSelectsInitiative(t *testing.T) {
	m := testSpecListModel()
	m.allSpecs = hierarchyFixture()
	m.applyFilter()

	m.cursor = 2 // SPEC-010, a slice
	m.toggleCollapse()

	if m.selectedSpecID() != "SPEC-004" {
		t.Errorf("selection = %s, want the initiative SPEC-004", m.selectedSpecID())
	}
	if len(m.filtered) != 2 {
		t.Errorf("slices should be hidden, got %d rows", len(m.filtered))
	}
}

func TestSliceMark(t *testing.T) {
	if got := dashboard.SliceMark(""); got != "" {
		t.Errorf("a standalone spec gets no marker, got %q", got)
	}
	if got := dashboard.SliceMark("SPEC-004"); got != IconSlice {
		t.Errorf("a slice gets the slice marker, got %q", got)
	}
	if w := lipgloss.Width(IconSlice); w != 1 {
		t.Errorf("the slice marker is %d cells; it must be 1 so the ID column never shifts", w)
	}
}
