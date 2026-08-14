package tui

import (
	"sort"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
)

// parentSpecTitle resolves an initiative's title for the detail pane's
// "Part of" line. It resolves through the same root/triage/archive search the
// rest of the tool uses, so an archived initiative is still named rather than
// silently dropping to a bare ID. Returns "" for a spec with no parent, an
// unresolvable parent, or an unconfigured specs repo — all of which degrade to
// showing just the ID.
//
// It resolves one spec rather than building a graph: the detail pane refetches
// on every watcher event, and a full scan there would re-read every spec in
// the repo for a single title.
func parentSpecTitle(rc *config.ResolvedConfig, parentID string) string {
	if parentID == "" || rc == nil || rc.SpecsRepoDir == "" {
		return ""
	}
	ref, ok := hierarchy.Find(rc.SpecsRepoDir, config.ArchiveDir(rc.Team), parentID)
	if !ok {
		return ""
	}
	return ref.Title
}

// Tree connectors for the nested Specs view. Box-drawing glyphs are one cell
// wide, but a terminal with no colour profile is also the terminal least
// likely to carry them, so colourDisabled() selects the ASCII pair — the same
// signal the markdown renderer uses to drop to plain text.
const (
	connectorTee        = "├─ "
	connectorElbow      = "└─ "
	connectorTeeASCII   = "|- "
	connectorElbowASCII = "`- "

	// connectorWidth is the display width of every connector, ASCII or not, so
	// column maths never depends on which pair is in use.
	connectorWidth = 3
)

// treeGutter returns the fixed-width leading cell that carries a row's place
// in the hierarchy: a connector on a slice, a fold indicator on an initiative,
// blanks on a standalone spec, and nothing at all when the list contains no
// hierarchy.
//
// It follows the bounty gutter's convention deliberately: the cell is present
// on every row once any spec in the list has a parent, so nesting can never
// shift the columns to its right, and a team that has never linked two specs
// sees exactly the layout it always had.
func treeGutter(item specListItem, anyHierarchy bool) string {
	if !anyHierarchy {
		return ""
	}
	switch {
	case item.depth > 0:
		return treeConnector(item.lastChild)
	case item.initiative && item.folded:
		return IconCollapsed + "  "
	case item.initiative:
		return IconExpanded + "  "
	default:
		return strings.Repeat(" ", connectorWidth)
	}
}

// hasHierarchy reports whether any row in the list is part of a tree, which is
// what turns the tree gutter on.
func hasHierarchy(items []specListItem) bool {
	for _, it := range items {
		if it.depth > 0 || it.initiative {
			return true
		}
	}
	return false
}

// treeConnector returns the leading connector for a child row: an elbow for
// the last slice of an initiative, a tee otherwise.
func treeConnector(last bool) string {
	if colourDisabled() {
		if last {
			return connectorElbowASCII
		}
		return connectorTeeASCII
	}
	if last {
		return connectorElbow
	}
	return connectorTee
}

// nestSpecs reorders a flat spec list so each initiative is immediately
// followed by its slices, and annotates every row with its place in the tree.
//
// Standalone specs keep their original position, so a team that never uses
// hierarchy sees exactly the list it always had. Only specs whose parent is
// present in the same list are nested: an archived initiative's live slices
// stay at top level in the active list rather than vanishing.
func nestSpecs(items []specListItem) []specListItem {
	byParent := make(map[string][]specListItem)
	present := make(map[string]bool, len(items))
	for _, it := range items {
		present[it.ID] = true
	}
	for _, it := range items {
		if it.Parent != "" && present[it.Parent] && it.Parent != it.ID {
			byParent[it.Parent] = append(byParent[it.Parent], it)
		}
	}
	if len(byParent) == 0 {
		return items
	}
	for parent := range byParent {
		kids := byParent[parent]
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].ID < kids[j].ID })
		byParent[parent] = kids
	}

	out := make([]specListItem, 0, len(items))
	for _, it := range items {
		if it.Parent != "" && present[it.Parent] && it.Parent != it.ID {
			continue // emitted under its initiative below
		}
		kids := byParent[it.ID]
		if len(kids) > 0 {
			it.initiative = true
			it.rollup = rollupOf(kids)
		}
		out = append(out, it)
		for i, kid := range kids {
			kid.depth = 1
			kid.lastChild = i == len(kids)-1
			out = append(out, kid)
		}
	}
	return out
}

// collapseSlices drops the slice rows of collapsed initiatives from an already
// nested list. The initiative row keeps its rollup, so a folded initiative
// still reports how much of it is done.
func collapseSlices(items []specListItem, collapsed map[string]bool) []specListItem {
	if len(collapsed) == 0 {
		return items
	}
	out := make([]specListItem, 0, len(items))
	for _, it := range items {
		if it.depth > 0 && collapsed[it.Parent] {
			continue
		}
		if it.initiative {
			it.folded = collapsed[it.ID]
		}
		out = append(out, it)
	}
	return out
}

// rollupOf counts a set of sibling slices by completion state. The list view
// has no pipeline config to hand, so completeness is read from the flag the
// loader already stamped on each row.
func rollupOf(kids []specListItem) hierarchy.Rollup {
	r := hierarchy.Rollup{Total: len(kids)}
	for _, k := range kids {
		if k.complete {
			r.Complete++
		}
	}
	r.Open = r.Total - r.Complete
	return r
}
