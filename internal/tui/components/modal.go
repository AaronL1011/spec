package components

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ModalKind distinguishes between confirmation and text input modals.
type ModalKind int

const (
	ModalConfirm ModalKind = iota
	ModalInput
	// ModalInfo is a read-only message dialog (e.g. a full error detail). It
	// takes no input and is dismissed with esc.
	ModalInfo
)

// Modal renders a centered overlay dialog for confirmations or short text input.
// The text-input variant embeds a bubbles textinput so the user gets full
// cursor navigation (arrows, home/end, word jumps, mid-text editing) rather
// than append-only entry.
type Modal struct {
	Title   string
	Message string
	Kind    ModalKind
	Visible bool
	input   textinput.Model
	width   int
	height  int
	styles  ModalStyles
}

// ModalStyles holds the styles for the modal component.
type ModalStyles struct {
	Border  lipgloss.Style
	Title   lipgloss.Style
	Message lipgloss.Style
	Input   lipgloss.Style
	Hint    lipgloss.Style
	// InputField, when set, themes the embedded text field (text colour,
	// accent cursor, muted placeholder) so it matches the app palette.
	InputField textinput.Styles
}

// NewModal creates a new modal.
func NewModal(styles ModalStyles) Modal {
	ti := textinput.New()
	// A subtle accent prompt marker frames the field without a heavy box.
	ti.Prompt = "› "
	if styles.InputField.Cursor.Color != nil {
		ti.SetStyles(styles.InputField)
	}
	return Modal{styles: styles, input: ti}
}

// ShowConfirm opens a confirmation modal.
func (m *Modal) ShowConfirm(title, message string) {
	m.Title = title
	m.Message = message
	m.Kind = ModalConfirm
	m.resetInput()
	m.Visible = true
}

// ShowInput opens a text input modal and focuses the embedded text field.
func (m *Modal) ShowInput(title, message string) {
	m.Title = title
	m.Message = message
	m.Kind = ModalInput
	m.resetInput()
	// Focus drives the field's active styling; the returned blink command is
	// intentionally discarded so the caret renders statically (no blink ticks).
	_ = m.input.Focus()
	m.Visible = true
}

// ShowInfo opens a read-only message modal (full-text display, esc to close).
func (m *Modal) ShowInfo(title, message string) {
	m.Title = title
	m.Message = message
	m.Kind = ModalInfo
	m.resetInput()
	m.Visible = true
}

// Hide closes the modal.
func (m *Modal) Hide() {
	m.Visible = false
	m.resetInput()
}

// resetInput clears and blurs the embedded text field.
func (m *Modal) resetInput() {
	m.input.SetValue("")
	m.input.Blur()
}

// Value returns the current text-input contents.
func (m Modal) Value() string { return m.input.Value() }

// SetValue replaces the text-input contents and parks the cursor at the end,
// so a pre-filled value (e.g. self-assign identity) is ready to edit or submit.
func (m *Modal) SetValue(s string) {
	m.input.SetValue(s)
	m.input.CursorEnd()
}

// Update forwards a message to the embedded text field so it can handle cursor
// movement, selection, and text entry. It is a no-op for non-input modals.
func (m *Modal) Update(msg tea.Msg) tea.Cmd {
	if m.Kind != ModalInput {
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// SetSize updates modal dimensions and re-fits the embedded text field so the
// stored model (not just a render copy) scrolls horizontally while typing.
func (m *Modal) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.input.SetWidth(m.fieldWidth())
}

// boxWidth computes the dialog frame width, clamped to a readable band and the
// terminal width.
func (m Modal) boxWidth() int {
	bw := m.width / 2
	if bw < 40 {
		bw = 40
	}
	if bw > 60 {
		bw = 60
	}
	if m.width > 0 && bw > m.width-2 {
		bw = m.width - 2
	}
	return bw
}

// fieldWidth is the horizontal-scroll budget for the embedded text field:
// inner content width minus the prompt marker (2), style padding (2), and the
// trailing cursor cell the input reserves (1), with one spare column so long
// input stays on one line instead of wrapping the frame.
func (m Modal) fieldWidth() int {
	fw := (m.boxWidth() - 4) - 6
	if fw < 8 {
		fw = 8
	}
	return fw
}

// View renders the modal overlay.
func (m Modal) View() string {
	if !m.Visible {
		return ""
	}

	boxWidth := m.boxWidth()
	innerWidth := boxWidth - 4 // padding

	var content strings.Builder
	content.WriteString(m.styles.Title.Render(m.Title))
	content.WriteString("\n\n")

	switch m.Kind {
	case ModalConfirm:
		content.WriteString(m.styles.Message.Width(innerWidth).Render(m.Message))
		content.WriteString("\n\n")
		content.WriteString(m.styles.Hint.Render("[y] confirm  [n/esc] cancel"))
	case ModalInfo:
		content.WriteString(m.styles.Message.Width(innerWidth).Render(m.Message))
		content.WriteString("\n\n")
		content.WriteString(m.styles.Hint.Render("[esc] close"))
	case ModalInput:
		// The Message is the field label, rendered tight above the field so the
		// prompt and its input read as one unit. The input uses an accent
		// prompt marker rather than a heavy box, which keeps the layout clean
		// and avoids border-wrap math at narrow widths.
		content.WriteString(m.styles.Message.Render(m.Message))
		content.WriteString("\n")
		m.input.SetWidth(m.fieldWidth())
		inputLine := m.styles.Input.Width(innerWidth).Render(m.input.View())
		content.WriteString(inputLine)
		content.WriteString("\n\n")
		content.WriteString(m.styles.Hint.Render("[enter] submit  ·  [esc] cancel"))
	}

	box := m.styles.Border.
		Width(boxWidth).
		Padding(1, 2).
		Render(content.String())

	// Center vertically
	boxLines := strings.Count(box, "\n") + 1
	topPad := (m.height - boxLines) / 2
	if topPad < 0 {
		topPad = 0
	}

	return strings.Repeat("\n", topPad) + box
}
