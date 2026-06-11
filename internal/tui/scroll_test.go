package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/markdown"
)

// TestDashboard_ScrollsWithCursor verifies the dashboard uses a scroll
// window so items remain visible on a small terminal.
func TestDashboard_ScrollsWithCursor(t *testing.T) {
	m := testDashboard()
	m.loading = false
	m.width = 60
	m.height = 5 // very small — only room for ~5 lines

	// 10 items — more than can fit.
	var items []dashboard.DashboardItem
	for i := range 10 {
		items = append(items, dashboard.DashboardItem{
			SpecID: specID(i), Title: title(i), Stage: "build",
		})
	}
	m.data = &dashboard.DashboardData{Do: items}
	m.items = m.buildRows()

	// Cursor at top — first item should be visible.
	got := m.view()
	if !strings.Contains(got, "SPEC-000") {
		t.Error("with cursor at top, first item should be visible")
	}

	// Move cursor to bottom.
	for range 9 {
		m, _ = m.update(keyMsg("j"))
	}
	got = m.view()
	if !strings.Contains(got, "SPEC-009") {
		t.Errorf("with cursor at bottom, last item should be visible, got:\n%s", got)
	}

	// First item should NOT be visible (scrolled away).
	if strings.Contains(got, "SPEC-000") {
		t.Error("first item should be scrolled off screen")
	}
}

// TestPipeline_ScrollsWithCursor verifies the pipeline view scrolls
// through stages on a small terminal.
func TestPipeline_ScrollsWithCursor(t *testing.T) {
	m := testPipelineModel()
	m.width = 60
	m.height = 6 // room for ~6 lines

	// Create 5 stages with 2 specs each — total ~15 rendered lines.
	var stages []pipelineStage
	for s := range 5 {
		var specs []pipelineSpec
		for i := range 2 {
			specs = append(specs, pipelineSpec{
				ID:    specID(s*10 + i),
				Title: title(s*10 + i),
			})
		}
		stages = append(stages, pipelineStage{
			Name:  stageName(s),
			Specs: specs,
		})
	}
	m.stages = stages
	m.stageIdx = 0
	m.specIdx = 0

	// First stage should be visible.
	got := m.view()
	if !strings.Contains(got, "stage_0") {
		t.Error("first stage should be visible with cursor at start")
	}

	// Navigate to last stage.
	for range 4 {
		m, _ = m.update(keyMsg("l"))
	}
	got = m.view()
	if !strings.Contains(got, "stage_4") {
		t.Errorf("last stage should be visible after navigating, got:\n%s", got)
	}
}

// TestSpecDetail_ScrollClamp verifies scroll doesn't go past content.
func TestSpecDetail_ScrollClamp(t *testing.T) {
	m := testSpecDetailModel()
	m.height = 5
	m.meta = &markdown.SpecMeta{
		ID: "SPEC-001", Title: "Test", Status: "build",
		Author: "alice", Updated: "2026-05-20",
	}
	m.sections = []markdown.Section{
		{Slug: "problem", Level: 2, Content: "text"},
		{Slug: "solution", Level: 2, Content: "text"},
	}
	m.contentLines = m.estimateContentLines()

	// Scroll down many times.
	for range 100 {
		m, _ = m.update(keyMsg("j"))
	}

	// Scroll should be clamped to maxScroll.
	mx := m.maxScroll()
	if m.scroll > mx {
		t.Errorf("scroll = %d, should not exceed maxScroll = %d", m.scroll, mx)
	}

	// Should still render without panic.
	got := m.view()
	if got == "" {
		t.Error("view should not be empty after scrolling")
	}
}

// TestSpecDetail_LastLineReachable verifies that scrolling to the bottom of a
// tall overview reveals its final line (the hint strip) rather than clipping it
// behind the status bar. This guards the accurate-line-count contract between
// overviewLines and estimateContentLines.
func TestSpecDetail_LastLineReachable(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 80
	m.height = 6 // small viewport forces scrolling
	m.meta = &markdown.SpecMeta{
		ID: "SPEC-001", Title: "Test", Status: "build",
		Author: "alice", Updated: "2026-05-20",
	}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Level: 2, Content: "text"},
		{Slug: "proposed_solution", Level: 2, Content: "text"},
		{Slug: "acceptance_criteria", Level: 2, Content: "text"},
		{Slug: "technical_implementation", Level: 2, Content: "text"},
	}
	m.contentLines = m.estimateContentLines()

	lines := m.overviewLines()
	lastLine := lines[len(lines)-1]

	// Scroll to the bottom.
	for range 100 {
		m, _ = m.update(keyMsg("j"))
	}

	if !strings.Contains(m.view(), lastLine) {
		t.Errorf("last overview line %q not visible at max scroll; view:\n%s", lastLine, m.view())
	}
}

// TestSpecDetail_ScrollOnResize verifies scroll clamps when terminal shrinks.
func TestSpecDetail_ScrollOnResize(t *testing.T) {
	m := testSpecDetailModel()
	m.height = 30
	m.meta = &markdown.SpecMeta{
		ID: "SPEC-001", Title: "Test", Status: "build",
		Author: "alice", Updated: "2026-05-20",
	}
	m.contentLines = 15
	m.scroll = 10 // scrolled down

	// Shrink terminal — scroll should clamp.
	m.setSize(60, 10)
	mx := m.maxScroll()
	if m.scroll > mx {
		t.Errorf("after resize: scroll = %d, maxScroll = %d", m.scroll, mx)
	}
}

func TestScrollWindowAround(t *testing.T) {
	tests := []struct {
		focus, total, visible int
		wantStart, wantEnd    int
	}{
		{0, 5, 10, 0, 5},    // all fits
		{0, 20, 5, 0, 5},    // at top
		{19, 20, 5, 15, 20}, // at bottom
		{10, 20, 5, 8, 13},  // middle
		{3, 20, 5, 1, 6},    // near top
	}
	for _, tt := range tests {
		s, e := scrollWindowAround(tt.focus, tt.total, tt.visible)
		if s != tt.wantStart || e != tt.wantEnd {
			t.Errorf("scrollWindowAround(%d,%d,%d) = (%d,%d), want (%d,%d)",
				tt.focus, tt.total, tt.visible, s, e, tt.wantStart, tt.wantEnd)
		}
	}
}

// TestSpecDetail_FirstScrollMovesViewport verifies that the very first
// j press shifts the visible content by one line.
func TestSpecDetail_FirstScrollMovesViewport(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 80
	m.height = 5 // small viewport
	m.meta = &markdown.SpecMeta{
		ID: "SPEC-001", Title: "Test", Status: "build",
		Author: "alice", Updated: "2026-05-20",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "done"},
			{Description: "Step 2", Status: "done"},
			{Description: "Step 3", Status: "done"},
		},
	}
	m.sections = []markdown.Section{
		{Slug: "problem", Level: 2, Content: "long text"},
		{Slug: "solution", Level: 2, Content: "long text"},
	}
	m.contentLines = m.estimateContentLines()

	before := m.view()

	// Single press of j should change the output.
	m, _ = m.update(keyMsg("j"))
	if m.scroll != 1 {
		t.Fatalf("scroll = %d after first j, want 1", m.scroll)
	}

	after := m.view()
	if before == after {
		t.Error("view should change after first j press")
	}
}

// helpers for generating test data
func specID(i int) string    { return fmt.Sprintf("SPEC-%03d", i) }
func title(i int) string     { return fmt.Sprintf("Spec title %d", i) }
func stageName(i int) string { return fmt.Sprintf("stage_%d", i) }
