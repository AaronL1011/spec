package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/thread"
)

// ── Messages ──────────────────────────────────────────────────────────────────

type specDetailDataMsg struct {
	Meta      *markdown.SpecMeta
	Sections  []markdown.Section
	Decisions []markdown.DecisionEntry
	Threads   []thread.Thread
	Hash      string
	Archived  bool
	BuildLine string
	Err       error
}

// threadsChangedMsg carries the refreshed thread set after a mutation.
type threadsChangedMsg struct {
	Threads []thread.Thread
	Err     error
	Toast   string
}

type sectionRenderedMsg struct {
	CacheKey     string
	Gen          uint64
	Content      string
	Err          error
	RenderMillis int64
}

type navigateToSpecMsg struct{ SpecID string }
type navigateBackMsg struct{}

// ── Model ─────────────────────────────────────────────────────────────────────

type specDetailModel struct {
	rc     *config.ResolvedConfig
	specID string
	// db is the shared store handle, used to read the build session for the
	// build status line. May be nil (e.g. in tests); the line is then omitted.
	db *store.DB
	// buildLine is the compact build status ("step N/M — [repo] desc · ACs x/y")
	// shown when a build session exists for the spec. Empty when none.
	buildLine string

	// Spec data
	meta        *markdown.SpecMeta
	sections    []markdown.Section
	decisions   []markdown.DecisionEntry
	loading     bool
	err         error
	notice      string // calm inline notice (e.g. file removed); non-fatal
	contentHash string
	isArchived  bool // true if spec is in archive/

	// Overview scroll
	scroll       int
	contentLines int

	// Reader
	readerMode     bool
	sectionIdx     int
	readerContent  string
	readerViewport viewport.Model
	readerErr      error
	readerCache    map[string]string

	// Threads — inline Q&A (SPEC-012)
	threads      []thread.Thread
	paneVisible  bool // thread pane shown (toggled with 't')
	paneFocused  bool // arrow keys target the pane, not the prose
	threadIdx    int  // selected thread within the focused section
	threadScroll int  // scroll offset within the (possibly tall) pane body
	input        threadInput

	// Render lifecycle — single-flight + pending coalesce
	renderInFlight bool
	renderGen      uint64
	activeCacheKey string
	pendingRequest *pendingRenderRequest

	// pendingScrollRestore holds a viewport YOffset to reapply once the next
	// section render lands, so a position-preserving refresh (SPEC-007) does
	// not snap the reader back to the top. -1 means no restore is pending.
	pendingScrollRestore int

	renderer Renderer
	theme    Theme
	width    int
	height   int
	styles   Styles
	keys     KeyMap
}

type pendingRenderRequest struct {
	SectionIdx int
	CacheKey   string
	Heading    string
	Owner      string
	Body       string
	Total      int
	Width      int
	Styles     Styles
}

// ── Constructor ───────────────────────────────────────────────────────────────

func newSpecDetail(rc *config.ResolvedConfig, specID string, styles Styles, keys KeyMap, theme Theme) specDetailModel {
	vp := viewport.New(80, 20)
	vp.KeyMap = viewport.KeyMap{} // viewport keys are managed by updateReader
	return specDetailModel{
		rc:                   rc,
		specID:               specID,
		loading:              true,
		styles:               styles,
		keys:                 keys,
		theme:                theme,
		readerCache:          make(map[string]string),
		renderer:             newRenderer(theme),
		readerViewport:       vp,
		paneVisible:          true,
		pendingScrollRestore: -1,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m specDetailModel) init() tea.Cmd { return m.fetchData() }

// ── Update ────────────────────────────────────────────────────────────────────

func (m specDetailModel) update(msg tea.Msg) (specDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case specDetailDataMsg:
		return m.handleDataMsg(msg)
	case sectionRenderedMsg:
		return m.handleRenderedMsg(msg)
	case threadsChangedMsg:
		return m.handleThreadsChanged(msg)
	case tea.KeyMsg:
		if m.readerMode {
			return m.updateReader(msg)
		}
		return m.updateOverview(msg)
	}
	return m, nil
}

func (m specDetailModel) handleThreadsChanged(msg threadsChangedMsg) (specDetailModel, tea.Cmd) {
	if msg.Err != nil {
		m.readerErr = msg.Err
		return m, nil
	}
	m.threads = msg.Threads
	// Clamp selection to the new thread set for the focused section.
	if n := len(m.threadsForSection(m.currentSectionSlug())); m.threadIdx >= n {
		m.threadIdx = max(0, n-1)
	}
	return m, nil
}

func (m specDetailModel) handleDataMsg(msg specDetailDataMsg) (specDetailModel, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		// A refresh failure on an already-loaded spec (e.g. the file was moved
		// or deleted out from under the reader) must not blank the view. Keep
		// the last-good content on screen and surface a calm inline notice.
		if m.meta != nil {
			m.notice = "spec moved or removed — press esc to go back"
			return m, nil
		}
		m.err = msg.Err
		return m, nil
	}
	m.notice = ""
	// No change — skip re-render. Hash-gating keeps an unchanged file (or an
	// editor's identical re-save) from disturbing the view at all. The build
	// line still refreshes: an agent may advance a step via MCP without
	// touching the spec file.
	if msg.Hash != "" && msg.Hash == m.contentHash && m.meta != nil {
		m.err = nil
		m.buildLine = msg.BuildLine
		return m, nil
	}
	// A refresh into an already-loaded spec (watcher-triggered or poll) must
	// preserve the user's position; the initial load starts fresh.
	if m.meta != nil {
		return m.applyRefresh(msg)
	}
	m.meta = msg.Meta
	m.sections = msg.Sections
	m.decisions = msg.Decisions
	m.threads = msg.Threads
	m.err = nil
	m.contentHash = msg.Hash
	m.isArchived = msg.Archived
	m.buildLine = msg.BuildLine
	m.readerCache = make(map[string]string)
	m.contentLines = m.estimateContentLines()
	if m.readerMode {
		if sections := m.readableSections(); m.sectionIdx >= len(sections) {
			m.sectionIdx = max(0, len(sections)-1)
		}
		m.cancelRender()
		return m.requestCurrentSectionRender()
	}
	m.scroll = 0
	return m, nil
}

// applyRefresh merges freshly-read spec data into an already-loaded model,
// preserving scroll offset, current section, thread selection, body scroll,
// and any in-progress ask/reply input. It is the position-preserving path for
// watcher- and poll-triggered refreshes (SPEC-007).
func (m specDetailModel) applyRefresh(msg specDetailDataMsg) (specDetailModel, tea.Cmd) {
	// Capture position before swapping data.
	prevScroll := m.readerViewport.YOffset
	prevSectionIdx := m.sectionIdx
	prevThreadScroll := m.threadScroll
	selectedID := ""
	if t, ok := m.selectedThread(); ok {
		selectedID = t.ID
	}

	m.meta = msg.Meta
	m.sections = msg.Sections
	m.decisions = msg.Decisions
	m.threads = msg.Threads
	m.err = nil
	m.contentHash = msg.Hash
	m.isArchived = msg.Archived
	m.buildLine = msg.BuildLine
	m.readerCache = make(map[string]string) // content changed — invalidate renders
	m.contentLines = m.estimateContentLines()

	if !m.readerMode {
		// Overview: clamp the list scroll against the new content height.
		if mx := m.maxScroll(); m.scroll > mx {
			m.scroll = mx
		}
		return m, nil
	}

	// Reader: keep the same section (clamped), restore thread selection by ID,
	// and preserve body scroll. The viewport offset is restored after the
	// section re-renders, in handleRenderedMsg, via pendingScrollRestore.
	sections := m.readableSections()
	m.sectionIdx = clampInt(prevSectionIdx, 0, len(sections)-1)
	m.restoreThreadSelection(selectedID)
	if mx := m.maxThreadScroll(); prevThreadScroll > mx {
		prevThreadScroll = mx
	}
	m.threadScroll = prevThreadScroll
	m.pendingScrollRestore = prevScroll
	m.cancelRender()
	return m.requestCurrentSectionRender()
}

// restoreThreadSelection re-selects the thread with the given ID within the
// current section after a refresh. When that thread no longer exists (resolved
// out of the open set, removed), selection falls back to the nearest valid
// index in the same section so the pane never points past its slice.
func (m *specDetailModel) restoreThreadSelection(id string) {
	ts := m.threadsForSection(m.currentSectionSlug())
	if len(ts) == 0 {
		m.threadIdx = 0
		return
	}
	if id != "" {
		for i, t := range ts {
			if t.ID == id {
				m.threadIdx = i
				return
			}
		}
	}
	m.threadIdx = clampInt(m.threadIdx, 0, len(ts)-1)
}

// clampInt bounds v to [lo, hi]. When hi < lo (e.g. an empty slice yields
// hi == -1), it returns lo.
func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m specDetailModel) handleRenderedMsg(msg sectionRenderedMsg) (specDetailModel, tea.Cmd) {
	// Ignore stale results from superseded renders.
	if msg.Gen != m.renderGen {
		return m, nil
	}
	m.renderInFlight = false
	m.activeCacheKey = ""

	if msg.Err != nil {
		m.readerErr = msg.Err
		// Still try to start any pending request.
		if next := m.dequeuePending(); next != nil {
			return m.startRender(*next)
		}
		return m, nil
	}

	m.readerCache[msg.CacheKey] = msg.Content

	// If user has navigated away while this was rendering, start the
	// pending request immediately rather than applying stale content.
	if next := m.dequeuePending(); next != nil && next.CacheKey != msg.CacheKey {
		return m.startRender(*next)
	}

	m.applyReaderContent(msg.Content)
	// Reapply a preserved scroll offset from a position-preserving refresh.
	if m.pendingScrollRestore >= 0 {
		offset := m.pendingScrollRestore
		m.pendingScrollRestore = -1
		if mx := m.maxScroll(); offset > mx {
			offset = mx
		}
		m.readerViewport.SetYOffset(offset)
	}
	m.readerErr = nil
	return m, nil
}

// ── Key Handling ──────────────────────────────────────────────────────────────

// wheelScroll scrolls the detail view by delta lines (negative = up). It
// mirrors the Up/Down key behaviour: the reader viewport (or focused thread
// pane) in reader mode, the overview offset otherwise. Driven by the mouse
// wheel so pointer and keyboard scrolling stay consistent.
func (m *specDetailModel) wheelScroll(delta int) {
	if m.readerMode {
		if m.paneFocused {
			m.threadScroll = clampCursor(m.threadScroll+delta, m.maxThreadScroll()+1)
			return
		}
		if delta < 0 {
			m.readerViewport.ScrollUp(-delta)
		} else {
			m.readerViewport.ScrollDown(delta)
		}
		return
	}
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if mx := m.maxScroll(); m.scroll > mx {
		m.scroll = mx
	}
}

func (m specDetailModel) updateOverview(msg tea.KeyMsg) (specDetailModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.scroll > 0 {
			m.scroll--
		}
	case key.Matches(msg, m.keys.Down):
		m.scroll++
		if mx := m.maxScroll(); m.scroll > mx {
			m.scroll = mx
		}
	case key.Matches(msg, m.keys.Open):
		m.readerMode = true
		m.sectionIdx = m.firstReadableSectionIndex()
		m.scroll = 0
		return m.requestCurrentSectionRender()
	case key.Matches(msg, m.keys.Back):
		return m, func() tea.Msg { return navigateBackMsg{} }
	}
	return m, nil
}

func (m specDetailModel) updateReader(msg tea.KeyMsg) (specDetailModel, tea.Cmd) {
	// Active ask/reply prompt captures all keys first.
	if nm, cmd, handled := m.handleThreadInputKey(msg); handled {
		return nm, cmd
	}
	// Thread action keys (a/r/x/t/tab) take precedence over navigation so
	// they work regardless of which pane has focus.
	if nm, cmd, handled := m.handleThreadActionKey(msg); handled {
		return nm, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Up):
		if m.paneFocused {
			// Scroll the pane body so long threads are fully readable.
			if m.threadScroll > 0 {
				m.threadScroll--
			}
			return m, nil
		}
		m.readerViewport.ScrollUp(1)
	case key.Matches(msg, m.keys.Down):
		if m.paneFocused {
			if m.threadScroll < m.maxThreadScroll() {
				m.threadScroll++
			}
			return m, nil
		}
		m.readerViewport.ScrollDown(1)
	case msg.Type == tea.KeyPgUp:
		m.readerViewport.PageUp()
	case msg.Type == tea.KeyPgDown:
		m.readerViewport.PageDown()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "n":
		if m.paneFocused {
			return m.selectThread(1), nil // next thread when reading threads
		}
		return m.withSection(m.sectionIdx + 1)
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "p":
		if m.paneFocused {
			return m.selectThread(-1), nil
		}
		return m.withSection(m.sectionIdx - 1)
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "g":
		m.readerViewport.GotoTop()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "G":
		m.readerViewport.GotoBottom()
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9':
		return m.withSection(int(msg.Runes[0]-'0') - 1)
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "0":
		for i, sec := range m.readableSections() {
			if sec.Slug == "decision_log" {
				return m.withSection(i)
			}
		}
	case key.Matches(msg, m.keys.Open), key.Matches(msg, m.keys.Back):
		m.cancelRender()
		m.readerMode = false
		m.scroll = 0
		m.contentLines = m.estimateContentLines()
		return m, nil
	}
	return m, nil
}

func (m specDetailModel) withSection(idx int) (specDetailModel, tea.Cmd) {
	sections := m.readableSections()
	if idx < 0 || idx >= len(sections) {
		return m, nil
	}
	m.sectionIdx = idx
	// Reset thread focus/selection when moving to a new section.
	m.threadIdx = 0
	m.threadScroll = 0
	m.paneFocused = false
	m.readerViewport.GotoTop()
	return m.requestCurrentSectionRender()
}

// ── Render Lifecycle ──────────────────────────────────────────────────────────

func (m specDetailModel) requestCurrentSectionRender() (specDetailModel, tea.Cmd) {
	sections := m.readableSections()
	if m.sectionIdx >= len(sections) {
		m.applyReaderContent("  (no sections)")
		return m, nil
	}

	effWidth := m.effectiveWidth()
	sec := sections[m.sectionIdx]
	cacheKey := m.readerCacheKey(sec, m.sectionIdx, effWidth)

	// Cache hit — instant apply, no cmd.
	if content, ok := m.readerCache[cacheKey]; ok {
		m.applyReaderContent(content)
		m.readerErr = nil
		return m, nil
	}

	req := pendingRenderRequest{
		SectionIdx: m.sectionIdx,
		CacheKey:   cacheKey,
		Heading:    sec.Heading,
		Owner:      sec.Owner,
		Body:       sec.Content,
		Total:      len(sections),
		Width:      effWidth,
		Styles:     m.styles,
	}

	if m.renderInFlight {
		// Coalesce: overwrite with latest intent, active render completes first.
		if cacheKey == m.activeCacheKey {
			return m, nil // Already rendering this exact section.
		}
		m.pendingRequest = &req
		return m, nil
	}

	return m.startRender(req)
}

func (m specDetailModel) startRender(req pendingRenderRequest) (specDetailModel, tea.Cmd) {
	m.renderGen++
	gen := m.renderGen
	m.renderInFlight = true
	m.activeCacheKey = req.CacheKey
	m.readerErr = nil

	renderer := m.renderer

	return m, func() tea.Msg {
		started := time.Now()
		content, err := renderSectionContent(
			context.Background(), renderer,
			req.Heading, req.Owner, req.Body,
			req.SectionIdx, req.Total, req.Width, req.Styles,
		)
		return sectionRenderedMsg{
			CacheKey:     req.CacheKey,
			Gen:          gen,
			Content:      content,
			Err:          err,
			RenderMillis: time.Since(started).Milliseconds(),
		}
	}
}

func (m *specDetailModel) dequeuePending() *pendingRenderRequest {
	if m.pendingRequest == nil {
		return nil
	}
	req := *m.pendingRequest
	m.pendingRequest = nil
	return &req
}

func (m *specDetailModel) cancelRender() {
	m.renderInFlight = false
	m.activeCacheKey = ""
	m.pendingRequest = nil
}

// ── Content Rendering ─────────────────────────────────────────────────────────

func renderSectionContent(ctx context.Context, renderer Renderer, heading, owner, body string, sectionIdx, total, width int, styles Styles) (string, error) {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(styles.Title.Render(Indent(1) + heading))
	b.WriteString("\n")
	if owner != "" && owner != "auto" {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("%s[%s]", Indent(1), owner)))
		b.WriteString("\n")
	}
	sepWidth := width - 2*Gutter
	if sepWidth < 10 {
		sepWidth = 10
	}
	b.WriteString(styles.Separator.Render(RuleLine(sepWidth)))
	b.WriteString("\n\n")

	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		b.WriteString(styles.Muted.Render(Indent(1) + "(empty section)"))
		b.WriteString("\n")
	} else {
		rendered, err := renderer.Render(ctx, trimmed, width-2)
		if err != nil {
			return "", err
		}
		for _, line := range splitLines(rendered) {
			b.WriteString(Indent(1))
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	nav := fmt.Sprintf("%s%s %d/%d", Indent(1), GlyphSection, sectionIdx+1, total)
	hints := HintStrip(styles,
		Hint("n", "next"), Hint("p", "prev"), Hint("1-9", "jump"),
		Hint("o", "overview"), Hint("tab", "switch view"))
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render(nav) + "  " + hints)
	return strings.TrimRight(b.String(), "\n"), nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m specDetailModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading spec…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}
	if m.meta == nil {
		return m.styles.Muted.Render("  Spec not found")
	}
	if m.readerMode {
		return m.viewReader()
	}
	return m.viewOverview()
}

func (m specDetailModel) viewReader() string {
	if m.readerErr != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.readerErr))
	}
	if m.notice != "" {
		return m.styles.Muted.Render("  "+m.notice) + "\n" + m.readerViewport.View()
	}
	// Blank while first render is in-flight — spinner in status bar is the indicator.
	if m.readerContent == "" {
		return ""
	}
	if m.width >= readerSidebarMinWidth {
		return m.viewReaderWithSidebar()
	}
	return m.viewReaderNarrow()
}

// Reader sidebar geometry. readerSidebarMinWidth is the terminal width at or
// above which the section navigator column is shown; readerSidebarWidth is the
// column's cell width. Both the renderer and the click hit-tester depend on
// these, so they live here as the single definition.
const (
	readerSidebarMinWidth = 100
	readerSidebarWidth    = 26
)

// sidebarLine is one rendered row of the reader's section navigator.
// sectionIdx indexes readableSections for clickable section rows, or is -1 for
// the header, blank, and padding rows.
type sidebarLine struct {
	text       string
	sectionIdx int
}

// buildSidebar builds the section navigator column already scrolled to keep
// the active section visible and padded/truncated to exactly `visible` rows.
// viewReaderWithSidebar() and sectionAtClick() both derive from it so the
// drawn column and its clickable rows stay in lockstep.
func (m specDetailModel) buildSidebar(sections []markdown.Section, visible int) []sidebarLine {
	lines := make([]sidebarLine, 0, len(sections)+2)
	// Use a plain accent-styled header rather than SectionTitle: the latter
	// carries MarginTop(1), which embeds a newline and would desync this
	// column's row count from the content column on its right.
	lines = append(lines,
		sidebarLine{text: m.styles.Accent.Bold(true).Render(" " + GlyphSection + " Sections"), sectionIdx: -1},
		sidebarLine{text: "", sectionIdx: -1},
	)
	for i, sec := range sections {
		fill := IconPending
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fill = IconFilled
		}
		// Open-thread badge keeps attention on unresolved review work.
		badge := ""
		if n := m.openCountForSection(sec.Slug); n > 0 {
			badge = fmt.Sprintf(" ●%d", n)
		}
		label := truncate(sec.Slug, readerSidebarWidth-7-len(badge))
		line := fmt.Sprintf(" %s %d %s%s", fill, i+1, label, badge)
		if i == m.sectionIdx {
			line = m.styles.Accent.Bold(true).Render(line)
		} else {
			line = m.styles.Muted.Render(line)
		}
		lines = append(lines, sidebarLine{text: line, sectionIdx: i})
	}
	for len(lines) < visible {
		lines = append(lines, sidebarLine{text: "", sectionIdx: -1})
	}
	if len(lines) > visible {
		ss, se := scrollWindow(m.sectionIdx+2, len(lines), visible)
		lines = lines[ss:se]
	}
	return lines
}

// sectionAtClick maps a content-local (x, y) within the reader to a sidebar
// section index. It returns ok=false when the reader has no sidebar (narrow
// terminals or not in reader mode), the click is outside the sidebar column,
// or the row is the header/blank/padding rather than a section entry.
func (m specDetailModel) sectionAtClick(x, y int) (int, bool) {
	if !m.readerMode || m.width < readerSidebarMinWidth {
		return 0, false
	}
	if x < 0 || x >= readerSidebarWidth {
		return 0, false
	}
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	lines := m.buildSidebar(m.readableSections(), visible)
	if y < 0 || y >= len(lines) {
		return 0, false
	}
	if lines[y].sectionIdx < 0 {
		return 0, false
	}
	return lines[y].sectionIdx, true
}

// viewReaderNarrow renders the reader on terminals too narrow for a sidebar.
// The thread pane drops to a full-width bottom drawer so the prose stays
// readable.
func (m specDetailModel) viewReaderNarrow() string {
	// Cap the pane to roughly half the reader so prose stays visible; the pane
	// body scrolls when a thread is taller than its budget.
	paneBudget := max(m.height/2, 6)
	pane := m.renderThreadPane(max(m.width, 20), paneBudget)
	if len(pane) == 0 {
		return m.readerViewport.View()
	}
	content := composeContentColumn(m.readerViewport.View(), pane, max(m.height, 3))
	return strings.Join(content, "\n")
}

func (m specDetailModel) viewReaderWithSidebar() string {
	const sidebarWidth = readerSidebarWidth
	visible := m.height
	if visible < 3 {
		visible = 3
	}

	sidebarModel := m.buildSidebar(m.readableSections(), visible)
	sidebar := make([]string, len(sidebarModel))
	for i, sl := range sidebarModel {
		sidebar[i] = sl.text
	}

	// Thread pane is drawn at the bottom of the content column. Build the
	// content column to exactly `visible` rows: prose on top, pane pinned to
	// the bottom, so the input line is always the last visible row.
	contentWidth := max(m.width-sidebarWidth-1, 20)
	// Cap the pane to roughly half the reader so prose stays visible; the pane
	// body scrolls when a thread is taller than its budget.
	paneBudget := max(visible/2, 6)
	pane := m.renderThreadPane(contentWidth, paneBudget)
	content := composeContentColumn(m.readerViewport.View(), pane, visible)

	sep := m.styles.Separator.Render(GlyphVSep)
	var out []string
	for i := 0; i < visible; i++ {
		sl := ""
		if i < len(sidebar) {
			sl = sidebar[i]
		}
		cl := ""
		if i < len(content) {
			cl = content[i]
		}
		pad := sidebarWidth - lipgloss.Width(sl)
		if pad < 0 {
			pad = 0
		}
		out = append(out, sl+strings.Repeat(" ", pad)+sep+cl)
	}
	return strings.Join(out, "\n")
}

// composeContentColumn lays out the reader's content column to exactly
// `height` rows: viewport prose fills the top and, when a thread pane is
// present, the pane is pinned to the bottom so its input line is always the
// final visible row. All entries are flattened to single rows so the row
// count is exact regardless of any margin-bearing styles.
func composeContentColumn(viewportView string, pane []string, height int) []string {
	pane = flattenLines(pane)
	if len(pane) > height {
		pane = pane[len(pane)-height:] // pane alone exceeds height: keep its tail
	}
	proseRows := height - len(pane)

	prose := flattenLines(splitLines(viewportView))
	if len(prose) > proseRows {
		prose = prose[:proseRows]
	}
	for len(prose) < proseRows {
		prose = append(prose, "")
	}

	prose = append(prose, pane...)
	if len(prose) > height {
		prose = prose[:height]
	}
	return prose
}

func (m specDetailModel) viewOverview() string {
	var b strings.Builder
	contentWidth := ContentWidth(m.width)

	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(m.styles.Muted.Render(Indent(1) + m.notice))
		b.WriteString("\n")
	}

	// ── Identity block ────────────────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(m.styles.Title.Render(fmt.Sprintf("  %s — %s", m.meta.ID, m.meta.Title)))
	b.WriteString("\n")
	var metaParts []string
	if m.meta.Author != "" {
		metaParts = append(metaParts, m.meta.Author)
	}
	if m.meta.Status != "" {
		metaParts = append(metaParts, m.meta.Status)
	}
	if m.meta.Cycle != "" {
		metaParts = append(metaParts, m.meta.Cycle)
	}
	if m.meta.Version != "" {
		metaParts = append(metaParts, "v"+m.meta.Version)
	}
	if m.meta.Updated != "" {
		metaParts = append(metaParts, "updated "+m.meta.Updated)
	}
	metaLine := truncate(strings.Join(metaParts, " · "), contentWidth)
	b.WriteString(m.styles.Muted.Render(Indent(1) + metaLine))
	b.WriteString("\n")

	// ── Build status line ──
	if m.buildLine != "" {
		b.WriteString(m.styles.Subtitle.Render(Indent(1) + truncate(m.buildLine, contentWidth)))
		b.WriteString("\n")
	}

	// ── Review status block ───────────────────────────────────────────────────
	if m.meta.Review != nil {
		b.WriteString("\n")
		var reviewStatus string
		var reviewStyle lipgloss.Style
		switch m.meta.Review.Status {
		case markdown.ReviewStatusApproved:
			reviewStatus = "approved"
			reviewStyle = m.styles.Success
		case markdown.ReviewStatusChangesRequested:
			reviewStatus = "changes requested"
			reviewStyle = m.styles.Warning
		default:
			reviewStatus = "awaiting approval"
			reviewStyle = m.styles.Warning
		}
		if len(m.meta.Review.Reviewers) > 0 {
			reviewStatus += " · " + strings.Join(m.meta.Review.Reviewers, ", ")
		}
		b.WriteString(m.styles.Subtitle.Render(Indent(1)+"Review  ") + reviewStyle.Render(reviewStatus))
		b.WriteString("\n")
	}

	// ── TL;DR block ──────────────────────────────────────────────────────────
	if preview := m.tldrPreview(contentWidth); preview != "" {
		b.WriteString("\n")
		b.WriteString(m.styles.SectionTitle.Render(Indent(1)+"TL;DR") + "\n")
		b.WriteString(preview)
		b.WriteString("\n")
	}

	// ── Decisions block ───────────────────────────────────────────────────────
	if len(m.decisions) > 0 {
		b.WriteString("\n")
		b.WriteString(m.styles.SectionTitle.Render(Indent(1)+"Decisions") + "\n")
		for _, d := range m.decisions {
			if d.Decision != "" {
				b.WriteString(m.styles.Muted.Render(fmt.Sprintf("%s%s #%d %s", Indent(2), IconActive, d.Number, truncate(d.Question, contentWidth-20))) + "\n")
				b.WriteString(m.styles.Success.Render(fmt.Sprintf("%s→ %s", Indent(3), truncate(d.Decision, contentWidth-10))) + "\n")
			} else {
				b.WriteString(m.styles.RowNormal.Render(fmt.Sprintf("%s%s #%d %s", Indent(2), IconOpen, d.Number, truncate(d.Question, contentWidth-20))) + "\n")
			}
		}
	}

	// ── Spec blocked block ───────────────────────────────────────────────────
	if m.meta.Status == pipeline.StatusBlocked {
		b.WriteString("\n")
		b.WriteString(m.styles.Error.Bold(true).Render(Indent(1)+IconBlocked+" Blocked") + "\n")
		if reason := latestEscapeReason(m.sections); reason != "" {
			b.WriteString(m.styles.Error.Render(Indent(2)+truncate(reason, contentWidth-8)) + "\n")
		}
	}

	// ── Sections list ─────────────────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(m.styles.SectionTitle.Render(Indent(1)+"Sections") + "\n")
	for _, sec := range m.sections {
		if sec.Level != 2 {
			continue
		}
		fill := IconPending
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fill = IconFilled
		}
		owner := ""
		if sec.Owner != "" && sec.Owner != "auto" {
			owner = m.styles.Muted.Render(fmt.Sprintf("  [%s]", sec.Owner))
		}
		fmt.Fprintf(&b, "%s%s %s%s\n", Indent(2), fill, sec.Slug, owner)
	}

	// ── Hint strip ────────────────────────────────────────────────────────────
	archiveHint := Hint("d", "archive")
	if m.isArchived {
		archiveHint = Hint("r", "restore")
	}
	hints := HintStrip(m.styles, archiveHint,
		Hint("o", "read sections"), Hint("e", "edit"), Hint("esc", "back"))
	b.WriteString(hints + "\n")

	lines := splitLines(b.String())
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	start := m.scroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// tldrPreview renders the TL;DR section content through the Glamour markdown
// renderer so it displays with full formatting on the detail screen. Returns
// empty string when absent or blank.
func (m specDetailModel) tldrPreview(width int) string {
	sec := markdown.FindSection(m.sections, "tl_dr")
	if sec == nil {
		return ""
	}
	text := strings.TrimSpace(sec.Content)
	if text == "" {
		return ""
	}
	renderWidth := width - 2*Gutter
	if renderWidth < 30 {
		renderWidth = 30
	}
	rendered, err := m.renderer.Render(context.Background(), text, renderWidth)
	if err != nil || strings.TrimSpace(rendered) == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range splitLines(rendered) {
		b.WriteString(Indent(1))
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m specDetailModel) readableSections() []markdown.Section {
	var out []markdown.Section
	for _, sec := range m.sections {
		if sec.Level == 2 {
			out = append(out, sec)
		}
	}
	return out
}

func (m specDetailModel) firstReadableSectionIndex() int {
	sections := m.readableSections()
	for i, sec := range sections {
		if sec.Slug == "problem_statement" {
			return i
		}
	}
	return 0
}

func (m specDetailModel) effectiveWidth() int {
	w := m.width
	if w >= 100 {
		w -= 27
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m specDetailModel) readerCacheKey(sec markdown.Section, idx, width int) string {
	return strings.Join([]string{
		m.contentHash,
		strconv.Itoa(idx),
		strconv.Itoa(width),
		strconv.Itoa(len(sec.Content)),
	}, ":")
}

func (m *specDetailModel) applyReaderContent(content string) {
	m.readerContent = content
	m.readerViewport.SetContent(content)
	m.contentLines = m.readerViewport.TotalLineCount()
	if m.contentLines == 0 {
		m.contentLines = 1
	}
}

func (m *specDetailModel) setSize(w, h int) {
	oldWidth := m.effectiveWidth()
	m.width = w
	m.height = h
	m.readerViewport.Width = m.effectiveWidth()
	m.readerViewport.Height = max(h, 3)
	if m.readerMode && m.effectiveWidth() != oldWidth {
		// Width changed — cached renders are invalid at new width.
		m.readerCache = make(map[string]string)
		m.cancelRender()
	}
	// Clamp overview scroll to new bounds.
	if mx := m.maxScroll(); m.scroll > mx {
		m.scroll = mx
	}
}

func (m specDetailModel) maxScroll() int {
	if m.readerMode {
		mx := m.readerViewport.TotalLineCount() - m.readerViewport.Height
		if mx < 0 {
			return 0
		}
		return mx
	}
	mx := m.contentLines - max(m.height, 3)
	if mx < 0 {
		return 0
	}
	return mx
}

func (m specDetailModel) estimateContentLines() int {
	if m.meta == nil {
		return 1
	}
	// Identity: blank + title + compact meta line
	lines := 3

	// Review block: blank + 1 line
	if m.meta.Review != nil {
		lines += 2
	}

	// TL;DR block: blank + header + rendered content lines
	if sec := markdown.FindSection(m.sections, "tl_dr"); sec != nil {
		if text := strings.TrimSpace(sec.Content); text != "" {
			n := len(strings.Split(text, "\n"))
			lines += 2 + n + 2 // raw lines + padding from renderer
		}
	}

	// Decisions block: blank + header + one line per entry (resolved gets an extra line)
	if len(m.decisions) > 0 {
		lines += 2
		for _, d := range m.decisions {
			lines++
			if d.Decision != "" {
				lines++
			}
		}
	}

	// Spec blocked block: blank + header + optional reason line
	if m.meta.Status == pipeline.StatusBlocked {
		lines += 2
		if latestEscapeReason(m.sections) != "" {
			lines++
		}
	}

	// Sections list: blank + header + one line per level-2 section
	lines += 2
	for _, sec := range m.sections {
		if sec.Level == 2 {
			lines++
		}
	}

	// Hint strip
	lines++

	return lines
}

// latestEscapeReason parses the most recent entry from the escape hatch log
// section and returns the reason text. Entries have the form:
//
//   - **2026-05-29** (user): Blocked from `stage`. Reason: the reason text
//
// Returns an empty string when the section is absent or contains no entries.
var escapeReasonRe = regexp.MustCompile(`(?i)Reason:\s*(.+)$`)

func latestEscapeReason(sections []markdown.Section) string {
	s := markdown.FindSection(sections, "escape_hatch_log")
	if s == nil {
		return ""
	}
	// Walk lines in reverse to find the most recent entry.
	lines := strings.Split(s.Content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := escapeReasonRe.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// ── Data Fetching ─────────────────────────────────────────────────────────────

// specFilePath resolves the on-disk path for this spec in the specs-repo clone,
// checking root, triage/, then archive/. Returns "" when unresolved.
func (m specDetailModel) specFilePath() string {
	if m.rc.SpecsRepoDir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(m.rc.SpecsRepoDir, m.specID+".md"),
		filepath.Join(m.rc.SpecsRepoDir, "triage", m.specID+".md"),
		filepath.Join(m.rc.SpecsRepoDir, config.ArchiveDir(m.rc.Team), m.specID+".md"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// watchPaths returns the files the reader should observe for live refresh:
// the spec markdown and its thread sidecar. Returns nil when the spec's
// on-disk location cannot be resolved (nothing to watch).
func (m specDetailModel) watchPaths() []string {
	path := m.specFilePath()
	if path == "" {
		return nil
	}
	sidecar := thread.NewSidecarStore(filepath.Dir(path)).SidecarPath(m.specID)
	return []string{path, sidecar}
}

func (m specDetailModel) fetchData() tea.Cmd {
	rc := m.rc
	specID := m.specID
	db := m.db
	return func() tea.Msg {
		if rc.SpecsRepoDir == "" {
			return specDetailDataMsg{Err: fmt.Errorf("specs repo not configured")}
		}
		path := filepath.Join(rc.SpecsRepoDir, specID+".md")
		isArchived := false
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(rc.SpecsRepoDir, "triage", specID+".md")
			if _, err := os.Stat(path); err != nil {
				// Check archive
				archDir := config.ArchiveDir(rc.Team)
				path = filepath.Join(rc.SpecsRepoDir, archDir, specID+".md")
				if _, err := os.Stat(path); err != nil {
					return specDetailDataMsg{Err: fmt.Errorf("spec %s not found", specID)}
				}
				isArchived = true
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return specDetailDataMsg{Err: err}
		}
		content := string(data)
		meta, err := markdown.ParseMeta(content)
		if err != nil {
			return specDetailDataMsg{Err: err}
		}
		decisions, _ := markdown.ParseDecisionLog(content)
		// Threads are a best-effort sidecar load: a parse error must never
		// block reading the spec.
		threads, _ := thread.NewSidecarStore(filepath.Dir(path)).List(specID)
		return specDetailDataMsg{
			Meta:      meta,
			Sections:  markdown.ExtractSections(content),
			Decisions: decisions,
			Threads:   threads,
			Hash:      contentHash(data),
			Archived:  isArchived,
			BuildLine: buildStatusLine(db, specID, content),
		}
	}
}

// buildStatusLine renders the compact build status for the spec-detail header
// from the persisted build session and the spec's acceptance criteria. It
// returns an empty string when no session exists (or db is nil).
func buildStatusLine(db *store.DB, specID, content string) string {
	if db == nil {
		return ""
	}
	session, err := build.LoadSession(db, specID)
	if err != nil || session == nil || len(session.Steps) == 0 {
		return ""
	}

	var line string
	if session.IsComplete() {
		line = fmt.Sprintf("Build: complete (%d/%d steps)", len(session.Steps), len(session.Steps))
	} else if step := session.CurrentPRStep(); step != nil {
		line = fmt.Sprintf("Build: step %d/%d — [%s] %s",
			step.Number, len(session.Steps), step.Repo, step.Description)
	}

	if total, checked := acceptanceCounts(content); total > 0 {
		line += fmt.Sprintf(" · ACs %d/%d", checked, total)
	}
	return line
}

// acceptanceCounts counts checked and total acceptance-criteria checkboxes in
// the spec's acceptance_criteria section.
func acceptanceCounts(content string) (total, checked int) {
	sections := markdown.ExtractSections(markdown.Body(content))
	ac := markdown.FindSection(sections, "acceptance_criteria")
	if ac == nil {
		return 0, 0
	}
	for _, line := range strings.Split(ac.Content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- [ ]"):
			total++
		case strings.HasPrefix(trimmed, "- [x]"), strings.HasPrefix(trimmed, "- [X]"):
			total++
			checked++
		}
	}
	return total, checked
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
