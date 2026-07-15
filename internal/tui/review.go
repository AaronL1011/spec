package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
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

	start, end := scrollWindow(m.cursor, len(m.items), m.visibleRows())

	now := time.Now()
	for i := start; i < end; i++ {
		item := m.items[i]
		b.WriteString(m.renderReviewRow(item, i == m.cursor, contentWidth, now))
		b.WriteString("\n")
	}

	return b.String()
}

// visibleRows is how many review rows fit on screen below the header rows.
func (m reviewModel) visibleRows() int {
	v := m.height - 5
	if v < 3 {
		v = 3
	}
	return v
}

// clickRow maps a content-local row y to a review item and selects it.
func (m *reviewModel) clickRow(y int) clickResult {
	row := y - listHeaderRows
	if row < 0 {
		return clickMissed
	}
	start, _ := scrollWindow(m.cursor, len(m.items), m.visibleRows())
	idx := start + row
	if idx < 0 || idx >= len(m.items) {
		return clickMissed
	}
	if idx == m.cursor {
		return clickActivated
	}
	m.cursor = idx
	return clickSelected
}

// wheelRows moves the review selection by delta rows (negative = up).
func (m *reviewModel) wheelRows(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.items))
}

func (m reviewModel) renderReviewRow(item reviewItem, selected bool, width int, now time.Time) string {
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
		// Reserve space for the fixed columns so the styled row never wraps:
		// indent + icon + PR label + repo + author + time-ago. The author column
		// sits immediately before the trailing time column, matching the spec
		// list layout (ID · TITLE · STATUS · AUTHOR · UPDATED).
		titleMax := width - 58
		if titleMax < 10 {
			titleMax = 10
		}
		ago := timeAgo(item.CreatedAt)
		line = fmt.Sprintf("%s%s %-10s %-*s  %-15s %-15s %s",
			Indent(1),
			ci,
			prLabel,
			titleMax, truncate(item.Title, titleMax),
			truncate(item.Repo, 15),
			truncate(item.Author, 15),
			ago,
		)
	}

	// Apply the time-urgency gradient, reusing the dashboard's REVIEW window and
	// easing so a PR reads at the same intensity here and on the dashboard. The
	// ramp foreground composes over the selection background so urgency stays
	// visible while a row is selected. Colouring is opt-in: staleFraction is 0
	// when no review window is configured, leaving rows at their normal colour.
	frac := m.reviewStaleFraction(item, now)
	switch {
	case selected:
		style := m.styles.RowSelected
		if frac > 0 {
			style = style.Foreground(m.styles.Theme.RampColor(frac))
		}
		return style.Render(line)
	case frac > 0:
		return m.styles.RowNormal.Foreground(m.styles.Theme.RampColor(frac)).Render(line)
	default:
		return m.styles.RowNormal.Render(line)
	}
}

// reviewStaleFraction returns the eased time-urgency intensity (0..1) for a
// review row from the PR's age (now - CreatedAt) against the team's configured
// REVIEW staleness window, reusing the same window + easing curve the
// dashboard's REVIEW section uses. Returns 0 when team config is absent or no
// window is configured, so review colouring stays strictly opt-in.
func (m reviewModel) reviewStaleFraction(item reviewItem, now time.Time) float64 {
	if m.rc == nil || m.rc.Team == nil {
		return 0
	}
	window, _ := m.rc.Team.Dashboard.ReviewWindow()
	curve := m.rc.Team.Dashboard.EasingCurve()
	return dashboard.ReviewUrgency(window, curve, item.CreatedAt, now)
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
		prs, err := reg.Repo().InvolvedPRs(context.Background(), rc.IdentityForCategory("repo"))
		if err != nil {
			return reviewDataMsg{Err: err}
		}
		// Security fix PRs (Dependabot/Renovate/Snyk/custom) live only in the
		// Security tab, so exclude them here by the provider's bot author and
		// branch prefix. No security provider configured ⇒ nothing filtered.
		authors, prefixes := securityPRSignatures(rc)
		items := make([]reviewItem, 0, len(prs))
		for _, pr := range prs {
			if isSecurityPR(pr, authors, prefixes) {
				continue
			}
			items = append(items, reviewItem{
				Number:    pr.Number,
				Title:     pr.Title,
				Repo:      pr.Repo,
				Author:    pr.Author,
				URL:       pr.URL,
				CIStatus:  pr.CIStatus,
				CreatedAt: pr.CreatedAt,
			})
		}
		return reviewDataMsg{Reviews: items}
	}
}
