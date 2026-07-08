package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/search"
)

// searchDebounce coalesces a burst of typing into one query so the FTS lookup
// runs at most once per pause. 80 ms is short enough to feel instant yet long
// enough that fast typing never fires a query per keystroke.
const searchDebounce = 80 * time.Millisecond

// searchLimit caps the overlay result list. The FTS query itself is cheap; the
// cap bounds render work and keeps the cursor reachable.
const searchLimit = 50

// maxSectionRowsPerSpec caps the visible section sub-rows under one spec
// group so a spec matching in many sections doesn't drown the list. Hidden
// hits are summarised as a "+N more matches" line.
const maxSectionRowsPerSpec = 3

// ── Messages ──────────────────────────────────────────────────────────────────

// searchDebounceMsg fires when the debounce window elapses, carrying the
// generation counter so a stale tick (superseded by newer typing) is ignored.
type searchDebounceMsg struct{ Gen int }

// searchResultsMsg carries ranked hits for one query generation. Stale
// generations (Gen != the overlay's current gen) are discarded on arrival so
// fast typing never renders out-of-order results.
type searchResultsMsg struct {
	Gen  int
	Hits []search.Hit
	Err  error
}

// navigateToSpecSectionMsg asks the App to open a spec detail pinned to a
// specific section (deep-link from a search hit).
type navigateToSpecSectionMsg struct {
	SpecID      string
	SectionSlug string
}

// searchReconcileDoneMsg clears the overlay's `indexing…` chip once the
// background reconcile finishes.
type searchReconcileDoneMsg struct {
	Stats search.Stats
	Err   error
}

// searchScopeLabel renders a scope for the footer chip.
func searchScopeLabel(s search.SearchScope) string {
	switch s {
	case search.ScopeActive:
		return "active"
	case search.ScopeArchived:
		return "archived"
	default:
		return "all"
	}
}

// nextSearchScope cycles all → active → archived → all.
func nextSearchScope(s search.SearchScope) search.SearchScope {
	switch s {
	case search.ScopeActive:
		return search.ScopeArchived
	case search.ScopeArchived:
		return search.ScopeAll
	default:
		return search.ScopeActive
	}
}

// searchOverlayModel is the global `/` search overlay: a centred text input
// whose debounced keystrokes run an FTS5 query and render ranked, section-
// anchored hits. Enter deep-links into the spec reader; Esc closes.
type searchOverlayModel struct {
	ix       *search.Indexer
	input    textinput.Model
	results  []search.Hit
	cursor   int
	scope    search.SearchScope
	visible  bool
	gen      int // monotonic; stale results/ticks are discarded by generation
	indexing bool
	width    int
	height   int
	styles   Styles
	theme    Theme
}

// newSearchOverlay builds the overlay with a themed text input. The indexer is
// injected on open so a nil store (degraded TUI start) still renders; Search
// then runs the live fallback scan.
func newSearchOverlay(styles Styles, theme Theme) searchOverlayModel {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "search…"
	in.SetStyles(textInputStyles(theme))
	return searchOverlayModel{styles: styles, theme: theme, input: in}
}

// open arms the overlay over the shared indexer and focuses the input so the
// next keystroke lands in the query. Query/results are cleared on each open
// (fresh search); reopening after a deep-link is handled by the App setting
// visible=true with the prior state intact.
func (m *searchOverlayModel) open(ix *search.Indexer, width, height int) {
	m.ix = ix
	m.visible = true
	m.results = nil
	m.cursor = 0
	m.scope = search.ScopeAll
	m.gen++
	m.input.SetValue("")
	m.input.Focus()
	m.setSize(width, height)
	m.markIndexing()
}

// markIndexing flags the chip on while a background reconcile is in flight.
// The completion message clears it. A nil indexer or empty index still shows
// the chip until the first reconcile finishes and the overlay is reopened.
func (m *searchOverlayModel) markIndexing() {
	if m.ix == nil {
		m.indexing = false
		return
	}
	empty, err := m.ix.IndexEmpty()
	m.indexing = err == nil && empty
}

func (m *searchOverlayModel) hide() {
	m.visible = false
	m.input.Blur()
}

// close hides and clears the overlay entirely (Esc with no pending jump).
func (m *searchOverlayModel) close() {
	m.hide()
	m.input.SetValue("")
	m.results = nil
	m.cursor = 0
}

func (m *searchOverlayModel) setSize(w, h int) {
	m.width = w
	m.height = h
	// Fit the text input inside the overlay frame, leaving room for the prompt
	// glyph and padding. Clamp so a tiny terminal still shows something.
	inWidth := w/2 - 4
	if inWidth < 20 {
		inWidth = 20
	}
	m.input.SetWidth(inWidth)
}

// openSearchOverlay arms the global search overlay, focusing its input so
// the next keystroke lands in the query. Safe from any top-level view.
func (a *App) openSearchOverlay() tea.Cmd {
	a.search.open(a.searchIx, a.width, a.contentHeight())
	return nil
}

// updateSearchOverlay routes a keystroke to the overlay model and copies the
// updated model back. Called at the top of handleKey Layer 1.
func (a *App) updateSearchOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	a.search, cmd = a.search.update(msg)
	return *a, cmd
}

// update handles keystrokes while the overlay is open. The input owns printable
// runes (so j/k etc. type into the query); arrow keys navigate results, tab
// cycles scope, enter deep-links, esc closes.
func (m searchOverlayModel) update(msg tea.Msg) (searchOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		// Only the most recent tick triggers a query.
		if msg.Gen != m.gen {
			return m, nil
		}
		return m, runSearch(m.ix, m.gen, m.input.Value(), m.scope)

	case searchResultsMsg:
		if msg.Gen != m.gen {
			return m, nil // stale — discard, a newer query is in flight
		}
		if msg.Err != nil {
			// Treat a query error as "no results" — never break the overlay.
			m.results = nil
			m.cursor = 0
			return m, nil
		}
		m.results = msg.Hits
		if n := len(m.visibleHitIndexes()); m.cursor >= n {
			m.cursor = max(0, n-1)
		}
		return m, nil

	case searchReconcileDoneMsg:
		m.indexing = false
		return m, nil

	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m searchOverlayModel) updateKey(msg tea.KeyPressMsg) (searchOverlayModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.close()
		return m, nil
	case "enter":
		if visible := m.visibleHitIndexes(); m.cursor >= 0 && m.cursor < len(visible) {
			hit := m.results[visible[m.cursor]]
			// Hide but keep state so Esc from the reader returns here.
			m.visible = false
			m.input.Blur()
			return m, func() tea.Msg { return navigateToSpecSectionMsg{SpecID: hit.SpecID, SectionSlug: hit.SectionSlug} }
		}
		return m, nil
	case "tab":
		m.scope = nextSearchScope(m.scope)
		m.gen++
		return m, m.armDebounce()
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down":
		if m.cursor < len(m.visibleHitIndexes())-1 {
			m.cursor++
		}
		return m, nil
	case "ctrl+c":
		m.close()
		return m, tea.Quit
	}

	// Printable runes, backspace, left/right: delegate to the text input, then
	// re-arm the debounce so a query fires after the pause.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.gen++
	return m, tea.Batch(cmd, m.armDebounce())
}

// armDebounce returns a tea.Tick command that fires a searchDebounceMsg after
// the debounce window, carrying the current generation.
func (m searchOverlayModel) armDebounce() tea.Cmd {
	gen := m.gen
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchDebounceMsg{Gen: gen}
	})
}

// runSearch executes the FTS query off the update loop and wraps the result in
// a generation-tagged message. A nil indexer (degraded start) yields no hits
// rather than panicking.
func runSearch(ix *search.Indexer, gen int, query string, scope search.SearchScope) tea.Cmd {
	return func() tea.Msg {
		if ix == nil || strings.TrimSpace(query) == "" {
			return searchResultsMsg{Gen: gen}
		}
		hits, err := ix.Search(context.Background(), query, search.Options{Scope: scope, Limit: searchLimit})
		return searchResultsMsg{Gen: gen, Hits: hits, Err: err}
	}
}

// specGroup is one spec's cluster of hits, in ranked order of the spec's best
// hit. hits index into the flat results slice.
type specGroup struct {
	specID   string
	title    string
	author   string
	archived bool
	hits     []int
}

// groupHits clusters the flat ranked hits by spec, preserving the order in
// which specs first appear (i.e. each spec is ranked by its best hit).
func groupHits(hits []search.Hit) []specGroup {
	var groups []specGroup
	idx := make(map[string]int)
	for i, h := range hits {
		gi, ok := idx[h.SpecID]
		if !ok {
			gi = len(groups)
			idx[h.SpecID] = gi
			groups = append(groups, specGroup{specID: h.SpecID, title: h.Title, author: h.Author, archived: h.Archived})
		}
		groups[gi].hits = append(groups[gi].hits, i)
	}
	return groups
}

// visibleHitIndexes flattens the grouped display back to the hit indices the
// cursor can land on: the first maxSectionRowsPerSpec hits of each group, in
// group order. Enter opens results[visible[cursor]].
func (m searchOverlayModel) visibleHitIndexes() []int {
	var out []int
	for _, g := range groupHits(m.results) {
		n := min(len(g.hits), maxSectionRowsPerSpec)
		out = append(out, g.hits[:n]...)
	}
	return out
}

// view renders the overlay: prompt + input, grouped result rows windowed to
// the available height, and a footer hint strip with the scope and an
// indexing chip. The footer is always visible; the result body scrolls.
func (m searchOverlayModel) view() string {
	var b strings.Builder

	b.WriteString(m.styles.Title.Render("  Search specs"))
	b.WriteString("\n\n")
	b.WriteString(m.styles.Accent.Render("  ❯ "))
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	// Chrome around the body: title(1) + blank(1) + prompt(1) + blank(1)
	// above, blank(1) + footer(1) below.
	const chromeLines = 6
	avail := max(m.height-chromeLines, 3)

	switch {
	case strings.TrimSpace(m.input.Value()) == "":
		b.WriteString(m.styles.Muted.Render("  Matches titles, authors, and spec content"))
		b.WriteString("\n")
	case len(m.results) == 0:
		b.WriteString(m.styles.Muted.Render("  No matches"))
		b.WriteString("\n")
	default:
		lines, cursorLine := m.renderResultLines()
		for _, l := range windowLines(lines, cursorLine, avail) {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

// windowLines returns a slice of at most avail lines that always contains
// cursorLine, scrolling the window as the cursor moves.
func windowLines(lines []string, cursorLine, avail int) []string {
	if len(lines) <= avail {
		return lines
	}
	top := 0
	if cursorLine >= avail {
		top = cursorLine - avail + 1
	}
	if top+avail > len(lines) {
		top = len(lines) - avail
	}
	return lines[top : top+avail]
}

// renderResultLines renders the grouped hits as styled lines and reports the
// line index the cursor sits on (for viewport windowing). Layout per group:
//
//	▸ SPEC-013  Sync management refinement
//	    § Conflict Resolution   …retry on push conflict…
//	    +2 more matches
func (m searchOverlayModel) renderResultLines() (lines []string, cursorLine int) {
	sel := 0 // running selectable-row counter, compared against m.cursor
	for _, g := range groupHits(m.results) {
		lines = append(lines, m.renderGroupHeader(g))
		n := min(len(g.hits), maxSectionRowsPerSpec)
		for _, hi := range g.hits[:n] {
			selected := sel == m.cursor
			if selected {
				cursorLine = len(lines)
			}
			lines = append(lines, m.renderSectionRow(m.results[hi], selected))
			sel++
		}
		if hidden := len(g.hits) - n; hidden > 0 {
			lines = append(lines, m.styles.Muted.Render(fmt.Sprintf("      +%d more match(es)", hidden)))
		}
	}
	return lines, cursorLine
}

// renderGroupHeader renders the one-line spec header for a group. Styled
// spans are concatenated per segment (never nested) so resets can't bleed.
func (m searchOverlayModel) renderGroupHeader(g specGroup) string {
	head := m.styles.Accent.Render(fmt.Sprintf("  %-10s", g.specID)) +
		"  " + m.styles.RowNormal.Bold(true).Render(truncate(g.title, 48))
	if g.author != "" {
		head += "  " + m.styles.Muted.Render("by "+truncate(g.author, 20))
	}
	if g.archived {
		head += "  " + m.styles.Muted.Render("archived")
	}
	return head
}

// renderSectionRow renders one selectable sub-row: section label + snippet.
// The selected row is rendered as a single plain string wrapped once in
// RowSelected (no inner styled spans, whose ANSI resets would cut the
// highlight mid-line); unselected rows style each segment independently.
func (m searchOverlayModel) renderSectionRow(h search.Hit, selected bool) string {
	label := sectionLabel(h.SectionHeading)
	if label == "" {
		label = "overview"
	}
	label = fmt.Sprintf("§ %-22s", truncate(label, 20))

	snippetBudget := max(m.width-len("    ")-len(label)-4, 10)
	segs := parseSnippet(h.Snippet)

	if selected {
		row := "  ▸ " + label + " " + plainSnippet(segs, snippetBudget)
		return m.styles.RowSelected.Render(row)
	}
	row := "    " + m.styles.Muted.Render(label) + " "
	row += m.renderSnippetSegs(segs, snippetBudget)
	return row
}

// sectionLabel strips the leading "## " / "# " markers for a cleaner row.
func sectionLabel(heading string) string {
	return strings.TrimSpace(strings.TrimLeft(heading, "# "))
}

// snippetSeg is one run of snippet text; term marks an FTS5-highlighted match.
type snippetSeg struct {
	text string
	term bool
}

// snippet highlight markers emitted by the FTS5 snippet() call in the store.
const (
	snippetOpen  = "⟨"
	snippetClose = "⟩"
)

// parseSnippet splits an FTS5 snippet into plain and highlighted-term
// segments. Newlines collapse to spaces so a row is always one line. The
// marker runes are multi-byte; all offsets use their byte lengths.
func parseSnippet(snippet string) []snippetSeg {
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	var segs []snippetSeg
	for snippet != "" {
		i := strings.Index(snippet, snippetOpen)
		if i < 0 {
			segs = append(segs, snippetSeg{text: snippet})
			break
		}
		if i > 0 {
			segs = append(segs, snippetSeg{text: snippet[:i]})
		}
		rest := snippet[i+len(snippetOpen):]
		j := strings.Index(rest, snippetClose)
		if j < 0 {
			segs = append(segs, snippetSeg{text: rest})
			break
		}
		segs = append(segs, snippetSeg{text: rest[:j], term: true})
		snippet = rest[j+len(snippetClose):]
	}
	return segs
}

// plainSnippet joins segments without styling, truncated to budget runes.
func plainSnippet(segs []snippetSeg, budget int) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return truncate(b.String(), budget)
}

// renderSnippetSegs styles each segment independently (muted text, accent
// terms), truncating across segments so styled output never exceeds budget.
func (m searchOverlayModel) renderSnippetSegs(segs []snippetSeg, budget int) string {
	var b strings.Builder
	remaining := budget
	for _, s := range segs {
		if remaining <= 0 {
			break
		}
		text := truncate(s.text, remaining)
		remaining -= len([]rune(text))
		if s.term {
			b.WriteString(m.styles.Accent.Render(text))
		} else {
			b.WriteString(m.styles.Muted.Render(text))
		}
	}
	return b.String()
}

// footer renders the hint strip plus scope chip and an indexing indicator.
func (m searchOverlayModel) footer() string {
	pairs := []HintPair{
		Hint("enter", "open"),
		Hint("↑/↓", "move"),
		Hint("tab", "scope"),
		Hint("esc", "close"),
	}
	strip := HintStrip(m.styles, pairs...)
	chips := "  " + m.styles.Muted.Render("scope: "+searchScopeLabel(m.scope))
	if m.indexing {
		chips += "  " + m.styles.Warning.Render("indexing…")
	}
	return strip + chips
}
