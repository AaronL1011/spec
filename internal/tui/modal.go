package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/tui/components"
)

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
			if a.modal.Value() != "" {
				// Capture input before Hide() clears it.
				input := a.modal.Value()
				a.modal.Hide()
				return a, a.executeActionWithInput(input)
			}
			return a, nil
		default:
			// Delegate to the embedded text field: arrows, home/end, backspace,
			// word jumps, and rune entry are all handled natively.
			return a, a.modal.Update(msg)
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
	a.modal.SetValue(selfAssignIdentity(a.rc))
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
