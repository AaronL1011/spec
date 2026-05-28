package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the TUI.
type KeyMap struct {
	// Navigation
	Up         key.Binding
	Down       key.Binding
	Enter      key.Binding
	Back       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding

	// View switching
	Tab1 key.Binding
	Tab2 key.Binding
	Tab3 key.Binding
	Tab4 key.Binding
	Tab5 key.Binding
	Tab6 key.Binding

	NextTab key.Binding
	PrevTab key.Binding

	// Global actions
	Help    key.Binding
	Search  key.Binding
	Refresh key.Binding
	Quit    key.Binding

	// Spec actions
	Advance key.Binding
	Edit    key.Binding
	Build   key.Binding
	Block   key.Binding
	Unblock key.Binding
	Revert  key.Binding
	Focus   key.Binding
	Unfocus key.Binding
	Open    key.Binding
	Yank    key.Binding
	Decide  key.Binding
	Push    key.Binding
	Sync    key.Binding

	// Archive
	Archive key.Binding
	Restore key.Binding

	// Creation
	NewSpec   key.Binding
	NewIntake key.Binding
	Standup   key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("K", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("J", "scroll down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),

		Tab1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "dashboard"),
		),
		Tab2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "pipeline"),
		),
		Tab3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "specs"),
		),
		Tab4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "triage"),
		),
		Tab5: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "reviews"),
		),
		Tab6: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "settings"),
		),

		NextTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next view"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev view"),
		),

		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "refresh"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),

		Advance: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "advance"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		Build: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "build"),
		),
		Block: key.NewBinding(
			key.WithKeys("B"),
			key.WithHelp("B", "block"),
		),
		Unblock: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "unblock"),
		),
		Revert: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "revert"),
		),
		Focus: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "focus"),
		),
		Unfocus: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "unfocus"),
		),
		Open: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open"),
		),
		Yank: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy id"),
		),
		Decide: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "decide"),
		),
		Push: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "push"),
		),
		Sync: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sync"),
		),

		Archive: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "archive"),
		),
		Restore: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restore"),
		),

		NewSpec: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new spec"),
		),
		NewIntake: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "intake"),
		),
		Standup: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "standup"),
		),
	}
}

// NavigationBindings returns the nav-related bindings for help display.
func (k KeyMap) NavigationBindings() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.NextTab, k.PrevTab}
}

// ViewBindings returns the view-switching bindings for help display.
func (k KeyMap) ViewBindings() []key.Binding {
	return []key.Binding{k.Tab1, k.Tab2, k.Tab3, k.Tab4, k.Tab5, k.Tab6}
}

// ActionBindings returns the action bindings for help display.
func (k KeyMap) ActionBindings() []key.Binding {
	return []key.Binding{k.Advance, k.Revert, k.Edit, k.Build, k.Block, k.Focus, k.Open, k.Yank, k.Decide, k.Push, k.Sync, k.Archive, k.Restore, k.NewSpec, k.NewIntake, k.Standup}
}

// GlobalBindings returns bindings shown in every context.
func (k KeyMap) GlobalBindings() []key.Binding {
	return []key.Binding{k.Help, k.Search, k.Refresh, k.Quit}
}

// SettingsBindings returns keybindings for the Settings tab edit flow.
func (k KeyMap) SettingsBindings() []key.Binding {
	return []key.Binding{
		k.Up,
		k.Down,
		k.Enter,
		k.Back,
		k.PageUp,
		k.PageDown,
		k.ScrollUp,
		k.ScrollDown,
		key.NewBinding(
			key.WithKeys("space", "l", "h"),
			key.WithHelp("space/l/h", "cycle role or theme"),
		),
	}
}
