package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m specDetailModel) enterPickMode() specDetailModel {
	m.pickMode = true
	centre := m.readerViewport.YOffset() + m.readerViewport.Height()/2
	if line, ok := m.anchors.nearestPickLine(centre); ok {
		m.pickLine = line
	} else {
		m.pickLine = centre
	}
	m.paneFocused = false
	return m
}

func (m specDetailModel) updatePickMode(msg tea.KeyPressMsg) (specDetailModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.movePick(-1)
	case key.Matches(msg, m.keys.Down):
		m.movePick(1)
	case msg.Code == tea.KeyPgUp:
		m.movePick(-max(m.readerViewport.Height()/2, 1))
	case msg.Code == tea.KeyPgDown:
		m.movePick(max(m.readerViewport.Height()/2, 1))
	case msg.Text == "s":
		m.pickMode = false
		nm := m.openAskInput("", "")
		nm.syncViewportHeight()
		return nm, nil
	case msg.Code == tea.KeyEnter:
		quote, prefix, ok := m.anchors.sourceBlockAt(m.pickLine)
		if !ok {
			return m, nil
		}
		m.pickMode = false
		nm := m.openAskInput(quote, prefix)
		nm.syncViewportHeight()
		return nm, nil
	case msg.Code == tea.KeyEsc:
		m.pickMode = false
		return m, nil
	}
	return m, nil
}

func (m *specDetailModel) movePick(delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for range max(absInt(delta), 1) {
		m.pickLine = m.anchors.stepPickLine(m.pickLine, step)
	}
	top := m.readerViewport.YOffset()
	height := max(m.readerViewport.Height(), 1)
	switch {
	case m.pickLine < top:
		m.readerViewport.SetYOffset(m.pickLine)
	case m.pickLine >= top+height:
		m.readerViewport.SetYOffset(m.pickLine - height + 1)
	}
}

func (m specDetailModel) openAskInput(quote, prefix string) specDetailModel {
	m.paneVisible = true
	m.input = threadInput{
		kind: "ask", section: m.currentSectionSlug(), quote: quote,
		quotePrefix: prefix, area: newThreadArea(m.theme),
	}
	m.sizeInputArea()
	return m
}

func (m specDetailModel) readerBodyView() string {
	view := m.readerViewport.View()
	if len(m.anchors.lines) == 0 && !m.pickMode {
		return view
	}
	lines := splitLines(view)
	top := m.readerViewport.YOffset()
	for i := range lines {
		line := top + i
		if m.pickMode && line == m.pickLine {
			lines[i] = overlayGutter(lines[i], m.styles.Accent.Bold(true).Render(IconCursor+" "))
			continue
		}
		state, ok := m.anchors.stateAt(line)
		if !ok {
			continue
		}
		badge := gutterBadge(state.Count)
		switch {
		case state.Selected:
			lines[i] = m.styles.RowSelected.Render(
				overlayGutter(stripANSI(lines[i]), m.styles.Accent.Bold(true).Render(badge)))
		case state.AllResolved:
			lines[i] = overlayGutter(lines[i], m.styles.Muted.Render(badge))
		case state.Unread:
			lines[i] = overlayGutter(lines[i], m.styles.Warning.Bold(true).Render(badge))
		default:
			lines[i] = overlayGutter(lines[i], m.styles.Accent.Render(badge))
		}
	}
	return strings.Join(lines, "\n")
}

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

func overlayGutter(line, marker string) string {
	if indent := Indent(1); strings.HasPrefix(line, indent) {
		return marker + line[len(indent):]
	}
	return marker + line
}
