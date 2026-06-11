package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

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
