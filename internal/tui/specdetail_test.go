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
