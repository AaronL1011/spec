package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

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

	m.AppendInput("API")
	m.AppendInput(" design")
	if m.Input != "API design" {
		t.Errorf("input = %q, want 'API design'", m.Input)
	}

	m.BackspaceInput()
	if m.Input != "API desig" {
		t.Errorf("after backspace: input = %q, want 'API desig'", m.Input)
	}

	got := m.View()
	if !strings.Contains(got, "submit") {
		t.Error("input modal should show submit hint")
	}
}

func TestModal_Hide(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Test", "msg")
	m.AppendInput("some text")
	m.Hide()

	if m.Visible {
		t.Error("should not be visible after Hide")
	}
	if m.Input != "" {
		t.Error("input should be cleared after Hide")
	}
}

func TestModal_BackspaceEmpty(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Test", "msg")
	// Backspace on empty input should not panic.
	m.BackspaceInput()
	if m.Input != "" {
		t.Errorf("input should still be empty, got %q", m.Input)
	}
}

func TestModal_BackspaceInput_RuneSafe(t *testing.T) {
	m := NewModal(testModalStyles())
	m.ShowInput("Title", "Enter:")
	m.AppendInput("café")
	m.BackspaceInput()
	if m.Input != "caf" {
		t.Errorf("backspace = %q, want %q", m.Input, "caf")
	}

	m.Input = "日本"
	m.BackspaceInput()
	if m.Input != "日" {
		t.Errorf("multibyte backspace = %q, want %q", m.Input, "日")
	}

	// Backspacing empty input must not panic or corrupt.
	m.Input = ""
	m.BackspaceInput()
	if m.Input != "" {
		t.Errorf("empty backspace = %q, want empty", m.Input)
	}
}
