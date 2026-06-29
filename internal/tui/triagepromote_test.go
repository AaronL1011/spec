package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestBuildPromotedSpec_InjectBody(t *testing.T) {
	body := "Login loop after SSO refresh — users bounced back immediately."
	content := buildPromotedSpec(&config.ResolvedConfig{}, "SPEC-015", "Login loop", "alice", "Cycle 0", "TRIAGE-003", body)

	if !strings.Contains(content, "## 1. Problem Statement") {
		t.Fatal("spec must contain §1 heading")
	}
	if !strings.Contains(content, body) {
		t.Error("triage body should be injected into §1")
	}
}

func TestBuildPromotedSpec_EmptyBody(t *testing.T) {
	content := buildPromotedSpec(&config.ResolvedConfig{}, "SPEC-015", "Login loop", "alice", "Cycle 0", "TRIAGE-003", "")
	// With empty body, §1 heading still exists but no extra injection.
	if !strings.Contains(content, "## 1. Problem Statement") {
		t.Fatal("spec must contain §1 heading even with empty body")
	}
}

func TestBuildPromotedSpec_SanitisesHeadings(t *testing.T) {
	body := "Overview\n## Bad heading\nMore text"
	content := buildPromotedSpec(&config.ResolvedConfig{}, "SPEC-015", "Title", "alice", "Cycle 0", "TRIAGE-001", body)
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
