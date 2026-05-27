package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// specDetailDataMsg carries loaded spec detail.
type specDetailDataMsg struct {
	Meta      *markdown.SpecMeta
	Sections  []markdown.Section
	Decisions []markdown.DecisionEntry
	Err       error
}

// navigateToSpecMsg requests the app open a spec detail view.
type navigateToSpecMsg struct {
	SpecID string
}

// navigateBackMsg requests the app return to the previous view.
type navigateBackMsg struct{}

// specDetailModel shows a read-only deep-dive of a single spec.
type specDetailModel struct {
	rc     *config.ResolvedConfig
	specID string

	meta         *markdown.SpecMeta
	sections     []markdown.Section
	decisions    []markdown.DecisionEntry
	loading      bool
	err          error
	scroll       int
	contentLines int // cached line count for scroll clamping

	// Reader mode
	readerMode  bool
	sectionIdx  int      // which section is being read
	readerLines []string // rendered lines for current section

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newSpecDetail(rc *config.ResolvedConfig, specID string, styles Styles, keys KeyMap) specDetailModel {
	return specDetailModel{
		rc:      rc,
		specID:  specID,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m specDetailModel) init() tea.Cmd {
	return m.fetchData()
}

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

		m.meta = msg.Meta
		m.sections = msg.Sections
		m.decisions = msg.Decisions
		m.err = nil
		m.contentLines = m.estimateContentLines()

		// Preserve reader mode across data refreshes.
		if wasReading {
			m.readerMode = true
			m.sectionIdx = secIdx
			m = m.withRenderedSection()
			m.contentLines = len(m.readerLines)
		} else {
			m.scroll = 0
		}
		return m, nil

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
		// Enter reader mode.
		m.readerMode = true
		m.sectionIdx = 0
		m.scroll = 0
		m = m.withRenderedSection()
		m.contentLines = len(m.readerLines)
		return m, tea.ClearScreen
	case key.Matches(msg, m.keys.Back):
		return m, func() tea.Msg { return navigateBackMsg{} }
	}
	return m, nil
}

func (m specDetailModel) updateReader(msg tea.KeyMsg) (specDetailModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.scroll > 0 {
			m.scroll--
		}
	case key.Matches(msg, m.keys.Down):
		m.scroll++
		mx := len(m.readerLines) - m.height
		if mx < 0 {
			mx = 0
		}
		if m.scroll > mx {
			m.scroll = mx
		}
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "n":
		m = m.withNextSection()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "p":
		m = m.withPrevSection()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "g":
		m.scroll = 0
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "G":
		mx := len(m.readerLines) - m.height
		if mx < 0 {
			mx = 0
		}
		m.scroll = mx
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9':
		idx := int(msg.Runes[0]-'0') - 1
		m = m.withSection(idx)
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "0":
		// Jump to Decision Log.
		for i, sec := range m.readableSections() {
			if sec.Slug == "decision_log" {
				m = m.withSection(i)
				break
			}
		}
	case key.Matches(msg, m.keys.Open):
		// Back to overview.
		m.readerMode = false
		m.scroll = 0
		m.contentLines = m.estimateContentLines()
		return m, tea.ClearScreen
	case key.Matches(msg, m.keys.Back):
		// Esc in reader goes to overview, not back to list.
		m.readerMode = false
		m.scroll = 0
		m.contentLines = m.estimateContentLines()
		return m, tea.ClearScreen
	}
	return m, nil
}

func (m specDetailModel) withSection(idx int) specDetailModel {
	sections := m.readableSections()
	if idx >= 0 && idx < len(sections) {
		m.sectionIdx = idx
		m.scroll = 0
		m = m.withRenderedSection()
		m.contentLines = len(m.readerLines)
	}
	return m
}

func (m specDetailModel) withNextSection() specDetailModel {
	return m.withSection(m.sectionIdx + 1)
}

func (m specDetailModel) withPrevSection() specDetailModel {
	return m.withSection(m.sectionIdx - 1)
}

// readableSections returns sections with level 2-3 (the main spec sections).
func (m specDetailModel) readableSections() []markdown.Section {
	return readableSectionsFrom(m.sections)
}

// readableSectionsFrom filters sections to level 2-3 (usable outside the model).
func readableSectionsFrom(sections []markdown.Section) []markdown.Section {
	var out []markdown.Section
	for _, sec := range sections {
		if sec.Level <= 3 {
			out = append(out, sec)
		}
	}
	return out
}

// effectiveWidth returns the content width for the reader, accounting for sidebar.
func (m specDetailModel) effectiveWidth() int {
	w := m.width
	if w >= 100 {
		w = w - 23 // sidebar(22) + separator(1)
	}
	return w
}

// renderSectionLines produces the display lines for a single section.
func renderSectionLines(renderer *mdRenderer, styles Styles, heading, owner, content string, idx, total, effWidth int) []string {
	var lines []string

	// Section header.
	lines = append(lines, "")
	lines = append(lines, styles.Title.Render(fmt.Sprintf("  %s", heading)))
	if owner != "" && owner != "auto" {
		lines = append(lines, styles.Muted.Render(fmt.Sprintf("  [%s]", owner)))
	}
	sepWidth := effWidth - 4
	if sepWidth < 10 {
		sepWidth = 10
	}
	lines = append(lines, styles.Separator.Render(strings.Repeat("─", sepWidth)))
	lines = append(lines, "")

	// Render content via glamour.
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		lines = append(lines, styles.Muted.Render("  (empty section)"))
	} else {
		for _, l := range renderer.render(trimmed) {
			lines = append(lines, "  "+l)
		}
	}

	// Footer with nav hints.
	lines = append(lines, "")
	nav := fmt.Sprintf("  § %d/%d", idx+1, total)
	hints := "n next · p prev · 1-9 jump · o overview"
	lines = append(lines, styles.Muted.Render(nav+"  "+hints))

	return lines
}

// withRenderedSection returns m with readerLines populated for the current section.
// Uses the pre-rendered cache when available; falls back to synchronous render.
func (m specDetailModel) withRenderedSection() specDetailModel {
	sections := m.readableSections()
	if m.sectionIdx >= len(sections) {
		m.readerLines = []string{"  (no sections)"}
		return m
	}

	effWidth := m.effectiveWidth()
	renderer := newMDRenderer(ResolveTheme(""), effWidth)
	sec := sections[m.sectionIdx]
	m.readerLines = renderSectionLines(renderer, m.styles, sec.Heading, sec.Owner, sec.Content, m.sectionIdx, len(sections), effWidth)
	return m
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

// viewReader renders the full content of the current section.
func (m specDetailModel) viewReader() string {
	// Defensive: if reader mode is on but lines weren't rendered
	// (e.g., value-receiver copy issue), render now.
	if len(m.readerLines) == 0 && len(m.sections) > 0 {
		m = m.withRenderedSection()
	}
	if len(m.readerLines) == 0 {
		return m.styles.Muted.Render("  (no content)")
	}

	visible := m.height
	if visible < 3 {
		visible = 3
	}

	// Wide terminals: show section sidebar.
	if m.width >= 100 {
		return m.viewReaderWithSidebar(visible)
	}

	start := m.scroll
	if start > len(m.readerLines) {
		start = len(m.readerLines)
	}
	end := start + visible
	if end > len(m.readerLines) {
		end = len(m.readerLines)
	}
	return strings.Join(m.readerLines[start:end], "\n")
}

// viewReaderWithSidebar renders reader mode with a section index on the left.
func (m specDetailModel) viewReaderWithSidebar(visible int) string {
	const sidebarWidth = 22

	// Build sidebar lines.
	sections := m.readableSections()
	var sidebar []string
	sidebar = append(sidebar, m.styles.SectionTitle.Render(" § Sections"))
	sidebar = append(sidebar, "")
	for i, sec := range sections {
		fill := "◻"
		if len(strings.TrimSpace(sec.Content)) > 20 {
			fill = "◼"
		}
		label := truncate(sec.Slug, sidebarWidth-5)
		line := fmt.Sprintf(" %s %d %s", fill, i+1, label)
		if i == m.sectionIdx {
			line = m.styles.Accent.Bold(true).Render(line)
		} else {
			line = m.styles.Muted.Render(line)
		}
		sidebar = append(sidebar, line)
	}

	// Pad sidebar to visible height.
	for len(sidebar) < visible {
		sidebar = append(sidebar, "")
	}
	if len(sidebar) > visible {
		// Scroll sidebar to keep current section visible.
		ss, se := scrollWindow(m.sectionIdx+2, len(sidebar), visible) // +2 for header
		sidebar = sidebar[ss:se]
	}

	// Content slice.
	start := m.scroll
	if start > len(m.readerLines) {
		start = len(m.readerLines)
	}
	end := start + visible
	if end > len(m.readerLines) {
		end = len(m.readerLines)
	}
	content := m.readerLines[start:end]
	for len(content) < visible {
		content = append(content, "")
	}

	// Compose side by side.
	sep := m.styles.Separator.Render("│")
	var out []string
	for i := range visible {
		sl := ""
		if i < len(sidebar) {
			sl = sidebar[i]
		}
		cl := ""
		if i < len(content) {
			cl = content[i]
		}
		// Pad sidebar to fixed width.
		pad := sidebarWidth - lipgloss.Width(sl)
		if pad < 0 {
			pad = 0
		}
		out = append(out, sl+strings.Repeat(" ", pad)+sep+cl)
	}
	return strings.Join(out, "\n")
}

// viewOverview renders the metadata/outline view (original detail view).
func (m specDetailModel) viewOverview() string {
	if m.meta == nil {
		return m.styles.Muted.Render("  Spec not found")
	}

	var b strings.Builder
	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Title block
	b.WriteString("\n")
	b.WriteString(m.styles.Title.Render(fmt.Sprintf("  %s — %s", m.meta.ID, m.meta.Title)))
	b.WriteString("\n\n")

	// Metadata grid
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

	// Build steps
	if len(m.meta.Steps) > 0 {
		b.WriteString(m.styles.SectionTitle.Render("  Build Steps"))
		b.WriteString("\n")
		for i, step := range m.meta.Steps {
			icon := stepIcon(step.Status)
			line := fmt.Sprintf("    %s %d. %s", icon, i+1, step.Description)
			if step.Repo != "" {
				line += m.styles.Muted.Render(fmt.Sprintf("  (%s)", step.Repo))
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Decisions
	if len(m.decisions) > 0 {
		b.WriteString(m.styles.SectionTitle.Render("  Decisions"))
		b.WriteString("\n")
		for _, d := range m.decisions {
			resolved := "○"
			if d.Decision != "" {
				resolved = "●"
			}
			line := fmt.Sprintf("    %s #%d %s", resolved, d.Number, truncate(d.Question, contentWidth-20))
			b.WriteString(m.styles.RowNormal.Render(line))
			b.WriteString("\n")
			if d.Decision != "" {
				b.WriteString(m.styles.Success.Render(fmt.Sprintf("      → %s", truncate(d.Decision, contentWidth-10))))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Section outline
	b.WriteString(m.styles.SectionTitle.Render("  Sections"))
	b.WriteString("\n")
	for _, sec := range m.sections {
		if sec.Level > 3 {
			continue // skip sub-sub-sections
		}
		indent := "    "
		if sec.Level == 3 {
			indent = "      "
		}
		contentLen := strings.TrimSpace(sec.Content)
		fillIcon := "◻"
		if len(contentLen) > 20 {
			fillIcon = "◼"
		}
		owner := ""
		if sec.Owner != "" && sec.Owner != "auto" {
			owner = m.styles.Muted.Render(fmt.Sprintf("  [%s]", sec.Owner))
		}
		b.WriteString(fmt.Sprintf("%s%s %s%s", indent, fillIcon, sec.Slug, owner))
		b.WriteString("\n")
	}

	// Reader mode hint.
	b.WriteString("\n")
	b.WriteString(m.styles.Muted.Render("  o to read sections"))
	b.WriteString("\n")

	// Apply scroll — direct viewport offset (not centered like list views).
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
	return fmt.Sprintf("  %s  %s\n",
		m.styles.Subtitle.Render(fmt.Sprintf("%-10s", label)),
		m.styles.RowNormal.Render(value),
	)
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
	m.width = w
	m.height = h
	if mx := m.maxScroll(); m.scroll > mx {
		m.scroll = mx
	}
}

// maxScroll returns the furthest scroll position that still shows content.
func (m specDetailModel) maxScroll() int {
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

// estimateContentLines counts how many lines the rendered content will produce.
func (m specDetailModel) estimateContentLines() int {
	if m.meta == nil {
		return 1
	}
	lines := 5 // title block + blank lines
	lines += 3 // status, author, updated (always present)
	if m.meta.Version != "" {
		lines++
	}
	if m.meta.Cycle != "" {
		lines++
	}
	if len(m.meta.Repos) > 0 {
		lines++
	}
	lines++ // blank after meta
	if len(m.meta.Steps) > 0 {
		lines += 1 + len(m.meta.Steps) + 1 // header + steps + blank
	}
	if len(m.decisions) > 0 {
		lines++ // header
		for _, d := range m.decisions {
			lines++ // question
			if d.Decision != "" {
				lines++ // resolution
			}
		}
		lines++ // blank
	}
	lines++ // sections header
	for _, sec := range m.sections {
		if sec.Level <= 3 {
			lines++
		}
	}
	// Generous padding — styled text can produce slightly more lines
	// than the logical count due to ANSI codes affecting width calculations.
	lines += 3
	return lines
}

func (m specDetailModel) fetchData() tea.Cmd {
	rc := m.rc
	specID := m.specID

	return func() tea.Msg {
		if rc.SpecsRepoDir == "" {
			return specDetailDataMsg{Err: fmt.Errorf("specs repo not configured")}
		}

		// Find the spec file.
		path := filepath.Join(rc.SpecsRepoDir, specID+".md")
		if _, err := os.Stat(path); err != nil {
			// Try triage/
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

		sections := markdown.ExtractSections(content)
		decisions, _ := markdown.ParseDecisionLog(content)

		return specDetailDataMsg{
			Meta:      meta,
			Sections:  sections,
			Decisions: decisions,
		}
	}
}
