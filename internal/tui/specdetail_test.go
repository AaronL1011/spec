package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
)

func testSpecDetailModel() specDetailModel {
	rc := testResolvedConfig()
	theme := ResolveTheme("catppuccin-mocha")
	styles := NewStyles(theme)
	keys := DefaultKeyMap()
	m := newSpecDetail(rc, "SPEC-001", styles, keys, theme)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestSpecDetail_LoadingView(t *testing.T) {
	rc := testResolvedConfig()
	theme := ResolveTheme("catppuccin-mocha")
	styles := NewStyles(theme)
	keys := DefaultKeyMap()
	m := newSpecDetail(rc, "SPEC-001", styles, keys, theme)

	got := m.view()
	if !strings.Contains(got, "Loading") {
		t.Errorf("loading view should contain 'Loading', got: %q", got)
	}
}

func TestSpecDetail_NotFound(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = nil

	got := m.view()
	if !strings.Contains(got, "not found") {
		t.Errorf("nil meta should show 'not found', got: %q", got)
	}
}

func TestSpecDetail_RendersMetadata(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{
		ID:      "SPEC-042",
		Title:   "Auth Service Rebuild",
		Status:  "build",
		Author:  "alice",
		Version: "0.2.0",
		Cycle:   "Sprint 14",
		Repos:   []string{"auth-service", "api-gateway"},
		Updated: "2026-05-20",
	}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Level: 2, Owner: "pm", Content: "Engineers are struggling with..."},
		{Slug: "proposed_solution", Level: 2, Owner: "pm", Content: "We will rebuild..."},
		{Slug: "acceptance_criteria", Level: 2, Owner: "qa", Content: ""},
	}
	m.decisions = []markdown.DecisionEntry{
		{Number: 1, Question: "Use OAuth2 or SAML?", Decision: "OAuth2", Rationale: "Simpler for SPA"},
		{Number: 2, Question: "Session storage?", Decision: ""},
	}

	got := m.view()

	// Title
	if !strings.Contains(got, "SPEC-042") {
		t.Error("should contain spec ID")
	}
	if !strings.Contains(got, "Auth Service Rebuild") {
		t.Error("should contain spec title")
	}

	// Metadata
	if !strings.Contains(got, "build") {
		t.Error("should contain status")
	}
	if !strings.Contains(got, "alice") {
		t.Error("should contain author")
	}

	// Sections
	if !strings.Contains(got, "problem_statement") {
		t.Error("should list problem_statement section")
	}

	// Decisions
	if !strings.Contains(got, "OAuth2") {
		t.Error("should show resolved decision")
	}
}

func TestSpecDetail_SpecBlocked(t *testing.T) {
	escapeSection := markdown.Section{
		Slug:    "escape_hatch_log",
		Heading: "## 8. Escape Hatch Log",
		Level:   2,
		Owner:   "auto",
		Content: "\n- **2026-05-29** (aaron): Blocked from `engineering`. Reason: waiting on legal review\n",
	}

	tests := []struct {
		name        string
		status      string
		sections    []markdown.Section
		wantBlocked bool
		wantReason  string
	}{
		{
			name:        "blocked status shows header and reason",
			status:      "blocked",
			sections:    []markdown.Section{escapeSection},
			wantBlocked: true,
			wantReason:  "waiting on legal review",
		},
		{
			name:        "blocked status with no escape log shows header only",
			status:      "blocked",
			sections:    nil,
			wantBlocked: true,
			wantReason:  "",
		},
		{
			name:        "non-blocked status shows no blocked block",
			status:      "draft",
			sections:    []markdown.Section{escapeSection},
			wantBlocked: false,
			wantReason:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testSpecDetailModel()
			m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test", Status: tt.status}
			m.sections = tt.sections
			got := m.view()
			if tt.wantBlocked && !strings.Contains(got, "Blocked") {
				t.Error("expected 'Blocked' header in view")
			}
			if !tt.wantBlocked && strings.Contains(got, "Blocked") {
				t.Error("unexpected 'Blocked' header in view")
			}
			if tt.wantReason != "" && !strings.Contains(got, tt.wantReason) {
				t.Errorf("expected reason %q in view, got:\n%s", tt.wantReason, got)
			}
		})
	}
}

func TestSpecDetail_ScrollDoesNotPanic(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.height = 5 // very small

	// Scroll down many times — should not panic.
	for range 50 {
		m, _ = m.update(keyMsg("j"))
	}
	// Scroll back up.
	for range 50 {
		m, _ = m.update(keyMsg("k"))
	}

	// Should still render.
	got := m.view()
	if got == "" {
		t.Error("view should not be empty after scrolling")
	}
}

func TestSpecDetail_ReaderModeToggle(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test", Status: "build", Author: "alice", Updated: "2026-05-20"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Heading: "## 1. Problem Statement", Level: 2, Owner: "pm", Content: "Engineers are drowning in tools."},
		{Slug: "proposed_solution", Heading: "## 4. Proposed Solution", Level: 2, Owner: "pm", Content: "Build a control plane."},
	}
	m.contentLines = m.estimateContentLines()

	// Should start in overview mode.
	if m.readerMode {
		t.Error("should start in overview mode")
	}

	// Overview should show section outline.
	got := m.view()
	if !strings.Contains(got, "problem_statement") {
		t.Error("overview should list section slugs")
	}
	if !strings.Contains(got, "read sections") {
		t.Error("overview should show 'read sections' hint")
	}

	// Press 'o' to enter reader mode.
	m, cmd := m.update(keyMsg("o"))
	if !m.readerMode {
		t.Fatal("should be in reader mode after 'o'")
	}
	if m.sectionIdx != 0 {
		t.Errorf("sectionIdx = %d, want 0", m.sectionIdx)
	}
	if cmd == nil {
		t.Error("entering reader mode should return a non-nil render command")
	}
	if cmd == nil {
		t.Error("should return a render cmd")
	}

	m, _ = m.update(cmd())

	// Reader view should show section content.
	got = m.view()
	if !strings.Contains(got, "Problem Statement") {
		t.Errorf("reader should show section heading, got: %s", got)
	}
}

func TestSpecDetail_ReaderNavigation(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 80
	m.height = 30
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test", Status: "build", Author: "alice", Updated: "2026-05-20"}
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## 1. Problem", Level: 2, Content: "Problem text."},
		{Slug: "goals", Heading: "## 2. Goals", Level: 2, Content: "Goals text."},
		{Slug: "solution", Heading: "## 3. Solution", Level: 2, Content: "Solution text."},
	}

	// Enter reader mode.
	m, _ = m.update(keyMsg("o"))
	if !m.readerMode {
		t.Fatal("should be in reader mode")
	}

	// Navigate to next section.
	m, _ = m.update(keyMsg("n"))
	if m.sectionIdx != 1 {
		t.Errorf("after 'n': sectionIdx = %d, want 1", m.sectionIdx)
	}

	// Navigate to previous.
	m, _ = m.update(keyMsg("p"))
	if m.sectionIdx != 0 {
		t.Errorf("after 'p': sectionIdx = %d, want 0", m.sectionIdx)
	}

	// Jump to section 3.
	m, _ = m.update(keyMsg("3"))
	if m.sectionIdx != 2 {
		t.Errorf("after '3': sectionIdx = %d, want 2", m.sectionIdx)
	}

	// Tab back to overview.
	m, _ = m.update(keyMsg("o"))
	if m.readerMode {
		t.Error("Tab should return to overview")
	}
}

func TestSpecDetail_ReaderBounds(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "only", Heading: "## Only", Level: 2, Content: "Content."},
	}

	// Enter reader.
	m, _ = m.update(keyMsg("o"))

	// 'n' at last section — should stay.
	m, _ = m.update(keyMsg("n"))
	if m.sectionIdx != 0 {
		t.Errorf("sectionIdx = %d, want 0 (only one section)", m.sectionIdx)
	}

	// 'p' at first section — should stay.
	m, _ = m.update(keyMsg("p"))
	if m.sectionIdx != 0 {
		t.Errorf("sectionIdx = %d, want 0", m.sectionIdx)
	}

	// Jump beyond range — should be no-op.
	m, _ = m.update(keyMsg("9"))
	if m.sectionIdx != 0 {
		t.Errorf("sectionIdx = %d, should stay at 0", m.sectionIdx)
	}
}

func TestSpecDetail_ReadableSections(t *testing.T) {
	m := testSpecDetailModel()
	m.sections = []markdown.Section{
		{Slug: "a", Level: 2},
		{Slug: "b", Level: 3},
		{Slug: "c", Level: 4}, // excluded
		{Slug: "d", Level: 2},
	}

	got := m.readableSections()
	if len(got) != 2 {
		t.Errorf("readableSections = %d, want 2 (only level 2)", len(got))
	}
	if got[0].Slug != "a" || got[1].Slug != "d" {
		t.Fatalf("unexpected readable section slugs: %#v", got)
	}
}

func TestSpecDetail_ReaderRenderOnDemand(t *testing.T) {
	m := testSpecDetailModel()
	m, _ = m.update(specDetailDataMsg{
		Meta: &markdown.SpecMeta{ID: "SPEC-001", Title: "Test", Status: "build", Author: "alice", Updated: "2026-05-20"},
		Sections: []markdown.Section{
			{Slug: "problem", Heading: "## Problem", Level: 2, Content: "Problem content."},
			{Slug: "solution", Heading: "## Solution", Level: 2, Content: "Solution content."},
		},
	})

	// Press 'o' — renders on demand, no cache needed.
	m, cmd := m.update(keyMsg("o"))
	if !m.readerMode {
		t.Fatal("should be in reader mode")
	}
	if cmd == nil {
		t.Fatal("expected async render command")
	}
	m, _ = m.update(cmd())
	if strings.TrimSpace(m.readerContent) == "" {
		t.Fatal("reader content should be populated")
	}

	// Navigate to next section.
	m, cmd = m.update(keyMsg("n"))
	if m.sectionIdx != 1 {
		t.Errorf("sectionIdx = %d, want 1", m.sectionIdx)
	}
	if cmd == nil {
		t.Fatal("expected async render command for next section")
	}
	m, _ = m.update(cmd())
	if strings.TrimSpace(m.readerContent) == "" {
		t.Error("section 2 reader content should be populated")
	}
}

func TestSpecDetail_ReaderIgnoresStaleRender(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## Problem", Level: 2, Content: "Problem content."},
		{Slug: "solution", Heading: "## Solution", Level: 2, Content: "Solution content."},
	}

	// Kick off render for section 0, hold the result.
	m, firstCmd := m.update(keyMsg("o"))
	firstMsg := firstCmd().(sectionRenderedMsg)

	// Navigate to section 1 while section 0 is still "in flight".
	m, secondCmd := m.update(keyMsg("n"))
	if secondCmd != nil {
		t.Fatal("nav during in-flight render should only store pending, not emit a cmd")
	}
	if m.pendingRequest == nil {
		t.Fatal("expected a pending request for section 1")
	}

	// Now deliver the section-0 result — should immediately kick off section-1 render.
	m, cmd := m.update(firstMsg)
	if cmd == nil {
		t.Fatal("pending request should produce a render cmd for section 1")
	}
	if !m.renderInFlight {
		t.Fatal("render should be in-flight for section 1")
	}
	m, _ = m.update(cmd())
	if m.sectionIdx != 1 {
		t.Fatalf("sectionIdx = %d, want 1", m.sectionIdx)
	}
	if !strings.Contains(m.view(), "Solution") {
		t.Fatalf("reader should show latest section, got: %s", m.view())
	}
}

func TestSpecDetail_ReaderUsesCacheForRenderedSection(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## Problem", Level: 2, Content: "Problem content."},
		{Slug: "solution", Heading: "## Solution", Level: 2, Content: "Solution content."},
	}

	m, cmd := m.update(keyMsg("o"))
	m, _ = m.update(cmd())
	m, cmd = m.update(keyMsg("n"))
	m, _ = m.update(cmd())
	m, cmd = m.update(keyMsg("p"))
	if cmd != nil {
		t.Fatal("returning to a rendered section should use the cache")
	}
	if !strings.Contains(m.view(), "Problem") {
		t.Fatalf("reader should show cached section, got: %s", m.view())
	}
}

func TestSpecDetail_KeyHandlingSchedulesAsyncRender(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "large", Heading: "## Large", Level: 2, Content: strings.Repeat("Long paragraph with **formatting** and [links](https://example.com).\n", 5000)},
	}

	start := time.Now()
	m, cmd := m.update(keyMsg("o"))
	elapsed := time.Since(start)
	if cmd == nil {
		t.Fatal("expected async render command")
	}
	if elapsed > 20*time.Millisecond {
		t.Fatalf("key handling took %v, want under 20ms", elapsed)
	}
	if strings.TrimSpace(m.readerContent) != "" {
		t.Fatal("key handling should not synchronously render reader content")
	}
}

func TestSpecDetail_FirstOpenReaderDoesNotShowNoContentPlaceholder(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "large", Heading: "## Large", Level: 2, Content: strings.Repeat("content\n", 1000)},
	}
	m.readerMode = true
	m.readerContent = ""

	got := m.view()
	if strings.Contains(got, "no content") {
		t.Fatalf("first open pending state should not show no-content placeholder, got: %s", got)
	}
	if strings.TrimSpace(m.readerContent) != "" {
		t.Fatal("view should not mutate reader content")
	}
}

func TestSpecDetail_ResizeDuringRenderRerendersCurrentSection(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## Problem", Level: 2, Content: strings.Repeat("Problem content line\n", 200)},
	}

	m, cmd := m.update(keyMsg("o"))
	if cmd == nil {
		t.Fatal("expected render cmd")
	}
	m, _ = m.update(cmd())
	before := m.readerContent
	if before == "" {
		t.Fatal("expected rendered content")
	}

	m.setSize(60, 20)
	m, cmd = m.requestCurrentSectionRender()
	if cmd == nil {
		t.Fatal("expected rerender cmd after resize")
	}
	m, _ = m.update(cmd())
	after := m.readerContent
	if after == "" {
		t.Fatal("expected rerendered content")
	}
	if before == after {
		t.Fatal("expected content to change after width resize rerender")
	}
}

func TestSpecDetail_FastNavSpamDoesNotLeavePendingArtifacts(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 100
	m.height = 20
	m.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Test"}
	m.sections = []markdown.Section{
		{Slug: "one", Heading: "## One", Level: 2, Content: strings.Repeat("One\n", 40)},
		{Slug: "two", Heading: "## Two", Level: 2, Content: strings.Repeat("Two\n", 40)},
		{Slug: "three", Heading: "## Three", Level: 2, Content: strings.Repeat("Three\n", 40)},
	}

	m, cmd := m.update(keyMsg("o"))
	if cmd == nil {
		t.Fatal("expected initial render cmd")
	}
	first := cmd().(sectionRenderedMsg)

	m, _ = m.update(keyMsg("n"))
	m, _ = m.update(keyMsg("n"))
	m, _ = m.update(keyMsg("p"))
	m, _ = m.update(keyMsg("n"))
	m, _ = m.update(first)

	for i := 0; i < 4 && m.renderInFlight; i++ {
		if cmd != nil {
			m, cmd = m.update(cmd())
		}
	}

	m, _ = m.update(keyMsg("o"))
	m, _ = m.update(keyMsg("esc"))
	if m.readerMode {
		t.Fatal("expected reader mode to be closed")
	}
	if strings.Contains(m.view(), "Rendering section") {
		t.Fatal("view should not contain stale rendering placeholder")
	}
}

func TestSpecDetail_SectionAtClick(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 120 // >= readerSidebarMinWidth so the sidebar is shown
	m.height = 30
	m.readerMode = true
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## 1. Problem", Level: 2, Content: "Problem text."},
		{Slug: "goals", Heading: "## 2. Goals", Level: 2, Content: "Goals text."},
		{Slug: "solution", Heading: "## 3. Solution", Level: 2, Content: "Solution text."},
	}

	// Sidebar rows: 0 header, 1 blank, 2 section0, 3 section1, 4 section2.
	cases := []struct {
		name    string
		x, y    int
		wantIdx int
		wantOK  bool
	}{
		{"header row", 5, 0, 0, false},
		{"blank row", 5, 1, 0, false},
		{"first section", 5, 2, 0, true},
		{"second section", 5, 3, 1, true},
		{"third section", 5, 4, 2, true},
		{"below sections", 5, 10, 0, false},
		{"right of sidebar", readerSidebarWidth, 2, 0, false},
	}
	for _, tc := range cases {
		idx, ok := m.sectionAtClick(tc.x, tc.y)
		if ok != tc.wantOK || (ok && idx != tc.wantIdx) {
			t.Errorf("%s: sectionAtClick(%d,%d) = (%d,%v), want (%d,%v)",
				tc.name, tc.x, tc.y, idx, ok, tc.wantIdx, tc.wantOK)
		}
	}
}

func TestSpecDetail_SectionAtClick_NoSidebarWhenNarrow(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 80 // below readerSidebarMinWidth → no sidebar
	m.height = 30
	m.readerMode = true
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## 1. Problem", Level: 2, Content: "x"},
	}
	if _, ok := m.sectionAtClick(5, 2); ok {
		t.Error("narrow reader has no sidebar; click should miss")
	}
}

func TestSpecDetail_SectionAtClick_OverviewMisses(t *testing.T) {
	m := testSpecDetailModel()
	m.width = 120
	m.readerMode = false // overview mode has no sidebar
	m.sections = []markdown.Section{
		{Slug: "problem", Heading: "## 1. Problem", Level: 2, Content: "x"},
	}
	if _, ok := m.sectionAtClick(5, 2); ok {
		t.Error("overview mode has no sidebar; click should miss")
	}
}

func TestSpecDetail_TLDRBlockShown(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-050", Title: "Feature X", Status: "draft"}
	m.sections = []markdown.Section{
		{Slug: "tl_dr", Heading: "## TL;DR", Level: 2, Owner: "anyone", Content: "Build a widget that solves Y."},
		{Slug: "problem_statement", Heading: "## 1. Problem Statement", Level: 2, Owner: "pm", Content: "Users struggle."},
	}
	m.contentLines = m.estimateContentLines()

	got := m.view()
	if !strings.Contains(got, "TL;DR") {
		t.Error("TL;DR block header should appear in detail view")
	}
	if !strings.Contains(got, "Build a widget") {
		t.Error("TL;DR content should appear in detail view")
	}
}

func TestSpecDetail_TLDRBlockHiddenWhenAbsent(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-050", Title: "Legacy Spec", Status: "draft"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Heading: "## 1. Problem Statement", Level: 2, Owner: "pm", Content: "Users struggle."},
	}
	m.contentLines = m.estimateContentLines()

	got := m.view()
	if strings.Contains(got, "TL;DR") {
		t.Error("TL;DR block should not appear when no tl_dr section exists")
	}
}

func TestSpecDetail_TLDRBlockHiddenWhenEmpty(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-050", Title: "New Spec", Status: "draft"}
	m.sections = []markdown.Section{
		{Slug: "tl_dr", Heading: "## TL;DR", Level: 2, Owner: "anyone", Content: "  \n\n  "},
		{Slug: "problem_statement", Heading: "## 1. Problem Statement", Level: 2, Owner: "pm", Content: ""},
	}
	m.contentLines = m.estimateContentLines()

	got := m.view()
	if strings.Contains(got, "TL;DR") {
		t.Error("TL;DR block should not appear when tl_dr section is blank")
	}
}
