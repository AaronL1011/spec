package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/urgency"
)

// securityDataMsg carries loaded vulnerability-alert data.
type securityDataMsg struct {
	Alerts []securityItem
	Err    error
}

type securityItem struct {
	Number    int
	Title     string
	Severity  adapter.Severity
	Package   string
	Repo      string
	URL       string
	CreatedAt time.Time
}

// securityModel shows open dependency-vulnerability alerts with SLA deadlines.
type securityModel struct {
	rc  *config.ResolvedConfig
	reg *adapter.Registry

	items   []securityItem
	loading bool
	loaded  bool // true once at least one fetch has succeeded
	err     error
	cursor  int

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newSecurity(rc *config.ResolvedConfig, reg *adapter.Registry, styles Styles, keys KeyMap) securityModel {
	return securityModel{
		rc:      rc,
		reg:     reg,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m securityModel) init() tea.Cmd {
	return m.fetchData()
}

func (m securityModel) update(msg tea.Msg) (securityModel, tea.Cmd) {
	switch msg := msg.(type) {
	case securityDataMsg:
		m.loading = false
		if msg.Err != nil {
			// Keep cached data after the first successful load; degrade gracefully.
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.items = msg.Alerts
		m.sortByDeadline()
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

// sortByDeadline orders alerts soonest-deadline-first — the question a security
// queue answers is "what breaches next". Alerts with no SLA window (unknown
// severity) sort last.
func (m *securityModel) sortByDeadline() {
	now := time.Now()
	rank := func(it securityItem) time.Duration {
		sla, ok := m.slaFor(it.Severity)
		if !ok {
			return 1<<62 - 1 // effectively last
		}
		return it.CreatedAt.Add(sla).Sub(now)
	}
	sort.SliceStable(m.items, func(i, j int) bool {
		return rank(m.items[i]) < rank(m.items[j])
	})
}

func (m securityModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading security alerts…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}

	var b strings.Builder

	b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  %d open vulnerabilities", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(m.styles.Success.Render(Indent(1) + IconToastOK + " No open vulnerabilities"))
		b.WriteString("\n")
		return b.String()
	}

	contentWidth := ContentWidth(m.width)
	start, end := scrollWindow(m.cursor, len(m.items), m.visibleRows())

	now := time.Now()
	for i := start; i < end; i++ {
		b.WriteString(m.renderSecurityRow(m.items[i], i == m.cursor, contentWidth, now))
		b.WriteString("\n")
	}

	return b.String()
}

// visibleRows is how many alert rows fit on screen below the header rows.
func (m securityModel) visibleRows() int {
	v := m.height - 5
	if v < 3 {
		v = 3
	}
	return v
}

// clickRow maps a content-local row y to an alert and selects it.
func (m *securityModel) clickRow(y int) clickResult {
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

// wheelRows moves the selection by delta rows (negative = up).
func (m *securityModel) wheelRows(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.items))
}

func (m securityModel) renderSecurityRow(item securityItem, selected bool, width int, now time.Time) string {
	sev := severityLabel(item.Severity)
	compact := width < 70

	var line string
	if compact {
		titleMax := width - 12
		if titleMax < 10 {
			titleMax = 10
		}
		line = fmt.Sprintf("%s%-5s %s", Indent(1), sev, truncate(item.Title, titleMax))
	} else {
		// indent + severity + title + package + repo + time-to-deadline.
		titleMax := width - 62
		if titleMax < 10 {
			titleMax = 10
		}
		line = fmt.Sprintf("%s%-5s %-*s  %-22s %-15s %s",
			Indent(1),
			sev,
			titleMax, truncate(item.Title, titleMax),
			truncate(item.Package, 22),
			truncate(item.Repo, 15),
			m.deadlineLabel(item, now),
		)
	}

	// Deadline-based gradient: cool when the deadline is far, red as it nears,
	// hottest once overdue. Mirrors the reviews/dashboard ramp but keyed to the
	// per-severity SLA rather than a staleness window.
	frac := m.deadlineFraction(item, now)
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

// slaFor resolves the SLA window for a severity from team policy.
func (m securityModel) slaFor(sev adapter.Severity) (time.Duration, bool) {
	if m.rc == nil || m.rc.Team == nil {
		return 0, false
	}
	return m.rc.Team.Security.SLAFor(string(sev))
}

// deadlineFraction returns the eased 0..1 intensity for an alert from its age
// against the severity's SLA window. Returns 0 when there is no window (unknown
// severity) or no detection time, so colouring stays opt-in and safe.
func (m securityModel) deadlineFraction(item securityItem, now time.Time) float64 {
	sla, ok := m.slaFor(item.Severity)
	if !ok || sla <= 0 || item.CreatedAt.IsZero() {
		return 0
	}
	curve := m.rc.Team.Dashboard.EasingCurve()
	return urgency.Value(now.Sub(item.CreatedAt), sla, curve)
}

// deadlineLabel renders the time remaining until an alert breaches its SLA, or
// "overdue" once past it. Alerts with no SLA window read "—".
func (m securityModel) deadlineLabel(item securityItem, now time.Time) string {
	sla, ok := m.slaFor(item.Severity)
	if !ok || item.CreatedAt.IsZero() {
		return "—"
	}
	remaining := item.CreatedAt.Add(sla).Sub(now)
	if remaining <= 0 {
		return "overdue"
	}
	return durationShort(remaining) + " left"
}

func (m securityModel) selectedURL() string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].URL
	}
	return ""
}

func (m securityModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m *securityModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m securityModel) fetchData() tea.Cmd {
	reg := m.reg
	return func() tea.Msg {
		if reg == nil || reg.Security() == nil {
			return securityDataMsg{}
		}
		alerts, err := reg.Security().Alerts(context.Background())
		if err != nil {
			return securityDataMsg{Err: err}
		}
		items := make([]securityItem, 0, len(alerts))
		for _, a := range alerts {
			items = append(items, securityItem{
				Number:    a.Number,
				Title:     a.Title,
				Severity:  a.Severity,
				Package:   a.Package,
				Repo:      a.Repo,
				URL:       a.URL,
				CreatedAt: a.CreatedAt,
			})
		}
		return securityDataMsg{Alerts: items}
	}
}

// severityLabel is the short, fixed-width badge for a severity.
func severityLabel(s adapter.Severity) string {
	switch s {
	case adapter.SeverityCritical:
		return "CRIT"
	case adapter.SeverityHigh:
		return "HIGH"
	case adapter.SeverityMedium:
		return "MED"
	case adapter.SeverityLow:
		return "LOW"
	default:
		return "?"
	}
}

// durationShort formats a positive duration compactly (e.g. "3d", "5h", "20m").
func durationShort(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return "<1m"
	}
}

// securityPRSignatures resolves the configured provider's fix-PR signatures
// (bot authors + branch prefixes) from team config. The canonical logic lives
// in the adapter package so the Reviews tab and dashboard share it.
func securityPRSignatures(rc *config.ResolvedConfig) (authors, branchPrefixes []string) {
	if rc == nil || rc.Team == nil {
		return nil, nil
	}
	return adapter.SecurityPRSignatures(rc.Team.Integrations.Security)
}

// isSecurityPR reports whether a PR is one of the security provider's fix PRs.
func isSecurityPR(pr adapter.PullRequest, authors, branchPrefixes []string) bool {
	return adapter.IsSecurityPR(pr, authors, branchPrefixes)
}
