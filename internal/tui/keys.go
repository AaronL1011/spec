package tui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// handleKey is the single entry point for all keyboard input.
// It follows a strict priority chain — the first match wins, no fall-through.
func (a App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ── Layer 1: Overlays (absorb all keys) ──────────────────────────
	// These are modal states that must capture every keystroke.

	if a.search.visible {
		return a.updateSearchOverlay(msg)
	}
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
	case key.Matches(msg, a.keys.Search):
		return a, a.openSearchOverlay()
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

// viewCapturingInput returns true when the active view is in a text
// input mode (e.g. a settings field) and keystrokes should be routed to
// the view instead of being interpreted as hotkeys.
func (a App) viewCapturingInput() bool {
	if a.activeView == ViewSettings {
		return a.settings.isEditing()
	}
	return false
}

// activeViewCanPopEsc reports whether the active view has dismissible state
// that esc should clear before the app treats esc as the exit-arm. The specs
// list no longer holds an in-place search filter (the global `/` overlay owns
// search now), so there is nothing for the list to pop; this stays false for
// every view.
func (a App) activeViewCanPopEsc() bool {
	return false
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
		if err := a.revert.openRevert(specID, stage, a.rc.Pipeline(), a.width, a.theme); err != nil {
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
	case key.Matches(msg, a.keys.Preview) && isSpecID(specID):
		return previewSpec(a.rc, specID), true
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
