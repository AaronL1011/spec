package tui

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
)

// revertOverlay tracks the inline revert form state.
// It collects two inputs: target stage (cycled from a list) and reason (free text).
type revertOverlay struct {
	active bool
	specID string

	field  int // 0=stage, 1=reason
	reason string

	// Stage selection
	stages   []string // valid revert targets (earlier stages)
	stageIdx int      // index into stages
}

const (
	revertFieldStage  = 0
	revertFieldReason = 1
	revertFieldCount  = 2
)

// openRevert initialises the overlay for the given spec.
// currentStage is the spec's current pipeline stage; the overlay computes
// which earlier stages are valid revert targets.
func (r *revertOverlay) openRevert(specID, currentStage string, pipeline config.PipelineConfig) error {
	idx := pipeline.StageIndex(currentStage)
	if idx <= 0 {
		return fmt.Errorf("no earlier stage to revert to")
	}

	targets := make([]string, 0, idx)
	for i := range idx {
		targets = append(targets, pipeline.Stages[i].Name)
	}

	r.active = true
	r.specID = specID
	r.field = revertFieldStage
	r.reason = ""
	r.stages = targets
	r.stageIdx = len(targets) - 1 // default to the immediately preceding stage
	return nil
}

func (r *revertOverlay) close() {
	r.active = false
}

func (r *revertOverlay) nextField() {
	if r.field < revertFieldCount-1 {
		r.field++
	}
}

func (r *revertOverlay) prevField() {
	if r.field > 0 {
		r.field--
	}
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

func (r *revertOverlay) appendToReason(s string) {
	r.reason += s
}

func (r *revertOverlay) backspaceReason() {
	if len(r.reason) > 0 {
		r.reason = r.reason[:len(r.reason)-1]
	}
}

func (r *revertOverlay) selectedStage() string {
	if r.stageIdx < 0 || r.stageIdx >= len(r.stages) {
		return ""
	}
	return r.stages[r.stageIdx]
}

func (r *revertOverlay) valid() bool {
	return r.selectedStage() != "" && r.reason != ""
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
		b.WriteString(styles.Accent.Render("▸ "))
		b.WriteString(label)
		b.WriteString(stageHint)
	} else {
		b.WriteString("  ")
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(stageValue))
	}
	b.WriteString("\n")

	// Reason field
	label = styles.Subtitle.Render(fmt.Sprintf("  %-10s", "Reason"))
	reasonValue := r.reason
	if r.field == revertFieldReason {
		b.WriteString(styles.Accent.Render("▸ "))
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(reasonValue))
		b.WriteString(styles.Accent.Render("▌"))
	} else {
		b.WriteString("  ")
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(reasonValue))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(styles.Muted.Render("  tab next field · enter cycle stage/submit · esc cancel"))

	return b.String()
}
