package tui

import "github.com/aaronl1011/spec/internal/config"

// triageAction names every gated operation on a triage item.
type triageAction string

const (
	// actionTriageView and actionTriageComment are open to all roles.
	actionTriageView    triageAction = "view"
	actionTriageComment triageAction = "comment"

	// The following actions are gated on pm or engineer role.
	actionTriageEdit     triageAction = "edit"
	actionTriageClose    triageAction = "close"
	actionTriageEscalate triageAction = "escalate"

	// Promote is gated on pm role only.
	actionTriagePromote triageAction = "promote"
)

// roleAllowed reports whether the resolved role permits the given triage action.
// When rc has no configured role the function defaults to read-only (view/comment
// allowed, mutations denied) so the TUI is usable without configuration.
func roleAllowed(action triageAction, rc *config.ResolvedConfig) bool {
	role := ""
	if rc != nil {
		role = rc.OwnerRole("")
	}

	switch action {
	case actionTriageView, actionTriageComment:
		// Available to everyone, including unconfigured users.
		return true
	case actionTriageEdit, actionTriageClose, actionTriageEscalate:
		// PM or engineer only.
		return role == "pm" || role == "engineer"
	case actionTriagePromote:
		// PM only.
		return role == "pm"
	default:
		return false
	}
}
