package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/markdown"
)

// sizedApp returns an app sized to a roomy terminal so every band has space.
func sizedApp(t *testing.T) App {
	t.Helper()
	app := testApp()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return model.(App)
}

// loadSpecs routes spec data into the specs view and selects that tab.
func loadSpecs(t *testing.T, a App, ids ...string) App {
	t.Helper()
	specs := make([]specListItem, len(ids))
	for i, id := range ids {
		specs[i] = specListItem{ID: id, Title: "Title " + id, Status: "do", Author: "x", Updated: "now"}
	}
	model, _ := a.Update(specListDataMsg{Specs: specs})
	a = model.(App)
	a.activeView = ViewSpecs
	a.propagateSize()
	return a
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

// tabColumn finds a screen column that hit-tests to the given tab index.
func tabColumn(a App, target int) int {
	for x := 0; x < a.width; x++ {
		if idx, ok := a.tabs.TabAt(x); ok && idx == target {
			return x
		}
	}
	return -1
}

func TestHandleMouse_TabClickSwitchesView(t *testing.T) {
	a := sizedApp(t)
	lay := a.layout()
	x := tabColumn(a, int(ViewTriage))
	if x < 0 {
		t.Fatal("could not locate Triage tab column")
	}
	model, _ := a.Update(leftClick(x, lay.tabsRow))
	if got := model.(App).activeView; got != ViewTriage {
		t.Errorf("after tab click activeView = %v, want Triage", got)
	}
}

func TestHandleMouse_MotionIgnored(t *testing.T) {
	a := sizedApp(t)
	before := a.activeView
	motion := tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: 5, Y: a.layout().tabsRow}
	model, cmd := a.Update(motion)
	if cmd != nil {
		t.Error("motion event should produce no command")
	}
	if got := model.(App).activeView; got != before {
		t.Errorf("motion changed view to %v", got)
	}
}

func TestHandleMouse_OverlayAbsorbsTabClick(t *testing.T) {
	a := sizedApp(t)
	a.help.visible = true // open an overlay
	x := tabColumn(a, int(ViewReviews))
	model, _ := a.Update(leftClick(x, a.layout().tabsRow))
	if got := model.(App).activeView; got != ViewDashboard {
		t.Errorf("overlay should absorb click; activeView = %v, want Dashboard", got)
	}
}

func TestHandleMouse_WheelMovesListSelection(t *testing.T) {
	a := loadSpecs(t, sizedApp(t), "SPEC-001", "SPEC-002", "SPEC-003")
	if a.specs.cursor != 0 {
		t.Fatalf("precondition: cursor = %d, want 0", a.specs.cursor)
	}
	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 5, Y: 10}
	model, _ := a.Update(wheel)
	if got := model.(App).specs.cursor; got != 1 {
		t.Errorf("after wheel down cursor = %d, want 1", got)
	}
}

func TestHandleMouse_ContentClickSelectsThenActivates(t *testing.T) {
	a := loadSpecs(t, sizedApp(t), "SPEC-001", "SPEC-002", "SPEC-003")
	lay := a.layout()
	// Row index 2 sits at contentTop + specListHeaderRows + 2.
	y := lay.contentTop + specListHeaderRows + 2

	// First click selects (cursor moves from 0 to 2), detail stays closed.
	model, _ := a.Update(leftClick(5, y))
	a = model.(App)
	if a.specs.cursor != 2 {
		t.Fatalf("first click: cursor = %d, want 2", a.specs.cursor)
	}
	if a.showDetail {
		t.Fatal("first click should not open detail")
	}

	// Second click on the same row activates → opens detail.
	model, cmd := a.Update(leftClick(5, y))
	a = model.(App)
	if !a.showDetail {
		t.Error("second click on selected row should open detail")
	}
	if cmd == nil {
		t.Error("activation should return a command")
	}
}

func TestHandleMouse_ClickBelowRowsMisses(t *testing.T) {
	a := loadSpecs(t, sizedApp(t), "SPEC-001", "SPEC-002")
	lay := a.layout()
	// Far below the two rows but still inside the content band.
	y := lay.contentTop + specListHeaderRows + 20
	model, _ := a.Update(leftClick(5, y))
	a = model.(App)
	if a.specs.cursor != 0 {
		t.Errorf("click below last row moved cursor to %d, want 0", a.specs.cursor)
	}
	if a.showDetail {
		t.Error("click on empty space should not open detail")
	}
}

func TestHandleMouse_ReaderSidebarClickJumpsSection(t *testing.T) {
	a := sizedApp(t)
	model, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = model.(App)

	// Open a detail view in reader mode with several sections.
	a.showDetail = true
	a.detail = newSpecDetail(a.rc, "SPEC-001", a.styles, a.keys, a.theme)
	a.detail.loading = false
	a.detail.setSize(a.width, a.contentHeight())
	a.detail.readerMode = true
	a.detail.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "T"}
	a.detail.sections = []markdown.Section{
		{Slug: "problem", Heading: "## 1. Problem", Level: 2, Content: "Problem text."},
		{Slug: "goals", Heading: "## 2. Goals", Level: 2, Content: "Goals text."},
		{Slug: "solution", Heading: "## 3. Solution", Level: 2, Content: "Solution text."},
	}

	lay := a.layout()
	// Sidebar section index 2 ("solution") sits at content row 4
	// (header, blank, sec0, sec1, sec2).
	y := lay.contentTop + 4
	model, cmd := a.Update(leftClick(3, y))
	a = model.(App)
	if a.detail.sectionIdx != 2 {
		t.Errorf("after sidebar click sectionIdx = %d, want 2", a.detail.sectionIdx)
	}
	if cmd == nil {
		t.Error("jumping to a section should return a render command")
	}
}
