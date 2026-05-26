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
