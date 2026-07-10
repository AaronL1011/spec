package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/thread"
)

func (m specDetailModel) helpContext() string {
	base := "Detail: " + m.specID
	if !m.readerMode {
		return base
	}
	switch {
	case m.pickMode:
		return base + " · pick"
	case m.input.active():
		return base + " · compose"
	case m.paneFocused:
		return base + " · threads"
	default:
		return base + " · reader"
	}
}

// contentX removes the wide reader sidebar from a screen-local x coordinate.
func (m specDetailModel) contentX(x int) int {
	if m.width >= readerSidebarMinWidth {
		return x - readerSidebarWidth - 1
	}
	return x
}

func (m specDetailModel) clickReader(x, y int) (specDetailModel, tea.Cmd, bool) {
	if !m.readerMode {
		return m, nil, false
	}
	if idx, ok := m.sectionAtClick(x, y); ok {
		nm, cmd := m.withSection(idx)
		return nm, cmd, true
	}
	if m.contentX(x) < 0 {
		return m, nil, false
	}
	proseHeight := m.readerViewport.Height()
	if y < proseHeight {
		line := m.readerViewport.YOffset() + y
		if m.pickMode {
			if pick, ok := m.anchors.nearestPickLine(line); ok {
				if pick == m.pickLine {
					nm, cmd := m.updatePickMode(tea.KeyPressMsg{Code: tea.KeyEnter})
					return nm, cmd, true
				}
				m.pickLine = pick
				return m, nil, true
			}
		}
		if ids := m.threadIDsAtLine(line); len(ids) > 0 {
			next := ids[0]
			for i, id := range ids {
				if id == m.selectedThreadID {
					next = ids[(i+1)%len(ids)]
					break
				}
			}
			m.selectedThreadID = next
			m.paneFocused = true
			m.rebuildAnchors()
			if t, ok := m.threadByID(next); ok {
				return m, m.markSeen(t), true
			}
		}
		return m, nil, false
	}

	if id, ok := m.threadIDAtPaneRow(y - proseHeight); ok {
		m.selectedThreadID = id
		m.paneFocused = true
		m.threadScroll = 0
		m.rebuildAnchors()
		if t, found := m.threadByID(id); found {
			return m, m.markSeen(t), true
		}
	}
	return m, nil, false
}

func (m specDetailModel) threadIDsAtLine(line int) []string {
	var ids []string
	for _, t := range m.threadsForSection(m.currentSectionSlug()) {
		if anchored, ok := m.anchors.renderedLineFor(t.ID); ok && anchored == line {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (m specDetailModel) threadIDAtPaneRow(row int) (string, bool) {
	if row < 2 {
		return "", false
	}
	visual := 2 - m.threadScroll
	for _, t := range m.threadsForSection(m.currentSectionSlug()) {
		lineCount := len(flattenLines(m.threadBodyLines([]thread.Thread{t}, m.paneContentWidth())))
		if row >= visual && row < visual+lineCount {
			return t.ID, true
		}
		visual += lineCount
	}
	return "", false
}

// wheelScrollAt routes by pointer location rather than keyboard focus.
func (m *specDetailModel) wheelScrollAt(y, delta int) {
	if m.readerMode && y >= m.readerViewport.Height() && m.paneHeight() > 0 {
		m.threadScroll = clampCursor(m.threadScroll+delta, m.maxThreadScroll()+1)
		return
	}
	m.wheelScroll(delta)
}

func compactHint(width int, parts ...string) string {
	for len(parts) > 1 && len([]rune(strings.Join(parts, " · ")))+1 > width {
		parts = parts[:len(parts)-1]
	}
	return " " + strings.Join(parts, " · ")
}
