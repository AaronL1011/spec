package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
)

type settingsField int

const (
	fieldName settingsField = iota
	fieldRole
	fieldHandle
	fieldTheme
	fieldRefresh
	fieldMouse
	fieldEditor
	fieldCount
)

type settingsEditMode int

const (
	settingsBrowse settingsEditMode = iota
	settingsEditing
)

// settingsAppliedMsg notifies the app that a field was confirmed in memory.
type settingsAppliedMsg struct {
	Field settingsField
}

// settingsPersistedMsg reports the result of an async user config write.
type settingsPersistedMsg struct {
	Field settingsField
	Err   error
}

// settingsThemePreviewMsg asks the app to apply a theme for live preview while
// the user edits the Theme field. The preview is not persisted: it is replaced
// on every selection change, reverted to the original on cancel, and committed
// only when the edit is confirmed.
type settingsThemePreviewMsg struct {
	Theme string
}

// previewTheme builds the command that triggers a live theme preview.
func previewTheme(name string) tea.Cmd {
	return func() tea.Msg { return settingsThemePreviewMsg{Theme: name} }
}

// settingsModel shows config, integration status, and editable user preferences.
type settingsModel struct {
	rc *config.ResolvedConfig

	focused   settingsField
	mode      settingsEditMode
	draft     string
	enumIdx   int
	dirty     bool
	fieldErr  string
	scroll    int
	width     int
	height    int
	styles    Styles
	keys      KeyMap
	snapshots map[settingsField]string
}

func newSettings(rc *config.ResolvedConfig, styles Styles, keys KeyMap) settingsModel {
	return settingsModel{
		rc:        rc,
		styles:    styles,
		keys:      keys,
		snapshots: make(map[settingsField]string),
	}
}

func (m settingsModel) isEditing() bool {
	return m.mode == settingsEditing
}

func (m *settingsModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m settingsModel) update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsPersistedMsg:
		if msg.Err != nil {
			m.revertField(msg.Field)
		} else {
			m.fieldErr = ""
			m.snapshots[msg.Field] = m.savedValue(msg.Field)
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == settingsEditing {
			return m.updateEditing(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m settingsModel) updateBrowse(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.focusPrev()
		m.ensureFocusVisible()
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.focusNext()
		m.ensureFocusVisible()
		return m, nil
	case key.Matches(msg, m.keys.PageDown):
		m.scrollBy(m.visibleHeight())
		return m, nil
	case key.Matches(msg, m.keys.PageUp):
		m.scrollBy(-m.visibleHeight())
		return m, nil
	case key.Matches(msg, m.keys.ScrollDown):
		m.scrollBy(1)
		return m, nil
	case key.Matches(msg, m.keys.ScrollUp):
		m.scrollBy(-1)
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.beginEdit()
	}
	return m, nil
}

func (m settingsModel) updateEditing(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		return m.cancelEdit()
	case key.Matches(msg, m.keys.Enter):
		return m.confirmEdit()
	case msg.Type == tea.KeySpace:
		if m.isEnumField(m.focused) {
			m.cycleEnumForward()
			return m, m.themePreviewCmd()
		} else if m.isTextField(m.focused) {
			m.appendDraft(" ")
		}
	case msg.Type == tea.KeyRunes:
		runes := string(msg.Runes)
		// l/h cycle enum fields (Role/Theme); for text fields they are
		// ordinary characters and must be inserted like any other rune.
		if m.isEnumField(m.focused) {
			switch runes {
			case " ", "l":
				m.cycleEnumForward()
				return m, m.themePreviewCmd()
			case "h":
				m.cycleEnumReverse()
				return m, m.themePreviewCmd()
			}
		} else if m.isTextField(m.focused) {
			m.appendDraft(runes)
		}
	case msg.Type == tea.KeyBackspace:
		if m.isTextField(m.focused) {
			m.backspaceDraft()
		}
	}
	return m, nil
}

// themePreviewCmd previews the current Theme draft, or nil for other fields.
func (m settingsModel) themePreviewCmd() tea.Cmd {
	if m.focused != fieldTheme {
		return nil
	}
	return previewTheme(m.draftEnumValue(fieldTheme))
}

func (m *settingsModel) focusNext() {
	m.focused = (m.focused + 1) % fieldCount
}

func (m *settingsModel) focusPrev() {
	m.focused = (m.focused + fieldCount - 1) % fieldCount
}

func (m *settingsModel) beginEdit() (settingsModel, tea.Cmd) {
	m.mode = settingsEditing
	m.fieldErr = ""
	m.draft = m.savedValue(m.focused)
	m.enumIdx = m.enumIndexForValue(m.focused, m.draft)
	m.dirty = false
	m.snapshots[m.focused] = m.draft
	return *m, nil
}

func (m settingsModel) cancelEdit() (settingsModel, tea.Cmd) {
	// Revert any live theme preview back to the value captured when the edit
	// began before discarding the draft.
	var cmd tea.Cmd
	if m.focused == fieldTheme {
		cmd = previewTheme(m.snapshots[fieldTheme])
	}
	m.mode = settingsBrowse
	m.fieldErr = ""
	m.draft = ""
	m.dirty = false
	return m, cmd
}

func (m settingsModel) confirmEdit() (settingsModel, tea.Cmd) {
	value, err := m.validateDraft()
	if err != nil {
		m.fieldErr = err.Error()
		return m, nil
	}

	m.ensureUserConfig()
	m.applyValue(m.focused, value)
	m.mode = settingsBrowse
	m.fieldErr = ""
	m.draft = ""
	m.dirty = false
	m.snapshots[m.focused] = value

	applied := func() tea.Msg {
		return settingsAppliedMsg{Field: m.focused}
	}
	return m, tea.Batch(applied, persistSettingsField(m.rc, m.focused))
}

func (m *settingsModel) revertField(field settingsField) {
	if snap, ok := m.snapshots[field]; ok {
		m.applyValue(field, snap)
	}
	m.fieldErr = ""
	if m.focused == field {
		m.draft = m.savedValue(field)
		m.enumIdx = m.enumIndexForValue(field, m.draft)
	}
}

func (m *settingsModel) ensureUserConfig() {
	if m.rc.User != nil {
		return
	}
	cfg, path := config.LoadUserConfigWithDefaults()
	m.rc.User = cfg
	if m.rc.UserConfigPath == "" {
		m.rc.UserConfigPath = path
	}
}

func (m settingsModel) savedValue(field settingsField) string {
	m.ensureUserConfigPtr()
	switch field {
	case fieldName:
		return m.rc.User.User.Name
	case fieldRole:
		r := m.rc.OwnerRole("")
		if r == "" {
			return config.ValidRoles()[0]
		}
		return r
	case fieldHandle:
		return m.rc.User.User.Handle
	case fieldTheme:
		if m.rc.User.Preferences.Theme != "" {
			return m.rc.User.Preferences.Theme
		}
		return "auto"
	case fieldRefresh:
		if m.rc.User.Preferences.RefreshInterval != "" {
			return m.rc.User.Preferences.RefreshInterval
		}
		return "30s"
	case fieldEditor:
		if m.rc.User.Preferences.Editor != "" {
			return m.rc.User.Preferences.Editor
		}
		return "vi"
	case fieldMouse:
		return onOff(m.rc.User.Preferences.Mouse)
	default:
		return ""
	}
}

func (m *settingsModel) ensureUserConfigPtr() {
	if m.rc.User == nil {
		m.ensureUserConfig()
	}
}

func (m *settingsModel) applyValue(field settingsField, value string) {
	m.ensureUserConfig()
	switch field {
	case fieldName:
		m.rc.User.User.Name = value
	case fieldRole:
		m.rc.User.User.OwnerRole = strings.ToLower(value)
	case fieldHandle:
		m.rc.User.User.Handle = value
	case fieldTheme:
		m.rc.User.Preferences.Theme = value
	case fieldRefresh:
		m.rc.User.Preferences.RefreshInterval = value
	case fieldEditor:
		m.rc.User.Preferences.Editor = value
	case fieldMouse:
		m.rc.User.Preferences.Mouse = value == "on"
	}
}

func (m settingsModel) validateDraft() (string, error) {
	switch m.focused {
	case fieldName:
		v := strings.TrimSpace(m.draft)
		if v == "" {
			return "", fmt.Errorf("name cannot be empty")
		}
		return v, nil
	case fieldRole:
		v := m.draftEnumValue(fieldRole)
		if !config.IsValidRole(v) {
			return "", fmt.Errorf("invalid role")
		}
		return strings.ToLower(v), nil
	case fieldHandle:
		v := strings.TrimSpace(m.draft)
		if v == "" {
			return "", fmt.Errorf("handle cannot be empty")
		}
		return v, nil
	case fieldTheme:
		v := m.draftEnumValue(fieldTheme)
		if !slices.Contains(ThemeNames(), v) {
			return "", fmt.Errorf("invalid theme")
		}
		return v, nil
	case fieldRefresh:
		v := strings.TrimSpace(m.draft)
		if _, err := parseRefreshPref(v); err != nil {
			return "", err
		}
		return v, nil
	case fieldEditor:
		v := strings.TrimSpace(m.draft)
		if v == "" {
			return "", fmt.Errorf("editor cannot be empty")
		}
		return v, nil
	case fieldMouse:
		v := m.draftEnumValue(fieldMouse)
		if v != "on" && v != "off" {
			return "", fmt.Errorf("mouse must be on or off")
		}
		return v, nil
	default:
		return "", fmt.Errorf("unknown field")
	}
}

func (m settingsModel) isTextField(field settingsField) bool {
	switch field {
	case fieldName, fieldHandle, fieldRefresh, fieldEditor:
		return true
	default:
		return false
	}
}

// isEnumField reports whether a field cycles through a fixed value set (and so
// is driven by space/l/h rather than text entry).
func (m settingsModel) isEnumField(field settingsField) bool {
	switch field {
	case fieldRole, fieldTheme, fieldMouse:
		return true
	default:
		return false
	}
}

// onOff maps a bool to the enum tokens used for boolean settings.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m *settingsModel) appendDraft(s string) {
	m.draft += s
	m.dirty = m.draft != m.snapshots[m.focused]
}

func (m *settingsModel) backspaceDraft() {
	if m.draft == "" {
		return
	}
	m.draft = dropLastRune(m.draft)
	m.dirty = m.draft != m.snapshots[m.focused]
}

func (m *settingsModel) cycleEnumForward() {
	list := m.enumList(m.focused)
	if len(list) == 0 {
		return
	}
	m.enumIdx = (m.enumIdx + 1) % len(list)
	m.draft = list[m.enumIdx]
	m.dirty = m.draft != m.snapshots[m.focused]
}

func (m *settingsModel) cycleEnumReverse() {
	list := m.enumList(m.focused)
	if len(list) == 0 {
		return
	}
	m.enumIdx = (m.enumIdx - 1 + len(list)) % len(list)
	m.draft = list[m.enumIdx]
	m.dirty = m.draft != m.snapshots[m.focused]
}

func (m settingsModel) enumList(field settingsField) []string {
	switch field {
	case fieldRole:
		return config.ValidRoles()
	case fieldTheme:
		return ThemeNames()
	case fieldMouse:
		return []string{"off", "on"}
	default:
		return nil
	}
}

func (m settingsModel) draftEnumValue(field settingsField) string {
	if m.isTextField(field) {
		return m.draft
	}
	list := m.enumList(field)
	if m.enumIdx >= 0 && m.enumIdx < len(list) {
		return list[m.enumIdx]
	}
	return m.draft
}

func (m settingsModel) enumIndexForValue(field settingsField, value string) int {
	list := m.enumList(field)
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return 0
}

func parseRefreshPref(pref string) (time.Duration, error) {
	if pref == "" {
		return 0, fmt.Errorf("refresh interval cannot be empty")
	}
	d, err := time.ParseDuration(pref)
	if err != nil {
		return 0, fmt.Errorf("invalid refresh interval")
	}
	if d < 5*time.Second {
		return 0, fmt.Errorf("refresh interval must be at least 5s")
	}
	return d, nil
}

func persistSettingsField(rc *config.ResolvedConfig, field settingsField) tea.Cmd {
	path := rc.UserConfigPath
	user := rc.User
	return func() tea.Msg {
		var err error
		if user != nil && path != "" {
			err = config.WriteUserConfig(path, user)
		} else {
			err = fmt.Errorf("user config not available")
		}
		return settingsPersistedMsg{Field: field, Err: err}
	}
}

func (m *settingsModel) view() string {
	return m.buildView()
}

type settingsLine struct {
	text     string
	field    settingsField
	editable bool
}

func (m settingsModel) visibleHeight() int {
	visible := m.height
	if visible < 3 {
		return 3
	}
	return visible
}

// totalScreenLines returns the number of screen lines after flattening all entries.
func (m settingsModel) totalScreenLines() int {
	entries := m.layoutLines()
	n := 0
	for _, e := range entries {
		n += strings.Count(e.text, "\n")
	}
	return n
}

func (m *settingsModel) maxScroll() int {
	mx := m.totalScreenLines() - m.visibleHeight()
	if mx < 0 {
		return 0
	}
	return mx
}

func (m *settingsModel) clampScroll() {
	if m.scroll < 0 {
		m.scroll = 0
	}
	if mx := m.maxScroll(); m.scroll > mx {
		m.scroll = mx
	}
}

func (m *settingsModel) scrollBy(delta int) {
	m.scroll += delta
	m.clampScroll()
}

// focusedScreenLine returns the screen-line index where the focused
// editable field starts within the flattened screen-line slice.
func (m settingsModel) focusedScreenLine(entries []settingsLine) int {
	line := 0
	for _, e := range entries {
		if e.editable && e.field == m.focused {
			return line
		}
		line += strings.Count(e.text, "\n")
	}
	return 0
}

// ensureFocusVisible adjusts scroll so the focused field is on screen,
// without resetting a manual scroll that already shows the field.
func (m *settingsModel) ensureFocusVisible() {
	entries := m.layoutLines()
	focusLine := m.focusedScreenLine(entries)
	visible := m.visibleHeight()
	if focusLine < m.scroll {
		m.scroll = focusLine
	} else if focusLine >= m.scroll+visible {
		m.scroll = focusLine - visible + 1
	}
	m.clampScroll()
}

func (m settingsModel) layoutLines() []settingsLine {
	var lines []settingsLine
	appendLine := func(text string, field settingsField, editable bool) {
		lines = append(lines, settingsLine{text: text, field: field, editable: editable})
	}

	appendLine(m.styles.SectionTitle.Render("  Identity")+"\n", fieldCount, false)
	appendLine(m.renderEditableRow("Name", fieldName)+"\n", fieldName, true)
	appendLine(m.renderEditableRow("Role", fieldRole)+"\n", fieldRole, true)
	appendLine(m.renderEditableRow("Handle", fieldHandle)+"\n", fieldHandle, true)
	if m.rc.TeamName() != "" {
		appendLine(m.renderReadOnlyRow("Team", m.rc.TeamName()), fieldCount, false)
	}
	if m.rc.CycleLabel() != "" {
		appendLine(m.renderReadOnlyRow("Cycle", m.rc.CycleLabel()), fieldCount, false)
	}
	appendLine("\n", fieldCount, false)

	appendLine(m.styles.SectionTitle.Render("  Appearance")+"\n", fieldCount, false)
	appendLine(m.renderEditableRow("Theme", fieldTheme)+"\n", fieldTheme, true)
	appendLine(m.renderEditableRow("Refresh", fieldRefresh)+"\n", fieldRefresh, true)
	appendLine(m.renderEditableRow("Mouse", fieldMouse)+"\n", fieldMouse, true)
	appendLine(m.renderEditableRow("Editor", fieldEditor)+"\n", fieldEditor, true)
	appendLine("\n", fieldCount, false)

	appendLine(m.styles.SectionTitle.Render("  Integrations")+"\n", fieldCount, false)
	for _, ig := range settingsIntegrations {
		appendLine(m.renderIntegrationRow(ig.name, ig.category), fieldCount, false)
	}
	appendLine("\n", fieldCount, false)

	appendLine(m.styles.SectionTitle.Render("  Config Paths")+"\n", fieldCount, false)
	appendLine(m.renderReadOnlyRow("User", m.rc.UserConfigPath), fieldCount, false)
	appendLine(m.renderReadOnlyRow("Team", m.rc.TeamConfigPath), fieldCount, false)
	if m.rc.SpecsRepoDir != "" {
		appendLine(m.renderReadOnlyRow("Specs", m.rc.SpecsRepoDir), fieldCount, false)
	}
	return lines
}

// buildView renders the visible slice of the settings panel.
func (m *settingsModel) buildView() string {
	entries := m.layoutLines()

	// Concatenate all entry texts, then split into screen lines.
	var full strings.Builder
	for _, e := range entries {
		full.WriteString(e.text)
	}
	allLines := strings.Split(full.String(), "\n")
	// Remove trailing empty line from final newline.
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	m.clampScroll()
	visible := m.visibleHeight()
	start := m.scroll
	end := start + visible
	if end > len(allLines) {
		end = len(allLines)
	}

	var b strings.Builder
	for i, l := range allLines[start:end] {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l)
	}
	return b.String()
}

type settingsIntegration struct {
	name     string
	category string
}

var settingsIntegrations = []settingsIntegration{
	{"Comms", "comms"},
	{"PM", "pm"},
	{"Docs", "docs"},
	{"Repo", "repo"},
	{"Agent", "agent"},
	{"AI", "ai"},
	{"Design", "design"},
	{"Deploy", "deploy"},
}

func (m settingsModel) displayValue(field settingsField) string {
	if m.mode == settingsEditing && m.focused == field {
		if m.isTextField(field) {
			return m.draft
		}
		return m.draftEnumValue(field)
	}
	return m.savedValue(field)
}

func (m settingsModel) renderEditableRow(label string, field settingsField) string {
	value := m.displayValue(field)
	if value == "" {
		value = "—"
	}

	editing := m.mode == settingsEditing && m.focused == field
	focused := m.mode == settingsBrowse && m.focused == field

	labelStyle := m.styles.Subtitle.Render(fmt.Sprintf("%-12s", label))
	if editing {
		labelStyle = m.styles.Accent.Render(fmt.Sprintf("%-12s", label+" editing…"))
	}

	var valuePart strings.Builder
	if editing && m.isTextField(field) {
		valuePart.WriteString(m.styles.RowNormal.Render(value))
		valuePart.WriteString(m.styles.Accent.Render(IconCaret))
	} else {
		valuePart.WriteString(m.styles.RowNormal.Render(value))
	}

	if editing && m.isEnumField(field) {
		valuePart.WriteString(" ")
		valuePart.WriteString(m.styles.Muted.Render("(space/l/h cycle)"))
	} else if m.mode == settingsBrowse {
		valuePart.WriteString(" ")
		valuePart.WriteString(m.styles.Muted.Render("(Enter)"))
	}

	line := fmt.Sprintf("    %s  %s", labelStyle, valuePart.String())

	if m.fieldErr != "" && (editing || focused) && m.focused == field {
		line += "\n    " + m.styles.Error.Render(m.fieldErr)
	}

	if focused {
		return m.styles.RowSelected.Render(line)
	}
	if editing {
		return m.styles.Accent.Render(IconCursor+" ") + line
	}
	return line
}

func (m settingsModel) renderReadOnlyRow(label, value string) string {
	if value == "" {
		value = "—"
	}
	return fmt.Sprintf("    %s  %s\n",
		m.styles.Subtitle.Render(fmt.Sprintf("%-12s", label)),
		m.styles.Muted.Render(value),
	)
}

func (m settingsModel) renderIntegrationRow(name, category string) string {
	provider := "—"
	status := m.styles.Muted
	if m.rc.HasIntegration(category) {
		provider = m.integrationProvider(category)
		status = m.styles.Success
	}
	label := fmt.Sprintf("    %-10s", name)
	return m.styles.RowNormal.Render(label) + status.Render(provider) + "\n"
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
