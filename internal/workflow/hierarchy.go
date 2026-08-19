package workflow

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
	"github.com/aaronl1011/spec/internal/pipeline/expr"
)

// specHierarchy builds the parent/child graph from the specs directory inside
// the repo clone. A failure is returned to the caller rather than swallowed;
// callers that only need decoration degrade, callers that gate do not.
func (d Deps) specHierarchy(specDir string) (*hierarchy.Graph, error) {
	return hierarchy.Load(specDir, config.ArchiveDir(d.Config.Team))
}

// childrenContext returns the deliverable-slice rollup for a spec, as the
// plain struct the expression context takes. It degrades to the zero value
// (which reads as "not an initiative") when the graph cannot be built, so an
// unreadable specs directory can only ever make children_complete fail closed.
func (d Deps) childrenContext(specDir, specID string) expr.ChildrenContext {
	g, err := d.specHierarchy(specDir)
	if err != nil {
		return expr.ChildrenContext{}
	}
	return g.Rollup(specID, d.Config.Pipeline()).ExprContext()
}

// checkHierarchy refuses a transition while the spec's parent link is broken.
//
// Only errors block: a dangling or self-referential parent makes every
// downstream hierarchy query undefined, so advancing on top of it would bake
// the corruption into a later stage. Warnings — a terminal parent, an archived
// parent, a depth-three link — never block, because they punish the wrong
// engineer for something that happened elsewhere.
func (d Deps) checkHierarchy(specDir, specID string) error {
	g, err := d.specHierarchy(specDir)
	if err != nil {
		// Unverifiable is not the same as broken. A dangling parent is only
		// provable against a readable specs tree, so a scan failure must not
		// block an otherwise valid transition.
		return nil //nolint:nilerr // see above: unverifiable ≠ broken
	}
	for _, f := range hierarchy.Check(g, specID, d.Config.Pipeline()) {
		if f.IsError() {
			return fmt.Errorf("parent link is broken: %s", f.Message)
		}
	}
	return nil
}
