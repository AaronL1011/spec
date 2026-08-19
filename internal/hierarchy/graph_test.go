package hierarchy

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// testPipeline is a minimal pipeline whose terminal stage is "done" (the last
// required stage) plus the auto-archiving "closed".
func testPipeline() config.PipelineConfig {
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "draft"},
			{Name: "build"},
			{Name: "done"},
			{Name: "closed", Optional: true, AutoArchive: true},
		},
	}
}

// twoLevelTree is the canonical fixture: one initiative with three slices in
// mixed states, plus an unrelated standalone spec.
func twoLevelTree() []SpecRef {
	return []SpecRef{
		{ID: "SPEC-004", Title: "API rate limiting", Status: "build"},
		{ID: "SPEC-009", Title: "Token bucket", Status: "done", Parent: "SPEC-004"},
		{ID: "SPEC-010", Title: "Redis backend", Status: "build", Parent: "SPEC-004"},
		{ID: "SPEC-011", Title: "Admin overrides", Status: "blocked", Parent: "SPEC-004"},
		{ID: "SPEC-014", Title: "Standalone", Status: "draft"},
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name         string
		refs         []SpecRef
		id           string
		wantChildren int
		wantParent   string
		wantIsSlice  bool
	}{
		{
			name:         "initiative indexes its slices",
			refs:         twoLevelTree(),
			id:           "SPEC-004",
			wantChildren: 3,
		},
		{
			name:        "slice resolves its initiative",
			refs:        twoLevelTree(),
			id:          "SPEC-009",
			wantParent:  "SPEC-004",
			wantIsSlice: true,
		},
		{
			name: "standalone spec has neither direction",
			refs: twoLevelTree(),
			id:   "SPEC-014",
		},
		{
			name:        "dangling parent still marks the spec a slice",
			refs:        []SpecRef{{ID: "SPEC-009", Parent: "SPEC-999"}},
			id:          "SPEC-009",
			wantIsSlice: true,
		},
		{
			name:        "self-parent is never indexed as its own child or resolved as its own parent",
			refs:        []SpecRef{{ID: "SPEC-009", Parent: "SPEC-009"}},
			id:          "SPEC-009",
			wantIsSlice: true,
		},
		{
			name: "duplicate IDs keep the first ref",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build"},
				{ID: "SPEC-004", Status: "done", Archived: true},
			},
			id: "SPEC-004",
		},
		{
			name: "empty IDs are ignored",
			refs: []SpecRef{{ID: "", Parent: "SPEC-004"}, {ID: "SPEC-004"}},
			id:   "SPEC-004",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Build(tt.refs)
			if got := len(g.Children(tt.id)); got != tt.wantChildren {
				t.Errorf("Children(%s) = %d, want %d", tt.id, got, tt.wantChildren)
			}
			if g.HasChildren(tt.id) != (tt.wantChildren > 0) {
				t.Errorf("HasChildren(%s) = %v, want %v", tt.id, g.HasChildren(tt.id), tt.wantChildren > 0)
			}
			parent, ok := g.Parent(tt.id)
			if tt.wantParent == "" && ok {
				t.Errorf("Parent(%s) = %s, want none", tt.id, parent.ID)
			}
			if tt.wantParent != "" && (!ok || parent.ID != tt.wantParent) {
				t.Errorf("Parent(%s) = %q/%v, want %s", tt.id, parent.ID, ok, tt.wantParent)
			}
			if g.IsSlice(tt.id) != tt.wantIsSlice {
				t.Errorf("IsSlice(%s) = %v, want %v", tt.id, g.IsSlice(tt.id), tt.wantIsSlice)
			}
		})
	}
}

func TestBuild_DuplicateIDsFirstWins(t *testing.T) {
	g := Build([]SpecRef{
		{ID: "SPEC-004", Status: "build"},
		{ID: "SPEC-004", Status: "closed", Archived: true},
	})
	ref, ok := g.Get("SPEC-004")
	if !ok {
		t.Fatal("SPEC-004 not indexed")
	}
	if ref.Status != "build" || ref.Archived {
		t.Errorf("got %+v, want the first ref (build, not archived)", ref)
	}
	if len(g.Refs()) != 1 {
		t.Errorf("Refs() = %d entries, want 1", len(g.Refs()))
	}
}

func TestGraph_NilSafe(t *testing.T) {
	var g *Graph
	if _, ok := g.Get("SPEC-001"); ok {
		t.Error("Get on nil graph should report false")
	}
	if _, ok := g.Parent("SPEC-001"); ok {
		t.Error("Parent on nil graph should report false")
	}
	if g.Children("SPEC-001") != nil || g.HasChildren("SPEC-001") || g.IsSlice("SPEC-001") || g.Refs() != nil {
		t.Error("nil graph should answer empty for every query")
	}
}

func TestGraph_ChildrenIsACopy(t *testing.T) {
	g := Build(twoLevelTree())
	kids := g.Children("SPEC-004")
	kids[0].ID = "MUTATED"
	if g.Children("SPEC-004")[0].ID == "MUTATED" {
		t.Error("Children returned the internal slice; callers can corrupt the graph")
	}
}

func TestRollup(t *testing.T) {
	tests := []struct {
		name string
		refs []SpecRef
		id   string
		want Rollup
	}{
		{
			name: "mixed states",
			refs: twoLevelTree(),
			id:   "SPEC-004",
			want: Rollup{Total: 3, Complete: 1, Open: 2, Blocked: 1},
		},
		{
			name: "zero children",
			refs: twoLevelTree(),
			id:   "SPEC-014",
			want: Rollup{},
		},
		{
			name: "all complete across both terminal stages",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build"},
				{ID: "SPEC-009", Status: "done", Parent: "SPEC-004"},
				{ID: "SPEC-010", Status: "closed", Parent: "SPEC-004"},
			},
			id:   "SPEC-004",
			want: Rollup{Total: 2, Complete: 2, Open: 0},
		},
		{
			name: "none complete",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build"},
				{ID: "SPEC-009", Status: "draft", Parent: "SPEC-004"},
			},
			id:   "SPEC-004",
			want: Rollup{Total: 1, Complete: 0, Open: 1},
		},
		{
			name: "unknown spec rolls up to nothing",
			refs: twoLevelTree(),
			id:   "SPEC-999",
			want: Rollup{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Build(tt.refs).Rollup(tt.id, testPipeline())
			if got != tt.want {
				t.Errorf("Rollup(%s) = %+v, want %+v", tt.id, got, tt.want)
			}
		})
	}
}

func TestRollup_IsComplete(t *testing.T) {
	tests := []struct {
		name string
		r    Rollup
		want bool
	}{
		{"childless is false, never vacuously true", Rollup{}, false},
		{"one open child", Rollup{Total: 2, Complete: 1, Open: 1}, false},
		{"all terminal", Rollup{Total: 2, Complete: 2}, true},
		{"single terminal child", Rollup{Total: 1, Complete: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.IsComplete(); got != tt.want {
				t.Errorf("IsComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}
