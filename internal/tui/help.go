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
	scroll  int    // top line of the viewport (0-based)
	width   int
	height  int
	styles  Styles
}

func newHelp(keys KeyMap, styles Styles) helpModel {
	return helpModel{keys: keys, styles: styles}
}

func (m *helpModel) toggle() {
	m.visible = !m.visible
	if m.visible {
		m.scroll = 0 // reset scroll each time help opens
	}
}
func (m *helpModel) setContext(label string) { m.context = label }
func (m *helpModel) setSize(w, h int)        { m.width = w; m.height = h }

func (m helpModel) update(msg tea.Msg) (helpModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Help) || msg.Type == tea.KeyEscape:
			m.visible = false
		case key.Matches(msg, m.keys.Down):
			m.scrollBy(1)
		case key.Matches(msg, m.keys.Up):
			m.scrollBy(-1)
		case key.Matches(msg, m.keys.PageDown):
			m.scrollBy(m.pageSize())
		case key.Matches(msg, m.keys.PageUp):
			m.scrollBy(-m.pageSize())
		case key.Matches(msg, m.keys.Home):
			m.scroll = 0
		case key.Matches(msg, m.keys.End):
			m.scroll = m.maxScroll(m.allLines())
		}
	}
	return m, nil
}

// pageSize returns the number of lines to jump for pgup/pgdn.
func (m helpModel) pageSize() int {
	if m.height > 4 {
		return m.height - 2
	}
	return 1
}

// scrollBy adjusts the scroll offset, clamped to [0, maxScroll].
func (m *helpModel) scrollBy(delta int) {
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// allLines renders the full (unclipped) content and returns it split into lines.
func (m helpModel) allLines() []string {
	return splitLines(m.renderContent())
}

// maxScroll returns the maximum scroll offset for a given line slice.
func (m helpModel) maxScroll(lines []string) int {
	// Reserve 1 line for the scrollbar hint at the bottom.
	visible := m.height - 1
	if visible < 1 {
		visible = 1
	}
	max := len(lines) - visible
	if max < 0 {
		return 0
	}
	return max
}

// renderContent returns the full unclipped help text.
func (m helpModel) renderContent() string {
	var b strings.Builder

	b.WriteString(m.styles.Title.Render("  Keyboard Shortcuts"))
	b.WriteString("\n\n")

	b.WriteString(m.section("Navigation", m.keys.NavigationBindings()))
	b.WriteString(m.section("Views", m.keys.ViewBindings()))

	if m.context == "Settings" {
		b.WriteString(m.section("Settings", m.keys.SettingsBindings()))
	} else {
		b.WriteString(m.section("Spec Actions", m.keys.ActionBindings()))
		b.WriteString(m.section("Creation", m.keys.CreationBindings()))
	}

	b.WriteString(m.section("Global", m.keys.GlobalBindings()))

	if m.context != "" && m.context != "Settings" {
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  context: %s", m.context)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m helpModel) view() string {
	if !m.visible {
		return ""
	}

	lines := splitLines(m.renderContent())

	// Clamp scroll.
	scroll := m.scroll
	if mx := m.maxScroll(lines); scroll > mx {
		scroll = mx
	}
	if scroll < 0 {
		scroll = 0
	}

	// Viewport: leave 1 line for the hint strip at the bottom.
	visible := m.height - 1
	if visible < 1 {
		visible = 1
	}

	end := scroll + visible
	if end > len(lines) {
		end = len(lines)
	}
	window := lines[scroll:end]

	// Hint strip — shows scroll position when content overflows.
	var hint string
	if len(lines) > visible {
		hint = HintStrip(m.styles,
			Hint("↑/k · ↓/j", "scroll"),
			Hint(fmt.Sprintf("%d/%d", scroll+1, len(lines)), ""),
			Hint("?", "close"),
			Hint("esc", "close"),
		)
	} else {
		hint = HintStrip(m.styles, Hint("?", "close"), Hint("esc", "close"))
	}

	// Pad short content so the hint strip is always anchored at the bottom.
	for len(window) < visible {
		window = append(window, "")
	}

	return strings.Join(window, "\n") + "\n" + hint
}

func (m helpModel) section(title string, bindings []key.Binding) string {
	var b strings.Builder
	b.WriteString(m.styles.SectionTitle.Render(Indent(1) + title))
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
		fmt.Fprintf(&b, "%s%s%s  %s\n",
			Indent(2),
			m.styles.Accent.Render(help.Key),
			strings.Repeat(" ", maxKey-len(help.Key)),
			m.styles.RowNormal.Render(help.Desc),
		)
	}
	b.WriteString("\n")
	return b.String()
}
