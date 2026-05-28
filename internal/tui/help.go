package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// helpModel renders a contextual keybinding overlay.
type helpModel struct {
	visible bool
	keys    KeyMap
	context string // active view label for context-specific hints
	width   int
	height  int
	styles  Styles
}

func newHelp(keys KeyMap, styles Styles) helpModel {
	return helpModel{keys: keys, styles: styles}
}

func (m *helpModel) toggle()                 { m.visible = !m.visible }
func (m *helpModel) setContext(label string) { m.context = label }
func (m *helpModel) setSize(w, h int)        { m.width = w; m.height = h }

func (m helpModel) update(msg tea.Msg) (helpModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(msg, m.keys.Help) || msg.Type == tea.KeyEscape {
			m.visible = false
		}
	}
	return m, nil
}

func (m helpModel) view() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	b.WriteString(m.styles.Title.Render("  Keyboard Shortcuts"))
	b.WriteString("\n\n")

	b.WriteString(m.section("Navigation", m.keys.NavigationBindings()))
	b.WriteString(m.section("Views", m.keys.ViewBindings()))
	b.WriteString(m.section("Actions", m.keys.ActionBindings()))
	b.WriteString(m.section("Global", m.keys.GlobalBindings()))

	if m.context != "" {
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  context: %s", m.context)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.styles.Muted.Render("  Press ? or esc to close"))

	lines := splitLines(b.String())
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	if len(lines) > visible {
		lines = lines[:visible]
	}
	return strings.Join(lines, "\n")
}

func (m helpModel) section(title string, bindings []key.Binding) string {
	var b strings.Builder
	b.WriteString(m.styles.SectionTitle.Render("  " + title))
	b.WriteString("\n")

	maxKey := 0
	for _, bind := range bindings {
		help := bind.Help()
		if len(help.Key) > maxKey {
			maxKey = len(help.Key)
		}
	}

	for _, bind := range bindings {
		help := bind.Help()
		fmt.Fprintf(&b, "    %s%s  %s\n",
			m.styles.Accent.Render(help.Key),
			strings.Repeat(" ", maxKey-len(help.Key)),
			m.styles.RowNormal.Render(help.Desc),
		)
	}
	b.WriteString("\n")
	return b.String()
}
