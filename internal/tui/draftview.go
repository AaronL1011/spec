package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/llm"
)

// viewDraft renders the draft-review modal: a titled frame, the draft body, a
// status line carrying provider cost, and the fixed action footer.
//
// The status line is not decoration. A local model can take tens of seconds, so
// showing elapsed time while generating is what lets a user tell progress from a
// hang, and showing model and token count afterwards is what makes the cost of a
// retry loop visible rather than invisible.
func (a App) viewDraft() string {
	th := a.theme
	width := clampInt(a.width-8, 40, 100)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Accent).
		Padding(0, 1).
		Width(width)

	title := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	errStyle := lipgloss.NewStyle().Foreground(th.Error)

	var b strings.Builder

	header := fmt.Sprintf("DRAFT · %s · %s", a.draft.title, a.draft.specID)
	if n := len(a.draft.attempts); n > 1 {
		header += fmt.Sprintf("  (attempt %d/%d)", a.draft.cursor+1, n)
	}
	b.WriteString(title.Render(header))
	b.WriteString("\n\n")

	switch a.draft.state {
	case draftGenerating:
		// Task name plus a live counter, not a bare spinner.
		fmt.Fprintf(&b, "Generating %s… %.1fs\n\n", a.draft.taskID, a.draft.elapsed.Seconds())
		b.WriteString(muted.Render("esc cancels"))

	case draftSteering:
		if att, ok := a.draft.current(); ok {
			b.WriteString(muted.Render(truncateLines(att.Content, 6, width-4)))
			b.WriteString("\n\n")
		}
		b.WriteString("Steer (one line), then enter:\n")
		b.WriteString("› " + a.draft.steerInput + "▏")
		b.WriteString("\n\n")
		b.WriteString(muted.Render("esc cancels the note and keeps the draft"))

	case draftReviewing:
		att, ok := a.draft.current()
		if !ok {
			break
		}
		b.WriteString(truncateLines(att.Content, 18, width-4))
		b.WriteString("\n\n")
		if att.Note != "" {
			b.WriteString(muted.Render("steer: "+att.Note) + "\n")
		}
		if line := draftStatusLine(att); line != "" {
			b.WriteString(muted.Render(line) + "\n")
		}
		if a.draft.lastErr != "" {
			b.WriteString(errStyle.Render(a.draft.lastErr) + "\n")
		}
		b.WriteString(muted.Render(a.draftFooter()))
	}

	return border.Render(b.String())
}

// draftFooter renders the available actions, offering only what the configured
// agent can actually do.
func (a App) draftFooter() string {
	parts := []string{"[a]ccept", "[e]dit", "[r]etry", "[R]etry+note"}
	if len(a.draft.attempts) > 1 {
		parts = append(parts, "[ ] attempts")
	}
	if svc := a.llmService(); svc != nil && svc.Capabilities().MCP {
		parts = append(parts, "[i]nteractive")
	}
	parts = append(parts, "[s]kip")
	return strings.Join(parts, "  ")
}

// draftStatusLine reports what the shown attempt cost.
func draftStatusLine(att llm.Attempt) string {
	if att.Result == nil {
		return ""
	}
	var parts []string
	if att.Result.Model != "" {
		parts = append(parts, att.Result.Model)
	}
	if att.Result.Duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", att.Result.Duration.Seconds()))
	}
	switch {
	case att.Result.Tokens.Total > 0:
		parts = append(parts, fmt.Sprintf("%d tokens", att.Result.Tokens.Total))
	case att.Result.Tokens.Output > 0:
		parts = append(parts, fmt.Sprintf("%d out tokens", att.Result.Tokens.Output))
	}
	if att.Edited {
		parts = append(parts, "edited")
	}
	return strings.Join(parts, " · ")
}

// truncateLines bounds a draft preview to the modal's height and width, marking
// the cut so a truncated draft is never mistaken for a short one.
func truncateLines(s string, maxLines, width int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if width > 8 {
		wrapped := make([]string, 0, len(lines))
		for _, line := range lines {
			wrapped = append(wrapped, wrapLine(line, width)...)
		}
		lines = wrapped
	}
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	kept := lines[:maxLines]
	return strings.Join(kept, "\n") + fmt.Sprintf("\n… %d more lines (accept and edit to see all)", len(lines)-maxLines)
}

// wrapLine hard-wraps one line at width, preserving empty lines.
func wrapLine(line string, width int) []string {
	if len(line) <= width {
		return []string{line}
	}
	var out []string
	for len(line) > width {
		cut := strings.LastIndex(line[:width], " ")
		if cut <= 0 {
			cut = width
		}
		out = append(out, line[:cut])
		line = strings.TrimLeft(line[cut:], " ")
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
