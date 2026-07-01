package dashboard

import (
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/identity"
)

// Viewer is an alias for identity.Viewer, kept so dashboard code and its
// existing tests read the same as before the identity package was extracted.
// internal/identity is the source of truth; this package is a thin caller.
type Viewer = identity.Viewer

// SpecView is the minimal projection of a spec the visibility rules need. It
// keeps the resolver pure — no file or config I/O — so the policy is one
// table-tested place and the aggregator stays thin.
type SpecView struct {
	Author      string
	Assignees   []string
	Status      string
	BlockedFrom string
}

// VisibleInDo reports whether the spec should appear in the viewer's DO
// section, applying the spec's current stage dashboard scope.
func VisibleInDo(pl config.PipelineConfig, s SpecView, v Viewer) bool {
	stage := pl.StageByName(s.Status)
	if stage == nil {
		return false
	}
	switch stage.Dashboard.Scope() {
	case config.DoScopeNone:
		return false
	case config.DoScopeAuthor:
		return identity.MatchesIdentity(s.Author, v)
	case config.DoScopeAssignee:
		if len(s.Assignees) == 0 {
			// Unclaimed work surfaces to the whole owning role so someone can
			// pick it up — unless the stage opts out of claimability.
			return stage.Dashboard.IsClaimable() && stage.HasOwner(v.Role)
		}
		return identity.AnyIdentity(s.Assignees, v)
	default: // DoScopeRole
		return stage.HasOwner(v.Role)
	}
}

// VisibleInBlocked reports whether a blocked spec should appear in the viewer's
// BLOCKED section, applying the team-level blocked config.
func VisibleInBlocked(pl config.PipelineConfig, cfg config.BlockedConfig, s SpecView, v Viewer) bool {
	if !cfg.RoleCanSee(v.Role) {
		return false
	}
	switch cfg.EffectiveScope() {
	case config.BlockedScopeInvolved:
		return isInvolved(s, v)
	case config.BlockedScopeOwningRole:
		stage := pl.StageByName(s.BlockedFrom)
		if stage == nil {
			// Unknown pre-block stage (e.g. blocked before blocked_from
			// existed): fall back to visible so the signal is not lost.
			return true
		}
		return stage.HasOwner(v.Role)
	default: // BlockedScopeAll
		return true
	}
}

// isInvolved reports whether the viewer authored or is assigned to the spec.
func isInvolved(s SpecView, v Viewer) bool {
	return identity.MatchesIdentity(s.Author, v) || identity.AnyIdentity(s.Assignees, v)
}
