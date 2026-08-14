package hierarchy

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/pipeline"
)

// Severity classifies a Finding. Errors make every downstream hierarchy query
// undefined and so fail validation; warnings describe a tree that is legible
// but untidy and let the author proceed.
type Severity string

const (
	// SeverityError fails `spec validate` and blocks `spec advance`.
	SeverityError Severity = "error"

	// SeverityWarning prints but does not block.
	SeverityWarning Severity = "warning"
)

// Rule names the invariant a Finding came from. They are user-facing: the
// validate output prints them so an author can see which rule fired.
const (
	// RuleParentExists — the named parent resolves to a spec.
	RuleParentExists = "parent_exists"

	// RuleSelfParent — a spec does not name itself as its parent.
	RuleSelfParent = "self_parent"

	// RuleParentDepth — the parent is not itself a slice (two levels only).
	RuleParentDepth = "parent_depth"

	// RuleChildHasChildren — a spec with a parent has no children of its own.
	RuleChildHasChildren = "child_has_children"

	// RuleParentTerminal — the parent has not already completed.
	RuleParentTerminal = "parent_terminal"

	// RuleParentArchived — the parent has not been archived.
	RuleParentArchived = "parent_archived"

	// RuleChildReopened — a child reopened after its parent closed. Second-order
	// divergence: warned, never repaired (one spec never mutates another's
	// stage).
	RuleChildReopened = "child_reopened"
)

// Finding is one violated invariant.
type Finding struct {
	// Rule is the invariant that fired, one of the Rule constants.
	Rule string

	// Severity is error or warning.
	Severity Severity

	// Message states the problem and the next action.
	Message string
}

// IsError reports whether the finding blocks validation.
func (f Finding) IsError() bool { return f.Severity == SeverityError }

// Check re-validates every parent/child invariant for a single spec.
//
// Link-time refusal (see Link) is the primary defence, but frontmatter is
// hand-editable: anyone can type `parent: SPEC-999` into a spec. Check is the
// backstop, with the severity split that keeps an initiative closing last week
// from retroactively wedging a slice today.
//
// An unknown id yields no findings: a caller that cannot see the spec has
// nothing to say about its links.
func Check(g *Graph, id string, pl config.PipelineConfig) []Finding {
	ref, ok := g.Get(id)
	if !ok {
		return nil
	}

	var findings []Finding
	findings = append(findings, checkParentLink(g, ref, pl)...)

	if g.HasChildren(id) {
		if ref.Parent != "" {
			findings = append(findings, Finding{
				Rule:     RuleChildHasChildren,
				Severity: SeverityWarning,
				Message: fmt.Sprintf("%s has a parent (%s) and children of its own — the hierarchy is two levels deep; detach one side with 'spec link %s --parent \"\"'",
					id, ref.Parent, id),
			})
		}
		findings = append(findings, checkReopenedChildren(g, ref, pl)...)
	}
	return findings
}

// checkParentLink validates the edge from ref up to its initiative.
func checkParentLink(g *Graph, ref SpecRef, pl config.PipelineConfig) []Finding {
	if ref.Parent == "" {
		return nil
	}
	if ref.Parent == ref.ID {
		return []Finding{{
			Rule:     RuleSelfParent,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s names itself as its parent — clear it with 'spec link %s --parent \"\"'", ref.ID, ref.ID),
		}}
	}

	parent, ok := g.Get(ref.Parent)
	if !ok {
		return []Finding{{
			Rule:     RuleParentExists,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s names parent %s, which does not exist — fix or clear it with 'spec link %s --parent <id>'", ref.ID, ref.Parent, ref.ID),
		}}
	}

	var findings []Finding
	if parent.Parent != "" {
		findings = append(findings, Finding{
			Rule:     RuleParentDepth,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("parent %s is itself a slice of %s — the hierarchy is two levels deep, so %s is a grandchild", parent.ID, parent.Parent, ref.ID),
		})
	}
	if parent.Archived {
		findings = append(findings, Finding{
			Rule:     RuleParentArchived,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("parent %s is archived — %s is a slice of a closed initiative", parent.ID, ref.ID),
		})
	} else if pipeline.IsTerminalStage(pl, parent.Status) {
		findings = append(findings, Finding{
			Rule:     RuleParentTerminal,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("parent %s is already at %s — %s is a slice of a completed initiative", parent.ID, parent.Status, ref.ID),
		})
	}
	return findings
}

// checkReopenedChildren reports the second-order divergence: this spec has
// completed, but a child has since been reverted or ejected back into flight.
// It is a warning by construction — the parent is never auto-reverted, because
// one document silently mutating another's stage is far worse to debug than a
// visible inconsistency, and would fan out spurious PM and comms effects.
func checkReopenedChildren(g *Graph, ref SpecRef, pl config.PipelineConfig) []Finding {
	if !ref.Archived && !pipeline.IsTerminalStage(pl, ref.Status) {
		return nil
	}
	var findings []Finding
	for _, child := range g.Children(ref.ID) {
		if child.Archived || pipeline.IsTerminalStage(pl, child.Status) {
			continue
		}
		findings = append(findings, Finding{
			Rule:     RuleChildReopened,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s closed · %s reopened to %s", ref.ID, child.ID, child.Status),
		})
	}
	return findings
}

// HasErrors reports whether any finding blocks validation.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.IsError() {
			return true
		}
	}
	return false
}
