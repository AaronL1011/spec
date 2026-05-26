package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/tui/components"
)

const defaultRefreshInterval = 30 * time.Second

// tickMsg fires on each refresh interval.
type tickMsg time.Time

// App is the top-level Bubble Tea model. It owns the tab strip, header,
// status bar, and delegates to the active view.
type App struct {
	rc   *config.ResolvedConfig
	reg  *adapter.Registry
	role string

	// Layout
	width  int
	height int

	// Theme
	theme  Theme
	styles Styles

	// Components
	header    components.Header
	tabs      components.TabStrip
	statusBar components.StatusBar
	keys      KeyMap

	// Views
	activeView View
	dashboard  dashboardModel
	pipeline   placeholderModel
	specs      placeholderModel
	triage     placeholderModel
	reviews    placeholderModel
	settings   placeholderModel

	// Refresh
	refreshInterval time.Duration
}

// New creates a new App ready to run as a tea.Program.
func New(rc *config.ResolvedConfig, reg *adapter.Registry, role string) App {
	themePref := ""
	if rc.User != nil {
		themePref = rc.User.Preferences.Theme
	}
	theme := ResolveTheme(themePref)
	styles := NewStyles(theme)
	keys := DefaultKeyMap()

	headerStyles := components.HeaderStyles{
		Bar:      styles.Header,
		Greeting: styles.Title,
		Meta:     styles.Subtitle,
	}
	header := components.NewHeader(rc.UserName(), role, rc.CycleLabel(), headerStyles)

	tabItems := make([]components.Tab, ViewCount)
	for i := range ViewCount {
		v := View(i)
		tabItems[i] = components.Tab{Label: v.Label(), Shortcut: v.Shortcut()}
	}
	tabStyles := components.TabStripStyles{
		Active:    styles.TabActive,
		Inactive:  styles.TabNormal,
		Bar:       styles.StatusBar,
		Separator: styles.Muted,
	}
	tabs := components.NewTabStrip(tabItems, tabStyles)

	sbStyles := components.StatusBarStyles{
		Bar:     styles.StatusBar,
		Label:   styles.TabActive,
		Pending: styles.Warning,
		Hint:    styles.Muted,
		Clock:   styles.Subtitle,
	}
	sb := components.NewStatusBar(sbStyles)
	sb.SetView(ViewDashboard.Label())

	dash := newDashboard(rc, reg, role, styles, keys)

	return App{
		rc:              rc,
		reg:             reg,
		role:            role,
		theme:           theme,
		styles:          styles,
		keys:            keys,
		header:          header,
		tabs:            tabs,
		statusBar:       sb,
		activeView:      ViewDashboard,
		dashboard:       dash,
		pipeline:        newPlaceholder("Pipeline", styles),
		specs:           newPlaceholder("Specs", styles),
		triage:          newPlaceholder("Triage", styles),
		reviews:         newPlaceholder("Reviews", styles),
		settings:        newPlaceholder("Settings", styles),
		refreshInterval: defaultRefreshInterval,
	}
}

// Init runs the initial commands — fetch data + start tick.
func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.dashboard.init(),
		a.tick(),
	)
}

// Update handles all messages.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.propagateSize()
		return a, nil

	case tickMsg:
		var cmd tea.Cmd
		if a.activeView == ViewDashboard {
			cmd = a.dashboard.refresh()
		}
		return a, tea.Batch(cmd, a.tick())

	case dashboardDataMsg:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.update(msg)
		a.statusBar.SetPending(a.dashboard.pendingCount())
		a.statusBar.SetRefresh(time.Now())
		return a, cmd

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit

		case key.Matches(msg, a.keys.Help):
			return a, nil

		case key.Matches(msg, a.keys.Refresh):
			return a, a.refreshActiveView()

		case key.Matches(msg, a.keys.Tab1):
			return a, a.switchView(ViewDashboard)
		case key.Matches(msg, a.keys.Tab2):
			return a, a.switchView(ViewPipeline)
		case key.Matches(msg, a.keys.Tab3):
			return a, a.switchView(ViewSpecs)
		case key.Matches(msg, a.keys.Tab4):
			return a, a.switchView(ViewTriage)
		case key.Matches(msg, a.keys.Tab5):
			return a, a.switchView(ViewReviews)
		case key.Matches(msg, a.keys.Tab6):
			return a, a.switchView(ViewSettings)
		case key.Matches(msg, a.keys.NextTab):
			return a, a.switchView(a.activeView.Next())
		case key.Matches(msg, a.keys.PrevTab):
			return a, a.switchView(a.activeView.Prev())
		}

		return a, a.delegateToActive(msg)
	}

	return a, a.delegateToActive(msg)
}

// View renders the full application.
func (a App) View() string {
	if a.width == 0 {
		return "Initialising…"
	}

	header := a.header.View()
	tabs := a.tabs.View()
	statusBar := a.statusBar.View()

	// Content area = total height minus header, tabs, status bar
	chromeHeight := 3
	contentHeight := a.height - chromeHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	content := a.activeViewContent()

	lines := splitLines(content)
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}

	var out string
	out += header + "\n"
	out += tabs + "\n"
	for _, l := range lines {
		out += l + "\n"
	}
	out += statusBar

	return out
}

func (a App) activeViewContent() string {
	switch a.activeView {
	case ViewDashboard:
		return a.dashboard.view()
	case ViewPipeline:
		return a.pipeline.view()
	case ViewSpecs:
		return a.specs.view()
	case ViewTriage:
		return a.triage.view()
	case ViewReviews:
		return a.reviews.view()
	case ViewSettings:
		return a.settings.view()
	default:
		return ""
	}
}

func (a *App) switchView(v View) tea.Cmd {
	a.activeView = v
	a.tabs.SetActive(int(v))
	a.statusBar.SetView(v.Label())
	return a.refreshActiveView()
}

func (a *App) refreshActiveView() tea.Cmd {
	if a.activeView == ViewDashboard {
		return a.dashboard.refresh()
	}
	return nil
}

func (a *App) delegateToActive(msg tea.Msg) tea.Cmd {
	if a.activeView == ViewDashboard {
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.update(msg)
		return cmd
	}
	return nil
}

func (a *App) propagateSize() {
	a.header.SetWidth(a.width)
	a.tabs.SetWidth(a.width)
	a.statusBar.SetWidth(a.width)

	contentHeight := a.height - 3
	if contentHeight < 1 {
		contentHeight = 1
	}
	a.dashboard.setSize(a.width, contentHeight)
	a.pipeline.setSize(a.width, contentHeight)
	a.specs.setSize(a.width, contentHeight)
	a.triage.setSize(a.width, contentHeight)
	a.reviews.setSize(a.width, contentHeight)
	a.settings.setSize(a.width, contentHeight)
}

func (a App) tick() tea.Cmd {
	return tea.Tick(a.refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
