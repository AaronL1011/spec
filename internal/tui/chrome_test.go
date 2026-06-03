package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChromeLayout_RegionAt(t *testing.T) {
	// headerHeight 2, contentHeight 5 → rows:
	//   0,1   header
	//   2     tabs
	//   3..7  content
	//   8     status
	c := chromeLayout{headerHeight: 2, tabsRow: 2, contentTop: 3, contentHeight: 5, statusRow: 8}

	cases := []struct {
		y    int
		want region
	}{
		{-1, regionNone},
		{0, regionHeader},
		{1, regionHeader},
		{2, regionTabs},
		{3, regionContent},
		{7, regionContent},
		{8, regionStatus},
		{9, regionNone},
	}
	for _, tc := range cases {
		if got := c.regionAt(tc.y); got != tc.want {
			t.Errorf("regionAt(%d) = %v, want %v", tc.y, got, tc.want)
		}
	}
}

func TestChromeLayout_SingleRowHeader(t *testing.T) {
	// headerHeight 1: tabs on row 1, content starts row 2.
	c := chromeLayout{headerHeight: 1, tabsRow: 1, contentTop: 2, contentHeight: 3, statusRow: 5}
	if got := c.regionAt(0); got != regionHeader {
		t.Errorf("row 0 region = %v, want header", got)
	}
	if got := c.regionAt(1); got != regionTabs {
		t.Errorf("row 1 region = %v, want tabs", got)
	}
	if got := c.regionAt(2); got != regionContent {
		t.Errorf("row 2 region = %v, want content", got)
	}
}

func TestChromeLayout_ContentRow(t *testing.T) {
	c := chromeLayout{headerHeight: 2, tabsRow: 2, contentTop: 3, contentHeight: 5, statusRow: 8}
	cases := []struct {
		y       int
		wantRow int
		wantOK  bool
	}{
		{2, 0, false}, // tabs row, not content
		{3, 0, true},  // first content row
		{7, 4, true},  // last content row
		{8, 0, false}, // status row
	}
	for _, tc := range cases {
		row, ok := c.contentRow(tc.y)
		if ok != tc.wantOK || (ok && row != tc.wantRow) {
			t.Errorf("contentRow(%d) = (%d,%v), want (%d,%v)", tc.y, row, ok, tc.wantRow, tc.wantOK)
		}
	}
}

// TestApp_LayoutMatchesHeight verifies the bands tile the terminal exactly:
// the status row is always the last screen row, so render and hit-test agree.
func TestApp_LayoutMatchesHeight(t *testing.T) {
	// Sizes large enough to fit chrome (header + tabs + status + >=1 content).
	// Below that floor the bands intentionally overflow, matching View()'s own
	// content clamp; that degenerate case is not a real terminal.
	app := testApp()
	for _, h := range []int{10, 24, 40} {
		model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: h})
		a := model.(App)
		lay := a.layout()
		if lay.statusRow != h-1 {
			t.Errorf("height %d: statusRow = %d, want %d", h, lay.statusRow, h-1)
		}
		if lay.contentTop+lay.contentHeight != lay.statusRow {
			t.Errorf("height %d: content band [%d,%d) does not abut status row %d",
				h, lay.contentTop, lay.contentTop+lay.contentHeight, lay.statusRow)
		}
	}
}
