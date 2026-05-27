package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

func testSpecDetailModel() specDetailModel {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSpecDetail(rc, "SPEC-001", styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestSpecDetail_LoadingView(t *testing.T) {
	rc := testResolvedConfig()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSpecDetail(rc, "SPEC-001", styles, keys)

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

func TestSpecDetail_BuildSteps(t *testing.T) {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{
		ID:    "SPEC-001",
		Title: "Test",
		Steps: []markdown.BuildStep{
			{Description: "Setup schema", Repo: "db-service", Status: "done"},
			{Description: "Add API endpoints", Repo: "api-service", Status: "in_progress"},
			{Description: "Update client", Repo: "web-app", Status: ""},
		},
	}

	got := m.view()
	if !strings.Contains(got, "Build Steps") {
		t.Error("should contain 'Build Steps' header")
	}
	if !strings.Contains(got, "Setup schema") {
		t.Error("should contain first step description")
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
	if !strings.Contains(got, "o to read") {
		t.Error("overview should show reader mode hint")
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
		t.Error("entering reader mode should return a non-nil cmd to trigger repaint")
	}
	if m.contentLines != len(m.readerLines) {
		t.Errorf("contentLines = %d, want %d (reader line count)", m.contentLines, len(m.readerLines))
	}

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
	if len(got) != 3 {
		t.Errorf("readableSections = %d, want 3 (level 4 excluded)", len(got))
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
	m, _ = m.update(keyMsg("o"))
	if !m.readerMode {
		t.Fatal("should be in reader mode")
	}
	if len(m.readerLines) == 0 {
		t.Fatal("readerLines should be populated")
	}

	// Navigate to next section.
	m, _ = m.update(keyMsg("n"))
	if m.sectionIdx != 1 {
		t.Errorf("sectionIdx = %d, want 1", m.sectionIdx)
	}
	if len(m.readerLines) == 0 {
		t.Error("section 2 readerLines should be populated")
	}
}

func TestStepIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"done", "✅"},
		{"in_progress", "🔧"},
		{"active", "🔧"},
		{"blocked", "🚫"},
		{"", "○"},
		{"pending", "○"},
	}
	for _, tt := range tests {
		if got := stepIcon(tt.status); got != tt.want {
			t.Errorf("stepIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
