package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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

	meta      *markdown.SpecMeta
	sections  []markdown.Section
	decisions []markdown.DecisionEntry
	loading   bool
	err       error
	scroll    int

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
		m.meta = msg.Meta
		m.sections = msg.Sections
		m.decisions = msg.Decisions
		m.err = nil
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.scroll > 0 {
				m.scroll--
			}
		case key.Matches(msg, m.keys.Down):
			m.scroll++
		case key.Matches(msg, m.keys.Back):
			return m, func() tea.Msg { return navigateBackMsg{} }
		}
	}
	return m, nil
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

	// Apply scrolling
	lines := splitLines(b.String())
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	visibleLines := m.height - 2
	if visibleLines < 3 {
		visibleLines = 3
	}
	end := m.scroll + visibleLines
	if end > len(lines) {
		end = len(lines)
	}
	if m.scroll < end {
		lines = lines[m.scroll:end]
	}

	return strings.Join(lines, "\n")
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
