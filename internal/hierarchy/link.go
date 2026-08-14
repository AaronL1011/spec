package hierarchy

import (
	"errors"
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/pipeline"
)

// Link preconditions. Callers match on these with errors.Is when they need to
// distinguish a refusal from a genuine failure; the wrapped message already
// names the next action for a human.
var (
	// ErrSpecNotFound means the spec being linked does not resolve.
	ErrSpecNotFound = errors.New("spec not found")

	// ErrParentNotFound means the proposed parent does not resolve.
	ErrParentNotFound = errors.New("parent spec not found")

	// ErrSelfParent means a spec was asked to be its own parent.
	ErrSelfParent = errors.New("a spec cannot be its own parent")

	// ErrDepthExceeded means the proposed parent is itself a slice.
	ErrDepthExceeded = errors.New("hierarchy is two levels deep")

	// ErrChildHasChildren means the spec being attached is already an initiative.
	ErrChildHasChildren = errors.New("a spec with children cannot gain a parent")

	// ErrParentTerminal means the proposed parent has already completed.
	ErrParentTerminal = errors.New("parent has already completed")

	// ErrParentArchived means the proposed parent has been archived.
	ErrParentArchived = errors.New("parent is archived")
)

// Link checks the preconditions for attaching childID to parentID. It mutates
// nothing: the caller performs the frontmatter write once this returns nil.
//
// Enforcement lives here, at the mutation point, rather than only in a gate,
// because children_complete is non-monotonic — a child can be reverted after
// its parent closed. Refusing the link makes "parent done with an incomplete
// child" unreachable by construction rather than merely detectable after the
// fact. Every refusal names the escape.
//
// Detaching (parentID == "") is always permitted and is the escape hatch for
// every rule below, so a mis-linked spec is never stuck.
func Link(g *Graph, childID, parentID string, pl config.PipelineConfig) error {
	if _, ok := g.Get(childID); !ok {
		return fmt.Errorf("%w: %s — check the ID and try again", ErrSpecNotFound, childID)
	}
	if parentID == "" {
		return nil
	}
	if parentID == childID {
		return fmt.Errorf("%w: %s cannot be a slice of itself", ErrSelfParent, childID)
	}
	if g.HasChildren(childID) {
		return fmt.Errorf("%w: %s already has slices of its own, and the hierarchy is two levels deep — detach them first with 'spec link <slice> --parent \"\"'",
			ErrChildHasChildren, childID)
	}

	return CanParent(g, parentID, pl)
}

// CanParent checks that parentID is eligible to receive a new deliverable
// slice: it exists, is not itself a slice, and has not already settled.
//
// It is split out of Link because `spec new --parent` must refuse the same
// parents before the child spec exists to be linked. Rules that concern the
// child (self-parenting, a child that is already an initiative) stay in Link.
func CanParent(g *Graph, parentID string, pl config.PipelineConfig) error {
	parent, ok := g.Get(parentID)
	if !ok {
		return fmt.Errorf("%w: %s — create it first, or check the ID", ErrParentNotFound, parentID)
	}
	if parent.Parent != "" {
		return fmt.Errorf("%w: %s is already a slice of %s, so it cannot also be an initiative — use --parent %s instead",
			ErrDepthExceeded, parentID, parent.Parent, parent.Parent)
	}
	if parent.Archived {
		return fmt.Errorf("%w: %s has been archived — reopen it before adding slices", ErrParentArchived, parentID)
	}
	if pipeline.IsTerminalStage(pl, parent.Status) {
		return fmt.Errorf("%w: %s is at %s — run 'spec revert %s' before adding a slice to it",
			ErrParentTerminal, parentID, parent.Status, parentID)
	}
	return nil
}
