package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
)

// View renders the full application.
// View returns the program's view. In Bubble Tea v2 the view is a struct that
// carries the rendered content plus declarative terminal state: the alt-screen
// flag and mouse mode that were program options in v1 now live here, so the
// in-app mouse toggle takes effect on the next render with no command.
func (a App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	if a.mouseEnabled() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// mouseEnabled reports whether mouse reporting should be requested, honouring
// the user's preference. It drives View.MouseMode each render.
func (a App) mouseEnabled() bool {
	return a.rc.User != nil && a.rc.User.Preferences.Mouse
}

// render composes the full-screen content string for the current state.
func (a App) render() string {
	if a.width == 0 {
		return "Initialising…"
	}

	a.statusBar.SetScroll(a.activeScrollInfo())
	a.dashboard.focusedSpecID = a.focusedSpecID

	// Update breadcrumb for reader mode.
	if a.showDetail && a.detail.readerMode {
		sections := a.detail.readableSections()
		if a.detail.sectionIdx < len(sections) {
			sec := sections[a.detail.sectionIdx]
			crumb := a.activeView.Label() + " › " + a.detail.specID + " › § " + sec.Slug
			// Surface open discussion as a calm awareness cue.
			if n := a.detail.totalOpenThreads(); n > 0 {
				crumb += fmt.Sprintf("  ●%d", n)
			}
			a.statusBar.SetView(crumb)
		}
	}

	header := a.header.View()
	tabs := a.tabs.View()
	statusBar := a.statusBar.View()

	lay := a.layout()

	// Help overlay covers the full terminal — skip chrome entirely.
	if a.help.visible {
		return a.help.view()
	}

	var content string
	switch {
	case a.search.visible:
		content = a.search.view()
	case a.standup.visible:
		content = a.standup.view()
	case a.intake.active:
		content = a.renderIntakeForm()
	case a.revert.active:
		content = renderRevert(a.revert, a.styles)
	case a.triageEdit.active:
		content = renderTriageEdit(a.triageEdit, a.styles)
	case a.triageClose.active:
		content = renderTriageClose(a.triageClose, a.styles)
	case a.triageNote.active:
		content = renderTriageNote(a.triageNote, a.styles)
	case a.modal.Visible:
		content = a.modal.View()
	case a.showDetail:
		content = a.detail.view()
	case a.showTriageDetail && a.triageDetail != nil:
		content = a.triageDetail.view(a.styles, a.rc)
	default:
		content = a.activeViewContent()
	}

	lines := normalizeContentLines(content, a.width, lay.contentHeight)

	var out string
	out += header + "\n"
	out += tabs + "\n"
	for _, l := range lines {
		out += l + "\n"
	}

	// A deliberate blank row separates body content from the status bar so the
	// eye gets a natural break point (see chromeLayout).
	out += "\n"

	// The canonical status element lives inside the status bar (SPEC-016), so
	// the bar is the single, always-present status surface — no separate toast
	// row is composited over it.
	out += statusBar

	return out
}

// selectedSpecID returns the spec ID of the currently selected item
// activeScrollInfo returns a scroll position string for the status bar.
func (a App) activeScrollInfo() string {
	if a.showDetail {
		if mx := a.detail.maxScroll(); mx > 0 {
			return fmt.Sprintf("%d/%d", a.detail.scroll+1, a.detail.contentLines)
		}
		return ""
	}
	switch a.activeView {
	case ViewDashboard:
		if n := len(a.dashboard.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.dashboard.cursor+1, n)
		}
	case ViewPipeline:
		if id := a.pipeline.selectedSpecID(); id != "" {
			// Count total specs across stages.
			total := 0
			pos := 0
			for si, stage := range a.pipeline.stages {
				for ri := range stage.Specs {
					if si == a.pipeline.stageIdx && ri == a.pipeline.specIdx {
						pos = total + 1
					}
					total++
				}
			}
			if total > 0 {
				return fmt.Sprintf("%d/%d", pos, total)
			}
		}
	case ViewSpecs:
		if n := len(a.specs.filtered); n > 0 {
			return fmt.Sprintf("%d/%d", a.specs.cursor+1, n)
		}
	case ViewTriage:
		if n := len(a.triage.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.triage.cursor+1, n)
		}
	case ViewReviews:
		if n := len(a.reviews.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.reviews.cursor+1, n)
		}
	case ViewSecurity:
		if n := len(a.security.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.security.cursor+1, n)
		}
	}
	return ""
}

// selectedSpecID returns the spec ID of the currently selected item
// in the active view, if applicable.
func (a App) selectedSpecID() string {
	switch a.activeView {
	case ViewDashboard:
		return a.dashboard.selectedSpecID()
	case ViewPipeline:
		return a.pipeline.selectedSpecID()
	case ViewSpecs:
		return a.specs.selectedSpecID()
	case ViewTriage:
		return a.triage.selectedItemID()
	default:
		return ""
	}
}

// selectedSpecStage returns the pipeline stage of the currently selected spec.
// It checks the detail view first, then falls back to view-specific data.
func (a App) selectedSpecStage() string {
	if a.showDetail && a.detail.meta != nil {
		return a.detail.meta.Status
	}
	switch a.activeView {
	case ViewDashboard:
		if a.dashboard.cursor >= 0 && a.dashboard.cursor < len(a.dashboard.items) {
			return a.dashboard.items[a.dashboard.cursor].detail
		}
	case ViewPipeline:
		if a.pipeline.stageIdx >= 0 && a.pipeline.stageIdx < len(a.pipeline.stages) {
			return a.pipeline.stages[a.pipeline.stageIdx].Name
		}
	case ViewSpecs:
		if a.specs.cursor >= 0 && a.specs.cursor < len(a.specs.filtered) {
			return a.specs.filtered[a.specs.cursor].Status
		}
	}
	return ""
}

func (a App) activeViewContent() string {
	switch a.activeView {
	case ViewDashboard:
		return a.dashboard.view()
	case ViewPipeline:
		return a.pipeline.view()
	case ViewSpecs:
		return a.specs.view()
	case ViewTriage:
		return a.triage.view()
	case ViewReviews:
		return a.reviews.view()
	case ViewSecurity:
		return a.security.view()
	case ViewSettings:
		return a.settings.view()
	default:
		return ""
	}
}

func normalizeContentLines(content string, width, height int) []string {
	lines := splitLines(content)
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = normalizeLineWidth(lines[i], width)
	}
	return lines
}

func normalizeLineWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = xansi.Truncate(line, width, "")
	w := xansi.StringWidth(line)
	if w < width {
		line += strings.Repeat(" ", width-w)
	}
	return line
}

// dropLastRune removes the final UTF-8 rune from s. Text-input handlers use
// it for backspace so deleting a multi-byte character removes the whole rune
// rather than a single byte, which would leave invalid UTF-8 in the string
// (corrupting rendering and any value persisted to a spec).
func dropLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
