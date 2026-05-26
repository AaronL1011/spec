package tui

import (
	"context"
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

// pipelineDataMsg carries loaded pipeline specs.
type pipelineDataMsg struct {
	Stages []pipelineStage
	Err    error
}

type pipelineStage struct {
	Name  string
	Owner string
	Icon  string
	Specs []pipelineSpec
}

type pipelineSpec struct {
	ID      string
	Title   string
	Updated string
}

// pipelineModel shows all specs grouped by pipeline stage.
type pipelineModel struct {
	rc *config.ResolvedConfig

	stages  []pipelineStage
	loading bool
	err     error

	// Navigation: which stage column, which spec row within it.
	stageIdx int
	specIdx  int

	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newPipeline(rc *config.ResolvedConfig, styles Styles, keys KeyMap) pipelineModel {
	return pipelineModel{
		rc:      rc,
		loading: true,
		styles:  styles,
		keys:    keys,
	}
}

func (m pipelineModel) init() tea.Cmd {
	return m.fetchData()
}

func (m pipelineModel) update(msg tea.Msg) (pipelineModel, tea.Cmd) {
	switch msg := msg.(type) {
	case pipelineDataMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.stages = msg.Stages
		m.err = nil
		m.clampCursor()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.specIdx > 0 {
				m.specIdx--
			}
		case key.Matches(msg, m.keys.Down):
			m.specIdx++
			m.clampCursor()
		case key.Matches(msg, key.NewBinding(key.WithKeys("left", "h"))):
			if m.stageIdx > 0 {
				m.stageIdx--
				m.specIdx = 0
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("right", "l"))):
			if m.stageIdx < len(m.stages)-1 {
				m.stageIdx++
				m.specIdx = 0
			}
		}
	}
	return m, nil
}

func (m pipelineModel) view() string {
	if m.loading {
		return m.styles.Muted.Render("  Loading pipeline…")
	}
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("  Error: %v", m.err))
	}
	if len(m.stages) == 0 {
		return m.styles.Muted.Render("  No pipeline stages configured")
	}

	var b strings.Builder

	for si, stage := range m.stages {
		isActiveStage := si == m.stageIdx

		// Stage header: icon + name + count
		icon := stage.Icon
		if icon == "" {
			icon = "○"
		}
		countStr := fmt.Sprintf(" %d", len(stage.Specs))

		var header string
		if isActiveStage {
			header = m.styles.SectionTitle.Render(fmt.Sprintf(" %s %s", icon, stage.Name))
		} else {
			header = m.styles.Subtitle.Render(fmt.Sprintf(" %s %s", icon, stage.Name))
		}
		header += m.styles.Muted.Render(countStr)

		if stage.Owner != "" {
			header += m.styles.Muted.Render(fmt.Sprintf("  (%s)", stage.Owner))
		}

		b.WriteString(header)
		b.WriteString("\n")

		if len(stage.Specs) == 0 {
			b.WriteString(m.styles.Muted.Render("    —"))
			b.WriteString("\n")
		} else {
			for ri, spec := range stage.Specs {
				selected := isActiveStage && ri == m.specIdx
				b.WriteString(m.renderPipelineRow(spec, selected))
				b.WriteString("\n")
			}
		}

		// Breathing room between stages
		if si < len(m.stages)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m pipelineModel) renderPipelineRow(spec pipelineSpec, selected bool) string {
	contentWidth := m.width - 6
	if contentWidth < 30 {
		contentWidth = 30
	}

	idStr := fmt.Sprintf("%-11s", spec.ID)
	titleMax := contentWidth - 14
	if titleMax < 10 {
		titleMax = 10
	}
	title := truncate(spec.Title, titleMax)

	line := fmt.Sprintf("    %s %s", idStr, title)

	if spec.Updated != "" {
		remaining := contentWidth - lipgloss.Width(line)
		if remaining > len(spec.Updated)+2 {
			line += strings.Repeat(" ", remaining-len(spec.Updated)) + spec.Updated
		}
	}

	if selected {
		return m.styles.RowSelected.Width(m.width - 2).Render(line)
	}
	return m.styles.RowNormal.Render(line)
}

func (m pipelineModel) selectedSpecID() string {
	if m.stageIdx < len(m.stages) {
		stage := m.stages[m.stageIdx]
		if m.specIdx < len(stage.Specs) {
			return stage.Specs[m.specIdx].ID
		}
	}
	return ""
}

func (m pipelineModel) refresh() tea.Cmd {
	return m.fetchData()
}

func (m *pipelineModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *pipelineModel) clampCursor() {
	if m.stageIdx >= len(m.stages) {
		m.stageIdx = max(0, len(m.stages)-1)
	}
	if m.stageIdx < len(m.stages) {
		specCount := len(m.stages[m.stageIdx].Specs)
		if m.specIdx >= specCount {
			m.specIdx = max(0, specCount-1)
		}
	}
}

func (m pipelineModel) fetchData() tea.Cmd {
	rc := m.rc
	return func() tea.Msg {
		stages, err := loadPipelineData(context.Background(), rc)
		return pipelineDataMsg{Stages: stages, Err: err}
	}
}

func loadPipelineData(_ context.Context, rc *config.ResolvedConfig) ([]pipelineStage, error) {
	if rc.SpecsRepoDir == "" {
		return nil, nil
	}

	pl := rc.Pipeline()

	// Build a map of stage name → specs.
	specsByStage := make(map[string][]pipelineSpec)
	entries, err := os.ReadDir(rc.SpecsRepoDir)
	if err != nil {
		return nil, fmt.Errorf("reading specs dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(rc.SpecsRepoDir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		specsByStage[meta.Status] = append(specsByStage[meta.Status], pipelineSpec{
			ID:      meta.ID,
			Title:   meta.Title,
			Updated: meta.Updated,
		})
	}

	// Build ordered stage list from pipeline config.
	var stages []pipelineStage
	for _, sc := range pl.Stages {
		owner := ""
		if len(sc.Owner) > 0 {
			owner = strings.Join(sc.Owner, ", ")
		} else if sc.OwnerRole != "" {
			owner = sc.OwnerRole
		}
		stages = append(stages, pipelineStage{
			Name:  sc.Name,
			Owner: owner,
			Icon:  sc.Icon,
			Specs: specsByStage[sc.Name],
		})
	}

	return stages, nil
}
