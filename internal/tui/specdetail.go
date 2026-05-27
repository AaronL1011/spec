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
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

type specDetailDataMsg struct {
	Meta      *markdown.SpecMeta
	Sections  []markdown.Section
	Decisions []markdown.DecisionEntry
	Hash      string
	Err       error
}

type sectionRenderedMsg struct {
	SpecID       string
	SectionIdx   int
	CacheKey     string
	Gen          uint64
	Content      string
	Err          error
	RenderMillis int64
	Prefetch     bool
}

type renderRequest struct {
	Ctx        context.Context
	SpecID     string
	SectionIdx int
	CacheKey   string
	Gen        uint64
	Heading    string
	Owner      string
	Body       string
	Total      int
	Width      int
	Styles     Styles
	Prefetch   bool
}

type navigateToSpecMsg struct{ SpecID string }
type navigateBackMsg struct{}

type specDetailModel struct {
	rc     *config.ResolvedConfig
	specID string

	meta      *markdown.SpecMeta
	sections  []markdown.Section
	decisions []markdown.DecisionEntry
	loading   bool
	err       error

	// Overview scroll.
	scroll       int
	contentLines int

	// Reader state.
	readerMode     bool
	sectionIdx     int
	readerContent  string
	readerViewport viewport.Model
	readerState    readerRenderState
	readerErr      error
	readerGen      uint64
	readerCache    map[string]string
	contentHash    string

	renderInFlight  bool
	renderQueued    bool
	renderCancel    context.CancelFunc
	queuedRequest   *renderRequest
	renderQueue     chan renderRequest
	renderResult    chan sectionRenderedMsg
	renderResultCmd tea.Cmd
	renderer        Renderer

	metrics renderMetrics

	theme  Theme
	width  int
	height int
	styles Styles
	keys   KeyMap
}

type renderMetrics struct {
	total    int64
	canceled int64
	slow     int64
}

type readerRenderState int

const (
	readerIdle readerRenderState = iota
	readerPending
	readerReady
	readerFailed
)

const slowRenderThreshold = 300 * time.Millisecond

func newSpecDetail(rc *config.ResolvedConfig, specID string, styles Styles, keys KeyMap, theme Theme) specDetailModel {
	vp := viewport.New(80, 20)
	vp.KeyMap = viewport.KeyMap{}
	m := specDetailModel{
		rc:             rc,
		specID:         specID,
		loading:        true,
		styles:         styles,
		keys:           keys,
		theme:          theme,
		readerCache:    make(map[string]string),
		renderQueue:    make(chan renderRequest, 8),
		renderResult:   make(chan sectionRenderedMsg, 8),
		renderer:       NewGlamourRenderer(),
		readerViewport: vp,
	}
	m.startRenderWorker()
	m.renderResultCmd = m.awaitRenderResult()
	return m
}

func (m specDetailModel) init() tea.Cmd { return m.fetchData() }

func (m specDetailModel) update(msg tea.Msg) (specDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case specDetailDataMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		wasReading := m.readerMode
		secIdx := m.sectionIdx
		if msg.Hash != "" && msg.Hash == m.contentHash && m.meta != nil {
			m.err = nil
			return m, nil
		}
		m.meta = msg.Meta
		m.sections = msg.Sections
		m.decisions = msg.Decisions
		m.err = nil
		m.contentHash = msg.Hash
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

	case sectionRenderedMsg:
		if msg.Err == context.Canceled {
			atomic.AddInt64(&m.metrics.canceled, 1)
		}
		if msg.Prefetch {
			if msg.Err == nil && msg.SpecID == m.specID && msg.Content != "" {
				if _, ok := m.readerCache[msg.CacheKey]; !ok {
					m.readerCache[msg.CacheKey] = msg.Content
				}
			}
			if m.renderQueued && m.queuedRequest != nil {
				req := *m.queuedRequest
				m.renderQueued = false
				m.queuedRequest = nil
				return m.startRender(req)
			}
			if m.renderInFlight {
				return m, m.awaitRenderResult()
			}
			return m, nil
		}

		m.renderInFlight = false
		m.renderCancel = nil

		if msg.SpecID != m.specID || msg.Gen != m.readerGen || msg.SectionIdx != m.sectionIdx {
			if m.renderQueued && m.queuedRequest != nil {
				req := *m.queuedRequest
				m.renderQueued = false
				m.queuedRequest = nil
				return m.startRender(req)
			}
			if m.renderInFlight {
				return m, m.awaitRenderResult()
			}
			return m, nil
		}

		if msg.Err != nil {
			if msg.Err == context.Canceled {
				if m.renderQueued && m.queuedRequest != nil {
					req := *m.queuedRequest
					m.renderQueued = false
					m.queuedRequest = nil
					return m.startRender(req)
				}
				if m.readerContent != "" {
					m.readerState = readerReady
				}
				return m, nil
			}
			m.readerErr = msg.Err
			m.readerState = readerFailed
			if m.renderQueued && m.queuedRequest != nil {
				req := *m.queuedRequest
				m.renderQueued = false
				m.queuedRequest = nil
				return m.startRender(req)
			}
			return m, nil
		}

		atomic.AddInt64(&m.metrics.total, 1)
		if time.Duration(msg.RenderMillis)*time.Millisecond > slowRenderThreshold {
			atomic.AddInt64(&m.metrics.slow, 1)
		}

		m.readerCache[msg.CacheKey] = msg.Content
		m.applyReaderContent(msg.Content)
		m.readerState = readerReady
		m.readerErr = nil

		if m.renderQueued && m.queuedRequest != nil {
			req := *m.queuedRequest
			m.renderQueued = false
			m.queuedRequest = nil
			return m.startRender(req)
		}
		return m, m.prefetchAdjacentSections()

	case tea.KeyMsg:
		if m.readerMode {
			return m.updateReader(msg)
		}
		return m.updateOverview(msg)
	}
	return m, nil
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
	switch {
	case key.Matches(msg, m.keys.Up):
		m.readerViewport.LineUp(1)
	case key.Matches(msg, m.keys.Down):
		m.readerViewport.LineDown(1)
	case msg.Type == tea.KeyPgUp:
		m.readerViewport.PageUp()
	case msg.Type == tea.KeyPgDown:
		m.readerViewport.PageDown()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "n":
		return m.withNextSection()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "p":
		return m.withPrevSection()
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
	m.scroll = m.readerViewport.YOffset
	m.contentLines = m.readerViewport.TotalLineCount()
	if m.contentLines == 0 {
		m.contentLines = 1
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

func (m specDetailModel) withNextSection() (specDetailModel, tea.Cmd) {
	return m.withSection(m.sectionIdx + 1)
}
func (m specDetailModel) withPrevSection() (specDetailModel, tea.Cmd) {
	return m.withSection(m.sectionIdx - 1)
}

func (m specDetailModel) readableSections() []markdown.Section {
	return readableSectionsFrom(m.sections)
}

func readableSectionsFrom(sections []markdown.Section) []markdown.Section {
	var out []markdown.Section
	for _, sec := range sections {
		if sec.Level == 2 || sec.Level == 3 {
			out = append(out, sec)
		}
	}
	return out
}

func (m specDetailModel) firstReadableSectionIndex() int {
	sections := m.readableSections()
	for i, sec := range sections {
		if sec.Level == 2 {
			return i
		}
	}
	if len(sections) > 0 {
		return 0
	}
	return 0
}

func (m specDetailModel) effectiveWidth() int {
	w := m.width
	if w >= 100 {
		w -= 23
	}
	if w < 20 {
		w = 20
	}
	return w
}

func renderSectionContent(ctx context.Context, renderer Renderer, req renderRequest) (string, error) {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(req.Styles.Title.Render(fmt.Sprintf("  %s", req.Heading)))
	b.WriteString("\n")
	if req.Owner != "" && req.Owner != "auto" {
		b.WriteString(req.Styles.Muted.Render(fmt.Sprintf("  [%s]", req.Owner)))
		b.WriteString("\n")
	}
	sepWidth := req.Width - 4
	if sepWidth < 10 {
		sepWidth = 10
	}
	b.WriteString(req.Styles.Separator.Render(strings.Repeat("─", sepWidth)))
	b.WriteString("\n\n")

	trimmed := strings.TrimSpace(req.Body)
	if trimmed == "" {
		b.WriteString(req.Styles.Muted.Render("  (empty section)"))
		b.WriteString("\n")
	} else {
		rendered, err := renderer.Render(ctx, trimmed, req.Width-2)
		if err != nil {
			return "", err
		}
		for _, line := range splitLines(rendered) {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	nav := fmt.Sprintf("  § %d/%d", req.SectionIdx+1, req.Total)
	hints := "n next · p prev · 1-9 jump · o overview"
	b.WriteString("\n")
	b.WriteString(req.Styles.Muted.Render(nav + "  " + hints))
	return strings.TrimRight(b.String(), "\n"), nil
}

func (m specDetailModel) requestCurrentSectionRender() (specDetailModel, tea.Cmd) {
	sections := m.readableSections()
	if m.sectionIdx >= len(sections) {
		m.applyReaderContent("  (no sections)")
		m.readerState = readerReady
		return m, nil
	}

	effWidth := m.effectiveWidth()
	sec := sections[m.sectionIdx]
	cacheKey := m.readerCacheKey(sec, m.sectionIdx, effWidth)
	if content, ok := m.readerCache[cacheKey]; ok {
		m.applyReaderContent(content)
		m.readerState = readerReady
		m.readerErr = nil
		return m, nil
	}

	m.readerState = readerPending
	m.readerErr = nil

	req := renderRequest{
		SpecID:     m.specID,
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
		m.renderQueued = true
		m.queuedRequest = &req
		if m.renderCancel != nil {
			m.renderCancel()
		}
		return m, nil
	}
	return m.startRender(req)
}

func (m specDetailModel) startRender(req renderRequest) (specDetailModel, tea.Cmd) {
	m.cancelRender()
	ctx, cancel := context.WithCancel(context.Background())
	m.readerGen++
	req.Gen = m.readerGen
	req.Ctx = ctx

	m.renderCancel = cancel
	m.renderInFlight = true

	select {
	case m.renderQueue <- req:
	default:
		go func() { m.renderQueue <- req }()
	}
	return m, m.renderResultCmd
}

func (m specDetailModel) prefetchAdjacentSections() tea.Cmd {
	if !m.readerMode || m.renderInFlight {
		return nil
	}
	sections := m.readableSections()
	if len(sections) == 0 {
		return nil
	}
	effWidth := m.effectiveWidth()
	mkReq := func(idx int) *renderRequest {
		if idx < 0 || idx >= len(sections) {
			return nil
		}
		sec := sections[idx]
		key := m.readerCacheKey(sec, idx, effWidth)
		if _, ok := m.readerCache[key]; ok {
			return nil
		}
		return &renderRequest{
			SpecID:     m.specID,
			SectionIdx: idx,
			CacheKey:   key,
			Heading:    sec.Heading,
			Owner:      sec.Owner,
			Body:       sec.Content,
			Total:      len(sections),
			Width:      effWidth,
			Styles:     m.styles,
			Prefetch:   true,
		}
	}
	left := mkReq(m.sectionIdx - 1)
	right := mkReq(m.sectionIdx + 1)
	if left == nil && right == nil {
		return nil
	}
	return func() tea.Msg {
		if left != nil {
			m.enqueuePrefetch(*left)
		}
		if right != nil {
			m.enqueuePrefetch(*right)
		}
		return nil
	}
}

func (m specDetailModel) enqueuePrefetch(req renderRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req.Ctx = ctx
	req.Gen = m.readerGen
	req.Prefetch = true
	select {
	case m.renderQueue <- req:
	default:
	}
}

func (m specDetailModel) readerCacheKey(sec markdown.Section, idx, width int) string {
	return strings.Join([]string{m.contentHash, strconv.Itoa(idx), strconv.Itoa(width), strconv.Itoa(len(sec.Content))}, ":")
}

func (m *specDetailModel) applyReaderContent(content string) {
	m.readerContent = content
	m.readerViewport.SetContent(content)
	m.scroll = m.readerViewport.YOffset
	m.contentLines = m.readerViewport.TotalLineCount()
	if m.contentLines == 0 {
		m.contentLines = 1
	}
}

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
	if m.readerState == readerFailed {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.readerErr))
	}
	if m.readerContent == "" {
		return m.styles.Muted.Render("  (no content)")
	}
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	if m.width >= 100 {
		return m.viewReaderWithSidebar(visible)
	}
	return m.readerViewport.View()
}

func (m specDetailModel) viewReaderWithSidebar(visible int) string {
	const sidebarWidth = 22
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
	if m.meta == nil {
		return m.styles.Muted.Render("  Spec not found")
	}
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
		b.WriteString(m.styles.SectionTitle.Render("  Build Steps"))
		b.WriteString("\n")
		for i, step := range m.meta.Steps {
			icon := stepIcon(step.Status)
			line := fmt.Sprintf("    %s %d. %s", icon, i+1, step.Description)
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
			resolved := "○"
			if d.Decision != "" {
				resolved = "●"
			}
			b.WriteString(m.styles.RowNormal.Render(fmt.Sprintf("    %s #%d %s", resolved, d.Number, truncate(d.Question, contentWidth-20))) + "\n")
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
		fillIcon := "◻"
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fillIcon = "◼"
		}
		owner := ""
		if sec.Owner != "" && sec.Owner != "auto" {
			owner = m.styles.Muted.Render(fmt.Sprintf("  [%s]", sec.Owner))
		}
		b.WriteString(fmt.Sprintf("%s%s %s%s\n", indent, fillIcon, sec.Slug, owner))
	}
	b.WriteString("\n" + m.styles.Muted.Render("  o to read sections") + "\n")
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

func (m specDetailModel) metaLine(label, value string) string {
	if value == "" {
		value = "—"
	}
	return fmt.Sprintf("  %s  %s\n", m.styles.Subtitle.Render(fmt.Sprintf("%-10s", label)), m.styles.RowNormal.Render(value))
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

func (m *specDetailModel) setSize(w, h int) {
	oldWidth := m.effectiveWidth()
	m.width = w
	m.height = h
	vh := h
	if vh < 3 {
		vh = 3
	}
	m.readerViewport.Width = m.effectiveWidth()
	m.readerViewport.Height = vh
	if m.readerMode && oldWidth != m.effectiveWidth() {
		m.cancelRender()
		if m.readerContent != "" {
			m.readerCache = make(map[string]string)
		}
		m.readerState = readerIdle
		m.readerGen++
	}
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
	if m.contentLines == 0 {
		return 0
	}
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	mx := m.contentLines - visible
	if mx < 0 {
		return 0
	}
	return mx
}

func (m specDetailModel) estimateContentLines() int {
	if m.meta == nil {
		return 1
	}
	lines := 5 + 3
	if m.meta.Version != "" {
		lines++
	}
	if m.meta.Cycle != "" {
		lines++
	}
	if len(m.meta.Repos) > 0 {
		lines++
	}
	lines++
	if len(m.meta.Steps) > 0 {
		lines += 1 + len(m.meta.Steps) + 1
	}
	if len(m.decisions) > 0 {
		lines++
		for _, d := range m.decisions {
			lines++
			if d.Decision != "" {
				lines++
			}
		}
		lines++
	}
	lines++
	for _, sec := range m.sections {
		if sec.Level <= 3 {
			lines++
		}
	}
	return lines + 3
}

func (m specDetailModel) fetchData() tea.Cmd {
	rc := m.rc
	specID := m.specID
	return func() tea.Msg {
		if rc.SpecsRepoDir == "" {
			return specDetailDataMsg{Err: fmt.Errorf("specs repo not configured")}
		}
		path := filepath.Join(rc.SpecsRepoDir, specID+".md")
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(rc.SpecsRepoDir, "triage", specID+".md")
			if _, err := os.Stat(path); err != nil {
				return specDetailDataMsg{Err: fmt.Errorf("spec %s not found", specID)}
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
		return specDetailDataMsg{
			Meta:      meta,
			Sections:  markdown.ExtractSections(content),
			Decisions: mustDecisions(content),
			Hash:      contentHash(data),
		}
	}
}

func mustDecisions(content string) []markdown.DecisionEntry {
	d, _ := markdown.ParseDecisionLog(content)
	return d
}

func (m *specDetailModel) startRenderWorker() {
	if m.renderQueue == nil || m.renderResult == nil || m.renderer == nil {
		return
	}
	go func(queue <-chan renderRequest, out chan<- sectionRenderedMsg, renderer Renderer) {
		for req := range queue {
			started := time.Now()
			content, err := renderSectionContent(req.Ctx, renderer, req)
			out <- sectionRenderedMsg{
				SpecID:       req.SpecID,
				SectionIdx:   req.SectionIdx,
				CacheKey:     req.CacheKey,
				Gen:          req.Gen,
				Content:      content,
				Err:          err,
				RenderMillis: time.Since(started).Milliseconds(),
				Prefetch:     req.Prefetch,
			}
		}
	}(m.renderQueue, m.renderResult, m.renderer)
}

func (m specDetailModel) awaitRenderResult() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.renderResult
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *specDetailModel) cancelRender() {
	if m.renderCancel != nil {
		m.renderCancel()
		m.renderCancel = nil
	}
	m.renderInFlight = false
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
