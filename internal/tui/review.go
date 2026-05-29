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
)

// reviewDataMsg carries loaded PR review data.
type reviewDataMsg struct {
	Reviews []reviewItem
	Err     error
}

type reviewItem struct {
	Number    int
	Title     string
	Repo      string
	Author    string
	URL       string
	CIStatus  string
	CreatedAt time.Time
}

// reviewModel shows pending PR reviews.
type reviewModel struct {
	rc  *config.ResolvedConfig
	reg *adapter.Registry

	items   []reviewItem
	loading bool
	loaded  bool // true once at least one fetch has succeeded
	err     error
	cursor  int

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newReview(rc *config.ResolvedConfig, reg *adapter.Registry, styles Styles, keys KeyMap) reviewModel {
	return reviewModel{
		rc:      rc,
		reg:     reg,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m reviewModel) init() tea.Cmd {
	return m.fetchData()
}

func (m reviewModel) update(msg tea.Msg) (reviewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case reviewDataMsg:
		m.loading = false
		if msg.Err != nil {
			// Keep cached data after the first successful load; degrade gracefully.
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.items = msg.Reviews
		m.err = nil
		m.loaded = true
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

func (m reviewModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading reviews…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}

	var b strings.Builder

	b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  %d reviews requested", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(m.styles.Success.Render(Indent(1) + IconToastOK + " No pending reviews"))
		b.WriteString("\n")
		return b.String()
	}

	contentWidth := ContentWidth(m.width)

	visibleRows := m.height - 5
	if visibleRows < 3 {
		visibleRows = 3
	}
	start, end := scrollWindow(m.cursor, len(m.items), visibleRows)

	for i := start; i < end; i++ {
		item := m.items[i]
		b.WriteString(m.renderReviewRow(item, i == m.cursor, contentWidth))
		b.WriteString("\n")
	}

	return b.String()
}

func (m reviewModel) renderReviewRow(item reviewItem, selected bool, width int) string {
	ci := ciIcon(item.CIStatus)
	prLabel := fmt.Sprintf("PR #%d", item.Number)

	compact := width < 70

	var line string
	if compact {
		titleMax := width - len(prLabel) - 6
		if titleMax < 10 {
			titleMax = 10
		}
		line = fmt.Sprintf("%s%s %-10s %s", Indent(1), ci, prLabel, truncate(item.Title, titleMax))
	} else {
		titleMax := width - 42
		if titleMax < 10 {
			titleMax = 10
		}
		ago := timeAgo(item.CreatedAt)
		line = fmt.Sprintf("%s%s %-10s %-*s  %-15s %s",
			Indent(1),
			ci,
			prLabel,
			titleMax, truncate(item.Title, titleMax),
			truncate(item.Repo, 15),
			ago,
		)
	}

	if selected {
		return m.styles.RowSelected.Render(line)
	}
	return m.styles.RowNormal.Render(line)
}

func ciIcon(status string) string {
	return CIIconFor(status)
}

func (m reviewModel) selectedURL() string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].URL
	}
	return ""
}

func (m reviewModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m *reviewModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m reviewModel) fetchData() tea.Cmd {
	reg := m.reg
	rc := m.rc
	return func() tea.Msg {
		if reg == nil {
			return reviewDataMsg{}
		}
		prs, err := reg.Repo().RequestedReviews(context.Background(), rc.UserHandle())
		if err != nil {
			return reviewDataMsg{Err: err}
		}
		items := make([]reviewItem, len(prs))
		for i, pr := range prs {
			items[i] = reviewItem{
				Number:    pr.Number,
				Title:     pr.Title,
				Repo:      pr.Repo,
				Author:    pr.Author,
				URL:       pr.URL,
				CIStatus:  pr.CIStatus,
				CreatedAt: pr.CreatedAt,
			}
		}
		return reviewDataMsg{Reviews: items}
	}
}
