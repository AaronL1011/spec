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
	loaded  bool // true once at least one fetch has succeeded
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
			// Keep cached data after the first successful load; degrade gracefully.
			if !m.loaded {
				m.err = msg.Err
			}
			return m, nil
		}
		m.stages = msg.Stages
		m.err = nil
		m.loaded = true
		// Start on the first non-empty stage so the cursor is immediately
		// on a selectable spec, not an empty "—" placeholder.
		if first := m.nextNonEmptyStage(-1); first >= 0 {
			m.stageIdx = first
			m.specIdx = 0
		} else {
			m.clampCursor()
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.specIdx > 0 {
				m.specIdx--
			} else if prev := m.prevNonEmptyStage(m.stageIdx); prev >= 0 {
				// At top of current stage — wrap to last spec of previous stage.
				m.stageIdx = prev
				m.specIdx = len(m.stages[prev].Specs) - 1
			}
		case key.Matches(msg, m.keys.Down):
			specCount := m.currentStageSpecCount()
			if m.specIdx < specCount-1 {
				m.specIdx++
			} else if next := m.nextNonEmptyStage(m.stageIdx); next >= 0 {
				// At bottom of current stage — wrap to first spec of next stage.
				m.stageIdx = next
				m.specIdx = 0
			}
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

	// Build all lines, track which line the cursor maps to.
	var allLines []string
	cursorLine := 0

	for si, stage := range m.stages {
		isActiveStage := si == m.stageIdx

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

		// Blank separator between stages (not before first).
		if si > 0 {
			allLines = append(allLines, "")
		}
		allLines = append(allLines, header)

		if len(stage.Specs) == 0 {
			allLines = append(allLines, m.styles.Muted.Render("    —"))
		} else {
			for ri, spec := range stage.Specs {
				selected := isActiveStage && ri == m.specIdx
				if selected {
					cursorLine = len(allLines)
				}
				allLines = append(allLines, m.renderPipelineRow(spec, selected))
			}
		}
	}

	// Scroll so the selected row is visible.
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	start, end := scrollWindowAround(cursorLine, len(allLines), visible)

	var b strings.Builder
	for _, l := range allLines[start:end] {
		b.WriteString(l)
		b.WriteString("\n")
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
		return m.styles.RowSelected.Render(line)
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

// currentStageSpecCount returns the spec count for the active stage.
func (m pipelineModel) currentStageSpecCount() int {
	if m.stageIdx < len(m.stages) {
		return len(m.stages[m.stageIdx].Specs)
	}
	return 0
}

// nextNonEmptyStage returns the index of the next stage that has specs,
// or -1 if none.
func (m pipelineModel) nextNonEmptyStage(from int) int {
	for i := from + 1; i < len(m.stages); i++ {
		if len(m.stages[i].Specs) > 0 {
			return i
		}
	}
	return -1
}

// prevNonEmptyStage returns the index of the previous stage that has specs,
// or -1 if none.
func (m pipelineModel) prevNonEmptyStage(from int) int {
	for i := from - 1; i >= 0; i-- {
		if len(m.stages[i].Specs) > 0 {
			return i
		}
	}
	return -1
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
