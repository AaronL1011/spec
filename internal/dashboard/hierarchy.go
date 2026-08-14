package dashboard

import (
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// specTree resolves the initiative hierarchy over the specs clone. It degrades
// to an empty graph: hierarchy on the dashboard is a marker and a detail line,
// so an unreadable specs repo must cost the viewer nothing.
func specTree(rc *config.ResolvedConfig) *hierarchy.Graph {
	g, err := hierarchy.Load(rc.SpecsRepoDir, config.ArchiveDir(rc.Team))
	if err != nil {
		return hierarchy.Build(nil)
	}
	return g
}

// SliceMark returns the one-cell marker that follows a slice's SPEC-ID,
// carrying hierarchy inside an existing row rather than adding chrome. The
// dashboard sorts by personal urgency, and the pipeline groups by stage;
// re-parenting rows would lie about both, so the relationship is marked here
// and nested only in the structural browser.
//
// It is exported so the TUI's dashboard and pipeline rows use the same marker
// as the printed dashboard, and the three cannot drift apart.
func SliceMark(parent string) string {
	if parent == "" {
		return ""
	}
	return glyph.Slice
}

// titleSuffix renders " — title" for a non-empty title.
func titleSuffix(title string) string {
	if title == "" {
		return ""
	}
	return " — " + title
}

// parentLabels returns a spec's initiative ID and title, or two empty strings
// when it stands alone or names a parent that does not resolve.
func parentLabels(g *hierarchy.Graph, specID string) (id, title string) {
	parent, ok := g.Parent(specID)
	if !ok {
		return "", ""
	}
	return parent.ID, parent.Title
}

// initiativeUrgency suppresses the time-urgency gradient for an initiative
// that still has open slices, returning the supplied fraction otherwise.
//
// A vision document sitting still is correct behaviour, not staleness: the
// work is happening in its slices. Without this, every initiative is
// permanently red, and a gradient that is always at maximum stops carrying
// information anywhere on the dashboard. Note that StageEnteredAt deliberately
// survives ordinary edits, so an initiative accrues dwell from the moment it
// enters a stage no matter how much its author writes — which is exactly why
// this suppression is needed rather than optional.
//
// It is implemented here, at the call site, rather than as a stale_after
// override in the pipeline config: the condition is a property of one spec's
// rollup, not of the stage every spec shares.
func initiativeUrgency(g *hierarchy.Graph, pl config.PipelineConfig, specID string, fraction float64) float64 {
	if g.Rollup(specID, pl).Open > 0 {
		return 0
	}
	return fraction
}
