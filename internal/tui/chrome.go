package tui

// This file is the single source of truth for the main screen's vertical
// geometry. The renderer (App.View) and the mouse hit-tester (App.handleMouse)
// both derive their row math from chromeLayout, so the two can never drift:
// a click on the tab strip lands on the same row the tab strip was drawn.
//
// Screen bands, top to bottom:
//
//	rows [0, headerHeight)                      header
//	row   headerHeight                          tab strip
//	rows [contentTop, contentTop+contentHeight) active view / overlay
//	row   statusRow                             status bar
//
// The total always equals a.height: headerHeight + 1 (tabs) + contentHeight +
// 1 (status) == height.

// chromeLayout describes where each fixed band of the main screen sits. All
// fields are 0-based screen rows except contentHeight, which is a count.
type chromeLayout struct {
	headerHeight  int
	tabsRow       int
	contentTop    int
	contentHeight int
	statusRow     int
}

// region classifies a screen row into the band that owns it.
type region int

const (
	regionNone region = iota
	regionHeader
	regionTabs
	regionContent
	regionStatus
)

// layout derives the current screen geometry from the live header height and
// terminal size. It mirrors the arithmetic in View() and contentHeight();
// both must stay defined here so mouse and render share one definition.
func (a App) layout() chromeLayout {
	headerHeight := a.header.Height()
	contentHeight := a.height - headerHeight - 2 // tabs + status bar
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentTop := headerHeight + 1 // header rows + tab strip
	return chromeLayout{
		headerHeight:  headerHeight,
		tabsRow:       headerHeight,
		contentTop:    contentTop,
		contentHeight: contentHeight,
		statusRow:     contentTop + contentHeight,
	}
}

// regionAt returns the band that owns screen row y.
func (c chromeLayout) regionAt(y int) region {
	switch {
	case y < 0:
		return regionNone
	case y < c.headerHeight:
		return regionHeader
	case y == c.tabsRow:
		return regionTabs
	case y >= c.contentTop && y < c.contentTop+c.contentHeight:
		return regionContent
	case y == c.statusRow:
		return regionStatus
	default:
		return regionNone
	}
}

// contentRow maps a screen row y to a 0-based row within the content band.
// It returns ok=false when y is outside the content band.
func (c chromeLayout) contentRow(y int) (row int, ok bool) {
	if y < c.contentTop || y >= c.contentTop+c.contentHeight {
		return 0, false
	}
	return y - c.contentTop, true
}

// clickResult reports what a content view did with a click on one of its rows.
type clickResult int

const (
	// clickMissed means the row was empty or out of range; nothing changed.
	clickMissed clickResult = iota
	// clickSelected means selection moved to a newly clicked row.
	clickSelected
	// clickActivated means the already-selected row was clicked again, which
	// the App promotes to the same action as pressing Enter.
	clickActivated
)

// mouseClickable is implemented by content views whose rows can be selected
// and activated with the mouse. The y argument is 0-based from the top of the
// content band (see chromeLayout.contentRow). Implementations own the mapping
// from y to their internal cursor because only they know their header rows and
// scroll window.
type mouseClickable interface {
	clickRow(y int) clickResult
	// wheelRows moves the selection/scroll by delta rows; negative is up.
	wheelRows(delta int)
}
