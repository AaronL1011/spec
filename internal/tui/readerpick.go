package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Anchor pick mode (discussion-03 §2.3a): explicit line selection for quoted
// asks. The reader has no text-selection primitive, and an implicit cursor
// (anchor to whatever sits at the top of the viewport) attaches silently
// wrong anchors — worse than a section-level one. Pick mode makes the anchor
// explicit: A shows a line cursor, enter confirms, esc falls back to a plain
// section-level ask.

// enterPickMode starts the anchor picker at the top visible line.
func (m specDetailModel) enterPickMode() specDetailModel {
	m.pickMode = true
	m.pickLine = m.readerViewport.YOffset()
	m.paneFocused = false
	return m
}

// updatePickMode handles keys while the picker is active. It absorbs every
// key (the overlay pattern) so reader hotkeys cannot fire mid-pick.
func (m specDetailModel) updatePickMode(msg tea.KeyPressMsg) (specDetailModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.movePick(-1)
	case key.Matches(msg, m.keys.Down):
		m.movePick(1)
	case msg.Code == tea.KeyPgUp:
		m.movePick(-max(m.readerViewport.Height(), 1))
	case msg.Code == tea.KeyPgDown:
		m.movePick(max(m.readerViewport.Height(), 1))
	case msg.Code == tea.KeyEnter:
		m.pickMode = false
		quote, prefix, ok := m.anchors.sourceBlockAt(m.pickLine)
		if !ok {
			// No matchable text under the cursor (chrome, rule, blank) —
			// fall back to a section-level ask rather than failing the motion.
			return m.openAskInput("", ""), nil
		}
		return m.openAskInput(quote, prefix), nil
	case msg.Code == tea.KeyEsc:
		// Fall back to a section-level ask (§2.3a).
		m.pickMode = false
		return m.openAskInput("", ""), nil
	}
	return m, nil
}

// movePick moves the pick cursor and keeps it visible in the viewport.
func (m *specDetailModel) movePick(delta int) {
	total := m.readerViewport.TotalLineCount()
	m.pickLine = clampInt(m.pickLine+delta, 0, max(total-1, 0))
	top := m.readerViewport.YOffset()
	h := max(m.readerViewport.Height(), 1)
	switch {
	case m.pickLine < top:
		m.readerViewport.SetYOffset(m.pickLine)
	case m.pickLine >= top+h:
		m.readerViewport.SetYOffset(m.pickLine - h + 1)
	}
}

// openAskInput opens the ask prompt anchored to the current section, carrying
// an optional quote anchor from the picker.
func (m specDetailModel) openAskInput(quote, prefix string) specDetailModel {
	m.paneVisible = true // an action is never silently lost behind a hidden pane
	m.input = threadInput{
		kind:        "ask",
		section:     m.currentSectionSlug(),
		quote:       quote,
		quotePrefix: prefix,
		area:        newThreadArea(m.theme),
	}
	m.sizeInputArea()
	return m
}

// ── Gutter overlay ──────────────────────────────────────────────────────────

// readerBodyView returns the viewport view with the gutter overlay applied:
// thread markers beside anchored lines, and the pick-mode cursor. This is
// view-time composition over cached rendered prose — the render cache itself
// is never mutated for anchor concerns (see anchorMap).
func (m specDetailModel) readerBodyView() string {
	view := m.readerViewport.View()
	if len(m.anchors.lines) == 0 && !m.pickMode {
		return view
	}
	lines := splitLines(view)
	top := m.readerViewport.YOffset()
	for i := range lines {
		abs := top + i
		if m.pickMode && abs == m.pickLine {
			lines[i] = overlayGutter(lines[i], m.styles.Accent.Bold(true).Render(IconCursor+" "))
			continue
		}
		if n := m.anchors.countAt(abs); n > 0 {
			lines[i] = overlayGutter(lines[i], m.styles.Accent.Render(gutterBadge(n)))
		}
	}
	return strings.Join(lines, "\n")
}

// gutterBadge renders the two-cell gutter marker for n co-anchored threads.
func gutterBadge(n int) string {
	switch {
	case n <= 1:
		return IconGutter + " "
	case n < 10:
		return IconGutter + strconv.Itoa(n)
	default:
		return IconGutter + "+"
	}
}

// overlayGutter replaces a line's two-cell indent with a marker without
// shifting content width. Lines without the standard indent (blank rows) are
// prefixed instead — their width is not layout-bearing.
func overlayGutter(line, marker string) string {
	if indent := Indent(1); strings.HasPrefix(line, indent) {
		return marker + line[len(indent):]
	}
	return marker + line
}
