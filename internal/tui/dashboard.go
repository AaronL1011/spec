package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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
	section string
	icon    string
	specID  string
	title   string
	detail  string
	urgency string
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

	currentSection := ""
	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	for i, row := range m.items {
		if row.section != currentSection {
			currentSection = row.section
			if i > 0 {
				b.WriteString("\n")
			}
			header := m.sectionHeader(currentSection, contentWidth)
			b.WriteString(header)
			b.WriteString("\n")
		}

		line := m.renderRow(row, i == m.cursor, contentWidth)
		b.WriteString(line)
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

func (m dashboardModel) buildRows() []dashboardRow {
	var rows []dashboardRow
	if m.data == nil {
		return rows
	}

	for _, item := range m.data.Do {
		icon := "⚡"
		if item.Urgency == "stale" {
			icon = "⏰"
		}
		rows = append(rows, dashboardRow{
			section: "DO",
			icon:    icon,
			specID:  item.SpecID,
			title:   item.Title,
			detail:  item.Stage,
			urgency: item.Urgency,
		})
	}

	for _, item := range m.data.Review {
		rows = append(rows, dashboardRow{
			section: "REVIEW",
			icon:    "📋",
			specID:  item.SpecID,
			title:   item.Title,
			detail:  item.Detail,
		})
	}

	for _, item := range m.data.Incoming {
		icon := "📨"
		if item.Urgency == "critical" {
			icon = "🔴"
		}
		rows = append(rows, dashboardRow{
			section: "INCOMING",
			icon:    icon,
			specID:  item.SpecID,
			title:   item.Title,
			detail:  item.Stage,
			urgency: item.Urgency,
		})
	}

	for _, item := range m.data.Blocked {
		rows = append(rows, dashboardRow{
			section: "BLOCKED",
			icon:    "🚫",
			specID:  item.SpecID,
			title:   item.Title,
			detail:  item.Detail,
		})
	}

	return rows
}

func (m dashboardModel) sectionHeader(section string, width int) string {
	label := " " + section + " "
	lineLen := width - len(label) - 1
	if lineLen < 4 {
		lineLen = 4
	}
	line := strings.Repeat("─", lineLen)
	return m.styles.SectionTitle.Render(label) + m.styles.Separator.Render(line)
}

func (m dashboardModel) renderRow(row dashboardRow, selected bool, width int) string {
	id := fmt.Sprintf("%-11s", row.specID)
	titleMax := width - 16 - len(row.detail)
	if titleMax < 10 {
		titleMax = 10
	}
	title := truncate(row.title, titleMax)
	title = fmt.Sprintf("%-*s", titleMax, title)

	line := fmt.Sprintf("  %s %s %s  %s", row.icon, id, title, row.detail)

	if selected {
		return m.styles.RowSelected.Width(width).Render(line)
	}
	return m.styles.RowNormal.Render(line)
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
