package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// Tab represents a single tab in the tab strip.
type Tab struct {
	Label    string
	Shortcut string
}

// TabStrip renders a horizontal tab bar.
type TabStrip struct {
	tabs   []Tab
	active int
	width  int
	styles TabStripStyles
}

// TabStripStyles holds the styles for the tab strip.
type TabStripStyles struct {
	Active    lipgloss.Style
	Inactive  lipgloss.Style
	Bar       lipgloss.Style
	Separator lipgloss.Style
}

// NewTabStrip creates a new tab strip.
func NewTabStrip(tabs []Tab, styles TabStripStyles) TabStrip {
	return TabStrip{tabs: tabs, styles: styles}
}

// SetActive updates the active tab index.
func (t *TabStrip) SetActive(idx int) {
	if idx >= 0 && idx < len(t.tabs) {
		t.active = idx
	}
}

// SetWidth updates the tab strip width.
func (t *TabStrip) SetWidth(w int) { t.width = w }

// compactThreshold is the width below which tabs show shortcuts only.
const compactThreshold = 85

// segments computes the rendered tab cells and the separator that joins them.
// View() and TabAt() both build from this so the drawn strip and its
// hit-testing geometry are derived from one definition and cannot drift.
func (t TabStrip) segments() (cells []string, sep string) {
	compact := t.width > 0 && t.width < compactThreshold

	cells = make([]string, len(t.tabs))
	for i, tab := range t.tabs {
		label := tab.Shortcut
		if !compact {
			label = tab.Shortcut + " " + tab.Label
		}
		if i == t.active {
			cells[i] = t.styles.Active.Render(label)
		} else {
			cells[i] = t.styles.Inactive.Render(label)
		}
	}

	if compact {
		sep = t.styles.Separator.Render(" ")
	} else {
		sep = t.styles.Separator.Render(" " + glyph.VSep + " ")
	}
	return cells, sep
}

// View renders the tab strip.
func (t TabStrip) View() string {
	cells, sep := t.segments()
	strip := strings.Join(cells, sep)
	return t.styles.Bar.Width(t.width).Render(strip)
}

// TabAt maps a 0-based screen column x to a tab index. It returns ok=false
// when x falls on a separator, on the bar's padding, or outside the strip.
// The geometry mirrors View(): the Bar style's left padding shifts the first
// cell, and each cell/separator advances the cursor by its rendered width.
func (t TabStrip) TabAt(x int) (idx int, ok bool) {
	cells, sep := t.segments()
	col := t.styles.Bar.GetPaddingLeft()
	sepWidth := lipgloss.Width(sep)
	for i, cell := range cells {
		w := lipgloss.Width(cell)
		if x >= col && x < col+w {
			return i, true
		}
		col += w
		if i < len(cells)-1 {
			col += sepWidth // skip the separator gap
		}
	}
	return 0, false
}
