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
		{Number: 42, Title: "Add auth middleware", Repo: "api-service", CIStatus: "passing", CreatedAt: time.Now().Add(-2 * time.Hour)},
		{Number: 43, Title: "Fix payment flow", Repo: "payments", CIStatus: "failing", CreatedAt: time.Now().Add(-24 * time.Hour)},
	}

	got := m.view()
	if !strings.Contains(got, "PR #42") {
		t.Error("should contain PR #42")
	}
	if !strings.Contains(got, "2 reviews") {
		t.Error("should show '2 reviews requested'")
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

func TestCIIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"passing", "✅"},
		{"failing", "❌"},
		{"pending", "🔄"},
		{"", "⬜"},
	}
	for _, tt := range tests {
		if got := ciIcon(tt.status); got != tt.want {
			t.Errorf("ciIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
