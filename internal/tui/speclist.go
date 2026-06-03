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

// specListDataMsg carries loaded spec metadata.
type specListDataMsg struct {
	Specs []specListItem
	Err   error
}

type specListItem struct {
	ID      string
	Title   string
	Status  string
	Author  string
	Updated string
}

// specListModel is a filterable list of all specs.
type specListModel struct {
	rc *config.ResolvedConfig

	allSpecs     []specListItem
	filtered     []specListItem
	loading      bool
	loaded       bool // true once at least one fetch has succeeded
	err          error
	cursor       int
	searchActive bool
	searchQuery  string
	archiveMode  bool // true = showing archived specs

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newSpecList(rc *config.ResolvedConfig, styles Styles, keys KeyMap) specListModel {
	return specListModel{
		rc:      rc,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m specListModel) init() tea.Cmd {
	return m.fetchData()
}

func (m specListModel) update(msg tea.Msg) (specListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case specListDataMsg:
		m.loading = false
		if msg.Err != nil {
			// Keep cached data after the first successful load; degrade gracefully.
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.allSpecs = msg.Specs
		m.err = nil
		m.loaded = true
		m.applyFilter()
		return m, nil

	case tea.KeyMsg:
		if m.searchActive {
			return m.updateSearch(msg)
		}
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keys.Back):
			// Esc when not searching clears the active filter.
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.applyFilter()
			}
		case key.Matches(msg, m.keys.Search):
			m.searchActive = true
			m.searchQuery = ""
		case key.Matches(msg, m.keys.ToggleArchive):
			// ` toggles archive list mode
			m.archiveMode = !m.archiveMode
			m.cursor = 0
			m.searchQuery = ""
			m.searchActive = false
			return m, m.fetchData()
		}
	}
	return m, nil
}

func (m specListModel) updateSearch(msg tea.KeyMsg) (specListModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		// First Esc exits search mode (keeps filter). Second clears filter.
		m.searchActive = false
	case tea.KeyBackspace:
		if m.searchQuery != "" {
			m.searchQuery = dropLastRune(m.searchQuery)
			m.applyFilter()
		}
	case tea.KeyEnter:
		m.searchActive = false
	case tea.KeySpace:
		m.searchQuery += " "
		m.applyFilter()
	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)
		m.applyFilter()
	}
	return m, nil
}

func (m specListModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading specs…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}

	var b strings.Builder

	// Search bar + hints
	label := "specs"
	if m.archiveMode {
		label = "archived specs"
	}
	switch {
	case m.searchActive:
		prompt := m.styles.Accent.Render("  / ") + m.searchQuery + m.styles.Muted.Render(IconCaret)
		b.WriteString(prompt)
	case m.searchQuery != "":
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  filter: %s  ", m.searchQuery)))
		b.WriteString(m.styles.Muted.Render("(/ to search, esc to clear)"))
	default:
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  %d %s  ", len(m.filtered), label)))
		toggle := Hint("`", "archive")
		if m.archiveMode {
			toggle = Hint("`", "specs")
		}
		b.WriteString(HintStrip(m.styles, toggle, Hint("/", "search"), Hint("?", "help")))
	}
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		switch {
		case m.searchQuery != "":
			b.WriteString(m.styles.Muted.Render("  No specs matching search"))
		case m.archiveMode:
			b.WriteString(m.styles.Muted.Render("  No archived specs"))
		default:
			b.WriteString(m.styles.Muted.Render("  No specs found"))
		}
		b.WriteString("\n")
		return b.String()
	}

	// Column header
	contentWidth := ContentWidth(m.width)
	headerLine := m.formatRow("ID", "TITLE", "STATUS", "AUTHOR", "UPDATED", contentWidth)
	b.WriteString(m.styles.Subtitle.Render(headerLine))
	b.WriteString("\n")
	b.WriteString(m.styles.Separator.Render(RuleLine(contentWidth)))
	b.WriteString("\n")

	// Visible window — scroll if needed.
	start, end := scrollWindow(m.cursor, len(m.filtered), m.visibleRows())

	for i := start; i < end; i++ {
		spec := m.filtered[i]
		line := m.formatRow(spec.ID, spec.Title, spec.Status, spec.Author, spec.Updated, contentWidth)
		if i == m.cursor {
			b.WriteString(m.styles.RowSelected.Render(line))
		} else {
			b.WriteString(m.styles.RowNormal.Render(line))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// specListHeaderRows is the number of fixed rows drawn above the first spec
// row: the search/count line, a blank, the column header, and the rule.
// view() and clickRow() must agree on this offset.
const specListHeaderRows = 4

// visibleRows is how many spec rows fit on screen below the header rows.
func (m specListModel) visibleRows() int {
	v := m.height - 6 // search bar, blank, header, separator, padding
	if v < 3 {
		v = 3
	}
	return v
}

// clickRow maps a content-local row y to a spec and selects it.
func (m *specListModel) clickRow(y int) clickResult {
	row := y - specListHeaderRows
	if row < 0 {
		return clickMissed
	}
	start, _ := scrollWindow(m.cursor, len(m.filtered), m.visibleRows())
	idx := start + row
	if idx < 0 || idx >= len(m.filtered) {
		return clickMissed
	}
	if idx == m.cursor {
		return clickActivated
	}
	m.cursor = idx
	return clickSelected
}

// wheelRows moves the spec selection by delta rows (negative = up).
func (m *specListModel) wheelRows(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.filtered))
}

func (m specListModel) formatRow(id, title, status, author, updated string, width int) string {
	compact := width < 70

	// Fixed column widths. The title column absorbs whatever is left.
	// The total must not exceed width so the styled row never wraps.
	const (
		indent    = 2
		idCol     = 11
		statusCol = 12
		authorCol = 10
		updateCol = 10
		gaps      = 4 // spaces between columns
	)

	if compact {
		fixed := indent + idCol + 1 + len(truncate(status, statusCol))
		titleMax := width - fixed - 1
		if titleMax < 8 {
			titleMax = 8
		}
		return fmt.Sprintf("  %-*s %-*s %s",
			idCol, truncate(id, idCol),
			titleMax, truncate(title, titleMax),
			truncate(status, statusCol),
		)
	}

	// Wide: all columns. Compute title width so total == width exactly.
	// Layout: indent + id + gap + title + gap + status + gap + author + gap + updated
	fixed := indent + idCol + 1 + 1 + statusCol + 1 + authorCol + 1 + updateCol
	titleMax := width - fixed
	if titleMax < 10 {
		titleMax = 10
	}
	return fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		idCol, truncate(id, idCol),
		titleMax, truncate(title, titleMax),
		statusCol, truncate(status, statusCol),
		authorCol, truncate(author, authorCol),
		updateCol, truncate(updated, updateCol),
	)
}

// isInputActive returns true when the search bar is capturing keystrokes.
func (m specListModel) isInputActive() bool {
	return m.searchActive
}

// hasActiveFilter returns true when a committed search filter is applied but the
// search bar is no longer capturing input. In this state Esc should clear the
// filter rather than arm the app's exit guard.
func (m specListModel) hasActiveFilter() bool {
	return !m.searchActive && m.searchQuery != ""
}

func (m specListModel) selectedSpecID() string {
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		return m.filtered[m.cursor].ID
	}
	return ""
}

func (m specListModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m *specListModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *specListModel) applyFilter() {
	if m.searchQuery == "" {
		m.filtered = m.allSpecs
	} else {
		q := strings.ToLower(m.searchQuery)
		m.filtered = nil
		for _, s := range m.allSpecs {
			if strings.Contains(strings.ToLower(s.ID), q) ||
				strings.Contains(strings.ToLower(s.Title), q) ||
				strings.Contains(strings.ToLower(s.Status), q) ||
				strings.Contains(strings.ToLower(s.Author), q) {
				m.filtered = append(m.filtered, s)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m specListModel) fetchData() tea.Cmd {
	rc := m.rc
	archiveMode := m.archiveMode
	return func() tea.Msg {
		specs, err := loadAllSpecs(context.Background(), rc, archiveMode)
		return specListDataMsg{Specs: specs, Err: err}
	}
}

func loadAllSpecs(ctx context.Context, rc *config.ResolvedConfig, archiveMode bool) ([]specListItem, error) {
	if rc.SpecsRepoDir == "" {
		return nil, nil
	}

	// Fetch remote changes (TTL-gated) before reading local files, so a refresh
	// surfaces teammates' pushes. Non-fatal: read cached files regardless and
	// report the error as a stale-data signal.
	syncErr := syncSpecsRepo(ctx, rc)

	specsDir := rc.SpecsRepoDir
	if archiveMode {
		archiveDir := config.ArchiveDir(rc.Team)
		specsDir = filepath.Join(specsDir, archiveDir)
	}

	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if archiveMode && os.IsNotExist(err) {
			return nil, nil // archive dir doesn't exist yet
		}
		return nil, fmt.Errorf("reading specs dir: %w", err)
	}

	var specs []specListItem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(specsDir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		specs = append(specs, specListItem{
			ID:      meta.ID,
			Title:   meta.Title,
			Status:  meta.Status,
			Author:  meta.Author,
			Updated: meta.Updated,
		})
	}
	return specs, syncErr
}

// clampCursor keeps a cursor index within [0, total-1]. An empty list yields
// 0. Shared by the list views' mouse-wheel handlers so wheel scrolling can
// never run the selection off either end.
func clampCursor(cursor, total int) int {
	if total <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor > total-1 {
		return total - 1
	}
	return cursor
}

// scrollWindow computes the visible slice for a scrollable list.
// cursor is the selected item index, total is the number of items,
// visible is how many items fit on screen.
func scrollWindow(cursor, total, visible int) (start, end int) {
	if total <= visible {
		return 0, total
	}
	half := visible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + visible
	if end > total {
		end = total
		start = end - visible
	}
	return start, end
}

// scrollWindowAround is like scrollWindow but operates on rendered line
// indices rather than item indices. Used when items produce varying
// numbers of lines (e.g. section headers, blank separators).
func scrollWindowAround(focusLine, totalLines, visible int) (start, end int) {
	return scrollWindow(focusLine, totalLines, visible)
}
