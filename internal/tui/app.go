package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/tui/components"
)

const defaultRefreshInterval = 30 * time.Second
const spinnerInterval = 100 * time.Millisecond

const (
	refreshKeyDashboard = "dashboard"
	refreshKeyPipeline  = "pipeline"
	refreshKeySpecs     = "specs"
	refreshKeyTriage    = "triage"
	refreshKeyReviews   = "reviews"
)

// tickMsg fires on each refresh interval.
type tickMsg time.Time

type spinnerTickMsg time.Time

type themePersistedMsg struct {
	Err error
}

// App is the top-level Bubble Tea model. It owns the tab strip, header,
// status bar, and delegates to the active view.
type App struct {
	rc   *config.ResolvedConfig
	reg  *adapter.Registry
	role string
	db   *store.DB

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

	// Views — top-level tabs
	activeView View
	dashboard  dashboardModel
	pipeline   pipelineModel
	specs      specListModel
	triage     triageModel
	reviews    reviewModel
	settings   settingsModel
	help       helpModel

	// Detail drill-down
	showDetail bool
	detail     specDetailModel
	detailFrom View // which view we drilled in from

	// Overlays
	modal   components.Modal
	toast   components.Toast
	standup standupOverlay
	intake  intakeFormState

	// Pending action context (for modal confirmations)
	pendingAction string
	pendingSpecID string

	// Focused spec ID — displayed with ★ in list views.
	focusedSpecID string

	// Refresh
	refreshInterval time.Duration
	refreshInFlight map[string]bool
	spinnerOn       bool
}

// New creates a new App ready to run as a tea.Program.
func New(rc *config.ResolvedConfig, reg *adapter.Registry, role string) App {
	db, _ := store.Open(store.DefaultDBPath())
	return newAppWithDB(rc, reg, role, db)
}

func newAppWithDB(rc *config.ResolvedConfig, reg *adapter.Registry, role string, db *store.DB) App {
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
		Stale:   styles.Muted,
	}
	sb := components.NewStatusBar(sbStyles)
	sb.SetView(ViewDashboard.Label())

	return App{
		rc:         rc,
		reg:        reg,
		role:       role,
		db:         db,
		theme:      theme,
		styles:     styles,
		keys:       keys,
		header:     header,
		tabs:       tabs,
		statusBar:  sb,
		activeView: ViewDashboard,
		dashboard:  newDashboard(rc, reg, role, styles, keys),
		pipeline:   newPipeline(rc, styles, keys),
		specs:      newSpecList(rc, styles, keys),
		triage:     newTriage(rc, styles, keys),
		reviews:    newReview(rc, reg, styles, keys),
		settings:   newSettings(rc, styles, keys),
		help:       newHelp(keys, styles),
		modal: components.NewModal(components.ModalStyles{
			Border:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Accent),
			Title:   styles.Title,
			Message: styles.Subtitle,
			Input:   lipgloss.NewStyle().Foreground(theme.Text).Background(theme.Surface).Padding(0, 1),
			Hint:    styles.Muted,
		}),
		toast: components.NewToast(components.ToastStyles{
			Success: lipgloss.NewStyle().Foreground(theme.Base).Background(theme.Success).Padding(0, 1),
			Error:   lipgloss.NewStyle().Foreground(theme.Base).Background(theme.Error).Padding(0, 1),
			Info:    lipgloss.NewStyle().Foreground(theme.Base).Background(theme.Accent).Padding(0, 1),
		}),
		refreshInterval: parseRefreshInterval(rc),
		refreshInFlight: make(map[string]bool),
		focusedSpecID:   loadFocusedSpec(db),
	}
}

// loadFocusedSpec reads the focused spec ID from the store.
func loadFocusedSpec(db *store.DB) string {
	if db == nil {
		return ""
	}
	id, _ := db.FocusedSpecGet()
	return id
}

// parseRefreshInterval reads the user's preferred refresh interval or
// returns the default.
func parseRefreshInterval(rc *config.ResolvedConfig) time.Duration {
	if rc.User != nil && rc.User.Preferences.RefreshInterval != "" {
		if d, err := time.ParseDuration(rc.User.Preferences.RefreshInterval); err == nil && d >= 5*time.Second {
			return d
		}
	}
	return defaultRefreshInterval
}

// isSpecID returns true if the ID looks like a spec (not a PR or triage item).
func isSpecID(id string) bool {
	return strings.HasPrefix(id, "SPEC-")
}

// Init runs the initial commands — fetch data + start tick.
func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.dashboard.init(),
		a.tick(),
		a.spinnerTick(),
	)
}

// Update handles all messages.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.propagateSize()
		if a.showDetail && a.detail.readerMode {
			var cmd tea.Cmd
			a.detail, cmd = a.detail.requestCurrentSectionRender()
			a.syncBusyState()
			return a, cmd
		}
		return a, nil

	case tickMsg:
		cmd := a.refreshActiveView()
		return a, tea.Batch(cmd, a.tick())

	case spinnerTickMsg:
		if a.spinnerOn {
			a.statusBar.NextSpinner()
		}
		return a, a.spinnerTick()

	// Data messages — route to the owning view regardless of which is active.
	case dashboardDataMsg:
		a.markRefreshDone(refreshKeyDashboard)
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.update(msg)
		a.statusBar.SetPending(a.dashboard.pendingCount())
		a.statusBar.SetRefresh(time.Now())
		return a, cmd

	case pipelineDataMsg:
		a.markRefreshDone(refreshKeyPipeline)
		var cmd tea.Cmd
		a.pipeline, cmd = a.pipeline.update(msg)
		return a, cmd

	case specListDataMsg:
		a.markRefreshDone(refreshKeySpecs)
		var cmd tea.Cmd
		a.specs, cmd = a.specs.update(msg)
		return a, cmd

	case triageDataMsg:
		a.markRefreshDone(refreshKeyTriage)
		var cmd tea.Cmd
		a.triage, cmd = a.triage.update(msg)
		return a, cmd

	case reviewDataMsg:
		a.markRefreshDone(refreshKeyReviews)
		var cmd tea.Cmd
		a.reviews, cmd = a.reviews.update(msg)
		return a, cmd

	case specDetailDataMsg:
		a.markDetailRefreshDone()
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		return a, cmd

	case sectionRenderedMsg:
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		a.syncBusyState()
		return a, cmd

	// Theme cycling from settings view.
	case cycleThemeMsg:
		return a, a.cycleTheme()

	case themePersistedMsg:
		if msg.Err != nil {
			a.toast.Show(msg.Err.Error(), components.ToastError, 5*time.Second)
		}
		return a, nil

	// Standup data arrived.
	case standupDataMsg:
		if msg.Err != nil {
			a.toast.Show(msg.Err.Error(), components.ToastError, 5*time.Second)
		} else {
			a.standup.show(msg.Text)
		}
		return a, nil

	// Navigation messages from views.
	case navigateToSpecMsg:
		return a, a.openDetail(msg.SpecID)

	case navigateBackMsg:
		return a, a.closeDetail()

	// Action results — show toast and refresh.
	case actionResultMsg:
		if msg.Err != nil {
			a.toast.Show(msg.Err.Error(), components.ToastError, 5*time.Second)
		} else {
			label := msg.Action
			if msg.Detail != "" {
				label += ": " + msg.Detail
			}
			if msg.SpecID != "" {
				label = msg.SpecID + " " + label
			}
			a.toast.Show(label, components.ToastSuccess, 3*time.Second)
		}
		// Refresh focused spec after focus/unfocus actions.
		if msg.Action == "focus" || msg.Action == "unfocus" {
			a.focusedSpecID = loadFocusedSpec(a.db)
		}
		return a, a.refreshActiveView()

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	// Non-key messages — delegate to active view.
	return a, a.delegateToActive(msg)
}

// handleKey is the single entry point for all keyboard input.
// It follows a strict priority chain — the first match wins, no fall-through.
func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ── Layer 1: Overlays (absorb all keys) ──────────────────────────
	// These are modal states that must capture every keystroke.

	if a.standup.visible {
		return a.updateStandup(msg)
	}
	if a.intake.active {
		return a.updateIntake(msg)
	}
	if a.help.visible {
		a.help, _ = a.help.update(msg)
		return a, nil
	}
	if a.modal.Visible {
		return a.updateModal(msg)
	}

	// ── Layer 2: Escape hatch (always works) ─────────────────────────

	if msg.Type == tea.KeyCtrlC {
		return a, tea.Quit
	}

	// ── Layer 3: Text input mode (view captures keystrokes) ─────────
	// The active view is consuming typed characters (e.g. search bar).
	// Only Ctrl+C (above) bypasses this.

	if a.viewCapturingInput() {
		return a, a.delegateToActive(msg)
	}

	// ── Layer 4: Detail view ─────────────────────────────────────────
	// When a spec detail is open, it handles all keys. The detail
	// model internally decides what to do (scroll, reader nav, etc.).
	// Global keys (quit, view switch) are checked first.

	if a.showDetail {
		return a.updateDetail(msg)
	}

	// ── Layer 5: Global keys (work on every top-level view) ─────────

	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit
	case key.Matches(msg, a.keys.Help):
		a.help.setContext(a.activeView.Label())
		a.help.toggle()
		return a, nil
	case key.Matches(msg, a.keys.Refresh):
		return a, a.refreshActiveView()

	// View switching.
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

	// Creation hotkeys.
	case key.Matches(msg, a.keys.NewIntake):
		a.intake.open()
		return a, nil
	case key.Matches(msg, a.keys.NewSpec):
		a.pendingAction = "new"
		a.modal.ShowInput("New Spec", "Title:")
		a.modal.SetSize(a.width, a.contentHeight())
		return a, nil
	case key.Matches(msg, a.keys.Standup):
		return a, generateStandup(a.rc, a.reg, a.db)

	// Enter drills into spec detail.
	case key.Matches(msg, a.keys.Enter):
		if specID := a.selectedSpecID(); isSpecID(specID) {
			return a, a.openDetail(specID)
		}

	// Open in browser (Reviews tab only).
	case key.Matches(msg, a.keys.Open):
		if a.activeView == ViewReviews {
			if url := a.reviews.selectedURL(); url != "" {
				return a, openInBrowser(url)
			}
		}
	}

	// ── Layer 6: Spec action hotkeys ─────────────────────────────────
	// These require a selected spec. If no spec is selected or the
	// binding doesn't match, we fall through to the view.

	if cmd, handled := a.handleSpecAction(a.selectedSpecID(), msg); handled {
		return a, cmd
	}

	// ── Layer 7: Delegate to active view ─────────────────────────────
	// Navigation (j/k), search (/), and view-specific keys.

	return a, a.delegateToActive(msg)
}

// View renders the full application.
func (a App) View() string {
	if a.width == 0 {
		return "Initialising…"
	}

	a.statusBar.SetScroll(a.activeScrollInfo())
	a.dashboard.focusedSpecID = a.focusedSpecID

	// Update breadcrumb for reader mode.
	if a.showDetail && a.detail.readerMode {
		sections := a.detail.readableSections()
		if a.detail.sectionIdx < len(sections) {
			sec := sections[a.detail.sectionIdx]
			a.statusBar.SetView(a.activeView.Label() + " › " + a.detail.specID + " › § " + sec.Slug)
		}
	}

	header := a.header.View()
	tabs := a.tabs.View()
	statusBar := a.statusBar.View()

	headerHeight := a.header.Height()
	chromeHeight := headerHeight + 2 // tabs + status bar
	contentHeight := a.height - chromeHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch {
	case a.standup.visible:
		content = a.standup.view()
	case a.intake.active:
		content = a.renderIntakeForm()
	case a.help.visible:
		content = a.help.view()
	case a.modal.Visible:
		content = a.modal.View()
	case a.showDetail:
		content = a.detail.view()
	default:
		content = a.activeViewContent()
	}

	lines := normalizeContentLines(content, a.width, contentHeight)

	var out string
	out += header + "\n"
	out += tabs + "\n"
	for _, l := range lines {
		out += l + "\n"
	}

	// Toast is shown within the status bar area.
	if a.toast.Visible() {
		toastStr := a.toast.View()
		// Render toast left-aligned, then fill remainder with status bar bg.
		gap := a.width - lipgloss.Width(toastStr)
		if gap < 0 {
			gap = 0
		}
		out += toastStr + a.styles.StatusBar.Render(strings.Repeat(" ", gap))
	} else {
		out += statusBar
	}

	return out
}

// selectedSpecID returns the spec ID of the currently selected item
// activeScrollInfo returns a scroll position string for the status bar.
func (a App) activeScrollInfo() string {
	if a.showDetail {
		if mx := a.detail.maxScroll(); mx > 0 {
			return fmt.Sprintf("%d/%d", a.detail.scroll+1, a.detail.contentLines)
		}
		return ""
	}
	switch a.activeView {
	case ViewDashboard:
		if n := len(a.dashboard.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.dashboard.cursor+1, n)
		}
	case ViewPipeline:
		if id := a.pipeline.selectedSpecID(); id != "" {
			// Count total specs across stages.
			total := 0
			pos := 0
			for si, stage := range a.pipeline.stages {
				for ri := range stage.Specs {
					if si == a.pipeline.stageIdx && ri == a.pipeline.specIdx {
						pos = total + 1
					}
					total++
				}
			}
			if total > 0 {
				return fmt.Sprintf("%d/%d", pos, total)
			}
		}
	case ViewSpecs:
		if n := len(a.specs.filtered); n > 0 {
			return fmt.Sprintf("%d/%d", a.specs.cursor+1, n)
		}
	case ViewTriage:
		if n := len(a.triage.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.triage.cursor+1, n)
		}
	case ViewReviews:
		if n := len(a.reviews.items); n > 0 {
			return fmt.Sprintf("%d/%d", a.reviews.cursor+1, n)
		}
	}
	return ""
}

// selectedSpecID returns the spec ID of the currently selected item
// in the active view, if applicable.
func (a App) selectedSpecID() string {
	switch a.activeView {
	case ViewDashboard:
		return a.dashboard.selectedSpecID()
	case ViewPipeline:
		return a.pipeline.selectedSpecID()
	case ViewSpecs:
		return a.specs.selectedSpecID()
	case ViewTriage:
		return a.triage.selectedItemID()
	default:
		return ""
	}
}

// viewCapturingInput returns true when the active view is in a text
// input mode (e.g. search bar) and keystrokes should be routed to
// the view instead of being interpreted as hotkeys.
func (a App) viewCapturingInput() bool {
	if a.activeView == ViewSpecs {
		return a.specs.isInputActive()
	}
	return false
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
	// Close detail if switching views.
	a.markDetailRefreshDone()
	if a.showDetail {
		a.detail.cancelRender()
	}
	a.showDetail = false
	a.activeView = v
	a.tabs.SetActive(int(v))
	a.statusBar.SetView(v.Label())
	a.syncBusyState()
	return a.initAndRefreshView(v)
}

// initAndRefreshView initialises a view if it hasn't been loaded, or refreshes it.
func (a *App) initAndRefreshView(v View) tea.Cmd {
	switch v {
	case ViewDashboard:
		return a.scheduleRefresh(refreshKeyDashboard, a.dashboard.refresh())
	case ViewPipeline:
		if a.pipeline.loading {
			return a.scheduleRefresh(refreshKeyPipeline, a.pipeline.init())
		}
		return a.scheduleRefresh(refreshKeyPipeline, a.pipeline.refresh())
	case ViewSpecs:
		if a.specs.loading {
			return a.scheduleRefresh(refreshKeySpecs, a.specs.init())
		}
		return a.scheduleRefresh(refreshKeySpecs, a.specs.refresh())
	case ViewTriage:
		if a.triage.loading {
			return a.scheduleRefresh(refreshKeyTriage, a.triage.init())
		}
		return a.scheduleRefresh(refreshKeyTriage, a.triage.refresh())
	case ViewReviews:
		if a.reviews.loading {
			return a.scheduleRefresh(refreshKeyReviews, a.reviews.init())
		}
		return a.scheduleRefresh(refreshKeyReviews, a.reviews.refresh())
	default:
		return nil
	}
}

func (a *App) refreshActiveView() tea.Cmd {
	if a.showDetail {
		if a.detail.readerMode {
			return nil
		}
		return a.scheduleRefresh(a.detailRefreshKey(), a.detail.fetchData())
	}
	return a.initAndRefreshView(a.activeView)
}

func (a *App) scheduleRefresh(key string, cmd tea.Cmd) tea.Cmd {
	if key == "" || cmd == nil {
		return nil
	}
	if a.refreshInFlight == nil {
		a.refreshInFlight = make(map[string]bool)
	}
	if a.refreshInFlight[key] {
		return nil
	}
	a.refreshInFlight[key] = true
	return cmd
}

func (a *App) markRefreshDone(key string) {
	if a.refreshInFlight != nil {
		a.refreshInFlight[key] = false
	}
}

func (a *App) markDetailRefreshDone() {
	if a.refreshInFlight == nil {
		return
	}
	for key := range a.refreshInFlight {
		if key == "detail" || strings.HasPrefix(key, "detail:") {
			a.refreshInFlight[key] = false
		}
	}
}

func (a App) detailRefreshKey() string {
	if !a.showDetail || a.detail.specID == "" {
		return "detail"
	}
	return "detail:" + a.detail.specID
}

func (a *App) openDetail(specID string) tea.Cmd {
	a.showDetail = true
	a.detailFrom = a.activeView
	a.detail = newSpecDetail(a.rc, specID, a.styles, a.keys, a.theme)
	a.detail.setSize(a.width, a.contentHeight())
	a.statusBar.SetView(a.activeView.Label() + " › " + specID)
	a.syncBusyState()
	return a.detail.init()
}

func (a *App) closeDetail() tea.Cmd {
	a.markDetailRefreshDone()
	a.detail.cancelRender()
	a.showDetail = false
	a.statusBar.SetView(a.activeView.Label())
	a.syncBusyState()
	return nil
}

func (a App) updateDetail(msg tea.KeyMsg) (App, tea.Cmd) {
	// Quit always works.
	if key.Matches(msg, a.keys.Quit) {
		return a, tea.Quit
	}

	// Help.
	if key.Matches(msg, a.keys.Help) {
		a.help.setContext("Detail: " + a.detail.specID)
		a.help.toggle()
		return a, nil
	}

	// In reader mode, reserve digit keys for section jumps.
	// Keep tab/shift+tab view switching available.
	if a.detail.readerMode {
		switch {
		case key.Matches(msg, a.keys.NextTab):
			return a, a.switchView(a.activeView.Next())
		case key.Matches(msg, a.keys.PrevTab):
			return a, a.switchView(a.activeView.Prev())
		}
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		a.syncBusyState()
		return a, cmd
	}

	// View switching closes detail and switches.
	switch {
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
	}

	// Overview mode: Esc goes back to the list.
	if key.Matches(msg, a.keys.Back) {
		a.closeDetail()
		return a, nil
	}

	// Action hotkeys on the detail's spec (overview mode only).
	if cmd, handled := a.handleSpecAction(a.detail.specID, msg); handled {
		return a, cmd
	}

	// Delegate remaining keys (j/k scroll, o for reader) to detail.
	var cmd tea.Cmd
	a.detail, cmd = a.detail.update(msg)
	a.syncBusyState()
	return a, cmd
}

// handleSpecAction processes action hotkeys for a given spec ID.
// Returns (cmd, true) if the key was consumed, (nil, false) otherwise.
func (a *App) handleSpecAction(specID string, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, a.keys.Advance) && isSpecID(specID):
		a.pendingAction = "advance"
		a.pendingSpecID = specID
		a.modal.ShowConfirm("Advance "+specID, "Advance this spec to the next pipeline stage?")
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	case key.Matches(msg, a.keys.Block) && isSpecID(specID):
		a.pendingAction = "block"
		a.pendingSpecID = specID
		a.modal.ShowInput("Block "+specID, "Reason for blocking:")
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	case key.Matches(msg, a.keys.Unblock) && isSpecID(specID):
		a.pendingAction = "unblock"
		a.pendingSpecID = specID
		a.modal.ShowConfirm("Unblock "+specID, "Resume this spec from blocked status?")
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	case key.Matches(msg, a.keys.Focus) && specID != "":
		return focusSpec(a.db, specID), true
	case key.Matches(msg, a.keys.Unfocus):
		return unfocusSpec(a.db), true
	case key.Matches(msg, a.keys.Yank) && specID != "":
		return yankSpecID(specID), true
	case key.Matches(msg, a.keys.Edit) && isSpecID(specID):
		editor := "vi"
		if a.rc.User != nil && a.rc.User.Preferences.Editor != "" {
			editor = a.rc.User.Preferences.Editor
		}
		return editSpec(a.rc, specID, editor), true
	case key.Matches(msg, a.keys.Build) && isSpecID(specID):
		return buildSpec(a.rc, specID), true
	case key.Matches(msg, a.keys.Decide) && isSpecID(specID):
		a.pendingAction = "decide"
		a.pendingSpecID = specID
		a.modal.ShowInput("Record Decision — "+specID, "Question or decision to record:")
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	}
	return nil, false
}

func (a App) updateModal(msg tea.KeyMsg) (App, tea.Cmd) {
	switch a.modal.Kind {
	case components.ModalConfirm:
		switch msg.Type {
		case tea.KeyRunes:
			if string(msg.Runes) == "y" {
				a.modal.Hide()
				return a, a.executeAction()
			}
			if string(msg.Runes) == "n" {
				a.modal.Hide()
				return a, nil
			}
		case tea.KeyEscape:
			a.modal.Hide()
			return a, nil
		}

	case components.ModalInput:
		switch msg.Type {
		case tea.KeyEscape:
			a.modal.Hide()
			return a, nil
		case tea.KeyEnter:
			if a.modal.Input != "" {
				a.modal.Hide()
				return a, a.executeAction()
			}
		case tea.KeyBackspace:
			a.modal.BackspaceInput()
		case tea.KeyRunes:
			a.modal.AppendInput(string(msg.Runes))
		}
	}
	return a, nil
}

// executeAction runs the pending action after modal confirmation.
func (a *App) executeAction() tea.Cmd {
	specID := a.pendingSpecID
	switch a.pendingAction {
	case "advance":
		return advanceSpec(a.rc, specID, a.role)
	case "block":
		reason := a.modal.Input
		if reason == "" {
			reason = "blocked from TUI"
		}
		return blockSpec(a.rc, specID, reason, a.rc.UserName())
	case "unblock":
		return unblockSpec(a.rc, specID)
	case "revert":
		reason := a.modal.Input
		if reason == "" {
			reason = "reverted from TUI"
		}
		pl := a.rc.Pipeline()
		target := ""
		if len(pl.Stages) > 0 {
			target = pl.Stages[0].Name
		}
		return revertSpec(a.rc, specID, target, reason, a.rc.UserName())
	case "decide":
		question := a.modal.Input
		if question == "" {
			return nil
		}
		return recordDecision(a.rc, specID, question)
	case "new":
		title := a.modal.Input
		if title == "" {
			return nil
		}
		return createSpec(a.rc, title)
	default:
		return nil
	}
}

// updateStandup handles keys within the standup overlay.
func (a App) updateStandup(msg tea.KeyMsg) (App, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEscape:
		a.standup.hide()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "c":
		// Copy standup text to clipboard.
		a.standup.hide()
		return a, yankText(a.standup.text)
	case key.Matches(msg, a.keys.Up):
		a.standup.scrollUp()
	case key.Matches(msg, a.keys.Down):
		a.standup.scrollDown()
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit
	}
	return a, nil
}

// updateIntake handles keys within the inline intake form.
func (a App) updateIntake(msg tea.KeyMsg) (App, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		a.intake.close()
	case tea.KeyTab:
		a.intake.nextField()
	case tea.KeyShiftTab:
		a.intake.prevField()
	case tea.KeyEnter:
		if a.intake.field == intakeFieldPriority {
			// Enter on priority cycles the value.
			a.intake.cyclePriority()
		} else if a.intake.field == intakeFieldTitle {
			// Enter on title advances to next field, not submit.
			a.intake.nextField()
		} else if a.intake.valid() {
			// Submit only from the last field (source).
			title := a.intake.title
			priority := a.intake.priority
			source := a.intake.source
			a.intake.close()
			return a, createTriageItem(a.rc, title, priority, source)
		}
	case tea.KeyBackspace:
		a.intake.backspaceField()
	case tea.KeyRunes:
		a.intake.appendToField(string(msg.Runes))
	}
	return a, nil
}

// renderIntakeForm draws the inline triage intake form.
func (a App) renderIntakeForm() string {
	f := a.intake
	var b strings.Builder

	b.WriteString(a.styles.Title.Render("  New Triage Item"))
	b.WriteString("\n\n")

	fields := []struct {
		label string
		value string
		idx   int
	}{
		{"Title", f.title, intakeFieldTitle},
		{"Priority", f.priority + "  " + a.styles.Muted.Render("(enter to cycle)"), intakeFieldPriority},
		{"Source", f.source, intakeFieldSource},
	}

	for _, fld := range fields {
		label := a.styles.Subtitle.Render(fmt.Sprintf("  %-10s", fld.label))
		value := fld.value
		if fld.idx == f.field {
			value += a.styles.Accent.Render("▌")
			b.WriteString(a.styles.Accent.Render("▸ "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(a.styles.RowNormal.Render(value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(a.styles.Muted.Render("  tab next field · enter submit/cycle · esc cancel"))

	return b.String()
}

func (a *App) delegateToActive(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.activeView {
	case ViewDashboard:
		a.dashboard, cmd = a.dashboard.update(msg)
	case ViewPipeline:
		a.pipeline, cmd = a.pipeline.update(msg)
	case ViewSpecs:
		a.specs, cmd = a.specs.update(msg)
	case ViewTriage:
		a.triage, cmd = a.triage.update(msg)
	case ViewReviews:
		a.reviews, cmd = a.reviews.update(msg)
	case ViewSettings:
		a.settings, cmd = a.settings.update(msg)
	}
	return cmd
}

func (a *App) propagateSize() {
	a.header.SetWidth(a.width)
	a.tabs.SetWidth(a.width)
	a.statusBar.SetWidth(a.width)

	ch := a.contentHeight()
	a.dashboard.setSize(a.width, ch)
	a.pipeline.setSize(a.width, ch)
	a.specs.setSize(a.width, ch)
	a.triage.setSize(a.width, ch)
	a.reviews.setSize(a.width, ch)
	a.settings.setSize(a.width, ch)
	if a.showDetail {
		a.detail.setSize(a.width, ch)
	}
	a.modal.SetSize(a.width, ch)
	a.help.setSize(a.width, ch)
	a.standup.setSize(a.width, ch)
}

// cycleTheme advances to the next theme, applies it, and persists the choice.
func (a *App) cycleTheme() tea.Cmd {
	names := ThemeNames()
	current := "auto"
	if a.rc.User != nil && a.rc.User.Preferences.Theme != "" {
		current = a.rc.User.Preferences.Theme
	}

	// Find current index, advance to next.
	next := 0
	for i, name := range names {
		if name == current {
			next = (i + 1) % len(names)
			break
		}
	}

	newName := names[next]
	a.applyTheme(newName)

	// Persist to user config.
	if a.rc.User != nil {
		a.rc.User.Preferences.Theme = newName
		userConfig := *a.rc.User
		userConfigPath := a.rc.UserConfigPath
		a.toast.Show("Theme: "+newName, components.ToastInfo, 2*time.Second)
		return func() tea.Msg {
			return themePersistedMsg{Err: config.WriteUserConfig(userConfigPath, &userConfig)}
		}
	}

	a.toast.Show("Theme: "+newName, components.ToastInfo, 2*time.Second)
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
	})
	a.statusBar.SetView(a.activeView.Label())
	a.statusBar.SetPending(a.dashboard.pendingCount())

	a.modal = components.NewModal(components.ModalStyles{
		Border:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(a.theme.Accent),
		Title:   a.styles.Title,
		Message: a.styles.Subtitle,
		Input:   lipgloss.NewStyle().Foreground(a.theme.Text).Background(a.theme.Surface).Padding(0, 1),
		Hint:    a.styles.Muted,
	})

	a.toast = components.NewToast(components.ToastStyles{
		Success: lipgloss.NewStyle().Foreground(a.theme.Base).Background(a.theme.Success).Padding(0, 1),
		Error:   lipgloss.NewStyle().Foreground(a.theme.Base).Background(a.theme.Error).Padding(0, 1),
		Info:    lipgloss.NewStyle().Foreground(a.theme.Base).Background(a.theme.Accent).Padding(0, 1),
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
		a.detail.readerCache = make(map[string]string)
		a.detail.readerGen++
	}

	a.propagateSize()
}

func (a App) contentHeight() int {
	headerH := a.header.Height()
	ch := a.height - headerH - 2
	if ch < 1 {
		ch = 1
	}
	return ch
}

func (a *App) syncBusyState() {
	busy := a.showDetail && a.detail.readerMode && (a.detail.renderInFlight || a.detail.renderQueued)
	a.spinnerOn = busy
	if !busy {
		a.statusBar.SetBusy(false, "")
		return
	}
	label := "rendering section"
	sections := a.detail.readableSections()
	if a.detail.sectionIdx >= 0 && a.detail.sectionIdx < len(sections) {
		label = "rendering § " + sections[a.detail.sectionIdx].Slug
	}
	a.statusBar.SetBusy(true, label)
}

func (a App) tick() tea.Cmd {
	return tea.Tick(a.refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (a App) spinnerTick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func normalizeContentLines(content string, width, height int) []string {
	lines := splitLines(content)
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = normalizeLineWidth(lines[i], width)
	}
	return lines
}

func normalizeLineWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = xansi.Truncate(line, width, "")
	w := xansi.StringWidth(line)
	if w < width {
		line += strings.Repeat(" ", width-w)
	}
	return line
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
