package tui

import tea "charm.land/bubbletea/v2"

// wheelStep is how many lines one mouse-wheel notch scrolls a detail view.
// List views move their selection by a single row per notch instead, so a
// wheel flick never skips past items unseen.
const wheelStep = 3

// Compile-time guarantee that every content view satisfies the click contract.
// If a view's clickRow/wheelRows signature drifts, the build breaks here rather
// than silently dropping mouse support for that tab.
var (
	_ mouseClickable = (*dashboardModel)(nil)
	_ mouseClickable = (*pipelineModel)(nil)
	_ mouseClickable = (*specListModel)(nil)
	_ mouseClickable = (*triageModel)(nil)
	_ mouseClickable = (*reviewModel)(nil)
)

// handleMouse is the single entry point for mouse input. It mirrors the
// keyboard priority chain in handleKey: overlays absorb first, then wheel
// scrolling, then region-based dispatch to the tab strip or the active view.
func (a App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Act only on left-button clicks and wheel events. Motion, drag, release,
	// and other buttons are ignored so hover/drag noise never drives the UI. In
	// Bubble Tea v2 the concrete message type encodes the action (click vs wheel
	// vs motion vs release); the Mouse() payload carries position and button.
	m := msg.Mouse()
	_, isWheel := msg.(tea.MouseWheelMsg)
	click, isClick := msg.(tea.MouseClickMsg)
	leftPress := isClick && click.Button == tea.MouseLeft
	if !isWheel && !leftPress {
		return a, nil
	}

	// Layer 1: overlays absorb every mouse event, just as they absorb keys.
	// A stray click must not leak through to the view beneath a dialog.
	if a.overlayActive() {
		return a, nil
	}

	// Layer 2: the wheel scrolls the focused surface regardless of where the
	// pointer sits, matching how terminals and editors treat the wheel.
	if isWheel {
		return a.handleWheel(m)
	}

	// Layer 3: a left click dispatches by the screen region it landed in.
	switch a.layout().regionAt(m.Y) {
	case regionTabs:
		if idx, ok := a.tabs.TabAt(m.X); ok {
			return a, a.switchView(View(idx))
		}
	case regionContent:
		return a.handleContentClick(m)
	}
	return a, nil
}

// overlayActive reports whether a modal-like surface is capturing input. It
// mirrors Layer 1 of handleKey so mouse and keyboard agree on what "modal"
// means.
func (a App) overlayActive() bool {
	return a.standup.visible || a.intake.active || a.revert.active ||
		a.help.visible || a.modal.Visible
}

// handleWheel routes a wheel notch to the focused scrollable: the open spec
// detail if present, otherwise the active view's selection.
func (a App) handleWheel(m tea.Mouse) (tea.Model, tea.Cmd) {
	up := m.Button == tea.MouseWheelUp
	down := m.Button == tea.MouseWheelDown
	if !up && !down {
		return a, nil // ignore horizontal wheel
	}

	if a.showDetail {
		delta := wheelStep
		if up {
			delta = -wheelStep
		}
		a.detail.wheelScroll(delta)
		a.syncBusyState()
		return a, nil
	}

	step := 1
	if up {
		step = -1
	}
	if c := a.activeClickable(); c != nil {
		c.wheelRows(step)
	}
	return a, nil
}

// handleContentClick maps a click in the content band to the active view's row
// hit-testing. A click on the already-selected row activates it, taking the
// same path as pressing Enter.
func (a App) handleContentClick(m tea.Mouse) (tea.Model, tea.Cmd) {
	row, ok := a.layout().contentRow(m.Y)
	if !ok {
		return a, nil
	}

	// Inside an open spec detail, the only clickable target is the reader's
	// section navigator; clicking a section jumps to it (same path as the
	// number-key jumps). Prose and thread clicks remain keyboard/wheel-driven.
	if a.showDetail {
		if idx, ok := a.detail.sectionAtClick(m.X, row); ok {
			var cmd tea.Cmd
			a.detail, cmd = a.detail.withSection(idx)
			a.syncBusyState()
			return a, cmd
		}
		return a, nil
	}

	c := a.activeClickable()
	if c == nil {
		return a, nil
	}
	if c.clickRow(row) == clickActivated {
		return a, a.activateSelection()
	}
	return a, nil
}

// activeClickable returns the active view as a mouseClickable, or nil for
// views without selectable rows (Settings). It returns a pointer into the
// receiver's view field so clickRow/wheelRows mutate the live model.
func (a *App) activeClickable() mouseClickable {
	switch a.activeView {
	case ViewDashboard:
		return &a.dashboard
	case ViewPipeline:
		return &a.pipeline
	case ViewSpecs:
		return &a.specs
	case ViewTriage:
		return &a.triage
	case ViewReviews:
		return &a.reviews
	default:
		return nil
	}
}
