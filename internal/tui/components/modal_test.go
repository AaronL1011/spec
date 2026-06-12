package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// typeRunes feeds each rune to the modal's embedded text field as a key press,
// mirroring how the app delegates keystrokes during input.
func typeRunes(m *Modal, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// pressBackspace sends a single backspace key press to the embedded field.
func pressBackspace(m *Modal) {
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
}

func testModalStyles() ModalStyles {
	return ModalStyles{
		Border:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Message: lipgloss.NewStyle(),
		Input:   lipgloss.NewStyle(),
		Hint:    lipgloss.NewStyle(),
	}
}

func TestModal_HiddenByDefault(t *testing.T) {
	m := NewModal(testModalStyles())
	if m.Visible {
		t.Error("modal should not be visible by default")
	}
	if got := m.View(); got != "" {
		t.Errorf("hidden modal should render empty, got: %q", got)
	}
}

func TestModal_ShowConfirm(t *testing.T) {
	m := NewModal(testModalStyles())
	m.SetSize(80, 24)
	m.ShowConfirm("Advance SPEC-001", "Are you sure?")

	if !m.Visible {
		t.Error("modal should be visible after ShowConfirm")
	}
	if m.Kind != ModalConfirm {
		t.Errorf("kind = %d, want ModalConfirm", m.Kind)
	}

	got := m.View()
	if !strings.Contains(got, "SPEC-001") {
		t.Error("confirm modal should contain title")
	}
	if !strings.Contains(got, "confirm") {
		t.Error("confirm modal should show confirm hint")
	}
}

func TestModal_ShowInput(t *testing.T) {
	m := NewModal(testModalStyles())
	m.SetSize(80, 24)
	m.ShowInput("Block SPEC-001", "Reason:")

	if m.Kind != ModalInput {
		t.Errorf("kind = %d, want ModalInput", m.Kind)
	}

	typeRunes(&m, "API design")
	if m.Value() != "API design" {
		t.Errorf("input = %q, want 'API design'", m.Value())
	}

	pressBackspace(&m)
	if m.Value() != "API desig" {
		t.Errorf("after backspace: input = %q, want 'API desig'", m.Value())
	}

	got := m.View()
	if !strings.Contains(got, "submit") {
		t.Error("input modal should show submit hint")
	}
}

func TestModal_Hide(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Test", "msg")
	typeRunes(&m, "some text")
	m.Hide()

	if m.Visible {
		t.Error("should not be visible after Hide")
	}
	if m.Value() != "" {
		t.Error("input should be cleared after Hide")
	}
}

func TestModal_BackspaceEmpty(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Test", "msg")
	// Backspace on empty input should not panic.
	pressBackspace(&m)
	if m.Value() != "" {
		t.Errorf("input should still be empty, got %q", m.Value())
	}
}

func TestModal_BackspaceInput_RuneSafe(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Title", "Enter:")
	typeRunes(&m, "café")
	pressBackspace(&m)
	if m.Value() != "caf" {
		t.Errorf("backspace = %q, want %q", m.Value(), "caf")
	}

	m.SetValue("日本")
	pressBackspace(&m)
	if m.Value() != "日" {
		t.Errorf("multibyte backspace = %q, want %q", m.Value(), "日")
	}

	// Backspacing empty input must not panic or corrupt.
	m.SetValue("")
	pressBackspace(&m)
	if m.Value() != "" {
		t.Errorf("empty backspace = %q, want empty", m.Value())
	}
}
