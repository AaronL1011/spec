package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
)

// dashboardDataMsg carries refreshed dashboard data.
type dashboardDataMsg struct {
	Data *dashboard.DashboardData
	Err  error
}

// dashboardModel is the home view — DO, REVIEW, INCOMING, BLOCKED.
type dashboardModel struct {
	rc   *config.ResolvedConfig
	reg  *adapter.Registry
	role string

	data          *dashboard.DashboardData
	loading       bool
	loaded        bool // true once at least one fetch has succeeded
	err           error
	cursor        int
	items         []dashboardRow
	focusedSpecID string
	width         int
	height        int
	styles        Styles
	keys          KeyMap
}

type dashboardRow struct {
	section       string
	icon          string
	specID        string
	title         string
	detail        string
	urgency       string
	url           string  // non-empty for REVIEW rows (PR URL)
	sortRank      int     // lower = higher priority within section
	staleFraction float64 // eased time-urgency intensity (0..1); 0 = fresh / no window
}

// newDashboard creates a new dashboard view.
func newDashboard(rc *config.ResolvedConfig, reg *adapter.Registry, role string, styles Styles, keys KeyMap) dashboardModel {
	return dashboardModel{
		rc:      rc,
		reg:     reg,
		role:    role,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m dashboardModel) init() tea.Cmd {
	return m.fetchData()
}

func (m dashboardModel) update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case dashboardDataMsg:
		m.loading = false
		if msg.Err != nil {
			// Only surface an error screen before the first successful load.
			// Afterwards, keep cached data on screen and let the app degrade
			// gracefully (the failure is surfaced via a toast).
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.data = msg.Data
		m.err = nil
		m.loaded = true
		m.items = m.buildRows()
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m dashboardModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading dashboard…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}
	if m.data == nil {
		return m.styles.Muted.Render("  No data")
	}

	if len(m.items) == 0 {
		return m.styles.Success.Render(Indent(1) + IconToastOK + " All clear — nothing needs your attention")
	}

	lines := m.layoutLines(ContentWidth(m.width))
	start, end := m.scrollBounds(len(lines))

	var b strings.Builder
	for _, l := range lines[start:end] {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	return b.String()
}

// dashLine is one rendered dashboard line. itemIdx indexes m.items for
// selectable rows, or is -1 for section headers and blank separators.
type dashLine struct {
	text    string
	itemIdx int
}

// layoutLines builds the full ordered line model for the dashboard: section
// headers, blank separators, and item rows. view(), cursorLineIndex(), and
// clickRow() all derive from this so the drawn layout and the click geometry
// can never drift.
func (m dashboardModel) layoutLines(contentWidth int) []dashLine {
	var lines []dashLine
	currentSection := ""
	for i, row := range m.items {
		if row.section != currentSection {
			currentSection = row.section
			count := m.sectionCount(currentSection)
			// A blank line separates each section (and gives the first one
			// breathing room under the tab strip). It is modelled as its own
			// line so one dashLine always maps to exactly one screen row — the
			// invariant clickRow relies on. sectionHeader must therefore not
			// embed its own newline (no MarginTop styling).
			lines = append(lines, dashLine{itemIdx: -1})
			lines = append(lines, dashLine{text: m.sectionHeader(currentSection, count, contentWidth), itemIdx: -1})
		}
		lines = append(lines, dashLine{text: m.renderRow(row, i == m.cursor, contentWidth), itemIdx: i})
	}
	return lines
}

// scrollBounds returns the visible [start,end) window of the line model,
// centred on the cursor's line. Shared by view() and clickRow().
func (m dashboardModel) scrollBounds(totalLines int) (start, end int) {
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	return scrollWindowAround(m.cursorLineIndex(), totalLines, visible)
}

// cursorLineIndex returns which rendered line the cursor row maps to,
// accounting for section headers and blank separators.
func (m dashboardModel) cursorLineIndex() int {
	// Structural only (which line the cursor row sits on), so the rendered
	// width is irrelevant; pass 0 to avoid recomputing styled text.
	for i, l := range m.layoutLines(0) {
		if l.itemIdx == m.cursor {
			return i
		}
	}
	return 0
}

// clickRow maps a content-local row y to a dashboard item and selects it.
func (m *dashboardModel) clickRow(y int) clickResult {
	lines := m.layoutLines(ContentWidth(m.width))
	start, end := m.scrollBounds(len(lines))
	li := start + y
	if y < 0 || li < start || li >= end || li >= len(lines) {
		return clickMissed
	}
	idx := lines[li].itemIdx
	if idx < 0 {
		return clickMissed // section header or blank separator
	}
	if idx == m.cursor {
		return clickActivated
	}
	m.cursor = idx
	return clickSelected
}

// wheelRows moves the dashboard selection by delta rows (negative = up).
func (m *dashboardModel) wheelRows(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.items))
}

func (m *dashboardModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m dashboardModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m dashboardModel) selectedSpecID() string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].specID
	}
	return ""
}

// selectedURL returns the URL for the currently selected row, if any.
// Only REVIEW rows carry a URL.
func (m dashboardModel) selectedURL() string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].url
	}
	return ""
}

func (m dashboardModel) pendingCount() int {
	if m.data == nil {
		return 0
	}
	return len(m.data.Do) + len(m.data.Review) + len(m.data.Incoming) + len(m.data.Blocked)
}

func (m dashboardModel) fetchData() tea.Cmd {
	rc := m.rc
	reg := m.reg
	role := m.role
	return func() tea.Msg {
		ctx := context.Background()
		// Fetch remote spec changes (TTL-gated) before aggregating, so the DO/
		// Blocked/Incoming sections reflect teammates' pushes — not only the
		// PR-review section that already crossed the network. Non-fatal.
		syncErr := syncSpecsRepo(ctx, rc)
		data, err := dashboard.Aggregate(ctx, rc, reg, role)
		if err == nil {
			err = syncErr
		}
		return dashboardDataMsg{Data: data, Err: err}
	}
}

// urgencyRank maps urgency strings to sort priority (lower = first).
func urgencyRank(u string) int {
	switch u {
	case "critical":
		return 0
	case "stale":
		return 1
	default:
		return 2
	}
}

func (m dashboardModel) buildRows() []dashboardRow {
	if m.data == nil {
		return nil
	}

	// Build rows per section, sort by urgency within each, then
	// assemble in priority order: BLOCKED → DO → REVIEW → INCOMING.
	// Blocked first because blocked items are the most urgent signal
	// (something is stuck and may be blocking others). DO next because
	// those are your active responsibilities. REVIEW and INCOMING are
	// awareness items.

	blocked := make([]dashboardRow, 0, len(m.data.Blocked))
	for _, item := range m.data.Blocked {
		blocked = append(blocked, dashboardRow{
			section:  "BLOCKED",
			icon:     IconBlocked,
			specID:   item.SpecID,
			title:    item.Title,
			detail:   item.Detail,
			urgency:  "critical",
			sortRank: 0,
		})
	}

	do := make([]dashboardRow, 0, len(m.data.Do))
	for _, item := range m.data.Do {
		icon := IconActive
		if item.Urgency == "stale" {
			icon = IconStale
		}
		detail := item.Stage
		if item.Assignee != "" {
			detail += "  ·  " + item.Assignee
		}
		do = append(do, dashboardRow{
			section:       "DO",
			icon:          icon,
			specID:        item.SpecID,
			title:         item.Title,
			detail:        detail,
			urgency:       item.Urgency,
			sortRank:      urgencyRank(item.Urgency),
			staleFraction: item.StaleFraction,
		})
	}
	sortRowsByUrgency(do)

	review := make([]dashboardRow, 0, len(m.data.Review))
	for _, item := range m.data.Review {
		review = append(review, dashboardRow{
			section:       "REVIEW",
			icon:          IconReview,
			specID:        item.SpecID,
			title:         item.Title,
			detail:        item.Detail,
			urgency:       item.Urgency,
			url:           item.URL,
			sortRank:      urgencyRank(item.Urgency),
			staleFraction: item.StaleFraction,
		})
	}
	sortRowsByUrgency(review)

	incoming := make([]dashboardRow, 0, len(m.data.Incoming))
	for _, item := range m.data.Incoming {
		icon := IconIncoming
		if item.Urgency == "critical" {
			icon = IconActive
		}
		incoming = append(incoming, dashboardRow{
			section:  "INCOMING",
			icon:     icon,
			specID:   item.SpecID,
			title:    item.Title,
			detail:   item.Stage,
			urgency:  item.Urgency,
			sortRank: urgencyRank(item.Urgency),
		})
	}
	sortRowsByUrgency(incoming)

	// Assemble in priority order. Sections with no items are skipped.
	var rows []dashboardRow
	rows = append(rows, blocked...)
	rows = append(rows, do...)
	rows = append(rows, review...)
	rows = append(rows, incoming...)
	return rows
}

func sortRowsByUrgency(rows []dashboardRow) {
	slices.SortStableFunc(rows, func(a, b dashboardRow) int {
		return a.sortRank - b.sortRank
	})
}

func (m dashboardModel) sectionCount(section string) int {
	n := 0
	for _, row := range m.items {
		if row.section == section {
			n++
		}
	}
	return n
}

func (m dashboardModel) sectionHeader(section string, count, width int) string {
	label := " " + section + " "
	countStr := fmt.Sprintf(" %d ", count)

	// Accent-bold, not SectionTitle: the latter carries MarginTop(1), which
	// embeds a leading newline and would make this one rendered line occupy two
	// screen rows, desyncing clickRow's row-to-item mapping.
	renderedLabel := m.styles.Accent.Bold(true).Render(label)
	renderedCount := m.styles.Subtitle.Render(countStr)

	used := lipgloss.Width(renderedLabel) + lipgloss.Width(renderedCount)
	lineLen := width - used
	if lineLen < 2 {
		lineLen = 2
	}
	line := RuleLine(lineLen)
	return renderedLabel + m.styles.Separator.Render(line) + renderedCount
}

func (m dashboardModel) renderRow(row dashboardRow, selected bool, width int) string {
	compact := width < 60

	// Focused spec indicator.
	icon := row.icon
	if m.focusedSpecID != "" && row.specID == m.focusedSpecID {
		icon = IconFocus
	}

	var line string
	if compact {
		idStr := row.specID
		titleMax := width - len(idStr) - 6 // indent + 1-cell icon + spaces
		if titleMax < 5 {
			titleMax = 5
		}
		title := truncate(row.title, titleMax)
		line = fmt.Sprintf("%s%s %s %s", Indent(1), icon, idStr, title)
	} else {
		// Wide: columnar layout — icon | id (fixed) | title (flex) | detail (right).
		idStr := fmt.Sprintf("%-11s", row.specID)
		detailLen := len(row.detail)
		titleMax := width - 16 - detailLen // 2 indent + 1 icon + space + 11 id + 2 gap
		if titleMax < 10 {
			titleMax = 10
		}
		title := truncate(row.title, titleMax)
		title = fmt.Sprintf("%-*s", titleMax, title)

		if detailLen > 0 {
			line = fmt.Sprintf("%s%s %s %s  %s", Indent(1), icon, idStr, title, row.detail)
		} else {
			line = fmt.Sprintf("%s%s %s %s", Indent(1), icon, idStr, title)
		}
	}

	// Apply urgency-aware styling. The continuous time-urgency gradient (whole
	// row, eased) takes precedence over the legacy discrete stale/critical
	// styling; the ramp foreground is composed over the selection background so
	// urgency stays visible while a row is selected.
	switch {
	case selected:
		style := m.styles.RowSelected
		if row.staleFraction > 0 {
			style = style.Foreground(m.styles.Theme.RampColor(row.staleFraction))
		}
		return style.Render(line)
	case row.urgency == "critical":
		return m.styles.Error.Render(line)
	case row.staleFraction > 0:
		return m.styles.RowNormal.Foreground(m.styles.Theme.RampColor(row.staleFraction)).Render(line)
	case row.urgency == "stale":
		return m.styles.Warning.Render(line)
	default:
		return m.styles.RowNormal.Render(line)
	}
}

// truncate shortens s to at most maxLen runes, appending an ellipsis when
// space allows. It operates on runes (not bytes) so multi-byte UTF-8 titles
// are never split mid-character, which would emit invalid UTF-8 and corrupt
// terminal rendering and width calculations.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// timeAgo formats a duration as a human-readable string.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
