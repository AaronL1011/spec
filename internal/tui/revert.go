package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
)

// revertOverlay tracks the inline revert form state.
// It collects two inputs: target stage (cycled from a list) and reason (free
// text). The reason field is a bubbles textinput so the user gets full cursor
// navigation (arrows, home/end, mid-text editing) rather than append-only entry.
type revertOverlay struct {
	active bool
	specID string

	field  int // 0=stage, 1=reason
	reason textinput.Model

	// Stage selection
	stages   []string // valid revert targets (earlier stages)
	stageIdx int      // index into stages
}

const (
	revertFieldStage  = 0
	revertFieldReason = 1
	revertFieldCount  = 2
)

// reasonLabelWidth is the column budget consumed by the cursor, the padded
// "Reason" label, and the gaps around it on the reason row. The textinput is
// sized to the remaining width so long input scrolls horizontally in place
// rather than clipping out of the overlay.
const reasonLabelWidth = 16

// openRevert initialises the overlay for the given spec.
// currentStage is the spec's current pipeline stage; the overlay computes
// which earlier stages are valid revert targets. width and theme size and
// theme the reason text field.
func (r *revertOverlay) openRevert(specID, currentStage string, pipeline config.PipelineConfig, width int, theme Theme) error {
	idx := pipeline.StageIndex(currentStage)
	if idx <= 0 {
		return fmt.Errorf("no earlier stage to revert to")
	}

	targets := make([]string, 0, idx)
	for i := range idx {
		targets = append(targets, pipeline.Stages[i].Name)
	}

	reason := textinput.New()
	reason.Prompt = ""
	reason.SetStyles(textInputStyles(theme))

	r.active = true
	r.specID = specID
	r.field = revertFieldStage
	r.reason = reason
	r.stages = targets
	r.stageIdx = len(targets) - 1 // default to the immediately preceding stage
	r.setWidth(width)
	r.focusField()
	return nil
}

// setWidth fits the reason text field to the available overlay width so its
// value scrolls horizontally instead of overflowing the frame.
func (r *revertOverlay) setWidth(width int) {
	w := width - reasonLabelWidth
	if w < 12 {
		w = 12
	}
	r.reason.SetWidth(w)
}

func (r *revertOverlay) close() {
	r.active = false
}

func (r *revertOverlay) nextField() {
	if r.field < revertFieldCount-1 {
		r.field++
		r.focusField()
	}
}

func (r *revertOverlay) prevField() {
	if r.field > 0 {
		r.field--
		r.focusField()
	}
}

// focusField gives keyboard focus to the reason input only while it is the
// active field, so the textinput consumes keystrokes (and renders its caret)
// exactly when the user is on it.
func (r *revertOverlay) focusField() {
	if r.field == revertFieldReason {
		// The blink command is intentionally discarded so the caret renders
		// statically (no blink ticks are pumped through the overlay).
		_ = r.reason.Focus()
	} else {
		r.reason.Blur()
	}
}

// updateReason forwards a message to the reason text field so it can handle
// cursor movement, selection, and text entry.
func (r *revertOverlay) updateReason(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	r.reason, cmd = r.reason.Update(msg)
	return cmd
}

func (r *revertOverlay) cycleStage() {
	if len(r.stages) == 0 {
		return
	}
	r.stageIdx = (r.stageIdx + 1) % len(r.stages)
}

func (r *revertOverlay) cycleStageReverse() {
	if len(r.stages) == 0 {
		return
	}
	r.stageIdx = (r.stageIdx - 1 + len(r.stages)) % len(r.stages)
}

// reasonText returns the trimmed-of-nothing raw reason value.
func (r revertOverlay) reasonText() string {
	return r.reason.Value()
}

func (r *revertOverlay) selectedStage() string {
	if r.stageIdx < 0 || r.stageIdx >= len(r.stages) {
		return ""
	}
	return r.stages[r.stageIdx]
}

func (r *revertOverlay) valid() bool {
	return r.selectedStage() != "" && r.reason.Value() != ""
}

// renderRevert draws the inline revert form.
func renderRevert(r revertOverlay, styles Styles) string {
	var b strings.Builder

	b.WriteString(styles.Title.Render("  Revert " + r.specID))
	b.WriteString("\n\n")

	// Stage field
	stageValue := r.selectedStage()
	stageHint := fmt.Sprintf("  %s  %s",
		stageValue,
		styles.Muted.Render(fmt.Sprintf("(%d/%d — enter to cycle)", r.stageIdx+1, len(r.stages))),
	)

	label := styles.Subtitle.Render(fmt.Sprintf("  %-10s", "Stage"))
	if r.field == revertFieldStage {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
		b.WriteString(label)
		b.WriteString(stageHint)
	} else {
		b.WriteString("  ")
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(stageValue))
	}
	b.WriteString("\n")

	// Reason field. The textinput renders its own caret when focused; the value
	// is shown verbatim so cursor position is preserved.
	label = styles.Subtitle.Render(fmt.Sprintf("  %-10s", "Reason"))
	if r.field == revertFieldReason {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(r.reason.View())
	} else {
		b.WriteString("  ")
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(r.reason.Value()))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(HintStrip(styles,
		Hint("tab", "next field"), Hint("enter", "cycle stage/submit"), Hint("esc", "cancel")))

	return b.String()
}
