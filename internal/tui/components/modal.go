package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ModalKind distinguishes between confirmation and text input modals.
type ModalKind int

const (
	ModalConfirm ModalKind = iota
	ModalInput
)

// Modal renders a centered overlay dialog for confirmations or short text input.
type Modal struct {
	Title   string
	Message string
	Kind    ModalKind
	Input   string
	Visible bool
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
}

// NewModal creates a new modal.
func NewModal(styles ModalStyles) Modal {
	return Modal{styles: styles}
}

// ShowConfirm opens a confirmation modal.
func (m *Modal) ShowConfirm(title, message string) {
	m.Title = title
	m.Message = message
	m.Kind = ModalConfirm
	m.Input = ""
	m.Visible = true
}

// ShowInput opens a text input modal.
func (m *Modal) ShowInput(title, message string) {
	m.Title = title
	m.Message = message
	m.Kind = ModalInput
	m.Input = ""
	m.Visible = true
}

// Hide closes the modal.
func (m *Modal) Hide() {
	m.Visible = false
	m.Input = ""
}

// AppendInput adds a character to the input.
func (m *Modal) AppendInput(s string) { m.Input += s }

// BackspaceInput removes the last character from the input.
func (m *Modal) BackspaceInput() {
	if len(m.Input) > 0 {
		m.Input = m.Input[:len(m.Input)-1]
	}
}

// SetSize updates modal dimensions.
func (m *Modal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// View renders the modal overlay.
func (m Modal) View() string {
	if !m.Visible {
		return ""
	}

	boxWidth := m.width / 2
	if boxWidth < 40 {
		boxWidth = 40
	}
	if boxWidth > 60 {
		boxWidth = 60
	}
	innerWidth := boxWidth - 4 // padding

	var content strings.Builder
	content.WriteString(m.styles.Title.Render(m.Title))
	content.WriteString("\n\n")
	content.WriteString(m.styles.Message.Width(innerWidth).Render(m.Message))
	content.WriteString("\n\n")

	switch m.Kind {
	case ModalConfirm:
		content.WriteString(m.styles.Hint.Render("[y] confirm  [n/esc] cancel"))
	case ModalInput:
		inputLine := m.styles.Input.Width(innerWidth).Render(m.Input + "▌")
		content.WriteString(inputLine)
		content.WriteString("\n")
		content.WriteString(m.styles.Hint.Render("[enter] submit  [esc] cancel"))
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
