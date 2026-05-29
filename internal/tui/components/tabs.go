package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

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

// View renders the tab strip.
func (t TabStrip) View() string {
	compact := t.width > 0 && t.width < compactThreshold

	var rendered []string
	for i, tab := range t.tabs {
		var label string
		if compact {
			label = tab.Shortcut
		} else {
			label = tab.Shortcut + " " + tab.Label
		}

		if i == t.active {
			rendered = append(rendered, t.styles.Active.Render(label))
		} else {
			rendered = append(rendered, t.styles.Inactive.Render(label))
		}
	}

	var sep string
	if compact {
		sep = t.styles.Separator.Render(" ")
	} else {
		sep = t.styles.Separator.Render(" " + glyph.VSep + " ")
	}
	strip := strings.Join(rendered, sep)

	return t.styles.Bar.Width(t.width).Render(strip)
}
