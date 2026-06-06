package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// triageEditOverlay tracks the inline edit form state for a triage item. The
// single-line fields use textinput and the multiline body uses textarea so the
// user gets full cursor navigation (arrows, home/end, word jumps) rather than
// append-only editing.
type triageEditOverlay struct {
	active   bool
	triageID string

	field    int // which field is focused
	title    textinput.Model
	priority string
	source   textinput.Model
	body     textarea.Model
}

const (
	triageEditFieldTitle    = 0
	triageEditFieldPriority = 1
	triageEditFieldSource   = 2
	triageEditFieldBody     = 3
	triageEditFieldCount    = 4
)

// openEdit initialises the overlay from an existing triage item.
func (o *triageEditOverlay) openEdit(item triageItem, width, height int) {
	o.active = true
	o.triageID = item.ID
	o.field = triageEditFieldTitle

	title := textinput.New()
	title.Prompt = ""
	title.SetValue(item.Title)
	title.CursorEnd()
	_ = title.Cursor.SetMode(cursor.CursorStatic)
	o.title = title

	o.priority = item.Priority
	if o.priority == "" {
		o.priority = "medium"
	}

	source := textinput.New()
	source.Prompt = ""
	source.SetValue(item.Source)
	source.CursorEnd()
	_ = source.Cursor.SetMode(cursor.CursorStatic)
	o.source = source

	body := textarea.New()
	body.Prompt = ""
	body.ShowLineNumbers = false
	body.SetValue(item.Body)
	_ = body.Cursor.SetMode(cursor.CursorStatic)
	o.body = body

	o.setSize(width, height)
	o.moveBodyToStart(item.Body)
	o.focusField()
}

func (o *triageEditOverlay) close() {
	o.active = false
}

// setSize lays out the field widths and the body height against the available
// overlay dimensions.
func (o *triageEditOverlay) setSize(width, height int) {
	fieldWidth := width - 18
	if fieldWidth < 20 {
		fieldWidth = 20
	}
	o.title.Width = fieldWidth
	o.source.Width = fieldWidth

	bodyWidth := width - 4
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	o.body.SetWidth(bodyWidth)

	bodyHeight := height - 12
	switch {
	case bodyHeight < 3:
		bodyHeight = 3
	case bodyHeight > 18:
		bodyHeight = 18
	}
	o.body.SetHeight(bodyHeight)
}

// moveBodyToStart positions the body cursor at the first line so the user lands
// on the context at the top rather than the end of any appended notes.
func (o *triageEditOverlay) moveBodyToStart(content string) {
	for i := 0; i <= strings.Count(content, "\n"); i++ {
		o.body.CursorUp()
	}
	o.body.CursorStart()
}

// focusField gives keyboard focus to the active field's input component and
// blurs the others. The priority field has no text component.
func (o *triageEditOverlay) focusField() {
	o.title.Blur()
	o.source.Blur()
	o.body.Blur()
	switch o.field {
	case triageEditFieldTitle:
		o.title.Focus()
	case triageEditFieldSource:
		o.source.Focus()
	case triageEditFieldBody:
		o.body.Focus()
	}
}

func (o *triageEditOverlay) nextField() {
	if o.field < triageEditFieldCount-1 {
		o.field++
		o.focusField()
	}
}

func (o *triageEditOverlay) prevField() {
	if o.field > 0 {
		o.field--
		o.focusField()
	}
}

func (o *triageEditOverlay) cyclePriority() {
	o.priority = nextPriority(o.priority)
}

// update forwards a message to the focused input component so it can handle
// cursor movement, selection, and text entry.
func (o *triageEditOverlay) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch o.field {
	case triageEditFieldTitle:
		o.title, cmd = o.title.Update(msg)
	case triageEditFieldSource:
		o.source, cmd = o.source.Update(msg)
	case triageEditFieldBody:
		o.body, cmd = o.body.Update(msg)
	}
	return cmd
}

func (o triageEditOverlay) valid() bool {
	return strings.TrimSpace(o.title.Value()) != ""
}

// nextPriority cycles through the standard priority values.
func nextPriority(current string) string {
	order := []string{"low", "medium", "high", "critical"}
	for i, p := range order {
		if p == current {
			return order[(i+1)%len(order)]
		}
	}
	return "medium"
}

// renderTriageEdit draws the inline edit form for a triage item.
func renderTriageEdit(o triageEditOverlay, styles Styles) string {
	var b strings.Builder

	b.WriteString(styles.Title.Render("  Edit " + o.triageID))
	b.WriteString("\n\n")

	b.WriteString(renderEditField(styles, "Title", o.title.View(), o.field == triageEditFieldTitle))
	priorityValue := o.priority + "  " + styles.Muted.Render("(enter to cycle)")
	b.WriteString(renderEditField(styles, "Priority", priorityValue, o.field == triageEditFieldPriority))
	b.WriteString(renderEditField(styles, "Source", o.source.View(), o.field == triageEditFieldSource))

	b.WriteString("\n")
	bodyLabel := fmt.Sprintf("%-10s", "Body")
	if o.field == triageEditFieldBody {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
		b.WriteString(styles.Subtitle.Render(bodyLabel))
	} else {
		b.WriteString("  ")
		b.WriteString(styles.Subtitle.Render(bodyLabel))
	}
	b.WriteString("\n")
	b.WriteString(o.body.View())
	b.WriteString("\n\n")

	b.WriteString(HintStrip(styles,
		Hint("tab", "next field"), Hint("↑↓←→", "move cursor"), Hint("ctrl+s", "save"), Hint("esc", "cancel")))

	return b.String()
}

// renderEditField renders a single-line labelled field row, marking focus with
// a caret. The value is rendered verbatim so an input's own cursor is preserved.
func renderEditField(styles Styles, label, value string, focused bool) string {
	var b strings.Builder
	if focused {
		b.WriteString(styles.Accent.Render(IconCursor + " "))
	} else {
		b.WriteString("  ")
	}
	b.WriteString(styles.Subtitle.Render(fmt.Sprintf("%-10s", label)))
	b.WriteString("  ")
	b.WriteString(value)
	b.WriteString("\n")
	return b.String()
}
