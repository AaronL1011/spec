package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/search"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/tui/components"
	"github.com/aaronl1011/spec/internal/tui/watch"
	"github.com/aaronl1011/spec/internal/update"
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

// tickMsg fires on each refresh interval.
type tickMsg time.Time

type spinnerTickMsg time.Time

// fileChangedMsg signals that one of the watched files backing the open spec
// changed on disk and the reader should re-read and re-render (SPEC-007).
type fileChangedMsg struct{ Paths []string }

// updateAvailableMsg is emitted by the startup update check when a newer
// release exists, carrying the version to surface in the status bar.
type updateAvailableMsg struct{ Latest string }

// App is the top-level Bubble Tea model. It owns the tab strip, header,
// status bar, and delegates to the active view.
type App struct {
	rc   *config.ResolvedConfig
	reg  *adapter.Registry
	role string
	db   *store.DB
	// version is the running binary's resolved version, injected from cmd. It
	// drives the passive "update available" check on startup; an empty or dev
	// version disables the check.
	version string

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
	// publisher pushes thread comments / inline edits to the specs repo in the
	// background so the UI never blocks on the network. nil when auto-push is
	// disabled (AutoPushOff); its methods are nil-safe.
	publisher *gitpkg.Publisher
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
	search  searchOverlayModel

	// searchIx is the shared FTS5 indexer backing the global search overlay
	// (SPEC-028). Nil-safe: a nil store degrades search to the live fallback.
	searchIx *search.Indexer

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

	// detailFromSearch records that the open spec was opened from the search
	// overlay so Esc (navigateBackMsg) returns to the overlay with the query
	// intact instead of the underlying view.
	detailFromSearch bool

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
func New(rc *config.ResolvedConfig, reg *adapter.Registry, role, version string) App {
	db, err := store.Open(store.DefaultDBPath())
	app := newAppWithDB(rc, reg, role, db)
	app.version = version
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
	// Flush any debounced comment/edit pushes before exit.
	a.publisher.Close()
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
		rc:              rc,
		reg:             reg,
		role:            role,
		db:              db,
		publisher:       newTUIPublisher(rc),
		theme:           theme,
		styles:          styles,
		keys:            keys,
		header:          header,
		tabs:            tabs,
		statusBar:       sb,
		activeView:      ViewDashboard,
		dashboard:       newDashboard(rc, reg, role, styles, keys),
		pipeline:        newPipeline(rc, styles, keys),
		specs:           newSpecList(rc, styles, keys),
		triage:          newTriage(rc, styles, keys),
		reviews:         newReview(rc, reg, styles, keys),
		settings:        newSettings(rc, styles, keys),
		help:            newHelp(keys, styles),
		modal:           components.NewModal(modalStyles(theme, styles)),
		search:          newSearchOverlay(styles, theme),
		searchIx:        search.NewIndexer(rc, db),
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
		a.checkForUpdate(),
		a.reconcileSearch(),
	)
}

// reconcileSearch kicks off a background incremental reindex of the FTS5 spec
// search index (SPEC-028). Non-blocking: the overlay falls back to a live
// scan until it completes, and a completion message clears the indexing chip.
func (a App) reconcileSearch() tea.Cmd {
	ix := a.searchIx
	if ix == nil {
		return nil
	}
	return func() tea.Msg {
		stats, err := ix.Reconcile(context.Background())
		return searchReconcileDoneMsg{Stats: stats, Err: err}
	}
}

// checkForUpdate runs the passive update check off the UI thread, emitting an
// updateAvailableMsg only when a newer release exists. It returns a nil message
// otherwise (and on any error), so a down network or dev build is a silent
// no-op rather than a disruption.
func (a App) checkForUpdate() tea.Cmd {
	version := a.version
	return func() tea.Msg {
		latest, available := update.CheckLatest(context.Background(), version)
		if !available {
			return nil
		}
		return updateAvailableMsg{Latest: latest}
	}
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

	case updateAvailableMsg:
		a.statusBar.SetUpdateAvailable(msg.Latest)
		return a, nil

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
			a.reconcileSearch(),
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
		} else {
			// Publish the comment in the background so it reaches the team
			// without a manual push; the UI never blocks on the network.
			a.publisher.Notify(a.detail.specID)
			if msg.Toast != "" {
				a.statusBar.SetStatusSuccess(msg.Toast, 2*time.Second)
			}
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

	case navigateToSpecSectionMsg:
		return a, a.openDetailAtSection(msg.SpecID, msg.SectionSlug)

	case navigateBackMsg:
		if a.detailFromSearch {
			// Esc from a search-opened reader returns to the overlay (query and
			// results intact) instead of the underlying view.
			a.detailFromSearch = false
			a.closeDetail()
			a.search.visible = true
			a.search.input.Focus()
			return a, nil
		}
		return a, a.closeDetail()

	// Search overlay messages — debounce ticks, ranked results, and the
	// background reconcile completion all route to the overlay model.
	case searchDebounceMsg:
		var cmd tea.Cmd
		a.search, cmd = a.search.update(msg)
		return a, cmd
	case searchResultsMsg:
		var cmd tea.Cmd
		a.search, cmd = a.search.update(msg)
		return a, cmd
	case searchReconcileDoneMsg:
		var cmd tea.Cmd
		a.search, cmd = a.search.update(msg)
		return a, cmd

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
		// After a successful spec mutation that changes the set of on-disk
		// specs (archive, restore, new), reconcile the search index so the
		// overlay sees the change without waiting for the next startup pass.
		if msg.Err == nil && (msg.Action == "archive" || msg.Action == "restore" || msg.Action == "new") {
			cmd := a.reconcileSearch()
			if cmd != nil {
				return a, tea.Batch(a.refreshActiveView(), cmd)
			}
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
