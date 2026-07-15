package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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
	case ViewSecurity:
		return refreshKeySecurity
	default:
		return ""
	}
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
