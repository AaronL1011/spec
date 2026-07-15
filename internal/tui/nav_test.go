package tui

import "testing"

func TestView_NextWraps(t *testing.T) {
	last := View(ViewCount - 1)
	if got := last.Next(); got != ViewDashboard {
		t.Errorf("last.Next() = %d, want %d (ViewDashboard)", got, ViewDashboard)
	}
}

func TestView_PrevWraps(t *testing.T) {
	if got := ViewDashboard.Prev(); got != View(ViewCount-1) {
		t.Errorf("ViewDashboard.Prev() = %d, want %d", got, ViewCount-1)
	}
}

func TestView_Labels(t *testing.T) {
	tests := []struct {
		view View
		want string
	}{
		{ViewDashboard, "Dashboard"},
		{ViewPipeline, "Pipeline"},
		{ViewSpecs, "Specs"},
		{ViewTriage, "Triage"},
		{ViewReviews, "Reviews"},
		{ViewSecurity, "Security"},
		{ViewSettings, "Settings"},
	}
	for _, tt := range tests {
		if got := tt.view.Label(); got != tt.want {
			t.Errorf("View(%d).Label() = %q, want %q", tt.view, got, tt.want)
		}
	}
}

func TestView_Shortcuts(t *testing.T) {
	for i := range ViewCount {
		v := View(i)
		if v.Shortcut() == "" {
			t.Errorf("View(%d).Shortcut() is empty", i)
		}
	}
}
