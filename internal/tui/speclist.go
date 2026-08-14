package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
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
	// Parent is the initiative this spec is a deliverable slice of, or "".
	Parent string
	// bountied marks the spec's glyph + ID in gold.
	bountied bool
	// complete records whether the spec has reached a terminal stage, resolved
	// at load time where the pipeline config is in scope.
	complete bool

	// depth, lastChild, initiative and rollup are assigned by nestSpecs and
	// describe the row's place in the rendered tree rather than the document.
	depth      int
	lastChild  bool
	initiative bool
	folded     bool
	rollup     hierarchy.Rollup
}

// specListModel is a filterable list of all specs.
type specListModel struct {
	rc *config.ResolvedConfig

	allSpecs    []specListItem
	filtered    []specListItem
	loading     bool
	loaded      bool // true once at least one fetch has succeeded
	err         error
	cursor      int
	archiveMode bool // true = showing archived specs

	// collapsed holds the initiative IDs whose slices are hidden. Keyed by ID
	// rather than by row index so the state survives a refetch that reorders or
	// resizes the list.
	collapsed map[string]bool

	// nested reports whether the visible list contains any hierarchy, which is
	// what turns the tree gutter on.
	nested bool

	width  int
	height int
	styles Styles
	keys   KeyMap

	// bountyFrame is the shared animation clock for the bounty marker, pushed
	// in from the app on each repaint tick.
	bountyFrame int
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

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keys.Collapse):
			m.toggleCollapse()
			return m, nil
		case key.Matches(msg, m.keys.ToggleArchive):
			// ` toggles archive list mode. (The global `/` search overlay now
			// owns spec search; this list no longer filters in place.)
			m.archiveMode = !m.archiveMode
			m.cursor = 0
			return m, m.fetchData()
		}
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

	// Count line + hints. The global `/` search overlay now owns spec search;
	// this list shows its count and the archive toggle hint only.
	label := "specs"
	if m.archiveMode {
		label = "archived specs"
	}
	b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  %d %s  ", len(m.filtered), label)))
	toggle := Hint("`", "archive")
	if m.archiveMode {
		toggle = Hint("`", "specs")
	}
	b.WriteString(HintStrip(m.styles, toggle, Hint("/", "global search"), Hint("?", "help")))
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		switch {
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
	header := specListItem{ID: "ID", Title: "TITLE", Status: "STATUS", Author: "AUTHOR", Updated: "UPDATED"}
	headerMark, headerRest := m.formatRow(header, m.rowGutter(header), contentWidth)
	b.WriteString(m.styles.Subtitle.Render(headerMark + headerRest))
	b.WriteString("\n")
	b.WriteString(m.styles.Separator.Render(RuleLine(contentWidth)))
	b.WriteString("\n")

	// Visible window — scroll if needed.
	start, end := scrollWindow(m.cursor, len(m.filtered), m.visibleRows())

	for i := start; i < end; i++ {
		spec := m.filtered[i]
		mark, rest := m.formatRow(spec, m.rowGutter(spec), contentWidth)
		base := m.styles.RowNormal
		if i == m.cursor {
			base = m.styles.RowSelected
		}
		if spec.bountied {
			b.WriteString(newBountyPainter(m.rc, m.styles.Theme, m.bountyFrame).paint(base, spec.ID, mark))
			b.WriteString(base.Render(rest))
		} else {
			b.WriteString(base.Render(mark + rest))
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

// formatRow lays out one spec row as two spans: the leading marker (indent,
// bounty gutter, and SPEC-ID) and the remainder (title, status, author, date).
// Splitting them lets a bountied row paint its marker gold while the rest of
// the row keeps whatever colour its state calls for.
//
// gutter is a fixed-width cell reserved for the bounty glyph. It is empty when
// the team has bounties off, so a team that never uses the feature sees exactly
// the layout it always had; when bounties are on, the cell is present on every
// row (blank when unbountied) so columns never shift.
func (m specListModel) formatRow(item specListItem, gutter string, width int) (mark, rest string) {
	compact := width < 70
	id, title, status, author, updated := item.ID, item.Title, item.Status, item.Author, item.Updated

	// Fixed column widths. The title column absorbs whatever is left.
	// The total must not exceed width so the styled row never wraps.
	const (
		indent    = 2
		idCol     = 11
		statusCol = 12
		authorCol = 10
		updateCol = 10
	)
	gutterW := lipgloss.Width(gutter)
	mark = fmt.Sprintf("  %s%-*s", gutter, idCol, truncate(id, idCol))

	if compact {
		fixed := indent + gutterW + idCol + 1 + len(truncate(status, statusCol))
		titleMax := width - fixed - 1
		if titleMax < 8 {
			titleMax = 8
		}
		rest = fmt.Sprintf(" %-*s %s",
			titleMax, truncate(title, titleMax),
			truncate(status, statusCol),
		)
		return mark, rest
	}

	// Wide: all columns. Compute title width so total == width exactly.
	// Layout: indent + gutter + id + gap + title + gap + status + gap + author + gap + updated
	fixed := indent + gutterW + idCol + 1 + 1 + statusCol + 1 + authorCol + 1 + updateCol
	titleMax := width - fixed
	if titleMax < 10 {
		titleMax = 10
	}
	// An initiative trades its UPDATED cell for its slice rollup: the date a
	// vision document was last touched says nothing useful, while "3/5" is the
	// only number that matters on that row.
	last := truncate(updated, updateCol)
	if item.initiative {
		last = fmt.Sprintf("%d/%d", item.rollup.Complete, item.rollup.Total)
	}
	rest = fmt.Sprintf(" %-*s %-*s %-*s %*s",
		titleMax, truncate(title, titleMax),
		statusCol, truncate(status, statusCol),
		authorCol, truncate(author, authorCol),
		updateCol, truncate(last, updateCol),
	)
	return mark, rest
}

// rowGutter assembles a row's full leading gutter: the bounty cell, then the
// hierarchy cell. Both are empty when their feature is not in play, so the
// layout only grows for teams that actually use them.
func (m specListModel) rowGutter(item specListItem) string {
	return m.bountyGutter(item.bountied) + treeGutter(item, m.nested)
}

// bountyGutter returns the fixed-width leading cell for a spec row: the gem
// for a bountied spec, blanks for an unbountied one, and nothing at all when
// the team has bounties disabled.
func (m specListModel) bountyGutter(bountied bool) string {
	if !m.rc.BountyEnabled() {
		return ""
	}
	if bountied {
		return IconBounty + " "
	}
	return "  "
}

// toggleCollapse folds or unfolds the slices of the initiative under the
// cursor. Pressing it on a slice folds its initiative and moves the selection
// there, so the row the user is looking at never disappears from under them.
func (m *specListModel) toggleCollapse() {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return
	}
	row := m.filtered[m.cursor]
	target := row.ID
	if !row.initiative {
		if row.Parent == "" {
			return
		}
		target = row.Parent
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	m.collapsed[target] = !m.collapsed[target]
	m.applyFilter()
	m.selectSpec(target)
}

// selectSpec moves the cursor to a spec by ID, leaving it alone when the spec
// is not currently visible.
func (m *specListModel) selectSpec(id string) {
	for i, it := range m.filtered {
		if it.ID == id {
			m.cursor = i
			return
		}
	}
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

// applyFilter sets the visible slice to the full spec set. The in-list
// substring filter was removed when the global `/` search overlay took over
// (SPEC-028); the list now only splits active vs archived via archiveMode,
// which is selected at fetch time.
func (m *specListModel) applyFilter() {
	m.filtered = collapseSlices(nestSpecs(m.allSpecs), m.collapsed)
	m.nested = hasHierarchy(m.filtered)
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

	pl := rc.Pipeline()
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
			ID:       meta.ID,
			Title:    meta.Title,
			Status:   meta.Status,
			Author:   meta.Author,
			Updated:  meta.Updated,
			Parent:   meta.Parent,
			bountied: meta.Bounty != nil,
			complete: pipeline.IsTerminalStage(pl, meta.Status),
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
