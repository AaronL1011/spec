package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// triageDataMsg carries loaded triage items.
type triageDataMsg struct {
	Items []triageItem
	Err   error
}

type triageItem struct {
	ID         string
	Title      string
	Priority   string
	Source     string
	ReportedBy string
	Created    string
}

// triageModel shows the incoming triage queue.
type triageModel struct {
	rc *config.ResolvedConfig

	items   []triageItem
	loading bool
	loaded  bool // true once at least one fetch has succeeded
	err     error
	cursor  int

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newTriage(rc *config.ResolvedConfig, styles Styles, keys KeyMap) triageModel {
	return triageModel{
		rc:      rc,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m triageModel) init() tea.Cmd {
	return m.fetchData()
}

func (m triageModel) update(msg tea.Msg) (triageModel, tea.Cmd) {
	switch msg := msg.(type) {
	case triageDataMsg:
		m.loading = false
		if msg.Err != nil {
			// Keep cached data after the first successful load; degrade gracefully.
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.items = msg.Items
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

func (m triageModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading triage…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}

	var b strings.Builder

	b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  %d items in triage", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(m.styles.Success.Render(Indent(1) + IconToastOK + " Triage queue is empty"))
		b.WriteString("\n")
		return b.String()
	}

	contentWidth := ContentWidth(m.width)

	start, end := scrollWindow(m.cursor, len(m.items), m.visibleRows())

	for i := start; i < end; i++ {
		item := m.items[i]
		b.WriteString(m.renderTriageRow(item, i == m.cursor, contentWidth))
		b.WriteString("\n")
	}

	return b.String()
}

// listHeaderRows is the number of fixed rows the triage and review lists draw
// above their first item row: a count line and a blank separator.
const listHeaderRows = 2

// visibleRows is how many item rows fit on screen below the header rows.
func (m triageModel) visibleRows() int {
	v := m.height - 5
	if v < 3 {
		v = 3
	}
	return v
}

// clickRow maps a content-local row y to a triage item and selects it.
func (m *triageModel) clickRow(y int) clickResult {
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

// wheelRows moves the triage selection by delta rows (negative = up).
func (m *triageModel) wheelRows(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.items))
}

func (m triageModel) renderTriageRow(item triageItem, selected bool, width int) string {
	icon := priorityIcon(item.Priority)

	idStr := fmt.Sprintf("%-11s", item.ID)
	titleMax := width - 20
	if titleMax < 10 {
		titleMax = 10
	}
	title := truncate(item.Title, titleMax)

	var detail string
	if item.Source != "" {
		detail = item.Source
	}
	if item.Created != "" {
		if detail != "" {
			detail += "  "
		}
		detail += item.Created
	}

	line := fmt.Sprintf("%s%s %s %-*s", Indent(1), icon, idStr, titleMax, title)
	if detail != "" && width > 70 {
		line += "  " + detail
	}

	switch {
	case selected:
		return m.styles.RowSelected.Render(line)
	case item.Priority == "critical" || item.Priority == "high":
		return m.styles.Error.Render(line)
	case item.Priority == "medium":
		return m.styles.Warning.Render(line)
	default:
		return m.styles.RowNormal.Render(line)
	}
}

func priorityIcon(p string) string {
	return PriorityIconFor(p)
}

func (m triageModel) selectedItemID() string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].ID
	}
	return ""
}

func (m triageModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m *triageModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m triageModel) fetchData() tea.Cmd {
	rc := m.rc
	return func() tea.Msg {
		items, err := loadTriageData(context.Background(), rc)
		return triageDataMsg{Items: items, Err: err}
	}
}

func loadTriageData(ctx context.Context, rc *config.ResolvedConfig) ([]triageItem, error) {
	if rc.SpecsRepoDir == "" {
		return nil, nil
	}

	// Fetch remote changes (TTL-gated) before reading, so a teammate's pushed
	// triage item appears on refresh. Non-fatal; cached files render regardless.
	syncErr := syncSpecsRepo(ctx, rc)

	triageDir := filepath.Join(rc.SpecsRepoDir, "triage")
	entries, err := os.ReadDir(triageDir)
	if err != nil {
		// No triage directory is not an error — just empty.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading triage dir: %w", err)
	}

	var items []triageItem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(triageDir, e.Name())
		meta, err := markdown.ReadTriageMeta(path)
		if err != nil {
			continue
		}
		items = append(items, triageItem{
			ID:         meta.ID,
			Title:      meta.Title,
			Priority:   meta.Priority,
			Source:     meta.Source,
			ReportedBy: meta.ReportedBy,
			Created:    meta.Created,
		})
	}
	return items, syncErr
}
