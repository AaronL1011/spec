package dashboard

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func scopePipeline() config.PipelineConfig {
	no := false
	return config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "draft", Owner: config.Owners{"pm"}},
			{Name: "engineering", Owner: config.Owners{"engineer"},
				Dashboard: config.StageDashboardConfig{DoScope: "assignee"}},
			{Name: "locked", Owner: config.Owners{"engineer"},
				Dashboard: config.StageDashboardConfig{DoScope: "assignee", Claimable: &no}},
			{Name: "plan_review", Owner: config.Owners{"engineer", "tl"}},
			{Name: "authoring", Owner: config.Owners{"engineer"},
				Dashboard: config.StageDashboardConfig{DoScope: "author"}},
			{Name: "hidden", Owner: config.Owners{"engineer"},
				Dashboard: config.StageDashboardConfig{DoScope: "none"}},
			{Name: "build", Owner: config.Owners{"engineer"}},
		},
	}
}

func TestVisibleInDo(t *testing.T) {
	pl := scopePipeline()
	ana := Viewer{Role: "engineer", Name: "Ana", Handle: "@ana"}
	ben := Viewer{Role: "engineer", Name: "Ben", Handle: "@ben"}
	tl := Viewer{Role: "tl", Name: "Dan", Handle: "@dan"}

	tests := []struct {
		name string
		spec SpecView
		v    Viewer
		want bool
	}{
		{"role scope, owner role", SpecView{Status: "draft"}, Viewer{Role: "pm"}, true},
		{"role scope, wrong role", SpecView{Status: "draft"}, ana, false},
		{"assignee scope, unassigned shows to role", SpecView{Status: "engineering"}, ana, true},
		{"assignee scope, assigned shows to assignee", SpecView{Status: "engineering", Assignees: []string{"@ana"}}, ana, true},
		{"assignee scope, assigned hides from other", SpecView{Status: "engineering", Assignees: []string{"@ana"}}, ben, false},
		{"assignee scope matches by name", SpecView{Status: "engineering", Assignees: []string{"Ana"}}, ana, true},
		{"non-claimable unassigned hides from role", SpecView{Status: "locked"}, ana, false},
		{"non-claimable assigned shows to assignee", SpecView{Status: "locked", Assignees: []string{"@ana"}}, ana, true},
		{"review stage opens to whole role", SpecView{Status: "plan_review", Assignees: []string{"@ana"}}, ben, true},
		{"review stage shows to tl owner", SpecView{Status: "plan_review"}, tl, true},
		{"author scope shows to author", SpecView{Status: "authoring", Author: "Ana"}, ana, true},
		{"author scope hides from non-author", SpecView{Status: "authoring", Author: "Ana"}, ben, false},
		{"none scope hides from everyone", SpecView{Status: "hidden", Assignees: []string{"@ana"}}, ana, false},
		{"unknown stage hides", SpecView{Status: "nope"}, ana, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleInDo(pl, tt.spec, tt.v); got != tt.want {
				t.Errorf("VisibleInDo = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVisibleInBlocked(t *testing.T) {
	pl := scopePipeline()
	ana := Viewer{Role: "engineer", Name: "Ana", Handle: "@ana"}
	pm := Viewer{Role: "pm", Name: "Pam", Handle: "@pam"}
	tl := Viewer{Role: "tl", Name: "Dan", Handle: "@dan"}

	tests := []struct {
		name string
		cfg  config.BlockedConfig
		spec SpecView
		v    Viewer
		want bool
	}{
		{"default all, any role", config.BlockedConfig{}, SpecView{Status: "blocked"}, pm, true},
		{"visible_to excludes role", config.BlockedConfig{VisibleTo: []string{"tl"}}, SpecView{Status: "blocked"}, ana, false},
		{"visible_to includes role", config.BlockedConfig{VisibleTo: []string{"tl", "engineer"}}, SpecView{Status: "blocked"}, ana, true},
		{"involved: author matches", config.BlockedConfig{Scope: "involved"}, SpecView{Status: "blocked", Author: "Ana"}, ana, true},
		{"involved: assignee matches", config.BlockedConfig{Scope: "involved"}, SpecView{Status: "blocked", Assignees: []string{"@ana"}}, ana, true},
		{"involved: uninvolved hidden", config.BlockedConfig{Scope: "involved"}, SpecView{Status: "blocked", Author: "Ben"}, ana, false},
		{"owning_role: matching role", config.BlockedConfig{Scope: "owning_role"}, SpecView{Status: "blocked", BlockedFrom: "build"}, ana, true},
		{"owning_role: non-matching role", config.BlockedConfig{Scope: "owning_role"}, SpecView{Status: "blocked", BlockedFrom: "build"}, pm, false},
		{"owning_role: tl owns draft? no", config.BlockedConfig{Scope: "owning_role"}, SpecView{Status: "blocked", BlockedFrom: "draft"}, tl, false},
		{"owning_role: unknown pre-block stays visible", config.BlockedConfig{Scope: "owning_role"}, SpecView{Status: "blocked", BlockedFrom: ""}, ana, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleInBlocked(pl, tt.cfg, tt.spec, tt.v); got != tt.want {
				t.Errorf("VisibleInBlocked = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAssigneeLabel(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"@ana"}, "@ana"},
		{[]string{"@ana", "@ben"}, "@ana +1"},
		{[]string{"@ana", "@ben", "@cleo"}, "@ana +2"},
	}
	for _, c := range cases {
		if got := assigneeLabel(c.in); got != c.want {
			t.Errorf("assigneeLabel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDoAssigneeLabel(t *testing.T) {
	pl := scopePipeline() // engineering=assignee, build=role
	cases := []struct {
		name string
		spec specInfo
		want string
	}{
		{"assignee-scoped unclaimed", specInfo{Status: "engineering"}, "unclaimed"},
		{"assignee-scoped claimed", specInfo{Status: "engineering", Assignees: []string{"@ana"}}, "@ana"},
		{"role-scoped unassigned", specInfo{Status: "build"}, ""},
		{"role-scoped but assigned", specInfo{Status: "build", Assignees: []string{"@ana"}}, "@ana"},
	}
	for _, c := range cases {
		if got := doAssigneeLabel(pl, c.spec); got != c.want {
			t.Errorf("%s: doAssigneeLabel = %q, want %q", c.name, got, c.want)
		}
	}
}
