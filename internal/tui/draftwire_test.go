package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// Affordances must render only when the configured provider supports them: no
// dead keys, no surprise errors. These pin that mapping.

func rcWithAgent(provider string, draftsEnabled bool) *config.ResolvedConfig {
	rc := &config.ResolvedConfig{
		User: &config.UserConfig{},
		Team: &config.TeamConfig{},
	}
	if provider != "" {
		rc.User.Agent = &config.ProviderConfig{Provider: provider, Extra: map[string]string{}}
		if provider == "openai-compatible" {
			rc.User.Agent.Generate.BaseURL = "http://localhost:1234/v1"
		}
	}
	enabled := draftsEnabled
	rc.User.Preferences.AgentDrafts = &enabled
	return rc
}

func TestAgentCapabilities_ByProvider(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		drafts      bool
		wantDraft   bool
		wantSession bool
	}{
		{
			name:     "no agent configured",
			provider: "",
		},
		{
			name:     "explicit none",
			provider: "none",
			drafts:   true,
		},
		{
			// A completion-only provider drafts but cannot run sessions, so
			// b/D must explain rather than fail.
			name:      "completion-only provider",
			provider:  "openai-compatible",
			drafts:    true,
			wantDraft: true,
		},
		{
			// A harness serves both planes.
			name:        "session-capable harness",
			provider:    "pi",
			drafts:      true,
			wantDraft:   true,
			wantSession: true,
		},
		{
			// Drafting switched off in preferences must hide draft affordances
			// while leaving sessions alone: they are separate capabilities.
			name:        "drafting disabled in preferences",
			provider:    "pi",
			drafts:      false,
			wantDraft:   false,
			wantSession: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testApp()
			a.rc = rcWithAgent(tt.provider, tt.drafts)

			canDraft, canSession := a.agentCapabilities()
			if canDraft != tt.wantDraft {
				t.Errorf("canGenerate = %v, want %v", canDraft, tt.wantDraft)
			}
			if canSession != tt.wantSession {
				t.Errorf("canSession = %v, want %v", canSession, tt.wantSession)
			}
		})
	}
}

// An unsupported action gets a one-line explanation: a silent key is
// indistinguishable from a broken one.
func TestAgentPlaneExplanation_NamesTheReason(t *testing.T) {
	tests := []struct {
		name     string
		rc       *config.ResolvedConfig
		contains string
	}{
		{
			name:     "no agent",
			rc:       rcWithAgent("", true),
			contains: "~/.spec/config.yaml",
		},
		{
			name:     "drafting disabled",
			rc:       rcWithAgent("pi", false),
			contains: "agent_drafts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testApp()
			a.rc = tt.rc
			if got := a.agentPlaneExplanation(); !strings.Contains(got, tt.contains) {
				t.Errorf("explanation = %q, want it to mention %q", got, tt.contains)
			}
		})
	}
}

// The kickoff carries what the user would otherwise have to retype, which is the
// point of escalating rather than opening a blank session.
func TestInteractiveKickoff_CarriesContext(t *testing.T) {
	kickoff := interactiveKickoff("SPEC-034", "proposed_solution",
		"the rejected draft text",
		[]string{"weigh the queue option", "two sentences on the mechanism"})

	for _, want := range []string{
		"SPEC-034",
		"proposed_solution",
		"the rejected draft text",
		"weigh the queue option",
		"two sentences on the mechanism",
		// The session must be told to write through the port, not edit markdown.
		"spec_section_write",
		// And to respect concurrency.
		"content_hash",
	} {
		if !strings.Contains(kickoff, want) {
			t.Errorf("kickoff missing %q:\n%s", want, kickoff)
		}
	}
}

// A plain D press has no rejected draft, so the kickoff must not invent one.
func TestInteractiveKickoff_WithoutPriorDraft(t *testing.T) {
	kickoff := interactiveKickoff("SPEC-001", "problem_statement", "", nil)
	if strings.Contains(kickoff, "rejected") {
		t.Errorf("a fresh session should not mention a rejected draft:\n%s", kickoff)
	}
	if !strings.Contains(kickoff, "problem_statement") {
		t.Errorf("the target section should still be named:\n%s", kickoff)
	}
}

// --- next-empty-section chaining ---

func writeSpecFixture(t *testing.T, dir, body string) *config.ResolvedConfig {
	t.Helper()
	// SpecsRepoDir already points at the repo's specs/ subdirectory, so the
	// fixture mirrors that rather than nesting another level.
	if err := os.WriteFile(filepath.Join(dir, "SPEC-001.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := rcWithAgent("pi", true)
	rc.SpecsRepoDir = dir
	return rc
}

func TestNextEmptyOwnerSection_SkipsAutoSections(t *testing.T) {
	// §8 and §11 are auto-maintained and §6 follows the QA policy, so none of
	// them should be offered — that would be offering to write what the tool owns.
	body := `---
id: SPEC-001
title: T
---

## 1. Problem Statement

filled

## 2. Goals Non Goals

## 6. Acceptance Criteria

## 8. Escape Hatch Log

## 11. Retrospective
`
	dir := t.TempDir()
	rc := writeSpecFixture(t, dir, body)

	got := nextEmptyOwnerSection(rc, "SPEC-001", "problem_statement")
	if got != "goals_non_goals" {
		t.Errorf("next empty = %q, want goals_non_goals (auto and QA sections skipped)", got)
	}
}

func TestNextEmptyOwnerSection_NoneRemaining(t *testing.T) {
	body := `---
id: SPEC-001
title: T
---

## 1. Problem Statement

filled

## 2. Goals Non Goals

also filled
`
	dir := t.TempDir()
	rc := writeSpecFixture(t, dir, body)

	if got := nextEmptyOwnerSection(rc, "SPEC-001", "problem_statement"); got != "" {
		t.Errorf("next empty = %q, want empty when nothing remains", got)
	}
}

// Chaining should move forward through the document rather than jumping back.
func TestNextEmptyOwnerSection_PrefersSectionsAfterTheCurrentOne(t *testing.T) {
	body := `---
id: SPEC-001
title: T
---

## 1. Problem Statement

## 2. Goals Non Goals

filled

## 4. Proposed Solution
`
	dir := t.TempDir()
	rc := writeSpecFixture(t, dir, body)

	got := nextEmptyOwnerSection(rc, "SPEC-001", "goals_non_goals")
	if got != "proposed_solution" {
		t.Errorf("next empty = %q, want the following empty section", got)
	}
}

// --- draft hint gating ---

func TestDraftHint_OnlyOnEmptyOwnerSections(t *testing.T) {
	tests := []struct {
		name    string
		section markdown.Section
		canDraf bool
		want    bool
	}{
		{
			name:    "empty owner section shows the hint",
			section: markdown.Section{Slug: "problem_statement", Content: "  \n"},
			canDraf: true,
			want:    true,
		},
		{
			name:    "non-empty section does not",
			section: markdown.Section{Slug: "problem_statement", Content: "already written"},
			canDraf: true,
		},
		{
			name:    "auto sections never do",
			section: markdown.Section{Slug: "escape_hatch_log", Content: ""},
			canDraf: true,
		},
		{
			name:    "acceptance criteria follows the QA policy",
			section: markdown.Section{Slug: "acceptance_criteria", Content: ""},
			canDraf: true,
		},
		{
			name:    "owner auto marker suppresses the hint",
			section: markdown.Section{Slug: "retrospective", Content: "", Owner: "auto"},
			canDraf: true,
		},
		{
			name:    "no capability means no hint",
			section: markdown.Section{Slug: "problem_statement", Content: ""},
			canDraf: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := specDetailModel{agentCanDraft: tt.canDraf}
			if got := m.draftHintFor(tt.section); got != tt.want {
				t.Errorf("draftHintFor(%+v) = %v, want %v", tt.section, got, tt.want)
			}
		})
	}
}

// With no agent, every existing key must keep working: the feature is additive.
func TestNoAgent_LeavesExistingKeysIntact(t *testing.T) {
	a := testApp()
	a.rc = rcWithAgent("", true)

	canDraft, canSession := a.agentCapabilities()
	if canDraft || canSession {
		t.Fatal("no agent should advertise no capabilities")
	}
	// The draft flow must not be active, so keys route normally.
	if a.draft.active() {
		t.Error("no draft flow should be active without an agent")
	}
}
