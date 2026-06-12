package components

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// fullyLoadedBar returns a status bar with every optional slot populated, so
// width changes exercise the measure-and-fit allocator end to end.
func fullyLoadedBar(width int) StatusBar {
	s := testStatusBar()
	s.SetView("Reviews › SPEC-1234")
	s.SetScroll("3/12")
	s.SetActiveRefreshKey("dashboard")
	s.SetRefresh("dashboard", time.Now().Add(-90*time.Second)) // stale
	s.SetUpdateAvailable("v0.4.0")
	s.SetWidth(width)
	return s
}

// TestStatusBar_NeverWraps asserts the bar stays a single row at every width —
// the overflow-then-clip behaviour this refactor replaces. The width budget is
// only enforced once the window can hold the mandatory status anchor; below
// that nothing can fit and the anchor is allowed to spill.
func TestStatusBar_NeverWraps(t *testing.T) {
	for w := 0; w <= 160; w++ {
		out := fullyLoadedBar(w).View()
		if strings.Contains(out, "\n") {
			t.Fatalf("width %d: status bar wrapped to multiple rows", w)
		}
		if w >= 20 && lipgloss.Width(out) > w {
			t.Fatalf("width %d: rendered width %d exceeds budget", w, lipgloss.Width(out))
		}
	}
}

// TestStatusBar_WideShowsEveryDetail asserts the rich view is undiminished:
// at a generous width every slot renders at full verbosity.
func TestStatusBar_WideShowsEveryDetail(t *testing.T) {
	out := fullyLoadedBar(160).View()
	for _, want := range []string{
		"SPEC-1234",    // full, untruncated view label
		"3/12",         // scroll position
		"r to refresh", // full freshness detail
		"spec update",  // full update call-to-action
		"? help",       // help hint
		":",            // clock
	} {
		if !strings.Contains(out, want) {
			t.Errorf("wide status bar = %q, missing %q", out, want)
		}
	}
}

// TestStatusBar_ClockShedsBeforeEverything asserts the lowest-priority slot is
// shown only when every higher-priority slot is also shown. The clock is
// allocated last and has the widest single form, so its presence is a proxy
// for "the whole bar fits" — a clean directional invariant of priority fit.
func TestStatusBar_ClockShedsBeforeEverything(t *testing.T) {
	for w := 0; w <= 160; w++ {
		out := fullyLoadedBar(w).View()
		if !strings.Contains(out, ":") {
			continue // clock not shown — nothing to assert
		}
		for _, must := range []struct{ name, substr string }{
			{"view label", "Rev"},
			{"scroll", "3/12"},
			{"freshness", glyph.Clock},
			{"update", glyph.Upgrade},
			{"help hint", "?"},
		} {
			if !strings.Contains(out, must.substr) {
				t.Errorf("width %d: clock shown without %s (%q)", w, must.name, must.substr)
			}
		}
	}
}

// TestStatusBar_StatusAnchored asserts the canonical status element survives at
// a width too narrow for any other slot.
func TestStatusBar_StatusAnchored(t *testing.T) {
	s := testStatusBar()
	s.SetView("Reviews › SPEC-1234")
	s.SetWidth(22)
	if got := s.View(); !strings.Contains(got, "No pending work") {
		t.Errorf("narrow bar = %q, want the status element retained", got)
	}
}

// TestStatusBar_LabelTruncates asserts the view label is shortened with an
// ellipsis rather than dropped when it cannot fully fit.
func TestStatusBar_LabelTruncates(t *testing.T) {
	s := testStatusBar()
	s.SetView("Reviews › SPEC-1234")
	s.SetWidth(30)
	got := s.View()
	if !strings.Contains(got, "…") {
		t.Errorf("status bar = %q, want a truncated label with an ellipsis", got)
	}
	if strings.Contains(got, "SPEC-1234") {
		t.Errorf("status bar = %q, label should be truncated at width 30", got)
	}
}

// TestStatusBar_ExitHintRetainedWhenArmed asserts the double-esc safety prompt
// is kept even when width is scarce, displacing lower-priority detail.
func TestStatusBar_ExitHintRetainedWhenArmed(t *testing.T) {
	s := fullyLoadedBar(46)
	s.SetExitArmed(true)
	if got := s.View(); !strings.Contains(got, "quit") {
		t.Errorf("armed narrow bar = %q, want the exit-safety hint retained", got)
	}
}
