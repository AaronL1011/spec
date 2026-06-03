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
		// Preserve the currently selected spec ID so we can restore navigation
		// position after refresh, instead of resetting to the top.
		savedSelected := m.selectedSpecID()
		savedStage := m.stageIdx

		m.stages = msg.Stages
		m.err = nil
		m.loaded = true

		// Try to restore the previously selected spec by ID.
		restored := false
		if savedSelected != "" {
			for si, st := range m.stages {
				for ri, sp := range st.Specs {
					if sp.ID == savedSelected {
						m.stageIdx = si
						m.specIdx = ri
						restored = true
						break
					}
				}
				if restored {
					break
				}
			}
		}

		if !restored {
			// Try to keep the same stage index if it's still in range and has specs.
			if savedStage >= 0 && savedStage < len(m.stages) && len(m.stages[savedStage].Specs) > 0 {
				m.stageIdx = savedStage
				m.specIdx = 0
			} else if first := m.nextNonEmptyStage(-1); first >= 0 {
				// Fall back to the first non-empty stage.
				m.stageIdx = first
				m.specIdx = 0
			} else {
				m.clampCursor()
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.stepUp()
		case key.Matches(msg, m.keys.Down):
			m.stepDown()
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

// stepUp moves the selection to the previous spec, wrapping to the last spec
// of the previous non-empty stage at a stage boundary. Shared by the Up key
// and the mouse wheel so keyboard and wheel navigation stay identical.
func (m *pipelineModel) stepUp() {
	if m.specIdx > 0 {
		m.specIdx--
	} else if prev := m.prevNonEmptyStage(m.stageIdx); prev >= 0 {
		m.stageIdx = prev
		m.specIdx = len(m.stages[prev].Specs) - 1
	}
}

// stepDown moves the selection to the next spec, wrapping to the first spec of
// the next non-empty stage at a stage boundary.
func (m *pipelineModel) stepDown() {
	if m.specIdx < m.currentStageSpecCount()-1 {
		m.specIdx++
	} else if next := m.nextNonEmptyStage(m.stageIdx); next >= 0 {
		m.stageIdx = next
		m.specIdx = 0
	}
}

// pipeLine is one rendered pipeline line. stageIdx/specIdx index a selectable
// spec row, or are -1 for stage headers, blank separators, and empty markers.
type pipeLine struct {
	text     string
	stageIdx int
	specIdx  int
}

// layoutLines builds the full ordered line model: stage headers, blank
// separators, empty-stage markers, and spec rows. view() and clickRow() both
// derive from this so the drawn layout and the click geometry cannot drift.
func (m pipelineModel) layoutLines() []pipeLine {
	var lines []pipeLine
	for si, stage := range m.stages {
		isActiveStage := si == m.stageIdx

		// Stage glyph is derived from stage POSITION (mono-width set), not the
		// emoji stored in config — keeps the pipeline visually uniform.
		icon := StageIconAt(si)
		countStr := fmt.Sprintf(" %d", len(stage.Specs))

		var header string
		if isActiveStage {
			label := m.styles.Accent.Bold(true).Render(fmt.Sprintf(" %s %s", icon, stage.Name))
			header = label
		} else {
			header = m.styles.Subtitle.Render(fmt.Sprintf(" %s %s", icon, stage.Name))
		}
		header += m.styles.Muted.Render(countStr)

		if stage.Owner != "" {
			header += m.styles.Muted.Render(fmt.Sprintf("  (%s)", stage.Owner))
		}

		// Blank separator between stages (not before first).
		if si > 0 {
			lines = append(lines, pipeLine{stageIdx: -1, specIdx: -1})
		}
		lines = append(lines, pipeLine{text: header, stageIdx: -1, specIdx: -1})

		if len(stage.Specs) == 0 {
			lines = append(lines, pipeLine{text: m.styles.Muted.Render(Indent(2) + "—"), stageIdx: -1, specIdx: -1})
		} else {
			for ri, spec := range stage.Specs {
				selected := isActiveStage && ri == m.specIdx
				lines = append(lines, pipeLine{text: m.renderPipelineRow(spec, selected), stageIdx: si, specIdx: ri})
			}
		}
	}
	return lines
}

// cursorLineIndex returns the line index of the selected spec row, or 0.
func (m pipelineModel) cursorLineIndex(lines []pipeLine) int {
	for i, l := range lines {
		if l.stageIdx == m.stageIdx && l.specIdx == m.specIdx {
			return i
		}
	}
	return 0
}

// scrollBounds returns the visible [start,end) window of the line model,
// centred on the selected spec row. Shared by view() and clickRow().
func (m pipelineModel) scrollBounds(lines []pipeLine) (start, end int) {
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	return scrollWindowAround(m.cursorLineIndex(lines), len(lines), visible)
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

	lines := m.layoutLines()
	start, end := m.scrollBounds(lines)

	var b strings.Builder
	for _, l := range lines[start:end] {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	return b.String()
}

// clickRow maps a content-local row y to a pipeline spec and selects it.
func (m *pipelineModel) clickRow(y int) clickResult {
	lines := m.layoutLines()
	start, end := m.scrollBounds(lines)
	li := start + y
	if y < 0 || li < start || li >= end || li >= len(lines) {
		return clickMissed
	}
	ln := lines[li]
	if ln.stageIdx < 0 {
		return clickMissed // stage header, separator, or empty marker
	}
	if ln.stageIdx == m.stageIdx && ln.specIdx == m.specIdx {
		return clickActivated
	}
	m.stageIdx = ln.stageIdx
	m.specIdx = ln.specIdx
	return clickSelected
}

// wheelRows moves the pipeline selection by delta rows (negative = up),
// reusing the same stage-aware stepping as the keyboard.
func (m *pipelineModel) wheelRows(delta int) {
	step := m.stepDown
	if delta < 0 {
		step = m.stepUp
		delta = -delta
	}
	for range delta {
		step()
	}
}

func (m pipelineModel) renderPipelineRow(spec pipelineSpec, selected bool) string {
	contentWidth := ContentWidth(m.width)

	idStr := fmt.Sprintf("%-11s", spec.ID)
	titleMax := contentWidth - 14
	if titleMax < 10 {
		titleMax = 10
	}
	title := truncate(spec.Title, titleMax)

	line := fmt.Sprintf("%s%s %s", Indent(2), idStr, title)

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

func loadPipelineData(ctx context.Context, rc *config.ResolvedConfig) ([]pipelineStage, error) {
	if rc.SpecsRepoDir == "" {
		return nil, nil
	}

	// Fetch remote changes (TTL-gated) so a refresh reflects teammates' pushes,
	// not just stale local files. A fetch failure is non-fatal: fall through to
	// read the cached local tree and report the error as a stale-data signal.
	syncErr := syncSpecsRepo(ctx, rc)

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
			Specs: specsByStage[sc.Name],
		})
	}

	return stages, syncErr
}
