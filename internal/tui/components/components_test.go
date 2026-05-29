package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func testHeaderStyles() HeaderStyles {
	return HeaderStyles{
		Bar:      lipgloss.NewStyle(),
		Greeting: lipgloss.NewStyle().Bold(true),
		Meta:     lipgloss.NewStyle(),
	}
}

func TestHeader_WideLayout(t *testing.T) {
	h := NewHeader("Alice", "engineer", "Sprint 12", testHeaderStyles())
	h.SetWidth(120)

	got := h.View()
	if strings.Count(got, "\n") > 0 {
		t.Error("wide header should be single line")
	}
	if h.Height() != 1 {
		t.Errorf("wide header Height = %d, want 1", h.Height())
	}
}

func TestHeader_NarrowLayout_Stacks(t *testing.T) {
	h := NewHeader("Alice", "engineer", "Sprint 12", testHeaderStyles())
	h.SetWidth(30) // too narrow for greeting + meta on one line

	if h.Height() != 2 {
		t.Errorf("narrow header Height = %d, want 2", h.Height())
	}

	got := h.View()
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Errorf("narrow header should have 2 lines, got %d", len(lines))
	}
}

// paddedHeaderStyles mirrors the real app, whose Bar style has horizontal
// padding. A padded bar exposes the wrap-overflow bug that an unpadded style
// hides, so the invariant test below uses it.
func paddedHeaderStyles() HeaderStyles {
	return HeaderStyles{
		Bar:      lipgloss.NewStyle().Padding(0, 1),
		Greeting: lipgloss.NewStyle().Bold(true),
		Meta:     lipgloss.NewStyle(),
	}
}

// TestHeader_ViewMatchesHeight guards the layout invariant: the app reserves
// exactly Height() rows for the header, so View() must render exactly that
// many lines. A mismatch overflows the terminal and corrupts the layout
// (a stale full-width bar bleeding to the top of the screen on re-render).
func TestHeader_ViewMatchesHeight(t *testing.T) {
	for _, w := range []int{200, 120, 80, 40, 30, 24, 12, 4} {
		h := NewHeader("Ada Lovelace", "engineer", "Sprint 12", paddedHeaderStyles())
		h.SetWidth(w)
		got := h.View()
		viewLines := strings.Count(got, "\n") + 1
		if viewLines != h.Height() {
			t.Errorf("w=%d: View() rendered %d lines, Height()=%d", w, viewLines, h.Height())
		}
		for _, line := range strings.Split(got, "\n") {
			if lw := lipgloss.Width(line); w >= 4 && lw > w {
				t.Errorf("w=%d: rendered line width %d exceeds header width %d", w, lw, w)
			}
		}
	}
}

func TestHeader_NoMeta(t *testing.T) {
	h := NewHeader("Alice", "", "", testHeaderStyles())
	h.SetWidth(80)

	if h.Height() != 1 {
		t.Errorf("no-meta header Height = %d, want 1", h.Height())
	}
}

func testTabStyles() TabStripStyles {
	return TabStripStyles{
		Active:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff0000")),
		Inactive:  lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		Bar:       lipgloss.NewStyle(),
		Separator: lipgloss.NewStyle(),
	}
}

func TestTabStrip_WideShowsLabels(t *testing.T) {
	tabs := []Tab{
		{Label: "Dashboard", Shortcut: "1"},
		{Label: "Pipeline", Shortcut: "2"},
	}
	ts := NewTabStrip(tabs, testTabStyles())
	ts.SetWidth(100)

	got := ts.View()
	if !strings.Contains(got, "Dashboard") {
		t.Error("wide tab strip should show full label 'Dashboard'")
	}
}

func TestTabStrip_CompactShowsShortcutsOnly(t *testing.T) {
	tabs := []Tab{
		{Label: "Dashboard", Shortcut: "1"},
		{Label: "Pipeline", Shortcut: "2"},
		{Label: "Specs", Shortcut: "3"},
		{Label: "Triage", Shortcut: "4"},
		{Label: "Reviews", Shortcut: "5"},
		{Label: "Settings", Shortcut: "6"},
	}
	ts := NewTabStrip(tabs, testTabStyles())
	ts.SetWidth(50) // below compactThreshold

	got := ts.View()
	if strings.Contains(got, "Dashboard") {
		t.Error("compact tab strip should not show full label 'Dashboard'")
	}
	if !strings.Contains(got, "1") {
		t.Error("compact tab strip should contain shortcut '1'")
	}
}

func TestTabStrip_SetActive(t *testing.T) {
	tabs := []Tab{
		{Label: "Dashboard", Shortcut: "1"},
		{Label: "Pipeline", Shortcut: "2"},
	}
	ts := NewTabStrip(tabs, testTabStyles())
	ts.SetWidth(100)

	// Both should render without panic.
	ts.SetActive(0)
	got := ts.View()
	if !strings.Contains(got, "Dashboard") {
		t.Error("should contain Dashboard")
	}

	ts.SetActive(1)
	got = ts.View()
	if !strings.Contains(got, "Pipeline") {
		t.Error("should contain Pipeline")
	}

	// Out of bounds should not panic.
	ts.SetActive(-1)
	ts.SetActive(99)
}

func TestStatusBar_PendingShown(t *testing.T) {
	styles := StatusBarStyles{
		Bar:     lipgloss.NewStyle(),
		Label:   lipgloss.NewStyle(),
		Pending: lipgloss.NewStyle(),
		Hint:    lipgloss.NewStyle(),
		Clock:   lipgloss.NewStyle(),
		Stale:   lipgloss.NewStyle(),
	}
	sb := NewStatusBar(styles)
	sb.SetView("Dashboard")
	sb.SetWidth(80)

	sb.SetPending(0)
	got := sb.View()
	if strings.Contains(got, "pending") {
		t.Error("status bar should not show 'pending' when count is 0")
	}

	sb.SetPending(5)
	got = sb.View()
	if !strings.Contains(got, "5 pending") {
		t.Errorf("status bar should show '5 pending', got: %q", got)
	}
}

// TestStatusBar_AlwaysSingleLine guards the invariant that the status bar
// occupies exactly one row. With a padded Bar style, filling to the full
// width previously overflowed the content box and wrapped onto a second
// line, pushing the whole layout past the terminal height.
func TestStatusBar_AlwaysSingleLine(t *testing.T) {
	styles := StatusBarStyles{
		Bar:     lipgloss.NewStyle().Padding(0, 1),
		Label:   lipgloss.NewStyle(),
		Pending: lipgloss.NewStyle(),
		Hint:    lipgloss.NewStyle(),
		Clock:   lipgloss.NewStyle(),
		Stale:   lipgloss.NewStyle(),
	}
	for _, w := range []int{200, 120, 80, 60, 40, 20} {
		sb := NewStatusBar(styles)
		sb.SetView("Dashboard")
		sb.SetPending(3)
		sb.SetScroll("12/40")
		sb.SetWidth(w)
		got := sb.View()
		if lines := strings.Count(got, "\n") + 1; lines != 1 {
			t.Errorf("w=%d: status bar rendered %d lines, want 1", w, lines)
		}
	}
}

func TestStatusBar_BusySpinnerShown(t *testing.T) {
	styles := StatusBarStyles{
		Bar:     lipgloss.NewStyle(),
		Label:   lipgloss.NewStyle(),
		Pending: lipgloss.NewStyle(),
		Hint:    lipgloss.NewStyle(),
		Clock:   lipgloss.NewStyle(),
		Stale:   lipgloss.NewStyle(),
	}
	sb := NewStatusBar(styles)
	sb.SetView("Specs")
	sb.SetWidth(100)
	sb.SetBusy(true, "rendering § problem_statement")

	got := sb.View()
	if !strings.Contains(got, "rendering § problem_statement") {
		t.Fatalf("busy label should be visible, got: %q", got)
	}

	sb.NextSpinner()
	got2 := sb.View()
	if got == got2 {
		t.Fatal("spinner frame advance should change rendered status bar")
	}

	sb.SetBusy(false, "")
	got3 := sb.View()
	if strings.Contains(got3, "rendering §") {
		t.Fatalf("busy label should clear when not busy, got: %q", got3)
	}
}
