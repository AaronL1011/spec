package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// placeholderModel is a minimal view that shows a "coming soon" message.
// Used for views not yet implemented (Pipeline, Specs, Triage, etc.).
type placeholderModel struct {
	label  string
	width  int
	height int
	styles Styles
}

func newPlaceholder(label string, styles Styles) placeholderModel {
	return placeholderModel{label: label, styles: styles}
}

func (p placeholderModel) init() tea.Cmd { return nil }

func (p placeholderModel) update(tea.Msg) (placeholderModel, tea.Cmd) { return p, nil }

func (p placeholderModel) view() string {
	return p.styles.Muted.Render(fmt.Sprintf("  %s — coming soon", p.label))
}

func (p *placeholderModel) setSize(w, h int) {
	p.width = w
	p.height = h
}
