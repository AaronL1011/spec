package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/bounty"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/tui/components"
)

func (a App) updateModal(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch a.modal.Kind {
	case components.ModalInfo:
		// Read-only dialog (e.g. full error detail): any of esc/enter/q closes it.
		switch {
		case msg.String() == "esc", msg.String() == "enter",
			msg.Text == "q":
			// A one-time notice records its dismissal on close, so it is loud
			// once instead of nagging every launch.
			if a.pendingAction == "ack-agent-migration" {
				acknowledgeAgentMigration(&a)
				a.pendingAction = ""
			}
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
			// Capture input before Hide() clears it.
			input := a.modal.Value()
			if input == "" && !allowsEmptySubmit(a.pendingAction) {
				return a, nil
			}
			a.modal.Hide()
			return a, a.executeActionWithInput(input)
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

// allowsEmptySubmit reports whether an input modal's pending action treats a
// blank Enter as a valid submission rather than "keep waiting for text". An
// interactive-session intent is optional by design: blank means "open a general
// authoring session", and refusing to close on Enter would read as a hang.
func allowsEmptySubmit(pendingAction string) bool {
	return strings.HasPrefix(pendingAction, "draft-session:")
}

// armDraftSessionModal opens the intent input for an interactive authoring
// session. The target section (possibly empty) rides in the action name, the
// same pattern draft-next chaining uses, so no new App field is needed.
func (a *App) armDraftSessionModal(specID, slug string) {
	a.pendingAction = "draft-session:" + slug
	a.pendingSpecID = specID
	title := "Interactive session · " + specID
	if slug != "" {
		title += " · §" + slug
	}
	a.modal.ShowInput(title, "What should the agent work on? Enter with no text opens a general authoring session.")
	a.modal.SetSize(a.width, a.contentHeight())
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

// armBountyModal opens the bounty prompt for a spec, pre-filled with the
// existing reason so a re-grant sharpens the wording instead of retyping it.
// Entering "-" clears the bounty.
//
// The role check happens here rather than in the action: refusing after the user
// has typed a reason wastes their effort, so an unauthorised role is told before
// the prompt opens.
func (a *App) armBountyModal(specID string) {
	if err := bounty.Authorize(a.role, a.rc.Bounties()); err != nil {
		a.statusBar.SetStatusError("Bounty not available", err.Error())
		return
	}
	a.pendingAction = "bounty"
	a.pendingSpecID = specID
	a.modal.ShowInput(IconSpark+" Bounty "+specID, "Why is this worth claiming now? · '-' to clear:")
	a.modal.SetValue(currentBountyReason(a.rc, specID))
	a.modal.SetSize(a.width, a.contentHeight())
}

// currentBountyReason reads a spec's existing bounty reason from the local
// clone, or "" when it has none. Best-effort: a read failure just means an
// empty prompt.
func currentBountyReason(rc *config.ResolvedConfig, specID string) string {
	meta, err := markdown.ReadMeta(resolveLocalSpecPath(rc, specID))
	if err != nil || !meta.HasBounty() {
		return ""
	}
	return meta.Bounty.Reason
}

// executeActionWithInput runs the pending action with the given input value.
// For confirm modals, input is empty. For input modals, it contains the user's text.
func (a *App) executeActionWithInput(input string) tea.Cmd {
	specID := a.pendingSpecID

	// Chaining into the next empty section carries its slug in the action name,
	// so accepting one draft can flow into the next without a dedicated field.
	if slug, ok := strings.CutPrefix(a.pendingAction, "draft-next:"); ok {
		return a.startSectionDraft(specID, slug)
	}

	// An interactive session launch carries the target section the same way; the
	// input is the user's stated intent, and blank is a valid "just open it".
	if slug, ok := strings.CutPrefix(a.pendingAction, "draft-session:"); ok {
		return interactiveDraftSession(a.rc, specID, interactiveKickoff(specID, slug, input, "", nil))
	}

	switch a.pendingAction {
	case "advance":
		return a.startAction("advancing "+specID, advanceSpec(a.rc, a.reg, a.db, specID, a.role))
	case "block":
		reason := input
		if reason == "" {
			reason = "blocked from TUI"
		}
		return a.startAction("blocking "+specID, blockSpec(a.rc, a.reg, a.db, specID, reason, a.role))
	case "build":
		return a.startAction("building "+specID, buildSpec(a.rc, specID))
	case "assign":
		return a.startAction("assigning "+specID, assignSpec(a.rc, specID, parseAssignInput(input)))
	case "bounty":
		// "-" clears; empty input is a no-op rather than a reasonless grant.
		if input == "-" {
			return a.startAction("clearing bounty on "+specID, clearBounty(a.rc, specID))
		}
		if strings.TrimSpace(input) == "" {
			return nil
		}
		return a.startAction("bountying "+specID, grantBounty(a.rc, a.role, specID, input))
	case "unblock":
		return a.startAction("unblocking "+specID, unblockSpec(a.rc, a.reg, a.db, specID, a.role))
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
