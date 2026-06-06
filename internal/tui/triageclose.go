package tui

import (
	"fmt"
	"strings"
)

// triageNoteOverlay tracks the inline single-line note prompt.
type triageNoteOverlay struct {
	active   bool
	triageID string
	note     string
}

// openNote opens the note prompt for the given triage item.
func (o *triageNoteOverlay) openNote(triageID string) {
	o.active = true
	o.triageID = triageID
	o.note = ""
}

func (o *triageNoteOverlay) close()          { o.active = false }
func (o *triageNoteOverlay) append(s string) { o.note += s }
func (o *triageNoteOverlay) backspace()      { o.note = dropLastRune(o.note) }
func (o triageNoteOverlay) valid() bool      { return strings.TrimSpace(o.note) != "" }

// renderTriageNote draws the inline single-line note prompt.
func renderTriageNote(o triageNoteOverlay, styles Styles) string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("  Add note to " + o.triageID))
	b.WriteString("\n\n")

	label := styles.Subtitle.Render("  Note      ")
	b.WriteString(styles.Accent.Render(IconCursor + " "))
	b.WriteString(label)
	b.WriteString("  ")
	b.WriteString(styles.RowNormal.Render(o.note))
	b.WriteString(styles.Accent.Render(IconCaret))
	b.WriteString("\n\n")
	b.WriteString(HintStrip(styles, Hint("enter", "submit"), Hint("esc", "cancel")))
	return b.String()
}

// triageCloseOverlay manages the close/resolution dialog for a triage item.
type triageCloseOverlay struct {
	active   bool
	triageID string

	field     int // 0=reason picker, 1=note
	reasonIdx int
	note      string
}

const (
	triageCloseFieldReason = 0
	triageCloseFieldNote   = 1
	triageCloseFieldCount  = 2
)

// closeReasons is the ordered list of resolution options.
var closeReasons = []string{"resolved", "won't-fix", "duplicate", "other"}

// openClose initialises the dialog for the given triage item.
func (o *triageCloseOverlay) openClose(triageID string) {
	o.active = true
	o.triageID = triageID
	o.field = triageCloseFieldReason
	o.reasonIdx = 0
	o.note = ""
}

func (o *triageCloseOverlay) close() { o.active = false }

func (o *triageCloseOverlay) nextField() {
	if o.field < triageCloseFieldCount-1 {
		o.field++
	}
}

func (o *triageCloseOverlay) prevField() {
	if o.field > 0 {
		o.field--
	}
}

func (o *triageCloseOverlay) cycleReason() {
	o.reasonIdx = (o.reasonIdx + 1) % len(closeReasons)
}

func (o *triageCloseOverlay) selectedReason() string {
	if o.reasonIdx < 0 || o.reasonIdx >= len(closeReasons) {
		return closeReasons[0]
	}
	return closeReasons[o.reasonIdx]
}

func (o *triageCloseOverlay) appendNote(s string) { o.note += s }
func (o *triageCloseOverlay) backspaceNote()      { o.note = dropLastRune(o.note) }

// renderTriageClose draws the inline close/resolution dialog.
func renderTriageClose(o triageCloseOverlay, styles Styles) string {
	var b strings.Builder

	b.WriteString(styles.Title.Render("  Close " + o.triageID))
	b.WriteString("\n\n")

	// Reason picker
	reasonValue := o.selectedReason()
	reasonHint := fmt.Sprintf("  %s  %s",
		reasonValue,
		styles.Muted.Render(fmt.Sprintf("(%d/%d — enter to cycle)", o.reasonIdx+1, len(closeReasons))),
	)
	label := styles.Subtitle.Render(fmt.Sprintf("  %-10s", "Resolution"))
	if o.field == triageCloseFieldReason {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
		b.WriteString(label)
		b.WriteString(reasonHint)
	} else {
		b.WriteString("  ")
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(reasonValue))
	}
	b.WriteString("\n")

	// Note field
	noteLabel := styles.Subtitle.Render(fmt.Sprintf("  %-10s", "Note"))
	noteValue := o.note
	if o.field == triageCloseFieldNote {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
		b.WriteString(noteLabel)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(noteValue))
		b.WriteString(styles.Accent.Render(IconCaret))
	} else {
		b.WriteString("  ")
		b.WriteString(noteLabel)
		b.WriteString("  ")
		b.WriteString(styles.RowNormal.Render(noteValue))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(HintStrip(styles,
		Hint("tab", "next field"), Hint("enter", "cycle/confirm"), Hint("esc", "cancel")))

	return b.String()
}
