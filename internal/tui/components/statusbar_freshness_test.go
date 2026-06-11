package components

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func testStatusBar() StatusBar {
	return NewStatusBar(StatusBarStyles{
		Bar:    lipgloss.NewStyle(),
		Label:  lipgloss.NewStyle(),
		Hint:   lipgloss.NewStyle(),
		Clock:  lipgloss.NewStyle(),
		Stale:  lipgloss.NewStyle(),
		Status: testStatusStyles(),
	})
}

// TestFreshness_HiddenUntilFirstLoad asserts a tab that has never loaded shows
// no data-age indicator (§5.2: the indicator is metadata about loaded data).
func TestFreshness_HiddenUntilFirstLoad(t *testing.T) {
	s := testStatusBar()
	s.SetActiveRefreshKey("dashboard")
	if got := s.freshnessIndicator(); got != "" {
		t.Errorf("freshness before first load = %q, want empty", got)
	}
}

// TestFreshness_FreshThenStale walks the three states for the active tab.
func TestFreshness_FreshThenStale(t *testing.T) {
	s := testStatusBar()
	s.SetStaleAfter(60 * time.Second)
	s.SetActiveRefreshKey("dashboard")

	// Just loaded → fresh.
	s.SetRefresh("dashboard", time.Now())
	if got := s.freshnessIndicator(); !strings.Contains(got, "fresh") {
		t.Errorf("just-loaded indicator = %q, want fresh", got)
	}

	// 30s old → "Ns ago".
	s.SetRefresh("dashboard", time.Now().Add(-30*time.Second))
	if got := s.freshnessIndicator(); !strings.Contains(got, "ago") {
		t.Errorf("30s-old indicator = %q, want 'ago'", got)
	}

	// Past the stale threshold → "stale · r to refresh".
	s.SetRefresh("dashboard", time.Now().Add(-90*time.Second))
	got := s.freshnessIndicator()
	if !strings.Contains(got, "stale") || !strings.Contains(got, "r to refresh") {
		t.Errorf("stale indicator = %q, want 'stale · r to refresh'", got)
	}
}

// TestFreshness_PerTabIsolation asserts each tab tracks its own age: refreshing
// one tab does not make another look fresh.
func TestFreshness_PerTabIsolation(t *testing.T) {
	s := testStatusBar()
	s.SetStaleAfter(60 * time.Second)

	s.SetRefresh("dashboard", time.Now())
	s.SetRefresh("reviews", time.Now().Add(-90*time.Second))

	s.SetActiveRefreshKey("dashboard")
	if got := s.freshnessIndicator(); !strings.Contains(got, "fresh") {
		t.Errorf("dashboard indicator = %q, want fresh", got)
	}

	s.SetActiveRefreshKey("reviews")
	if got := s.freshnessIndicator(); !strings.Contains(got, "stale") {
		t.Errorf("reviews indicator = %q, want stale (independent clock)", got)
	}
}

// TestFreshness_OfflineAffordance asserts offline overrides the age label.
func TestFreshness_OfflineAffordance(t *testing.T) {
	s := testStatusBar()
	s.SetActiveRefreshKey("dashboard")
	s.SetRefresh("dashboard", time.Now())
	s.SetOffline(true)

	got := s.freshnessIndicator()
	if !strings.Contains(got, "cached") || !strings.Contains(got, "offline") {
		t.Errorf("offline indicator = %q, want 'cached · offline'", got)
	}
}

// TestFreshness_EmptyKeyNoReset asserts SetRefresh with an empty key never
// records freshness (so keyless callers cannot accidentally mark a tab fresh).
func TestFreshness_EmptyKeyNoReset(t *testing.T) {
	s := testStatusBar()
	s.SetActiveRefreshKey("dashboard")
	s.SetRefresh("", time.Now())
	if got := s.freshnessIndicator(); got != "" {
		t.Errorf("empty-key SetRefresh produced %q, want empty", got)
	}
}
