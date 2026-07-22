package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

// promoteFields builds the SpecFields a promote test needs, with fixed
// author/cycle/date so assertions stay focused on body injection.
func promoteFields(id, title, source string) markdown.SpecFields {
	return markdown.SpecFields{ID: id, Title: title, Author: "alice", Cycle: "Cycle 0", Source: source, Date: "2026-01-01"}
}

func TestBuildPromotedSpec_InjectBody(t *testing.T) {
	body := "Login loop after SSO refresh — users bounced back immediately."
	content := buildPromotedSpec("", markdown.TemplateConfig{}, promoteFields("SPEC-015", "Login loop", "TRIAGE-003"), body)

	if !strings.Contains(content, "## 1. Problem Statement") {
		t.Fatal("spec must contain §1 heading")
	}
	if !strings.Contains(content, body) {
		t.Error("triage body should be injected into §1")
	}
}

func TestBuildPromotedSpec_EmptyBody(t *testing.T) {
	content := buildPromotedSpec("", markdown.TemplateConfig{}, promoteFields("SPEC-015", "Login loop", "TRIAGE-003"), "")
	// With empty body, §1 heading still exists but no extra injection.
	if !strings.Contains(content, "## 1. Problem Statement") {
		t.Fatal("spec must contain §1 heading even with empty body")
	}
}

func TestBuildPromotedSpec_SanitisesHeadings(t *testing.T) {
	body := "Overview\n## Bad heading\nMore text"
	content := buildPromotedSpec("", markdown.TemplateConfig{}, promoteFields("SPEC-015", "Title", "TRIAGE-001"), body)
	// The ## heading in the body must be demoted.
	if strings.Contains(content, "\n## Bad heading\n") {
		t.Error("level-2 heading in triage body must be demoted in the spec")
	}
	if !strings.Contains(content, "**Bad heading**") {
		t.Error("demoted heading should be rendered as bold label")
	}
}

func TestSanitiseBodyForSpec_DemotesHeadings(t *testing.T) {
	body := "## Should be demoted\n### This is fine"
	got := sanitiseBodyForSpec(body)
	if strings.Contains(got, "## Should") {
		t.Error("## heading should be demoted")
	}
	if !strings.Contains(got, "**Should be demoted**") {
		t.Error("## heading should become bold")
	}
	// ### headings are left intact (only ## collides with spec skeleton)
	if !strings.Contains(got, "### This is fine") {
		t.Error("### heading should be preserved")
	}
}

func TestSanitiseBodyForSpec_Empty(t *testing.T) {
	if got := sanitiseBodyForSpec(""); got != "" {
		t.Errorf("empty body should return empty, got %q", got)
	}
}

// extractFrontmatterBlock tests are now in internal/markdown/markdown_test.go
// since the function moved to the markdown package.

func TestBuildPromotedSpec_CustomTemplateHeadingStillReceivesBody(t *testing.T) {
	// A team template with an unnumbered, marker-less Problem Statement
	// heading must still receive the triage body — injection is slug-based,
	// not literal-string based, so a fluid template keeps working.
	dir := t.TempDir()
	custom := `---
id: <% id %>
title: <% title %>
---

# <% id %> - <% title %>

## Problem Statement

## User Stories                <!-- owner: pm -->

## Acceptance Criteria         <!-- owner: qa -->
`
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	body := "Users bounced back to login after SSO refresh."
	content := buildPromotedSpec(dir, markdown.TemplateConfig{}, promoteFields("SPEC-020", "Login loop", "TRIAGE-004"), body)

	if !strings.Contains(content, "## Problem Statement\n\n"+body) {
		t.Errorf("triage body not injected under custom Problem Statement heading:\n%s", content)
	}
}
