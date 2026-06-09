package components

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// paddedTabStyles mirrors the real app, whose Active/Inactive/Bar styles carry
// horizontal padding. The padding shifts column geometry, so TabAt must honour
// it — an unpadded style would hide that offset bug.
func paddedTabStyles() TabStripStyles {
	return TabStripStyles{
		Active:    lipgloss.NewStyle().Padding(0, 1),
		Inactive:  lipgloss.NewStyle().Padding(0, 1),
		Bar:       lipgloss.NewStyle().Padding(0, 1),
		Separator: lipgloss.NewStyle(),
	}
}

func testTabs() TabStrip {
	tabs := []Tab{
		{Label: "Dashboard", Shortcut: "1"},
		{Label: "Pipeline", Shortcut: "2"},
		{Label: "Specs", Shortcut: "3"},
	}
	return NewTabStrip(tabs, paddedTabStyles())
}

// TestTabAt_RoundTripsView checks that the column reported for the first cell
// of each tab maps back to that tab — i.e. TabAt agrees with the geometry the
// View() segments produce, including the bar's left padding.
func TestTabAt_FullWidth(t *testing.T) {
	ts := testTabs()
	ts.SetWidth(120) // wide → full labels, " │ " separators

	// Walk the strip and assert every tab is hit at least once and that the
	// reported index is monotonic across the strip.
	cells, sep := ts.segments()
	col := ts.styles.Bar.GetPaddingLeft()
	sepW := lipgloss.Width(sep)
	for want, cell := range cells {
		w := lipgloss.Width(cell)
		// First and last column of this cell must resolve to `want`.
		for _, x := range []int{col, col + w - 1} {
			got, ok := ts.TabAt(x)
			if !ok || got != want {
				t.Errorf("TabAt(%d) = (%d,%v), want (%d,true)", x, got, ok, want)
			}
		}
		// A column inside the separator gap must miss.
		if want < len(cells)-1 && sepW > 0 {
			if _, ok := ts.TabAt(col + w); ok {
				t.Errorf("TabAt(%d) on separator should miss", col+w)
			}
		}
		col += w + sepW
	}
}

func TestTabAt_Compact(t *testing.T) {
	ts := testTabs()
	ts.SetWidth(40) // below compactThreshold → shortcut-only labels

	// Shortcut "1" tab starts at the bar's left padding.
	x := ts.styles.Bar.GetPaddingLeft()
	got, ok := ts.TabAt(x)
	if !ok || got != 0 {
		t.Errorf("compact TabAt(%d) = (%d,%v), want (0,true)", x, got, ok)
	}
}

func TestTabAt_OutOfRange(t *testing.T) {
	ts := testTabs()
	ts.SetWidth(120)
	if _, ok := ts.TabAt(-1); ok {
		t.Error("TabAt(-1) should miss")
	}
	if _, ok := ts.TabAt(10_000); ok {
		t.Error("TabAt(far right) should miss")
	}
	// The bar's left padding column is not part of any tab cell.
	if pad := ts.styles.Bar.GetPaddingLeft(); pad > 0 {
		if _, ok := ts.TabAt(0); ok {
			t.Error("TabAt(0) on bar padding should miss")
		}
	}
}
