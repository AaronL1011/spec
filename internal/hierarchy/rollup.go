package hierarchy

import (
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/pipeline/expr"
)

// Rollup summarises an initiative's deliverable slices.
//
// Total is zero for a spec with no children, and every predicate built on a
// Rollup treats that as "not an initiative" rather than as vacuous success —
// see Complete's doc comment for why that matters.
type Rollup struct {
	// Total is the number of specs naming this spec as their parent.
	Total int

	// Complete is how many of those are in a terminal stage.
	Complete int

	// Open is Total - Complete.
	Open int

	// Blocked is how many children sit in the escape-hatch stage. A blocked
	// child is also counted in Open.
	Blocked int
}

// Rollup counts id's children by completion state. Terminal detection uses the
// pipeline's derived terminal stages, never a hardcoded stage list, so a team
// that renames "done" keeps working.
func (g *Graph) Rollup(id string, pl config.PipelineConfig) Rollup {
	var r Rollup
	for _, child := range g.Children(id) {
		r.Total++
		switch {
		case child.Corrupt:
			// Unknown must fail closed: an unreadable slice counts as open, so
			// children_complete can never pass over a file nobody can read.
		case pipeline.IsTerminalStage(pl, child.Status):
			r.Complete++
		case child.Status == pipeline.StatusBlocked:
			r.Blocked++
		}
	}
	r.Open = r.Total - r.Complete
	return r
}

// IsComplete reports whether every child is terminal and there is at least one
// child.
//
// A childless spec is deliberately false, never vacuously true. The
// children_complete gate is composed under `any:` beside delivery gates such as
// pr_stack_exists; a false branch there is inert, whereas a vacuously-true one
// would silently waive the delivery gate for every ordinary spec in the repo.
func (r Rollup) IsComplete() bool {
	return r.Total > 0 && r.Complete == r.Total
}

// ExprContext projects the rollup into the gate expression environment.
//
// The conversion lives here rather than at each of the three gate call sites
// (CLI, workflow engine, MCP server) so the rollup a gate sees can never drift
// between surfaces. It is also why internal/pipeline takes a plain
// expr.ChildrenContext rather than a hierarchy.Rollup: this package already
// imports internal/pipeline for terminal-stage detection, so the dependency
// can only point this way.
func (r Rollup) ExprContext() expr.ChildrenContext {
	return expr.ChildrenContext{
		Total:    r.Total,
		Complete: r.Complete,
		Open:     r.Open,
		Blocked:  r.Blocked,
	}
}
