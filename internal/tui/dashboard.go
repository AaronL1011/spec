package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	data    *dashboard.DashboardData
	loading bool
	err     error
	cursor  int
	items   []dashboardRow
	width   int
	height  int
	styles  Styles
	keys    KeyMap
}

type dashboardRow struct {
	section  string
	icon     string
	specID   string
	title    string
	detail   string
	urgency  string
	sortRank int // lower = higher priority within section
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
			m.err = msg.Err
			return m, nil
		}
		m.data = msg.Data
		m.err = nil
		m.items = m.buildRows()
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		return m, nil

	case tea.KeyMsg:
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

	var b strings.Builder

	if len(m.items) == 0 {
		b.WriteString("\n")
		b.WriteString(m.styles.Success.Render("  ✓ All clear — nothing needs your attention"))
		b.WriteString("\n")
		return b.String()
	}

	contentWidth := m.width - 4
	if contentWidth < 30 {
		contentWidth = 30
	}

	currentSection := ""
	for i, row := range m.items {
		if row.section != currentSection {
			currentSection = row.section
			count := m.sectionCount(currentSection)
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.sectionHeader(currentSection, count, contentWidth))
			b.WriteString("\n")
		}

		b.WriteString(m.renderRow(row, i == m.cursor, contentWidth))
		b.WriteString("\n")
	}

	return b.String()
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
		data, err := dashboard.Aggregate(context.Background(), rc, reg, role)
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
			icon:     "🚫",
			specID:   item.SpecID,
			title:    item.Title,
			detail:   item.Detail,
			urgency:  "critical",
			sortRank: 0,
		})
	}

	do := make([]dashboardRow, 0, len(m.data.Do))
	for _, item := range m.data.Do {
		icon := "⚡"
		if item.Urgency == "stale" {
			icon = "⏰"
		}
		do = append(do, dashboardRow{
			section:  "DO",
			icon:     icon,
			specID:   item.SpecID,
			title:    item.Title,
			detail:   item.Stage,
			urgency:  item.Urgency,
			sortRank: urgencyRank(item.Urgency),
		})
	}
	sortRowsByUrgency(do)

	review := make([]dashboardRow, 0, len(m.data.Review))
	for _, item := range m.data.Review {
		review = append(review, dashboardRow{
			section:  "REVIEW",
			icon:     "📋",
			specID:   item.SpecID,
			title:    item.Title,
			detail:   item.Detail,
			urgency:  item.Urgency,
			sortRank: urgencyRank(item.Urgency),
		})
	}
	sortRowsByUrgency(review)

	incoming := make([]dashboardRow, 0, len(m.data.Incoming))
	for _, item := range m.data.Incoming {
		icon := "📨"
		if item.Urgency == "critical" {
			icon = "🔴"
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

	renderedLabel := m.styles.SectionTitle.Render(label)
	renderedCount := m.styles.Subtitle.Render(countStr)

	used := lipgloss.Width(renderedLabel) + lipgloss.Width(renderedCount)
	lineLen := width - used
	if lineLen < 2 {
		lineLen = 2
	}
	line := strings.Repeat("─", lineLen)
	return renderedLabel + m.styles.Separator.Render(line) + renderedCount
}

func (m dashboardModel) renderRow(row dashboardRow, selected bool, width int) string {
	compact := width < 60

	var line string
	if compact {
		// Narrow: icon + id + truncated title, detail on same line if room.
		idStr := row.specID
		titleMax := width - len(idStr) - 6 // icon + spaces
		if titleMax < 5 {
			titleMax = 5
		}
		title := truncate(row.title, titleMax)
		line = fmt.Sprintf("  %s %s %s", row.icon, idStr, title)
	} else {
		// Wide: columnar layout — icon | id (fixed) | title (flex) | detail (right).
		idStr := fmt.Sprintf("%-11s", row.specID)
		detailLen := len(row.detail)
		titleMax := width - 17 - detailLen // 2 indent + icon(2) + space + 11 id + 2 gap
		if titleMax < 10 {
			titleMax = 10
		}
		title := truncate(row.title, titleMax)
		title = fmt.Sprintf("%-*s", titleMax, title)

		if detailLen > 0 {
			line = fmt.Sprintf("  %s %s %s  %s", row.icon, idStr, title, row.detail)
		} else {
			line = fmt.Sprintf("  %s %s %s", row.icon, idStr, title)
		}
	}

	// Apply urgency-aware styling.
	switch {
	case selected:
		return m.styles.RowSelected.Width(width).Render(line)
	case row.urgency == "critical":
		return m.styles.Error.Render(line)
	case row.urgency == "stale":
		return m.styles.Warning.Render(line)
	default:
		return m.styles.RowNormal.Render(line)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
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
