package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/components"
)

// applySettingsField applies immediate UI effects after a settings field is
// confirmed. It returns a command for fields whose effect is a Bubble Tea
// side effect (e.g. enabling the terminal's mouse reporting), or nil.
func (a *App) applySettingsField(field settingsField) tea.Cmd {
	switch field {
	case fieldMouse:
		// Mouse mode is declared on the View in Bubble Tea v2 (see App.View), so
		// flipping the preference takes effect on the next render with no command.
		return nil
	case fieldName, fieldRole:
		if field == fieldRole {
			a.role = strings.ToLower(a.rc.OwnerRole(""))
			// Propagate to the dashboard model so its next refresh uses the new role.
			a.dashboard.role = a.role
		}
		a.header = components.NewHeader(
			a.rc.UserName(), a.role, a.rc.CycleLabel(),
			components.HeaderStyles{
				Bar:      a.styles.Header,
				Greeting: a.styles.Title,
				Meta:     a.styles.Subtitle,
			},
		)
		a.header.SetWidth(a.width)
	case fieldTheme:
		themeName := "auto"
		if a.rc.User != nil && a.rc.User.Preferences.Theme != "" {
			themeName = a.rc.User.Preferences.Theme
		}
		a.applyTheme(themeName)
	case fieldRefresh:
		a.refreshInterval = parseRefreshInterval(a.rc)
	}
	return nil
}

// applyTheme rebuilds all styles from a named theme and propagates to
// every view and component.
func (a *App) applyTheme(name string) {
	a.theme = ResolveTheme(name)
	a.styles = NewStyles(a.theme)

	// Rebuild components that store individual style values.
	a.header = components.NewHeader(
		a.rc.UserName(), a.role, a.rc.CycleLabel(),
		components.HeaderStyles{
			Bar:      a.styles.Header,
			Greeting: a.styles.Title,
			Meta:     a.styles.Subtitle,
		},
	)

	tabItems := make([]components.Tab, ViewCount)
	for i := range ViewCount {
		v := View(i)
		tabItems[i] = components.Tab{Label: v.Label(), Shortcut: v.Shortcut()}
	}
	a.tabs = components.NewTabStrip(tabItems, components.TabStripStyles{
		Active:    a.styles.TabActive,
		Inactive:  a.styles.TabNormal,
		Bar:       a.styles.StatusBar,
		Separator: a.styles.Muted,
	})
	a.tabs.SetActive(int(a.activeView))

	a.statusBar = components.NewStatusBar(components.StatusBarStyles{
		Bar:     a.styles.StatusBar,
		Label:   a.styles.TabActive,
		Pending: a.styles.Warning,
		Hint:    a.styles.Muted,
		Clock:   a.styles.Subtitle,
		Stale:   a.styles.Muted,
		Status:  statusStyles(a.theme),
	})
	a.statusBar.SetView(a.activeView.Label())
	a.statusBar.SetPending(a.dashboard.pendingCount())
	a.statusBar.SetActiveRefreshKey(refreshKeyForView(a.activeView))
	a.statusBar.SetStaleAfter(2 * a.refreshInterval)

	a.modal = components.NewModal(components.ModalStyles{
		Border:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(a.theme.Accent),
		Title:   a.styles.Title,
		Message: a.styles.Subtitle,
		Input:   lipgloss.NewStyle().Foreground(a.theme.Text).Background(a.theme.Surface).Padding(0, 1),
		Hint:    a.styles.Muted,
	})

	// Propagate styles to all views.
	a.dashboard.styles = a.styles
	a.pipeline.styles = a.styles
	a.specs.styles = a.styles
	a.triage.styles = a.styles
	a.reviews.styles = a.styles
	a.settings.styles = a.styles
	a.help.styles = a.styles
	if a.showDetail {
		a.detail.styles = a.styles
		a.detail.theme = a.theme
		a.detail.renderer = newRenderer(a.theme)
		a.detail.readerCache = make(map[string]string)
	}

	a.propagateSize()
}
