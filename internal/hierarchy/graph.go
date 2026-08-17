// Package hierarchy models the two-level parent/child relationship between
// specs: an initiative spec above, the deliverable slices executed beneath it.
//
// The graph, rollup, invariant and link logic is pure — it takes a slice of
// SpecRef plus a pipeline config and touches neither the filesystem nor the
// database, so every rule is table-testable without fixtures. Scan (scan.go)
// is the single impure entry point that turns a specs directory into refs.
package hierarchy

// SpecRef is the minimal projection of a spec that hierarchy reasoning needs.
// Callers build these from whatever spec load path they already have, which is
// what keeps the rest of this package free of I/O.
type SpecRef struct {
	// ID is the spec identifier, e.g. "SPEC-009".
	ID string

	// Title is the spec title, carried so rollup rendering does not need a
	// second read of the file.
	Title string

	// Status is the spec's current pipeline stage.
	Status string

	// Parent is the ID of this spec's initiative, or "" when it stands alone.
	Parent string

	// Path is the spec file's location on disk. Empty is legal; it is carried
	// for callers that need to open the document after a graph query.
	Path string

	// Archived reports whether the spec lives in the archive directory. It is
	// supplied by the caller rather than derived from Path so this package
	// never has to know a team's configured archive directory name.
	Archived bool

	// Corrupt marks a spec file that exists but whose frontmatter could not be
	// parsed. ID and Path are trustworthy — both derive from the filename —
	// and Parent may carry a best-effort salvage (see salvageParent), but
	// Title and Status are unknowable and stay empty. Corrupt refs exist to
	// prove existence: a slice of an unreadable initiative draws a warning,
	// not the dangling-parent error that would block its advance over someone
	// else's typo.
	Corrupt bool
}

// Graph indexes specs by ID and by parent. It is built once per command and
// answers both directions of the relationship.
//
// Build indexes exactly one level. A grandchild edge present in hand-edited
// frontmatter is reported by Check as a finding and is never traversed, so no
// operation here can recurse.
type Graph struct {
	byID     map[string]SpecRef
	children map[string][]SpecRef
	order    []string
}

// Build indexes refs into a graph. Refs are indexed in the order given and the
// first ref for an ID wins, mirroring the precedence of the spec path resolver
// (root, then triage/, then archive/): a spec that exists in two places
// resolves to the same document here as it does everywhere else. Refs with an
// empty ID are ignored.
func Build(refs []SpecRef) *Graph {
	g := &Graph{
		byID:     make(map[string]SpecRef, len(refs)),
		children: make(map[string][]SpecRef),
	}
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if _, seen := g.byID[ref.ID]; seen {
			continue
		}
		g.byID[ref.ID] = ref
		g.order = append(g.order, ref.ID)
	}
	// Index children in the same order, skipping self-parenting edges so a
	// corrupt document cannot make a spec its own child.
	for _, id := range g.order {
		ref := g.byID[id]
		if ref.Parent == "" || ref.Parent == ref.ID {
			continue
		}
		g.children[ref.Parent] = append(g.children[ref.Parent], ref)
	}
	return g
}

// Get returns the ref for id.
func (g *Graph) Get(id string) (SpecRef, bool) {
	if g == nil {
		return SpecRef{}, false
	}
	ref, ok := g.byID[id]
	return ref, ok
}

// Parent returns id's initiative spec. It reports false when id is unknown,
// has no parent, names itself, or names a parent that does not exist. The
// latter two are the corrupt cases Check reports; refusing to resolve them
// here means no caller can render a spec as its own initiative.
func (g *Graph) Parent(id string) (SpecRef, bool) {
	if g == nil {
		return SpecRef{}, false
	}
	ref, ok := g.byID[id]
	if !ok || ref.Parent == "" || ref.Parent == ref.ID {
		return SpecRef{}, false
	}
	parent, ok := g.byID[ref.Parent]
	return parent, ok
}

// Children returns id's deliverable slices in index order. The result is a
// copy, so callers may sort it freely.
func (g *Graph) Children(id string) []SpecRef {
	if g == nil {
		return nil
	}
	kids := g.children[id]
	if len(kids) == 0 {
		return nil
	}
	out := make([]SpecRef, len(kids))
	copy(out, kids)
	return out
}

// HasChildren reports whether id is an initiative — that is, whether any spec
// declares it as a parent. "Is this an initiative?" is always derived from the
// graph and never declared in frontmatter, so the two can never disagree.
func (g *Graph) HasChildren(id string) bool {
	if g == nil {
		return false
	}
	return len(g.children[id]) > 0
}

// IsSlice reports whether id declares a parent, whether or not that parent
// resolves.
func (g *Graph) IsSlice(id string) bool {
	if g == nil {
		return false
	}
	ref, ok := g.byID[id]
	return ok && ref.Parent != ""
}

// Refs returns every indexed ref in index order.
func (g *Graph) Refs() []SpecRef {
	if g == nil {
		return nil
	}
	out := make([]SpecRef, 0, len(g.order))
	for _, id := range g.order {
		out = append(out, g.byID[id])
	}
	return out
}
