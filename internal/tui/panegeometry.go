package tui

const (
	paneModePeek         = "peek"
	paneModeReview       = "review"
	paneModeConversation = "conversation"
)

// readerPaneBudget is the largest row budget the thread pane may consume.
func (m specDetailModel) readerPaneBudget() int {
	if m.height <= 1 {
		return 0
	}
	var budget int
	switch m.paneMode {
	case paneModePeek:
		budget = 4
	case paneModeConversation:
		budget = max(m.height*2/3, 8)
	default:
		budget = max(m.height/2, 6)
	}
	if budget >= m.height {
		budget = m.height - 1
	}
	return max(budget, 0)
}

func (m *specDetailModel) cyclePaneMode() {
	switch m.paneMode {
	case paneModePeek:
		m.paneMode = paneModeReview
	case paneModeReview:
		m.paneMode = paneModeConversation
	default:
		m.paneMode = paneModePeek
	}
	m.syncViewportHeight()
}

// paneHeight returns the actual visible pane height. It is the single source
// used by layout, viewport sizing, hit-testing, and scroll bounds.
func (m specDetailModel) paneHeight() int {
	budget := m.readerPaneBudget()
	if budget == 0 || !m.paneActiveForCurrentSection() {
		return 0
	}
	return min(len(m.renderThreadPane(m.paneContentWidth(), budget)), budget)
}

// syncViewportHeight ensures the prose viewport owns only the rows not used by
// the bottom pane. Keeping the current offset makes pane transitions stable.
func (m *specDetailModel) syncViewportHeight() {
	height := max(m.height-m.paneHeight(), 1)
	m.readerViewport.SetHeight(height)
	if mx := m.maxScroll(); m.readerViewport.YOffset() > mx {
		m.readerViewport.SetYOffset(mx)
	}
}
