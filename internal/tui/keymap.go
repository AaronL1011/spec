package tui

import "charm.land/bubbles/v2/key"

// KeyMap defines all keybindings for the TUI.
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Scroll (contextual, e.g. settings theme list)
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
	Help        key.Binding
	Search      key.Binding
	Refresh     key.Binding
	Quit        key.Binding
	ExpandError key.Binding // E — open the full text of the current error in a modal

	// Spec actions
	Advance       key.Binding
	Edit          key.Binding
	Build         key.Binding
	Block         key.Binding // x — toggle block (confirm modal)
	Unblock       key.Binding // u — explicit unblock (kept for dispatch)
	ToggleArchive key.Binding // ` — toggle archive list in spec tab
	Revert        key.Binding
	Focus         key.Binding // f — toggle focus
	Open          key.Binding
	Yank          key.Binding
	Decide        key.Binding
	Push          key.Binding
	Sync          key.Binding
	Archive       key.Binding // g a — archive (confirm modal)
	Restore       key.Binding // g r — restore (confirm modal)

	// Creation
	NewSpec   key.Binding
	NewIntake key.Binding
	Standup   key.Binding // g s — standup

	// Prefix keys
	GPrefix key.Binding // g — arms prefix for g a / g r / g s
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
		Home: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "top"),
		),
		End: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "bottom"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up"),
			key.WithHelp("shift+↑", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down"),
			key.WithHelp("shift+↓", "scroll down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back / arm exit"),
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
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "hard quit"),
		),

		Advance: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "advance"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		ExpandError: key.NewBinding(
			key.WithKeys("E"),
			key.WithHelp("E", "expand error"),
		),
		Build: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "start/resume build (MCP agent)"),
		),
		Block: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "toggle block"),
		),
		ToggleArchive: key.NewBinding(
			key.WithKeys("`"),
			key.WithHelp("`", "toggle archive list"),
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
			key.WithHelp("f", "toggle focus"),
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
			key.WithKeys(),
			key.WithHelp("g a", "archive"),
		),
		Restore: key.NewBinding(
			key.WithKeys(),
			key.WithHelp("g r", "restore"),
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
			key.WithKeys(),
			key.WithHelp("g s", "standup"),
		),

		GPrefix: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "prefix"),
		),
	}
}

// NavigationBindings returns the nav-related bindings for help display.
func (k KeyMap) NavigationBindings() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End,
		k.ScrollUp, k.ScrollDown, k.Enter, k.Back,
	}
}

// ViewBindings returns the view-switching bindings for help display.
func (k KeyMap) ViewBindings() []key.Binding {
	return []key.Binding{k.Tab1, k.Tab2, k.Tab3, k.Tab4, k.Tab5, k.Tab6, k.NextTab, k.PrevTab}
}

// ActionBindings returns the spec action bindings for help display.
func (k KeyMap) ActionBindings() []key.Binding {
	return []key.Binding{
		k.Advance, k.Revert, k.Edit, k.Build, k.Block, k.Unblock,
		k.Focus, k.Open, k.Yank, k.Decide, k.Push, k.Sync,
		k.Archive, k.Restore, k.ToggleArchive,
	}
}

// CreationBindings returns the creation/tool bindings for help display.
func (k KeyMap) CreationBindings() []key.Binding {
	return []key.Binding{k.NewSpec, k.NewIntake, k.Standup}
}

// GlobalBindings returns bindings shown in every context.
func (k KeyMap) GlobalBindings() []key.Binding {
	return []key.Binding{k.Help, k.Search, k.Refresh, k.ExpandError, k.Quit}
}

// TriageBindings returns keybindings shown when the triage view is active.
func (k KeyMap) TriageBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter/space", "open detail")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "add note")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit (pm/eng)")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "close (pm/eng)")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "escalate/de-escalate (pm/eng)")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "promote to spec (pm)")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close detail")),
	}
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
	}
}

// AllBindings returns every binding for the full help view.
func (k KeyMap) AllBindings() []key.Binding {
	var all []key.Binding
	all = append(all, k.NavigationBindings()...)
	all = append(all, k.ViewBindings()...)
	all = append(all, k.ActionBindings()...)
	all = append(all, k.CreationBindings()...)
	all = append(all, k.GlobalBindings()...)
	return all
}
