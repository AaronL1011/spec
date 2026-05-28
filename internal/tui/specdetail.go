package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// ── Messages ──────────────────────────────────────────────────────────────────

type specDetailDataMsg struct {
	Meta      *markdown.SpecMeta
	Sections  []markdown.Section
	Decisions []markdown.DecisionEntry
	Hash      string
	Archived  bool
	Err       error
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

	// Spec data
	meta        *markdown.SpecMeta
	sections    []markdown.Section
	decisions   []markdown.DecisionEntry
	loading     bool
	err         error
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

	// Render lifecycle — single-flight + pending coalesce
	renderInFlight bool
	renderGen      uint64
	activeCacheKey string
	pendingRequest *pendingRenderRequest

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
		rc:             rc,
		specID:         specID,
		loading:        true,
		styles:         styles,
		keys:           keys,
		theme:          theme,
		readerCache:    make(map[string]string),
		renderer:       NewGlamourRenderer(theme),
		readerViewport: vp,
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
	case tea.KeyMsg:
		if m.readerMode {
			return m.updateReader(msg)
		}
		return m.updateOverview(msg)
	}
	return m, nil
}

func (m specDetailModel) handleDataMsg(msg specDetailDataMsg) (specDetailModel, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}
	// No change — skip re-render.
	if msg.Hash != "" && msg.Hash == m.contentHash && m.meta != nil {
		m.err = nil
		return m, nil
	}
	wasReading := m.readerMode
	secIdx := m.sectionIdx
	m.meta = msg.Meta
	m.sections = msg.Sections
	m.decisions = msg.Decisions
	m.err = nil
	m.contentHash = msg.Hash
	m.isArchived = msg.Archived
	m.readerCache = make(map[string]string)
	m.contentLines = m.estimateContentLines()
	if wasReading {
		m.readerMode = true
		m.sectionIdx = secIdx
		if sections := m.readableSections(); m.sectionIdx >= len(sections) {
			m.sectionIdx = max(0, len(sections)-1)
		}
		m.cancelRender()
		return m.requestCurrentSectionRender()
	}
	m.scroll = 0
	return m, nil
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
	m.readerErr = nil
	return m, nil
}

// ── Key Handling ──────────────────────────────────────────────────────────────

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
	switch {
	case key.Matches(msg, m.keys.Up):
		m.readerViewport.ScrollUp(1)
	case key.Matches(msg, m.keys.Down):
		m.readerViewport.ScrollDown(1)
	case msg.Type == tea.KeyPgUp:
		m.readerViewport.PageUp()
	case msg.Type == tea.KeyPgDown:
		m.readerViewport.PageDown()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "n":
		return m.withSection(m.sectionIdx + 1)
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "p":
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
	b.WriteString(styles.Title.Render(fmt.Sprintf("  %s", heading)))
	b.WriteString("\n")
	if owner != "" && owner != "auto" {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  [%s]", owner)))
		b.WriteString("\n")
	}
	sepWidth := width - 4
	if sepWidth < 10 {
		sepWidth = 10
	}
	b.WriteString(styles.Separator.Render(strings.Repeat("─", sepWidth)))
	b.WriteString("\n\n")

	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		b.WriteString(styles.Muted.Render("  (empty section)"))
		b.WriteString("\n")
	} else {
		rendered, err := renderer.Render(ctx, trimmed, width-2)
		if err != nil {
			return "", err
		}
		for _, line := range splitLines(rendered) {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	nav := fmt.Sprintf("  § %d/%d", sectionIdx+1, total)
	hints := "n next · p prev · 1-9 jump · o overview · tab switch view"
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render(nav + "  " + hints))
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
	// Blank while first render is in-flight — spinner in status bar is the indicator.
	if m.readerContent == "" {
		return ""
	}
	if m.width >= 100 {
		return m.viewReaderWithSidebar()
	}
	return m.readerViewport.View()
}

func (m specDetailModel) viewReaderWithSidebar() string {
	const sidebarWidth = 26
	visible := m.height
	if visible < 3 {
		visible = 3
	}

	sections := m.readableSections()
	var sidebar []string
	sidebar = append(sidebar, m.styles.SectionTitle.Render(" § Sections"), "")
	for i, sec := range sections {
		fill := "◻"
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fill = "◼"
		}
		line := fmt.Sprintf(" %s %d %s", fill, i+1, truncate(sec.Slug, sidebarWidth-5))
		if i == m.sectionIdx {
			line = m.styles.Accent.Bold(true).Render(line)
		} else {
			line = m.styles.Muted.Render(line)
		}
		sidebar = append(sidebar, line)
	}
	for len(sidebar) < visible {
		sidebar = append(sidebar, "")
	}
	if len(sidebar) > visible {
		ss, se := scrollWindow(m.sectionIdx+2, len(sidebar), visible)
		sidebar = sidebar[ss:se]
	}

	content := splitLines(m.readerViewport.View())
	for len(content) < visible {
		content = append(content, "")
	}
	if len(content) > visible {
		content = content[:visible]
	}

	sep := m.styles.Separator.Render("│")
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

func (m specDetailModel) viewOverview() string {
	var b strings.Builder
	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	b.WriteString("\n")
	b.WriteString(m.styles.Title.Render(fmt.Sprintf("  %s — %s", m.meta.ID, m.meta.Title)))
	b.WriteString("\n\n")
	b.WriteString(m.metaLine("Status", m.meta.Status))
	b.WriteString(m.metaLine("Author", m.meta.Author))
	if m.meta.Version != "" {
		b.WriteString(m.metaLine("Version", m.meta.Version))
	}
	if m.meta.Cycle != "" {
		b.WriteString(m.metaLine("Cycle", m.meta.Cycle))
	}
	if len(m.meta.Repos) > 0 {
		b.WriteString(m.metaLine("Repos", strings.Join(m.meta.Repos, ", ")))
	}
	b.WriteString(m.metaLine("Updated", m.meta.Updated))
	b.WriteString("\n")

	if len(m.meta.Steps) > 0 {
		b.WriteString(m.styles.SectionTitle.Render("  Build Steps") + "\n")
		for i, step := range m.meta.Steps {
			line := fmt.Sprintf("    %s %d. %s", stepIcon(step.Status), i+1, step.Description)
			if step.Repo != "" {
				line += m.styles.Muted.Render(fmt.Sprintf("  (%s)", step.Repo))
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	if len(m.decisions) > 0 {
		b.WriteString(m.styles.SectionTitle.Render("  Decisions") + "\n")
		for _, d := range m.decisions {
			dot := "○"
			if d.Decision != "" {
				dot = "●"
			}
			b.WriteString(m.styles.RowNormal.Render(fmt.Sprintf("    %s #%d %s", dot, d.Number, truncate(d.Question, contentWidth-20))) + "\n")
			if d.Decision != "" {
				b.WriteString(m.styles.Success.Render(fmt.Sprintf("      → %s", truncate(d.Decision, contentWidth-10))) + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(m.styles.SectionTitle.Render("  Sections") + "\n")
	for _, sec := range m.sections {
		if sec.Level > 3 {
			continue
		}
		indent := "    "
		if sec.Level == 3 {
			indent = "      "
		}
		fill := "◻"
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fill = "◼"
		}
		owner := ""
		if sec.Owner != "" && sec.Owner != "auto" {
			owner = m.styles.Muted.Render(fmt.Sprintf("  [%s]", sec.Owner))
		}
		fmt.Fprintf(&b, "%s%s %s%s\n", indent, fill, sec.Slug, owner)
	}
	// Contextual action hints
	var hints string
	if m.isArchived {
		hints = fmt.Sprintf("  %s archive  %s read sections  %s edit  %s back",
			m.styles.Accent.Render("r"),
			m.styles.Accent.Render("o"),
			m.styles.Accent.Render("e"),
			m.styles.Accent.Render("esc"))
	} else {
		hints = fmt.Sprintf("  %s archive  %s read sections  %s edit  %s back",
			m.styles.Accent.Render("d"),
			m.styles.Accent.Render("o"),
			m.styles.Accent.Render("e"),
			m.styles.Accent.Render("esc"))
	}
	b.WriteString(m.styles.Muted.Render(hints) + "\n")

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
	for i, sec := range m.sections {
		if sec.Level == 2 {
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

func (m specDetailModel) metaLine(label, value string) string {
	if value == "" {
		value = "—"
	}
	return fmt.Sprintf("  %s  %s\n",
		m.styles.Subtitle.Render(fmt.Sprintf("%-10s", label)),
		m.styles.RowNormal.Render(value),
	)
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

func stepIcon(status string) string {
	switch status {
	case "done":
		return "✅"
	case "in_progress", "active":
		return "🔧"
	case "blocked":
		return "🚫"
	default:
		return "○"
	}
}

func (m specDetailModel) estimateContentLines() int {
	if m.meta == nil {
		return 1
	}
	lines := 8 // title + meta block
	if m.meta.Version != "" {
		lines++
	}
	if m.meta.Cycle != "" {
		lines++
	}
	if len(m.meta.Repos) > 0 {
		lines++
	}
	if len(m.meta.Steps) > 0 {
		lines += 2 + len(m.meta.Steps)
	}
	if len(m.decisions) > 0 {
		lines += 2
		for _, d := range m.decisions {
			lines++
			if d.Decision != "" {
				lines++
			}
		}
	}
	lines += 2
	for _, sec := range m.sections {
		if sec.Level <= 3 {
			lines++
		}
	}
	return lines
}

// ── Data Fetching ─────────────────────────────────────────────────────────────

func (m specDetailModel) fetchData() tea.Cmd {
	rc := m.rc
	specID := m.specID
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
		return specDetailDataMsg{
			Meta:      meta,
			Sections:  markdown.ExtractSections(content),
			Decisions: decisions,
			Hash:      contentHash(data),
			Archived:  isArchived,
		}
	}
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
