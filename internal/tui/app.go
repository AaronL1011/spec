package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/tui/components"
	"github.com/aaronl1011/spec/internal/tui/watch"
)

// watchDebounce coalesces a burst of writes (e.g. an agent editing a spec in a
// loop) into a single reader refresh.
const watchDebounce = 250 * time.Millisecond

const defaultRefreshInterval = 30 * time.Second
const spinnerInterval = 100 * time.Millisecond

// exitArmWindow is the duration after the first top-level esc press during
// which a second esc confirms quit. Too short feels broken; too long lets a
// stray second esc quit. 1.5 s matches the design spec.
const exitArmWindow = 1500 * time.Millisecond

const (
	refreshKeyDashboard = "dashboard"
	refreshKeyPipeline  = "pipeline"
	refreshKeySpecs     = "specs"
	refreshKeyTriage    = "triage"
	refreshKeyReviews   = "reviews"
)

// refreshKeyForView maps a top-level tab to its data-refresh key, used to drive
// the per-tab staleness indicator. Views without their own fetch (settings,
// help) return "" so the indicator hides.
func refreshKeyForView(v View) string {
	switch v {
	case ViewDashboard:
		return refreshKeyDashboard
	case ViewPipeline:
		return refreshKeyPipeline
	case ViewSpecs:
		return refreshKeySpecs
	case ViewTriage:
		return refreshKeyTriage
	case ViewReviews:
		return refreshKeyReviews
	default:
		return ""
	}
}

// tickMsg fires on each refresh interval.
type tickMsg time.Time

type spinnerTickMsg time.Time

// fileChangedMsg signals that one of the watched files backing the open spec
// changed on disk and the reader should re-read and re-render (SPEC-007).
type fileChangedMsg struct{ Paths []string }

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

	// File watcher for the open spec's markdown + thread sidecar (SPEC-007).
	// nil when no spec is open. Bound to the detail view lifecycle.
	watcher *watch.Watcher
	// watchRefreshPending marks that the next specDetailDataMsg was triggered
	// by a file-change event, so a calm "updated" cue can be shown on a real
	// content change.
	watchRefreshPending bool
	detailFrom          View // which view we drilled in from

	// Overlays
	modal   components.Modal
	standup standupOverlay
	intake  intakeFormState
	revert  revertOverlay

	// Triage detail pane and action overlays.
	showTriageDetail      bool
	triageDetail          *triageDetailPane
	triageDetailRehydrate string // triage ID to rehydrate after next list refresh
	triageEdit            triageEditOverlay
	triageClose           triageCloseOverlay
	triageNote            triageNoteOverlay

	// Exit escalation — double-esc-to-quit state.
	exitArmed   bool
	exitArmedAt time.Time

	// g-prefix state machine for g a / g r / g s sequences.
	gPrefixArmed bool

	// Pending action context (for modal confirmations)
	pendingAction string
	pendingSpecID string

	// In-flight mutation tracking (for spinner)
	actionInFlight bool
	actionLabel    string

	// Focused spec ID — displayed with ★ in list views.
	focusedSpecID string

	// Refresh
	refreshInterval time.Duration
	refreshInFlight map[string]bool
	spinnerOn       bool
}

// New creates a new App ready to run as a tea.Program. The caller is
// responsible for invoking Close once the program exits.
func New(rc *config.ResolvedConfig, reg *adapter.Registry, role string) App {
	db, err := store.Open(store.DefaultDBPath())
	app := newAppWithDB(rc, reg, role, db)
	if err != nil {
		// Degrade gracefully: focus and standup persistence are unavailable,
		// but the rest of the TUI still works. Tell the user why.
		app.statusBar.SetStatusError(
			"Local store unavailable",
			"Focus & standup persistence are disabled because the local store could not be opened:\n\n"+err.Error(),
		)
	}
	return app
}

// Close releases resources held by the App, notably the local store and any
// active file watcher. It is safe to call when the store failed to open.
func (a App) Close() error {
	if a.watcher != nil {
		_ = a.watcher.Close()
	}
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}

func newAppWithDB(rc *config.ResolvedConfig, reg *adapter.Registry, role string, db *store.DB) App {
	// Inject the store-backed sync audit/freshness recorder so TUI actions
	// auto-push with tracked audit and the read path honours the freshness TTL.
	setTUIRecorder(db)
	// Warm the terminal background detection now, while stdin is still ours.
	// Once Bubble Tea's event loop owns stdin the OSC query reply is swallowed
	// and the call blocks until timeout, so doing it here keeps the "auto"
	// theme instant even when the user cycles onto it mid-session.
	_ = hasDarkBackground()

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
		Status:  statusStyles(theme),
	}
	sb := components.NewStatusBar(sbStyles)
	sb.SetView(ViewDashboard.Label())
	sb.SetActiveRefreshKey(refreshKeyDashboard)
	// Flag a tab stale once its data is twice the refresh interval old, so the
	// "stale · r to refresh" affordance only appears after a poll has plausibly
	// been missed rather than between two healthy ticks.
	sb.SetStaleAfter(2 * parseRefreshInterval(rc))

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
		if d, err := parseRefreshPref(rc.User.Preferences.RefreshInterval); err == nil {
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
		a.notifyStaleRefresh(msg.Err, a.dashboard.loaded)
		a.statusBar.SetPending(a.dashboard.pendingCount())
		a.markDataFresh(refreshKeyDashboard, msg.Err)
		return a, cmd

	case pipelineDataMsg:
		a.markRefreshDone(refreshKeyPipeline)
		var cmd tea.Cmd
		a.pipeline, cmd = a.pipeline.update(msg)
		a.notifyStaleRefresh(msg.Err, a.pipeline.loaded)
		a.markDataFresh(refreshKeyPipeline, msg.Err)
		return a, cmd

	case specListDataMsg:
		a.markRefreshDone(refreshKeySpecs)
		var cmd tea.Cmd
		a.specs, cmd = a.specs.update(msg)
		a.notifyStaleRefresh(msg.Err, a.specs.loaded)
		a.markDataFresh(refreshKeySpecs, msg.Err)
		return a, cmd

	case triageDataMsg:
		a.markRefreshDone(refreshKeyTriage)
		var cmd tea.Cmd
		a.triage, cmd = a.triage.update(msg)
		a.notifyStaleRefresh(msg.Err, a.triage.loaded)
		a.markDataFresh(refreshKeyTriage, msg.Err)
		a.rehydrateTriageDetail()
		return a, cmd

	case reviewDataMsg:
		a.markRefreshDone(refreshKeyReviews)
		var cmd tea.Cmd
		a.reviews, cmd = a.reviews.update(msg)
		a.notifyStaleRefresh(msg.Err, a.reviews.loaded)
		a.markDataFresh(refreshKeyReviews, msg.Err)
		return a, cmd

	case fileChangedMsg:
		// A watched file changed. Re-read the open spec (hash-gated, position-
		// preserving) and re-arm the watcher for the next change. If the reader
		// closed between event and delivery, just stop.
		if !a.showDetail || a.watcher == nil {
			return a, nil
		}
		a.watchRefreshPending = true
		return a, tea.Batch(
			a.scheduleRefresh(a.detailRefreshKey(), a.detail.fetchData()),
			waitForChange(a.watcher),
		)

	case specDetailDataMsg:
		a.markDetailRefreshDone()
		prevHash := a.detail.contentHash
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		// Surface a calm cue only when a watcher-triggered refresh actually
		// changed content (hash moved). Silent on no-op and initial load.
		if a.watchRefreshPending {
			a.watchRefreshPending = false
			if msg.Err == nil && a.detail.contentHash != prevHash && prevHash != "" {
				a.statusBar.SetStatusSuccess("Updated", 2*time.Second)
			}
		}
		return a, cmd

	case sectionRenderedMsg:
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		a.syncBusyState()
		return a, cmd

	case threadsChangedMsg:
		// A thread mutation (ask/reply/resolve) completed. Route the refreshed
		// thread set to the detail model so the pane updates immediately, and
		// surface the accompanying toast. This must be handled here rather than
		// via delegateToActive, which only routes to the active tab view.
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		if msg.Err != nil {
			a.statusBar.SetStatusError("Thread update failed", msg.Err.Error())
		} else if msg.Toast != "" {
			a.statusBar.SetStatusSuccess(msg.Toast, 2*time.Second)
		}
		return a, cmd

	case settingsAppliedMsg:
		return a, a.applySettingsField(msg.Field)

	case settingsThemePreviewMsg:
		// Live, non-persisted preview while editing the Theme field.
		a.applyTheme(msg.Theme)
		return a, nil

	case settingsPersistedMsg:
		var cmd tea.Cmd
		a.settings, cmd = a.settings.update(msg)
		if msg.Err != nil {
			a.statusBar.SetStatusError("Settings save failed", msg.Err.Error())
			return a, cmd
		}
		switch msg.Field {
		case fieldTheme:
			name := "auto"
			if a.rc.User != nil && a.rc.User.Preferences.Theme != "" {
				name = a.rc.User.Preferences.Theme
			}
			a.statusBar.SetStatusSuccess("Theme: "+name, 2*time.Second)
		default:
			a.statusBar.SetStatusSuccess("Settings saved", 2*time.Second)
		}
		return a, cmd

	// Standup data arrived.
	case standupDataMsg:
		if msg.Err != nil {
			a.statusBar.SetStatusError("Standup failed", msg.Err.Error())
		} else {
			a.standup.show(msg.Text)
		}
		return a, nil

	// Navigation messages from views.
	case navigateToSpecMsg:
		return a, a.openDetail(msg.SpecID)

	case navigateBackMsg:
		return a, a.closeDetail()

	case triageDetailOpenMsg:
		a.showTriageDetail = true
		a.triageDetail = newTriageDetailPane(msg.Item, a.width, a.contentHeight())
		return a, nil

	case triageDetailCloseMsg:
		a.showTriageDetail = false
		a.triageDetail = nil
		return a, nil

	case triagePromoteResultMsg:
		a.actionInFlight = false
		a.actionLabel = ""
		a.syncBusyState()
		if msg.Err != nil {
			a.statusBar.SetStatusError("promote failed", msg.Err.Error())
			return a, nil
		}
		a.showTriageDetail = false
		a.triageDetail = nil
		a.statusBar.SetStatusSuccess("promoted "+msg.TriageID+" → "+msg.SpecID, 5*time.Second)
		// Navigate to the new spec detail and refresh triage list.
		return a, tea.Batch(
			a.openDetail(msg.SpecID),
			a.scheduleRefresh(refreshKeyTriage, a.triage.refresh()),
		)

	// Action results — show toast and refresh.
	case actionResultMsg:
		a.actionInFlight = false
		a.actionLabel = ""
		a.syncBusyState()

		if msg.Err != nil {
			summary := msg.Action + " failed"
			if msg.SpecID != "" {
				summary = msg.SpecID + " " + summary
			}
			a.statusBar.SetStatusError(summary, msg.Err.Error())
		} else {
			label := msg.Action
			if msg.Detail != "" {
				label += ": " + msg.Detail
			}
			if msg.SpecID != "" {
				label = msg.SpecID + " " + label
			}
			a.statusBar.SetStatusSuccess(label, 3*time.Second)
		}
		// Refresh focused spec after focus/unfocus actions.
		if msg.Action == "focus" || msg.Action == "unfocus" {
			a.focusedSpecID = loadFocusedSpec(a.db)
		}
		// After a successful triage mutation, refresh the list and manage the
		// detail pane: close hides it (archived); edit/comment/escalate
		// rehydrate the item from the refreshed list so the pane stays current.
		if msg.Err == nil && isTriggerTriage(msg.Action) {
			switch msg.Action {
			case "triage/close":
				a.showTriageDetail = false
				a.triageDetail = nil
				a.triageDetailRehydrate = ""
			default:
				if a.showTriageDetail && a.triageDetail != nil {
					a.triageDetailRehydrate = a.triageDetail.item.ID
				}
			}
			return a, tea.Batch(
				a.scheduleRefresh(refreshKeyTriage, a.triage.refresh()),
			)
		}
		return a, a.refreshActiveView()

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case tea.MouseMsg:
		return a.handleMouse(msg)
	}

	// Non-key messages — delegate to active view.
	return a, a.delegateToActive(msg)
}

// handleKey is the single entry point for all keyboard input.
// It follows a strict priority chain — the first match wins, no fall-through.
func (a App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ── Layer 1: Overlays (absorb all keys) ──────────────────────────
	// These are modal states that must capture every keystroke.

	if a.standup.visible {
		return a.updateStandup(msg)
	}
	if a.intake.active {
		return a.updateIntake(msg)
	}
	if a.revert.active {
		return a.updateRevert(msg)
	}
	if a.triageEdit.active {
		return a.updateTriageEdit(msg)
	}
	if a.triageClose.active {
		return a.updateTriageClose(msg)
	}
	if a.triageNote.active {
		return a.updateTriageNote(msg)
	}
	if a.help.visible {
		a.help, _ = a.help.update(msg)
		return a, nil
	}
	if a.modal.Visible {
		return a.updateModal(msg)
	}

	// ── Layer 2: Escape hatch (always works) ─────────────────────────

	if msg.String() == "ctrl+c" {
		return a, tea.Quit
	}

	// Disarm g-prefix on any key that isn't handled by the g-prefix block.
	// The prefix block below will re-arm / dispatch before returning.

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

	// ── Layer 4.5: Triage detail pane (focused mode) ─────────────────
	// When the triage detail pane is open it captures keys, including esc to
	// close. This must run before the global esc escalation below so esc does
	// not get swallowed by the exit-arm logic.
	if a.showTriageDetail {
		return a.updateTriageDetail(msg)
	}

	// ── Layer 5: Global keys (work on every top-level view) ─────────

	// esc escalation: pop → arm → quit.
	if key.Matches(msg, a.keys.Back) {
		// Let the active view pop its own dismissible state first (e.g. clear a
		// committed search filter) before we treat esc as an exit-arm.
		if a.activeViewCanPopEsc() {
			return a, a.delegateToActive(msg)
		}
		if a.exitArmed && time.Since(a.exitArmedAt) <= exitArmWindow {
			return a, tea.Quit
		}
		// Nothing to pop at the top level — arm exit.
		a.exitArmed = true
		a.exitArmedAt = time.Now()
		a.statusBar.SetExitArmed(true)
		return a, nil
	}

	// Any key other than esc disarms the exit arm.
	if a.exitArmed {
		a.exitArmed = false
		a.statusBar.SetExitArmed(false)
	}

	// g-prefix state machine: g alone arms the prefix.
	// A subsequent a / r / s dispatches Archive / Restore / Standup.
	if a.gPrefixArmed {
		a.gPrefixArmed = false
		switch msg.Text {
		case "a":
			if specID := a.selectedSpecID(); isSpecID(specID) {
				a.pendingAction = "archive"
				a.pendingSpecID = specID
				a.modal.ShowConfirm("Archive "+specID, "Remove this spec from the active list?")
				a.modal.SetSize(a.width, a.contentHeight())
			}
			return a, nil
		case "r":
			if specID := a.selectedSpecID(); isSpecID(specID) {
				a.pendingAction = "restore"
				a.pendingSpecID = specID
				a.modal.ShowConfirm("Restore "+specID, "Return this spec to the active list?")
				a.modal.SetSize(a.width, a.contentHeight())
			}
			return a, nil
		case "c":
			if specID := a.selectedSpecID(); isSpecID(specID) {
				a.armAssignModal(specID)
			}
			return a, nil
		case "s":
			return a, generateStandup(a.rc, a.reg, a.db)
		}
		// Unrecognised follow-up key — fall through normally.
	}

	switch {
	case key.Matches(msg, a.keys.GPrefix):
		a.gPrefixArmed = true
		return a, nil
	case key.Matches(msg, a.keys.Help):
		a.help.setContext(a.activeView.Label())
		a.help.toggle()
		return a, nil
	case key.Matches(msg, a.keys.ExpandError):
		a.expandError()
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

	// Enter / Space: open detail for the selected item.
	case key.Matches(msg, a.keys.Enter):
		if cmd := a.activateSelection(); cmd != nil {
			return a, cmd
		}
	case msg.String() == "space" && a.activeView == ViewTriage:
		return a, a.openTriageDetailForSelected()
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
// View returns the program's view. In Bubble Tea v2 the view is a struct that
// carries the rendered content plus declarative terminal state: the alt-screen
// flag and mouse mode that were program options in v1 now live here, so the
// in-app mouse toggle takes effect on the next render with no command.
func (a App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	if a.mouseEnabled() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// mouseEnabled reports whether mouse reporting should be requested, honouring
// the user's preference. It drives View.MouseMode each render.
func (a App) mouseEnabled() bool {
	return a.rc.User != nil && a.rc.User.Preferences.Mouse
}

// render composes the full-screen content string for the current state.
func (a App) render() string {
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
			crumb := a.activeView.Label() + " › " + a.detail.specID + " › § " + sec.Slug
			// Surface open discussion as a calm awareness cue.
			if n := a.detail.totalOpenThreads(); n > 0 {
				crumb += fmt.Sprintf("  ●%d", n)
			}
			a.statusBar.SetView(crumb)
		}
	}

	header := a.header.View()
	tabs := a.tabs.View()
	statusBar := a.statusBar.View()

	lay := a.layout()

	// Help overlay covers the full terminal — skip chrome entirely.
	if a.help.visible {
		return a.help.view()
	}

	var content string
	switch {
	case a.standup.visible:
		content = a.standup.view()
	case a.intake.active:
		content = a.renderIntakeForm()
	case a.revert.active:
		content = renderRevert(a.revert, a.styles)
	case a.triageEdit.active:
		content = renderTriageEdit(a.triageEdit, a.styles)
	case a.triageClose.active:
		content = renderTriageClose(a.triageClose, a.styles)
	case a.triageNote.active:
		content = renderTriageNote(a.triageNote, a.styles)
	case a.modal.Visible:
		content = a.modal.View()
	case a.showDetail:
		content = a.detail.view()
	case a.showTriageDetail && a.triageDetail != nil:
		content = a.triageDetail.view(a.styles, a.rc)
	default:
		content = a.activeViewContent()
	}

	lines := normalizeContentLines(content, a.width, lay.contentHeight)

	var out string
	out += header + "\n"
	out += tabs + "\n"
	for _, l := range lines {
		out += l + "\n"
	}

	// The canonical status element lives inside the status bar (SPEC-016), so
	// the bar is the single, always-present status surface — no separate toast
	// row is composited over it.
	out += statusBar

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

// selectedSpecStage returns the pipeline stage of the currently selected spec.
// It checks the detail view first, then falls back to view-specific data.
func (a App) selectedSpecStage() string {
	if a.showDetail && a.detail.meta != nil {
		return a.detail.meta.Status
	}
	switch a.activeView {
	case ViewDashboard:
		if a.dashboard.cursor >= 0 && a.dashboard.cursor < len(a.dashboard.items) {
			return a.dashboard.items[a.dashboard.cursor].detail
		}
	case ViewPipeline:
		if a.pipeline.stageIdx >= 0 && a.pipeline.stageIdx < len(a.pipeline.stages) {
			return a.pipeline.stages[a.pipeline.stageIdx].Name
		}
	case ViewSpecs:
		if a.specs.cursor >= 0 && a.specs.cursor < len(a.specs.filtered) {
			return a.specs.filtered[a.specs.cursor].Status
		}
	}
	return ""
}

// viewCapturingInput returns true when the active view is in a text
// input mode (e.g. search bar) and keystrokes should be routed to
// the view instead of being interpreted as hotkeys.
func (a App) viewCapturingInput() bool {
	if a.activeView == ViewSpecs {
		return a.specs.isInputActive()
	}
	if a.activeView == ViewSettings {
		return a.settings.isEditing()
	}
	return false
}

// activeViewCanPopEsc reports whether the active view has dismissible state that
// esc should clear (e.g. a committed search filter) before the app treats esc
// as the exit-arm. This keeps the double-esc exit guard from hijacking esc when
// the user is trying to clear a filter.
func (a App) activeViewCanPopEsc() bool {
	return a.activeView == ViewSpecs && a.specs.hasActiveFilter()
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
		a.stopWatch()
	}
	a.showDetail = false
	a.closeTriageDetailPane()
	a.activeView = v
	a.tabs.SetActive(int(v))
	a.statusBar.SetView(v.Label())
	a.statusBar.SetActiveRefreshKey(refreshKeyForView(v))
	a.syncBusyState()
	// The dashboard makes a network call (PR reviews), so re-fetching every time
	// the user switches back to it is spammy. On switch, fetch only if it has
	// never loaded (e.g. the startup fetch failed); otherwise leave updates to
	// the auto-timer tick and manual refresh, both of which still refresh it
	// unconditionally via refreshActiveView.
	if v == ViewDashboard && a.dashboard.loaded {
		return nil
	}
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
	a.syncBusyState()
	return cmd
}

// markDataFresh resets the given tab's staleness clock when its data load
// succeeds. A failed poll deliberately does not reset it: the data on screen is
// no fresher than before, so the age indicator should keep climbing as the
// honest signal that the latest refresh did not land. A failed poll also marks
// the bar offline so the affordance reads "cached · offline"; a success clears
// it. Each tab is keyed independently so the indicator reflects the tab the
// user is viewing, not a global last-refresh.
func (a *App) markDataFresh(key string, err error) {
	if err != nil {
		a.statusBar.SetOffline(true)
		return
	}
	a.statusBar.SetOffline(false)
	a.statusBar.SetRefresh(key, time.Now())
}

// expandError opens the full, untruncated text of the current sticky error in
// a read-only modal. It returns true if an error was present (and the modal was
// opened), false otherwise so callers can fall through. This is the escape
// valve for the slot's fixed-width truncation: the headline stays sized to the
// bar while the actionable detail is always one keypress (E) away.
func (a *App) expandError() bool {
	if !a.statusBar.HasError() {
		return false
	}
	detail := a.statusBar.ErrorDetail()
	if detail == "" {
		return false
	}
	a.modal.ShowInfo("Error", detail)
	a.modal.SetSize(a.width, a.contentHeight())
	return true
}

// notifyStaleRefresh shows a non-destructive toast when a background refresh
// fails while a view already holds cached data. The cached data stays on
// screen (the view suppresses its error screen once loaded), so the toast is
// the only signal that the latest poll did not succeed.
func (a *App) notifyStaleRefresh(err error, loaded bool) {
	if err == nil || !loaded {
		return
	}
	a.statusBar.SetStatusError("Refresh failed — showing cached data", err.Error())
}

func (a *App) markRefreshDone(key string) {
	if a.refreshInFlight != nil {
		a.refreshInFlight[key] = false
	}
	a.syncBusyState()
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
	a.syncBusyState()
}

// anyRefreshInFlight returns true if any view currently has an active refresh.
func (a *App) anyRefreshInFlight() bool {
	for _, inFlight := range a.refreshInFlight {
		if inFlight {
			return true
		}
	}
	return false
}

func (a App) detailRefreshKey() string {
	if !a.showDetail || a.detail.specID == "" {
		return "detail"
	}
	return "detail:" + a.detail.specID
}

// activateSelection runs the "open" action for the current selection in the
// active view: open a PR/review URL in the browser, otherwise drill into the
// selected spec's detail. It returns nil when nothing is selected. Both the
// Enter key and a mouse click on an already-selected row route through here so
// the two input paths can never diverge.
func (a *App) activateSelection() tea.Cmd {
	switch a.activeView {
	case ViewDashboard:
		if url := a.dashboard.selectedURL(); url != "" {
			return openInBrowser(url)
		}
	case ViewReviews:
		if url := a.reviews.selectedURL(); url != "" {
			return openInBrowser(url)
		}
	case ViewTriage:
		// Enter on the triage list opens the inline detail pane.
		return a.openTriageDetailForSelected()
	}
	if specID := a.selectedSpecID(); isSpecID(specID) {
		return a.openDetail(specID)
	}
	return nil
}

func (a *App) openDetail(specID string) tea.Cmd {
	a.showDetail = true
	a.detailFrom = a.activeView
	a.detail = newSpecDetail(a.rc, specID, a.styles, a.keys, a.theme)
	a.detail.db = a.db
	a.detail.setSize(a.width, a.contentHeight())
	a.statusBar.SetView(a.activeView.Label() + " › " + specID)
	a.syncBusyState()
	return tea.Batch(a.detail.init(), a.startWatch())
}

func (a *App) closeDetail() tea.Cmd {
	a.markDetailRefreshDone()
	a.detail.cancelRender()
	a.stopWatch()
	a.showDetail = false
	a.statusBar.SetView(a.activeView.Label())
	a.syncBusyState()
	return nil
}

// startWatch begins watching the open spec's files and returns a command that
// delivers the first change event. Watching the open spec keeps the reader
// live without the user quitting and reopening (SPEC-007). A watcher that
// cannot register native notifications degrades silently to polling.
func (a *App) startWatch() tea.Cmd {
	paths := a.detail.watchPaths()
	if len(paths) == 0 {
		return nil
	}
	if a.watcher != nil {
		// Reuse the existing watcher across spec navigation.
		_ = a.watcher.Retarget(paths)
		return waitForChange(a.watcher)
	}
	w, _ := watch.New(context.Background(), paths, watchDebounce)
	a.watcher = w
	return waitForChange(w)
}

// stopWatch tears down the file watcher when the reader closes.
func (a *App) stopWatch() {
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
}

// waitForChange blocks on the watcher's channel and surfaces the next change
// as a fileChangedMsg. It returns nil (ending the wait loop) when the channel
// is closed, which happens when the watcher is stopped.
func waitForChange(w *watch.Watcher) tea.Cmd {
	if w == nil {
		return nil
	}
	ch := w.C
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return fileChangedMsg{Paths: ev.Paths}
	}
}

func (a App) updateDetail(msg tea.KeyPressMsg) (App, tea.Cmd) {
	// Hard quit always works.
	if msg.String() == "ctrl+c" {
		return a, tea.Quit
	}

	// While an inline ask/reply prompt is open, every printable key is text —
	// including '?', which would otherwise toggle help. Route straight to the
	// detail model so the thread input captures the keystroke.
	if a.detail.readerMode && a.detail.input.active() {
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		a.syncBusyState()
		return a, cmd
	}

	// Help.
	if key.Matches(msg, a.keys.Help) {
		a.help.setContext("Detail: " + a.detail.specID)
		a.help.toggle()
		return a, nil
	}

	// Expand the current error to full text (no-op when there is none, and only
	// when the reader is not capturing typed input).
	if key.Matches(msg, a.keys.ExpandError) && !a.detail.input.active() {
		if a.expandError() {
			return a, nil
		}
	}

	// In reader mode, reserve digit keys for section jumps.
	// Keep tab/shift+tab view switching available, except when the reader
	// is using tab to move focus between prose and the thread pane, or while
	// an inline ask/reply prompt is capturing input.
	if a.detail.readerMode {
		readerUsesTab := a.detail.paneActiveForCurrentSection()
		capturing := a.detail.input.active()
		switch {
		case key.Matches(msg, a.keys.NextTab) && !readerUsesTab && !capturing:
			return a, a.switchView(a.activeView.Next())
		case key.Matches(msg, a.keys.PrevTab) && !capturing:
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

	// Overview mode: esc goes back to the list.
	if key.Matches(msg, a.keys.Back) {
		a.closeDetail()
		return a, nil
	}

	// g-prefix state machine inside detail view.
	if a.gPrefixArmed {
		a.gPrefixArmed = false
		specID := a.detail.specID
		switch {
		case msg.Text == "a" && isSpecID(specID):
			if !a.detail.isArchived {
				a.pendingAction = "archive"
				a.pendingSpecID = specID
				a.modal.ShowConfirm("Archive "+specID, "Remove this spec from the active list?")
				a.modal.SetSize(a.width, a.contentHeight())
			}
			return a, nil
		case msg.Text == "r" && isSpecID(specID):
			if a.detail.isArchived {
				a.pendingAction = "restore"
				a.pendingSpecID = specID
				a.modal.ShowConfirm("Restore "+specID, "Return this spec to the active list?")
				a.modal.SetSize(a.width, a.contentHeight())
			}
			return a, nil
		case msg.Text == "c" && isSpecID(specID):
			a.armAssignModal(specID)
			return a, nil
		case msg.Text == "s":
			return a, generateStandup(a.rc, a.reg, a.db)
		}
	}
	if key.Matches(msg, a.keys.GPrefix) {
		a.gPrefixArmed = true
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
func (a *App) handleSpecAction(specID string, msg tea.KeyPressMsg) (tea.Cmd, bool) {
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
	case key.Matches(msg, a.keys.Revert) && isSpecID(specID):
		stage := a.selectedSpecStage()
		if err := a.revert.openRevert(specID, stage, a.rc.Pipeline()); err != nil {
			a.statusBar.SetStatusError("Revert unavailable", err.Error())
			return nil, true
		}
		return nil, true
	case key.Matches(msg, a.keys.Focus) && specID != "":
		// f toggles focus: set if not already focused, clear if it is.
		if a.focusedSpecID == specID {
			return unfocusSpec(a.db), true
		}
		return focusSpec(a.db, specID), true
	case key.Matches(msg, a.keys.Yank) && specID != "":
		return yankSpecID(specID), true
	case key.Matches(msg, a.keys.Edit) && isSpecID(specID):
		editor := "vi"
		if a.rc.User != nil && a.rc.User.Preferences.Editor != "" {
			editor = a.rc.User.Preferences.Editor
		}
		return editSpec(a.rc, specID, editor), true
	case key.Matches(msg, a.keys.Build) && isSpecID(specID):
		// Pre-flight stage guard runs before the confirm modal so an invalid
		// spec surfaces inline and never reaches the confirm step.
		if err := a.preflightBuild(specID); err != nil {
			a.statusBar.SetStatusError("Build unavailable", err.Error())
			return nil, true
		}
		a.pendingAction = "build"
		a.pendingSpecID = specID
		a.modal.ShowConfirm("Build "+specID, a.buildConfirmBody(specID))
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	case key.Matches(msg, a.keys.Push) && isSpecID(specID):
		return a.startAction("pushing "+specID, pushSpec(a.rc, specID)), true
	case key.Matches(msg, a.keys.Sync) && isSpecID(specID):
		return a.startAction("syncing "+specID, syncSpec(a.rc, a.reg, a.db, specID, a.role)), true
	case key.Matches(msg, a.keys.Decide) && isSpecID(specID):
		a.pendingAction = "decide"
		a.pendingSpecID = specID
		a.modal.ShowInput("Record Decision — "+specID, "Question or decision to record:")
		a.modal.SetSize(a.width, a.contentHeight())
		return nil, true
	// Archive: only on non-archived specs. When in list view, only in active list mode.
	// When in detail, only when the spec is not archived.
	// Uses a confirmation modal as a safety guard.
	case key.Matches(msg, a.keys.Archive) && isSpecID(specID):
		if a.showDetail && a.detail.isArchived {
			// archived spec in detail view: ignore
		} else {
			a.pendingAction = "archive"
			a.pendingSpecID = specID
			a.modal.ShowConfirm("Archive "+specID, "Remove this spec from the active list?")
			a.modal.SetSize(a.width, a.contentHeight())
			return nil, true
		}
	// Restore: only on archived specs. When in list view, only in archive list mode.
	// When in detail, only when the spec is archived.
	// Uses a confirmation modal as a safety guard.
	case key.Matches(msg, a.keys.Restore) && isSpecID(specID):
		isArch := (a.activeView == ViewSpecs && a.specs.archiveMode) || (a.showDetail && a.detail.isArchived)
		if isArch {
			a.pendingAction = "restore"
			a.pendingSpecID = specID
			a.modal.ShowConfirm("Restore "+specID, "Return this spec to the active list?")
			a.modal.SetSize(a.width, a.contentHeight())
			return nil, true
		}
	}
	return nil, false
}

func (a App) updateModal(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch a.modal.Kind {
	case components.ModalInfo:
		// Read-only dialog (e.g. full error detail): any of esc/enter/q closes it.
		switch {
		case msg.String() == "esc", msg.String() == "enter",
			msg.Text == "q":
			a.modal.Hide()
		}
		return a, nil

	case components.ModalConfirm:
		switch msg.String() {
		default:
			if msg.Text == "y" {
				a.modal.Hide()
				return a, a.executeAction()
			}
			if msg.Text == "n" {
				a.modal.Hide()
				return a, nil
			}
		case "esc":
			a.modal.Hide()
			return a, nil
		}

	case components.ModalInput:
		switch msg.String() {
		case "esc":
			a.modal.Hide()
			return a, nil
		case "enter":
			if a.modal.Input != "" {
				// Capture input before Hide() clears it.
				input := a.modal.Input
				a.modal.Hide()
				return a, a.executeActionWithInput(input)
			}
		case "backspace":
			a.modal.BackspaceInput()
		case "space":
			a.modal.AppendInput(" ")
		default:
			if msg.Text != "" {
				a.modal.AppendInput(msg.Text)
			}
		}
	}
	return a, nil
}

// executeAction runs the pending action after modal confirmation (for confirm modals).
func (a *App) executeAction() tea.Cmd {
	return a.executeActionWithInput("")
}

// armAssignModal opens the assign/claim input modal for a spec, pre-filled with
// the current user's identity so a bare Enter claims it. Editing the field
// assigns other people; entering "-" clears all assignees.
func (a *App) armAssignModal(specID string) {
	a.pendingAction = "assign"
	a.pendingSpecID = specID
	a.modal.ShowInput("Assign "+specID, "Space-separated handles · '-' to unassign:")
	a.modal.Input = selfAssignIdentity(a.rc)
	a.modal.SetSize(a.width, a.contentHeight())
}

// executeActionWithInput runs the pending action with the given input value.
// For confirm modals, input is empty. For input modals, it contains the user's text.
func (a *App) executeActionWithInput(input string) tea.Cmd {
	specID := a.pendingSpecID
	switch a.pendingAction {
	case "advance":
		return a.startAction("advancing "+specID, advanceSpec(a.rc, specID, a.role))
	case "block":
		reason := input
		if reason == "" {
			reason = "blocked from TUI"
		}
		return a.startAction("blocking "+specID, blockSpec(a.rc, specID, reason, a.rc.UserName()))
	case "build":
		return a.startAction("building "+specID, buildSpec(a.rc, specID))
	case "assign":
		return a.startAction("assigning "+specID, assignSpec(a.rc, specID, parseAssignInput(input)))
	case "unblock":
		return a.startAction("unblocking "+specID, unblockSpec(a.rc, specID))
	case "archive":
		if a.showDetail {
			a.closeDetail()
		}
		return a.startAction("archiving "+specID, archiveSpec(a.rc, specID))
	case "restore":
		if a.showDetail {
			a.closeDetail()
		}
		return a.startAction("restoring "+specID, restoreSpec(a.rc, specID))
	case "decide":
		if input == "" {
			return nil
		}
		return a.startAction("recording decision", recordDecision(a.rc, specID, input))
	case "new":
		if input == "" {
			return nil
		}
		return a.startAction("creating spec", createSpec(a.rc, input))
	case "promote-triage":
		// Promote a triage item to a formal SPEC-NNN.
		if a.triageDetail == nil {
			return nil
		}
		item := a.triageDetail.item
		a.closeTriageDetailPane()
		return a.startAction("promoting "+item.ID, promoteTriageItem(a.rc, item))
	default:
		return nil
	}
}

// updateStandup handles keys within the standup overlay.
func (a App) updateStandup(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		return a, tea.Quit
	case msg.String() == "esc":
		a.standup.hide()
	case msg.Text == "c":
		// Copy standup text to clipboard.
		a.standup.hide()
		return a, yankText(a.standup.text)
	case key.Matches(msg, a.keys.Up):
		a.standup.scrollUp()
	case key.Matches(msg, a.keys.Down):
		a.standup.scrollDown()
	}
	return a, nil
}

// updateIntake handles keys within the inline intake form.
func (a App) updateIntake(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.intake.close()
	case "tab":
		a.intake.nextField()
	case "shift+tab":
		a.intake.prevField()
	case "enter":
		switch {
		case a.intake.field == intakeFieldPriority:
			// Enter on priority cycles the value.
			a.intake.cyclePriority()
		case a.intake.field == intakeFieldTitle:
			// Enter on title advances to next field, not submit.
			a.intake.nextField()
		case a.intake.valid():
			// Submit only from the last field (source).
			title := a.intake.title
			priority := a.intake.priority
			source := a.intake.source
			a.intake.close()
			return a, a.startAction("creating triage item", createTriageItem(a.rc, title, priority, source))
		}
	case "backspace":
		a.intake.backspaceField()
	case "space":
		a.intake.appendToField(" ")
	default:
		if msg.Text != "" {
			a.intake.appendToField(msg.Text)
		}
	}
	return a, nil
}

// updateRevert handles keys within the inline revert form.
func (a App) updateRevert(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.revert.close()
	case "tab":
		a.revert.nextField()
	case "shift+tab":
		a.revert.prevField()
	case "enter":
		switch {
		case a.revert.field == revertFieldStage:
			// Enter on stage cycles the value.
			a.revert.cycleStage()
		case a.revert.field == revertFieldReason && a.revert.valid():
			// Submit from reason field when both fields are filled.
			specID := a.revert.specID
			stage := a.revert.selectedStage()
			reason := a.revert.reason
			a.revert.close()
			return a, a.startAction("reverting "+specID, revertSpec(a.rc, specID, stage, reason, a.rc.UserName()))
		}
	case "backspace":
		switch a.revert.field {
		case revertFieldReason:
			a.revert.backspaceReason()
		case revertFieldStage:
			a.revert.cycleStageReverse()
		}
	case "space":
		if a.revert.field == revertFieldReason {
			a.revert.appendToReason(" ")
		}
	default:
		if msg.Text == "" {
			break
		}
		switch a.revert.field {
		case revertFieldReason:
			a.revert.appendToReason(msg.Text)
		case revertFieldStage:
			// On stage field, any rune cycles forward.
			a.revert.cycleStage()
		}
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
			value += a.styles.Accent.Render(IconCaret)
			b.WriteString(a.styles.Accent.Render(IconCursor + " "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(a.styles.RowNormal.Render(value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(HintStrip(a.styles,
		Hint("tab/shift+tab", "next/prev field"), Hint("enter", "submit/cycle"), Hint("esc", "cancel")))

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
	if a.showTriageDetail && a.triageDetail != nil {
		a.triageDetail.setSize(a.width, ch)
	}
	a.reviews.setSize(a.width, ch)
	a.settings.setSize(a.width, ch)
	if a.showDetail {
		a.detail.setSize(a.width, ch)
	}
	a.modal.SetSize(a.width, ch)
	a.help.setSize(a.width, a.height)
	a.standup.setSize(a.width, ch)
	if a.triageEdit.active {
		a.triageEdit.setSize(a.width, ch)
	}
}

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

func (a App) contentHeight() int {
	return a.layout().contentHeight
}

// startAction marks an action as in-flight and returns the command.
// The spinner will show the label until an actionResultMsg arrives.
func (a *App) startAction(label string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	a.actionInFlight = true
	a.actionLabel = label
	a.syncBusyState()
	return cmd
}

// syncBusyState reconciles the canonical status element with the app's
// in-flight work. It only ever sets the *pending* kind (or clears it); the
// transient success/error outcomes are set at the call sites that produce them
// (actionResultMsg, settings persist, etc.) and decay on their own. Clearing
// pending here must not clobber a still-live success/error cue, so it returns
// to idle only when nothing is in flight AND the element is currently pending.
func (a *App) syncBusyState() {
	// Action mutations take priority for the pending label.
	if a.actionInFlight {
		a.spinnerOn = true
		label := a.actionLabel
		if label == "" {
			label = "Working…"
		}
		a.statusBar.SetStatusPending(label)
		return
	}

	// Refresh in-flight — pending while fetching data. A background refresh is
	// frequent and low-salience, so it must not stomp a fresh success/error
	// outcome the user just triggered (e.g. an action that auto-refreshes on
	// completion). Only claim the slot when it isn't already showing a live
	// transient outcome.
	if a.anyRefreshInFlight() {
		if a.statusBar.ShowingOutcome() {
			return
		}
		a.spinnerOn = true
		a.statusBar.SetStatusPending("Refreshing…")
		return
	}

	busy := a.showDetail && a.detail.readerMode && a.detail.renderInFlight
	a.spinnerOn = busy
	if !busy {
		// Nothing in flight. Drop a lingering pending cue back to idle, but
		// leave a live success/error outcome to decay on its own timer.
		if a.statusBar.Animating() {
			a.statusBar.SetStatusIdle()
		}
		return
	}
	label := "Rendering section…"
	sections := a.detail.readableSections()
	if a.detail.sectionIdx >= 0 && a.detail.sectionIdx < len(sections) {
		label = "Rendering § " + sections[a.detail.sectionIdx].Slug
	}
	a.statusBar.SetStatusPending(label)
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

// dropLastRune removes the final UTF-8 rune from s. Text-input handlers use
// it for backspace so deleting a multi-byte character removes the whole rune
// rather than a single byte, which would leave invalid UTF-8 in the string
// (corrupting rendering and any value persisted to a spec).
func dropLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
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

// isTriggerTriage reports whether an action trigger belongs to the triage subsystem.
func isTriggerTriage(action string) bool {
	return len(action) > 7 && action[:7] == "triage/"
}

// openTriageDetailForSelected opens the triage detail pane for the selected item.
func (a *App) openTriageDetailForSelected() tea.Cmd {
	if a.activeView != ViewTriage {
		return nil
	}
	item := a.triage.selectedItem()
	if item == nil {
		return nil
	}
	a.showTriageDetail = true
	a.triageDetail = newTriageDetailPane(*item, a.width, a.contentHeight())
	return nil
}

// closeTriageDetailPane closes the triage detail pane.
func (a *App) closeTriageDetailPane() {
	a.showTriageDetail = false
	a.triageDetail = nil
	a.triageDetailRehydrate = ""
}

// rehydrateTriageDetail refreshes the open detail pane from the canonical list
// data after a triage list refresh. If the item no longer exists (e.g. deleted
// by promotion), the detail pane closes gracefully.
func (a *App) rehydrateTriageDetail() {
	if a.triageDetailRehydrate == "" {
		return
	}
	id := a.triageDetailRehydrate
	a.triageDetailRehydrate = ""

	item := a.triage.findItemByID(id)
	if item == nil {
		a.closeTriageDetailPane()
		return
	}
	if a.triageDetail != nil {
		a.triageDetail.updateItem(*item)
	}
}

// updateTriageDetail handles all keys while the triage detail pane is the
// focused mode. It mirrors updateDetail: global keys (quit, help, view switch)
// are honoured first, esc closes the pane, and the remaining keys drive triage
// actions and scrolling. Unrecognised keys are absorbed so they cannot leak
// into the list beneath the pane.
func (a App) updateTriageDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return a, tea.Quit
	}

	if key.Matches(msg, a.keys.Help) {
		a.help.setContext(a.activeView.Label())
		a.help.toggle()
		return a, nil
	}

	// View switching closes the pane (via switchView) and switches.
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
	case key.Matches(msg, a.keys.NextTab):
		return a, a.switchView(a.activeView.Next())
	case key.Matches(msg, a.keys.PrevTab):
		return a, a.switchView(a.activeView.Prev())
	}

	// esc, triage actions (e/c/x/n/p), and scrolling. Any other key is absorbed.
	return a, a.handleTriageDetailKey(msg)
}

// handleTriageDetailKey resolves a key press into a triage-detail action while
// the pane is open: esc closes it, e/c/x/n/p drive role-gated actions, and j/k
// scroll. It returns a command to run, or nil when the key has no effect.
func (a *App) handleTriageDetailKey(msg tea.KeyPressMsg) tea.Cmd {
	if !a.showTriageDetail || a.triageDetail == nil {
		return nil
	}
	item := a.triageDetail.item

	switch {
	case msg.String() == "esc":
		a.closeTriageDetailPane()
		return nil

	case msg.Text == "e":
		if !roleAllowed(actionTriageEdit, a.rc) {
			return nil
		}
		a.triageEdit.openEdit(item, a.width, a.contentHeight())
		return nil

	case msg.Text == "c":
		if !roleAllowed(actionTriageClose, a.rc) {
			return nil
		}
		a.triageClose.openClose(item.ID)
		return nil

	case msg.Text == "x":
		if !roleAllowed(actionTriageEscalate, a.rc) {
			return nil
		}
		return a.startAction("escalating "+item.ID, escalateTriageItem(a.rc, item.ID, item.isUrgent(), a.rc.UserName()))

	case msg.Text == "n":
		if !roleAllowed(actionTriageComment, a.rc) {
			return nil
		}
		a.triageNote.openNote(item.ID)
		return nil

	case msg.Text == "p":
		if !roleAllowed(actionTriagePromote, a.rc) {
			return nil
		}
		// Show a confirmation modal before promoting.
		a.pendingAction = "promote-triage"
		a.pendingSpecID = item.ID
		a.modal.ShowConfirm(
			"Promote "+item.ID,
			"Create a new spec from this triage item? The triage entry will be deleted.",
		)
		a.modal.SetSize(a.width, a.contentHeight())
		return nil

	case msg.String() == "up" || msg.Text == "k":
		a.triageDetail.scrollUp()
		return nil

	case msg.String() == "down" || msg.Text == "j":
		a.triageDetail.scrollDown()
		return nil
	}
	return nil
}

// updateTriageEdit handles keys within the inline triage edit form. Global form
// keys (cancel, save, field navigation, priority cycle) are handled first; every
// other key is delegated to the focused input component so cursor movement and
// text entry work natively.
func (a App) updateTriageEdit(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.triageEdit.close()
		return a, nil
	case "ctrl+s":
		if !a.triageEdit.valid() {
			return a, nil
		}
		id := a.triageEdit.triageID
		title := a.triageEdit.title.Value()
		priority := a.triageEdit.priority
		source := a.triageEdit.source.Value()
		body := a.triageEdit.body.Value()
		a.triageEdit.close()
		return a, a.startAction("editing "+id, editTriageItem(a.rc, id, title, priority, source, body))
	case "tab":
		a.triageEdit.nextField()
		return a, nil
	case "shift+tab":
		a.triageEdit.prevField()
		return a, nil
	case "enter":
		switch a.triageEdit.field {
		case triageEditFieldPriority:
			a.triageEdit.cyclePriority()
			return a, nil
		case triageEditFieldTitle, triageEditFieldSource:
			a.triageEdit.nextField()
			return a, nil
		}
		// Body field: fall through so the textarea inserts a newline.
	}

	// Delegate to the focused input (arrows, home/end, backspace, runes, etc.).
	return a, a.triageEdit.update(msg)
}

// updateTriageClose handles keys within the inline triage close dialog.
func (a App) updateTriageClose(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.triageClose.close()
	case "tab":
		a.triageClose.nextField()
	case "shift+tab":
		a.triageClose.prevField()
	case "enter":
		switch a.triageClose.field {
		case triageCloseFieldReason:
			a.triageClose.nextField()
		case triageCloseFieldNote:
			id := a.triageClose.triageID
			reason := a.triageClose.selectedReason()
			note := a.triageClose.note
			actor := a.rc.UserName()
			a.triageClose.close()
			return a, a.startAction("closing "+id, closeTriageItem(a.rc, id, reason, note, actor))
		}
	case "backspace":
		if a.triageClose.field == triageCloseFieldNote {
			a.triageClose.backspaceNote()
		} else {
			a.triageClose.cycleReason()
		}
	case "space":
		if a.triageClose.field == triageCloseFieldNote {
			a.triageClose.appendNote(" ")
		}
	default:
		if msg.Text == "" {
			break
		}
		if a.triageClose.field == triageCloseFieldNote {
			a.triageClose.appendNote(msg.Text)
		} else {
			a.triageClose.cycleReason()
		}
	}
	return a, nil
}

// updateTriageNote handles keys within the inline triage note prompt.
func (a App) updateTriageNote(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.triageNote.close()
	case "enter":
		if a.triageNote.valid() {
			id := a.triageNote.triageID
			note := a.triageNote.note
			actor := a.rc.UserName()
			a.triageNote.close()
			return a, a.startAction("commenting on "+id, commentTriageItem(a.rc, id, note, actor))
		}
	case "backspace":
		a.triageNote.backspace()
	case "space":
		a.triageNote.append(" ")
	default:
		if msg.Text != "" {
			a.triageNote.append(msg.Text)
		}
	}
	return a, nil
}
