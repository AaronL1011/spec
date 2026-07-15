package tui

import (
	"context"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/tui/watch"
)

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
	case ViewSecurity:
		if url := a.security.selectedURL(); url != "" {
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
	return a.openDetailWithIntent(specID, false)
}

func (a *App) openDetailWithIntent(specID string, reviewIntent bool) tea.Cmd {
	a.showDetail = true
	a.detailFrom = a.activeView
	a.detail = newSpecDetail(a.rc, specID, a.styles, a.keys, a.theme)
	a.detail.reviewIntent = reviewIntent
	a.detail.db = a.db
	a.detail.setSize(a.width, a.contentHeight())
	a.statusBar.SetView(a.activeView.Label() + " › " + specID)
	a.syncBusyState()
	return tea.Batch(a.detail.init(), a.startWatch())
}

// openDetailAtSection opens a spec detail pinned to a specific section, used
// by the search overlay to deep-link straight to the matching passage. The
// section is resolved once the spec data lands (sections are not known until
// the first specDetailDataMsg); until then a pending slug is stashed on the
// detail model. Missing slug falls back to the first readable section with a
// soft notice. Records detailFromSearch so Esc returns to the overlay.
func (a *App) openDetailAtSection(specID, sectionSlug string) tea.Cmd {
	a.detailFromSearch = true
	cmd := a.openDetail(specID)
	a.detail.pendingSectionSlug = sectionSlug
	a.statusBar.SetView("search › " + specID)
	return cmd
}

func (a *App) closeDetail() tea.Cmd {
	a.markDetailRefreshDone()
	if a.db != nil && a.detail.readerMode {
		_ = a.db.ReaderPositionSet(a.detail.specID, a.detail.currentSectionSlug(), a.detail.readerViewport.YOffset())
	}
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

	// Help follows the active detail sub-mode so bindings never contradict
	// the surface currently receiving keys.
	if key.Matches(msg, a.keys.Help) {
		a.help.setContext(a.detail.helpContext())
		a.help.toggle()
		return a, nil
	}

	// Global search works from inside the reader too: `/` opens the overlay
	// over the open spec, and Esc from a search-opened reader returns there.
	if key.Matches(msg, a.keys.Search) {
		return a, a.openSearchOverlay()
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
		return a, a.switchView(ViewSecurity)
	case key.Matches(msg, a.keys.Tab7):
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
