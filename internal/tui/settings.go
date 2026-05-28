package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
)

// settingsModel shows config, integration status, and theme info.
type settingsModel struct {
	rc *config.ResolvedConfig

	scroll int
	width  int
	height int
	styles Styles
	keys   KeyMap
}

func newSettings(rc *config.ResolvedConfig, styles Styles, keys KeyMap) settingsModel {
	return settingsModel{
		rc:     rc,
		styles: styles,
		keys:   keys,
	}
}

func (m settingsModel) init() tea.Cmd { return nil }

// cycleThemeMsg requests the app cycle to the next theme.
type cycleThemeMsg struct{}

func (m settingsModel) update(msg tea.Msg) (settingsModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.scroll > 0 {
				m.scroll--
			}
		case key.Matches(msg, m.keys.Down):
			m.scroll++
		case msg.Type == tea.KeyRunes && string(msg.Runes) == "t":
			return m, func() tea.Msg { return cycleThemeMsg{} }
		}
	}
	return m, nil
}

func (m settingsModel) view() string {
	var b strings.Builder

	// Identity
	b.WriteString(m.styles.SectionTitle.Render("  Identity"))
	b.WriteString("\n")
	b.WriteString(m.kv("Name", m.rc.UserName()))
	b.WriteString(m.kv("Role", m.rc.OwnerRole("")))
	b.WriteString(m.kv("Handle", m.rc.UserHandle()))
	if m.rc.TeamName() != "" {
		b.WriteString(m.kv("Team", m.rc.TeamName()))
	}
	if m.rc.CycleLabel() != "" {
		b.WriteString(m.kv("Cycle", m.rc.CycleLabel()))
	}
	b.WriteString("\n")

	// Theme
	b.WriteString(m.styles.SectionTitle.Render("  Appearance"))
	b.WriteString("\n")
	themeName := "auto"
	if m.rc.User != nil && m.rc.User.Preferences.Theme != "" {
		themeName = m.rc.User.Preferences.Theme
	}
	b.WriteString(m.kv("Theme", themeName+"  "+m.styles.Muted.Render("(t to cycle)")))
	refreshInterval := "30s"
	if m.rc.User != nil && m.rc.User.Preferences.RefreshInterval != "" {
		refreshInterval = m.rc.User.Preferences.RefreshInterval
	}
	b.WriteString(m.kv("Refresh", refreshInterval))
	editor := "vi"
	if m.rc.User != nil && m.rc.User.Preferences.Editor != "" {
		editor = m.rc.User.Preferences.Editor
	}
	b.WriteString(m.kv("Editor", editor))
	b.WriteString("\n")

	// Integrations
	b.WriteString(m.styles.SectionTitle.Render("  Integrations"))
	b.WriteString("\n")

	type integration struct {
		name     string
		category string
	}
	integrations := []integration{
		{"Comms", "comms"},
		{"PM", "pm"},
		{"Docs", "docs"},
		{"Repo", "repo"},
		{"Agent", "agent"},
		{"AI", "ai"},
		{"Design", "design"},
		{"Deploy", "deploy"},
	}

	for _, ig := range integrations {
		provider := "—"
		status := m.styles.Muted
		if m.rc.HasIntegration(ig.category) {
			provider = m.integrationProvider(ig.category)
			status = m.styles.Success
		}
		label := fmt.Sprintf("    %-10s", ig.name)
		b.WriteString(m.styles.RowNormal.Render(label))
		b.WriteString(status.Render(provider))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Config paths
	b.WriteString(m.styles.SectionTitle.Render("  Config Paths"))
	b.WriteString("\n")
	b.WriteString(m.kv("User", m.rc.UserConfigPath))
	b.WriteString(m.kv("Team", m.rc.TeamConfigPath))
	if m.rc.SpecsRepoDir != "" {
		b.WriteString(m.kv("Specs", m.rc.SpecsRepoDir))
	}

	// Apply scroll.
	lines := splitLines(b.String())
	visible := m.height
	if visible < 3 {
		visible = 3
	}
	start := m.scroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

func (m settingsModel) kv(label, value string) string {
	if value == "" {
		value = "—"
	}
	return fmt.Sprintf("    %s  %s\n",
		m.styles.Subtitle.Render(fmt.Sprintf("%-12s", label)),
		m.styles.RowNormal.Render(value),
	)
}

func (m settingsModel) integrationProvider(category string) string {
	if m.rc.Team == nil {
		return "—"
	}
	switch category {
	case "comms":
		return m.rc.Team.Integrations.Comms.Provider
	case "pm":
		return m.rc.Team.Integrations.PM.Provider
	case "docs":
		return m.rc.Team.Integrations.Docs.Provider
	case "repo":
		return m.rc.Team.Integrations.Repo.Provider
	case "agent":
		return m.rc.Team.Integrations.Agent.Provider
	case "ai":
		return m.rc.Team.Integrations.AI.Provider
	case "design":
		return m.rc.Team.Integrations.Design.Provider
	case "deploy":
		if m.rc.Team.Integrations.Deploy.Provider != "" {
			return m.rc.Team.Integrations.Deploy.Provider
		}
	}
	return "—"
}

func (m *settingsModel) setSize(w, h int) {
	m.width = w
	m.height = h
}
