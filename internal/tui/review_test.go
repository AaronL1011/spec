package tui

import (
	"strings"
	"testing"
	"time"
)

func testReviewModel() reviewModel {
	rc := testResolvedConfig()
	reg := testRegistry()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newReview(rc, reg, styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestReview_EmptyQueue(t *testing.T) {
	m := testReviewModel()
	m.items = nil

	got := m.view()
	if !strings.Contains(got, "No pending") {
		t.Errorf("empty reviews should show 'No pending', got: %q", got)
	}
}

func TestReview_WithItems(t *testing.T) {
	m := testReviewModel()
	m.items = []reviewItem{
		{Number: 42, Title: "Add auth middleware", Repo: "api-service", Author: "octocat", CIStatus: "passing", CreatedAt: time.Now().Add(-2 * time.Hour)},
		{Number: 43, Title: "Fix payment flow", Repo: "payments", Author: "hubot", CIStatus: "failing", CreatedAt: time.Now().Add(-24 * time.Hour)},
	}

	got := m.view()
	if !strings.Contains(got, "PR #42") {
		t.Error("should contain PR #42")
	}
	if !strings.Contains(got, "2 reviews") {
		t.Error("should show '2 reviews requested'")
	}
	if !strings.Contains(got, "octocat") {
		t.Error("wide review row should render the PR author")
	}
}

func TestReview_CursorNavigation(t *testing.T) {
	m := testReviewModel()
	m.items = []reviewItem{
		{Number: 1, URL: "https://github.com/repo/pull/1"},
		{Number: 2, URL: "https://github.com/repo/pull/2"},
	}

	m, _ = m.update(keyMsg("j"))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	if got := m.selectedURL(); got != "https://github.com/repo/pull/2" {
		t.Errorf("selectedURL = %q, want pull/2", got)
	}
}

func TestReview_StaleFractionOptIn(t *testing.T) {
	m := testReviewModel()
	now := time.Now()
	old := reviewItem{Number: 1, CreatedAt: now.Add(-48 * time.Hour)}

	// No review window configured → colouring is opt-in, fraction stays 0.
	if f := m.reviewStaleFraction(old, now); f != 0 {
		t.Errorf("with no window configured, staleFraction = %v, want 0", f)
	}

	// Configure a 2d window → a PR opened 48h ago is fully stale (f == 1),
	// reusing the dashboard's REVIEW window + easing.
	m.rc.Team.Dashboard.Review.StaleAfter = "2d"
	if f := m.reviewStaleFraction(old, now); f <= 0 {
		t.Errorf("with a 2d window, 48h-old PR staleFraction = %v, want > 0", f)
	}

	// A PR opened right now is not stale against the same window.
	fresh := reviewItem{Number: 2, CreatedAt: now}
	if f := m.reviewStaleFraction(fresh, now); f != 0 {
		t.Errorf("fresh PR staleFraction = %v, want 0", f)
	}
}

func TestReview_StaleRowRenders(t *testing.T) {
	m := testReviewModel()
	m.rc.Team.Dashboard.Review.StaleAfter = "2d"
	m.items = []reviewItem{
		{Number: 7, Title: "Old PR", Repo: "api", Author: "octocat", CreatedAt: time.Now().Add(-72 * time.Hour)},
	}

	// A stale row exercises the ramp-colour path; it must still render its
	// content without panicking.
	got := m.view()
	if !strings.Contains(got, "PR #7") || !strings.Contains(got, "octocat") {
		t.Errorf("stale review row should still render its content, got: %q", got)
	}
}

func TestCIIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"passing", IconDone},
		{"failing", IconRejected},
		{"pending", IconChanges},
		{"", IconPending},
	}
	for _, tt := range tests {
		if got := ciIcon(tt.status); got != tt.want {
			t.Errorf("ciIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
