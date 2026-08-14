package cmd

import (
	"fmt"
	"sort"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
)

// loadHierarchyIn builds the parent/child graph from a specs content directory.
// Use it inside a WithSpecsRepo mutator, where the just-pulled clone is
// authoritative, passing specsDir(repoPath).
func loadHierarchyIn(baseDir string, rc *config.ResolvedConfig) (*hierarchy.Graph, error) {
	return hierarchy.Load(baseDir, config.ArchiveDir(rc.Team))
}

// loadHierarchy builds the parent/child graph from the local specs clone. It is
// the read-path counterpart of loadHierarchyIn.
func loadHierarchy(rc *config.ResolvedConfig) (*hierarchy.Graph, error) {
	if rc.SpecsRepoDir == "" {
		return nil, fmt.Errorf("specs repo not configured — ensure spec.config.yaml has specs_repo settings")
	}
	return loadHierarchyIn(rc.SpecsRepoDir, rc)
}

// hierarchyView is a spec's place in the tree, resolved once per command so a
// surface renders both directions, its rollup, and its invariant findings from
// a single scan.
type hierarchyView struct {
	// Parent is the initiative this spec is a slice of, or nil when it stands
	// alone or names a parent that does not resolve.
	Parent *hierarchy.SpecRef

	// Children are this spec's deliverable slices, ordered by ID.
	Children []hierarchy.SpecRef

	// Rollup counts Children by completion state.
	Rollup hierarchy.Rollup

	// Findings are the violated parent/child invariants for this spec.
	Findings []hierarchy.Finding
}

// specHierarchyView resolves a spec's parent, slices and invariant findings
// from one scan of the specs tree.
//
// parentID is the spec's declared parent, used only for the degraded case: when
// the graph cannot be built the view reports "cannot verify" rather than a
// dangling parent. Getting that the wrong way round would turn every offline
// `spec validate` into a false failure, because EnsureSpecsRepo clones the
// whole repo and a genuine dangling parent is only provable against a healthy
// clone.
func specHierarchyView(rc *config.ResolvedConfig, specID, parentID string) hierarchyView {
	g, err := loadHierarchy(rc)
	if err != nil {
		return hierarchyView{Findings: unverifiableParent(parentID)}
	}
	view := hierarchyView{
		Children: g.Children(specID),
		Rollup:   g.Rollup(specID, rc.Pipeline()),
		Findings: hierarchy.Check(g, specID, rc.Pipeline()),
	}
	if parent, ok := g.Parent(specID); ok {
		view.Parent = &parent
	}
	sortSpecRefs(view.Children)
	return view
}

// unverifiableParent is the offline-safe substitute for the dangling-parent
// error: a warning, never a failure.
func unverifiableParent(parentID string) []hierarchy.Finding {
	if parentID == "" {
		return nil
	}
	return []hierarchy.Finding{{
		Rule:     hierarchy.RuleParentExists,
		Severity: hierarchy.SeverityWarning,
		Message:  fmt.Sprintf("cannot verify parent %s (specs repo unavailable)", parentID),
	}}
}

// printHierarchyFindings renders invariant findings and reports whether any of
// them block validation.
func printHierarchyFindings(findings []hierarchy.Finding) bool {
	if len(findings) == 0 {
		return false
	}
	fmt.Println("Hierarchy:")
	for _, f := range findings {
		marker := "⚠"
		if f.IsError() {
			marker = "✗"
		}
		fmt.Printf("  %s %s — %s\n", marker, f.Rule, f.Message)
	}
	fmt.Println()
	return hierarchy.HasErrors(findings)
}

// sortSpecRefs orders refs by ID, which for SPEC-NNN is also creation order.
func sortSpecRefs(refs []hierarchy.SpecRef) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
}
