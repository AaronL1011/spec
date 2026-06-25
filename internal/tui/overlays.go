package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// updateStandup handles keys within the standup overlay.
func (a App) updateStandup(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		return a, tea.Quit
	case msg.String() == "esc":
		a.standup.hide()
	case msg.Text == "c":
		// Copy standup text to clipboard.
		a.standup.hide()
		return a, yankText(a.standup.text)
	case key.Matches(msg, a.keys.Up):
		a.standup.scrollUp()
	case key.Matches(msg, a.keys.Down):
		a.standup.scrollDown()
	}
	return a, nil
}

// updateIntake handles keys within the inline intake form.
func (a App) updateIntake(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.intake.close()
	case "tab":
		a.intake.nextField()
	case "shift+tab":
		a.intake.prevField()
	case "enter":
		switch {
		case a.intake.field == intakeFieldPriority:
			// Enter on priority cycles the value.
			a.intake.cyclePriority()
		case a.intake.field == intakeFieldTitle:
			// Enter on title advances to next field, not submit.
			a.intake.nextField()
		case a.intake.valid():
			// Submit only from the last field (source).
			title := a.intake.title
			priority := a.intake.priority
			source := a.intake.source
			a.intake.close()
			return a, a.startAction("creating triage item", createTriageItem(a.rc, title, priority, source))
		}
	case "backspace":
		a.intake.backspaceField()
	case "space":
		a.intake.appendToField(" ")
	default:
		if msg.Text != "" {
			a.intake.appendToField(msg.Text)
		}
	}
	return a, nil
}

// updateRevert handles keys within the inline revert form.
func (a App) updateRevert(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.revert.close()
	case "tab":
		a.revert.nextField()
	case "shift+tab":
		a.revert.prevField()
	case "enter":
		switch {
		case a.revert.field == revertFieldStage:
			// Enter on stage cycles the value.
			a.revert.cycleStage()
		case a.revert.field == revertFieldReason && a.revert.valid():
			// Submit from reason field when both fields are filled.
			specID := a.revert.specID
			stage := a.revert.selectedStage()
			reason := a.revert.reasonText()
			a.revert.close()
			return a, a.startAction("reverting "+specID, revertSpec(a.rc, a.reg, a.db, specID, stage, reason, a.role))
		}
		return a, nil
	case "backspace":
		if a.revert.field == revertFieldStage {
			a.revert.cycleStageReverse()
			return a, nil
		}
		// Reason field: fall through to the textinput so backspace edits at the
		// cursor position rather than always trimming the tail.
	default:
		if a.revert.field == revertFieldStage {
			// On the stage field, any printable rune cycles the stage forward;
			// non-printing keys (arrows, etc.) are ignored.
			if msg.Text != "" {
				a.revert.cycleStage()
			}
			return a, nil
		}
	}

	// Reason field: delegate to the textinput (arrows, home/end, backspace,
	// word jumps, rune entry).
	return a, a.revert.updateReason(msg)
}

// renderIntakeForm draws the inline triage intake form.
func (a App) renderIntakeForm() string {
	f := a.intake
	var b strings.Builder

	b.WriteString(a.styles.Title.Render("  New Triage Item"))
	b.WriteString("\n\n")

	fields := []struct {
		label string
		value string
		idx   int
	}{
		{"Title", f.title, intakeFieldTitle},
		{"Priority", f.priority + "  " + a.styles.Muted.Render("(enter to cycle)"), intakeFieldPriority},
		{"Source", f.source, intakeFieldSource},
	}

	for _, fld := range fields {
		label := a.styles.Subtitle.Render(fmt.Sprintf("  %-10s", fld.label))
		value := fld.value
		if fld.idx == f.field {
			value += a.styles.Accent.Render(IconCaret)
			b.WriteString(a.styles.Accent.Render(IconCursor + " "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(a.styles.RowNormal.Render(value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(HintStrip(a.styles,
		Hint("tab/shift+tab", "next/prev field"), Hint("enter", "submit/cycle"), Hint("esc", "cancel")))

	return b.String()
}
