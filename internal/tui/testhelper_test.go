package tui

import tea "github.com/charmbracelet/bubbletea"

// keyMsg creates a tea.KeyMsg from a string for testing.
func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
