package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oldScaffoldSpec is the pre-SPEC-025 fmt.Sprintf skeleton, captured here so
// the embedded default template must stay byte-identical to it. Drift is a
// bug, not an accepted change (SPEC-025 §7.2).
func oldScaffoldSpec(id, title, author, cycle, source string) string {
	date := time.Now().Format("2006-01-02")
	return `---
id: ` + id + `
title: ` + title + `
status: draft
version: 0.1.0
author: ` + author + `
cycle: ` + cycle + `
epic_key: ""
repos: []
revert_count: 0
source: ` + source + `
created: ` + date + `
updated: ` + date + `
---

# ` + id + ` - ` + title + `

## TL;DR                             <!-- owner: anyone -->

## 1. Problem Statement           <!-- owner: pm -->

## 2. Goals & Non-Goals           <!-- owner: pm -->

## 3. User Stories                <!-- owner: pm -->

## 4. Proposed Solution           <!-- owner: pm -->

### 4.1 Concept Overview

### 4.2 Architecture / Approach

## 5. Design Inputs               <!-- owner: designer -->

## 6. Acceptance Criteria         <!-- owner: qa -->

## 7. Technical Implementation    <!-- owner: engineer -->

### 7.1 Architecture Notes

### 7.2 Dependencies & Risks

### 7.3 PR Stack Plan
<!--
Parsed into a DAG and executed by 'spec build'. One line = one node:
    N. [repo:layer] Description (after: A, B)
  - [repo] must be listed in 'repos:' above and mapped in ~/.spec/config.yaml
    under workspaces: (validated before the build starts).
  - :layer is optional and routes skills (e.g. rails-api, go-grpc, react-web).
  - (after: ...) are dependency edges to earlier node numbers; nodes with no
    unmet dependency run in the same wave (in parallel). Omit for a root node.
Draft-PR URLs are appended automatically by the finisher (do not author them);
the pr-review gate passes only once every leaf node has one. Example:
    1. [auth-service:rails-api] Add token-bucket limiter
    2. [api-gateway:go-grpc] Add rate-limit middleware (after: 1)
-->

## 8. Escape Hatch Log            <!-- auto: spec eject -->

## 9. QA Validation Notes         <!-- owner: qa -->

## 10. Deployment Notes           <!-- owner: engineer -->

## 11. Retrospective              <!-- auto: spec retro -->

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
`
}

func TestScaffoldSpec_ByteIdenticalToLegacy(t *testing.T) {
	got := ScaffoldSpec("SPEC-042", "Auth refactor", "Aaron", "Cycle 7", "direct")
	want := oldScaffoldSpec("SPEC-042", "Auth refactor", "Aaron", "Cycle 7", "direct")
	if got != want {
		t.Errorf("embedded default scaffold drifted from legacy fmt.Sprintf output")
		// Pinpoint the first difference for diagnosis.
		for i := 0; i < len(got) && i < len(want); i++ {
			if got[i] != want[i] {
				t.Errorf("first diff at byte %d: got=%q want=%q", i, got[i], want[i])
				break
			}
		}
		if len(got) != len(want) {
			t.Errorf("length: got=%d want=%d", len(got), len(want))
		}
	}
}

func TestScaffoldTriage_ByteIdenticalToLegacy(t *testing.T) {
	got := ScaffoldTriage("TRIAGE-001", "Bug report", "high", "support", "#8821", "Aaron")
	date := time.Now().Format("2006-01-02")
	want := `---
id: TRIAGE-001
title: Bug report
status: triage
priority: high
source: support
source_ref: #8821
reported_by: Aaron
created: ` + date + `
---

# TRIAGE-001 - Bug report

## Context

## Notes
`
	if got != want {
		t.Errorf("triage scaffold drifted: got=%q want=%q", got, want)
	}
}

func TestScaffoldTriage_DefaultsPriority(t *testing.T) {
	got := ScaffoldTriage("TRIAGE-002", "X", "", "support", "", "")
	if !strings.Contains(got, "priority: medium") {
		t.Errorf("empty priority should default to medium; got:\n%s", got)
	}
}

func TestRenderSpec_FrontmatterDefaultsAdded(t *testing.T) {
	tpl := `---
id: <% id %>
title: <% title %>
status: draft
created: <% date %>
---

# <% id %> - <% title %>
`
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001", Title: "T", Date: "2026-01-01"}, []KV{
		{Key: "service_area", Value: "payments"},
		{Key: "compliance", Value: "sox"},
	})
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if !strings.Contains(out, "service_area: payments") || !strings.Contains(out, "compliance: sox") {
		t.Errorf("defaults not injected:\n%s", out)
	}
	// Defaults land before the closing --- and after computed fields.
	if strings.Index(out, "compliance: sox") > strings.Index(out, "\n---\n\n#") {
		t.Errorf("defaults injected after frontmatter close:\n%s", out)
	}
}

func TestRenderSpec_AssigneesInjected(t *testing.T) {
	tpl := `---
id: <% id %>
title: <% title %>
status: draft
---

# <% id %> - <% title %>
`
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001", Title: "T", Assignees: []string{"@ana"}}, nil)
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if !strings.Contains(out, `assignees: ["@ana"]`) {
		t.Errorf("assignees not injected into frontmatter:\n%s", out)
	}
	// Round-trips through the frontmatter parser as a real list.
	meta, err := ParseMeta(out)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	if len(meta.Assignees) != 1 || meta.Assignees[0] != "@ana" {
		t.Errorf("Assignees = %v, want [@ana]", meta.Assignees)
	}
}

func TestRenderSpec_NoAssigneesMeansNoKey(t *testing.T) {
	tpl := "---\nid: <% id %>\n---\n\n# <% id %>\n"
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001"}, nil)
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if strings.Contains(out, "assignees:") {
		t.Errorf("empty assignees must not emit a key:\n%s", out)
	}
}

func TestRenderSpec_TemplateAssigneesWin(t *testing.T) {
	// A team template that hardcodes an assignees key keeps it — injection
	// never duplicates or overrides an existing frontmatter key.
	tpl := "---\nid: <% id %>\nassignees: [\"@oncall\"]\n---\n\n# <% id %>\n"
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001", Assignees: []string{"@ana"}}, nil)
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if strings.Contains(out, "@ana") {
		t.Errorf("injected assignees overrode template's own key:\n%s", out)
	}
	if !strings.Contains(out, `assignees: ["@oncall"]`) {
		t.Errorf("template assignees lost:\n%s", out)
	}
}

func TestRenderSpec_AssigneesWinOverFrontmatterDefaults(t *testing.T) {
	tpl := "---\nid: <% id %>\n---\n\n# <% id %>\n"
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001", Assignees: []string{"@ana"}}, []KV{
		{Key: "assignees", Value: "SHOULD-NOT-WIN"},
	})
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if strings.Contains(out, "SHOULD-NOT-WIN") {
		t.Errorf("team default overrode the runtime claim:\n%s", out)
	}
	if !strings.Contains(out, `assignees: ["@ana"]`) {
		t.Errorf("runtime claim lost:\n%s", out)
	}
}

func TestRenderSpec_FrontmatterDefaultsDoNotOverrideComputed(t *testing.T) {
	tpl := `---
id: <% id %>
title: <% title %>
created: <% date %>
---

# <% id %>
`
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001", Title: "T", Date: "2026-01-01"}, []KV{
		{Key: "id", Value: "SHOULD-NOT-WIN"},
		{Key: "title", Value: "SHOULD-NOT-WIN"},
	})
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	if strings.Contains(out, "SHOULD-NOT-WIN") {
		t.Errorf("default overrode computed field:\n%s", out)
	}
	if !strings.Contains(out, "id: SPEC-001") {
		t.Errorf("computed id lost:\n%s", out)
	}
}

func TestRenderSpec_FrontmatterDefaultsInsertionOrder(t *testing.T) {
	tpl := `---
id: <% id %>
---

# <% id %>
`
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001"}, []KV{
		{Key: "zeta", Value: "1"},
		{Key: "alpha", Value: "2"},
	})
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	// Declaration order preserved (zeta before alpha), not sorted.
	zi := strings.Index(out, "zeta:")
	ai := strings.Index(out, "alpha:")
	if zi < 0 || ai < 0 || zi > ai {
		t.Errorf("declaration order not preserved:\n%s", out)
	}
}

func TestRenderSpec_UnknownPlaceholderIsError(t *testing.T) {
	tpl := `---
id: <% id %>
bogus: <% nope %>
---
`
	_, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001"}, nil)
	if err == nil {
		t.Fatal("expected parse error for unknown placeholder, got nil")
	}
}

func TestResolveTemplate_FallsBackToDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	content, source := ResolveTemplate(SpecTemplate, dir, "")
	if source != "default" {
		t.Errorf("source = %q, want default", source)
	}
	if content != defaultSpecTpl {
		t.Error("missing team file should return embedded default content")
	}
}

// validTeamSpecTemplate is a minimal spec template that passes fatal
// validation (parses, no unresolved placeholders, gate-critical sections
// present).
const validTeamSpecTemplate = `---
id: <% id %>
title: <% title %>
---

# <% id %> - <% title %>

## 1. Problem Statement           <!-- owner: pm -->

## 3. User Stories                <!-- owner: pm -->

## 6. Acceptance Criteria         <!-- owner: qa -->
`

func TestResolveTemplate_UsesTeamFileWhenPresent(t *testing.T) {
	dir := t.TempDir()
	custom := validTeamSpecTemplate
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	content, source := ResolveTemplate(SpecTemplate, dir, "")
	if source != "team" {
		t.Errorf("source = %q, want team", source)
	}
	if content != custom {
		t.Error("team file content not returned")
	}
}

func TestResolveTemplate_FallsBackOnParseError(t *testing.T) {
	dir := t.TempDir()
	bad := "---\nid: <% id %>\nbogus: <% nope %>\n---\n"
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	content, source := ResolveTemplate(SpecTemplate, dir, "")
	if source != "default" {
		t.Errorf("unparseable team file should fall back; source = %q", source)
	}
	if content != defaultSpecTpl {
		t.Error("should fall back to embedded default")
	}
}

func TestScaffoldSpecFromConfig_UsesTeamTemplate(t *testing.T) {
	dir := t.TempDir()
	custom := "---\nid: <% id %>\ntitle: <% title %>\nstatus: draft\ncreated: <% date %>\n---\n\n# <% id %> - <% title %>\n\n## 1. Problem Statement           <!-- owner: pm -->\n\n## 3. User Stories                <!-- owner: pm -->\n\n## 6. Acceptance Criteria         <!-- owner: qa -->\n"
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := TemplateConfig{SpecPath: "templates/spec.md", FrontmatterDefaults: []KV{{Key: "service_area", Value: "payments"}}}
	out := ScaffoldSpecFromConfig(dir, tc, SpecFields{ID: "SPEC-009", Title: "Custom", Date: "2026-01-01"})
	if !strings.Contains(out, "service_area: payments") {
		t.Errorf("frontmatter default not seeded:\n%s", out)
	}
	if !strings.HasPrefix(out, "---\nid: SPEC-009") {
		t.Errorf("team template not rendered:\n%s", out)
	}
}

func TestScaffoldSpecFromConfig_FallsBackOnBrokenTeamTemplate(t *testing.T) {
	dir := t.TempDir()
	bad := "---\nid: <% id %>\nbogus: <% nope %>\n---\n"
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ScaffoldSpecFromConfig(dir, TemplateConfig{SpecPath: "templates/spec.md"}, SpecFields{ID: "SPEC-001", Title: "T", Date: "2026-01-01"})
	// Should fall back to the embedded default, which contains the standard
	// section skeleton.
	if !strings.Contains(out, "## 1. Problem Statement") {
		t.Errorf("broken team template did not fall back to default:\n%s", out)
	}
}

func TestValidateTemplate_DefaultSpecIsFatalClean(t *testing.T) {
	issues := ValidateTemplate(SpecTemplate, defaultSpecTpl)
	for _, iss := range issues {
		if iss.Fatal {
			t.Errorf("default spec template has fatal issue: %s", iss.Message)
		}
	}
}

func TestValidateTemplate_DefaultTriageIsFatalClean(t *testing.T) {
	issues := ValidateTemplate(TriageTemplate, defaultTriageTpl)
	for _, iss := range issues {
		if iss.Fatal {
			t.Errorf("default triage template has fatal issue: %s", iss.Message)
		}
	}
}

func TestValidateTemplate_UnknownPlaceholderIsFatal(t *testing.T) {
	bad := "---\nid: <% id %>\nbogus: <% nope %>\n---\n# <% id %>\n"
	issues := ValidateTemplate(SpecTemplate, bad)
	found := false
	for _, iss := range issues {
		if iss.Fatal {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a fatal issue for unknown placeholder")
	}
}

func TestValidateTemplate_MissingGateCriticalSectionIsFatal(t *testing.T) {
	// Has problem_statement and user_stories but not acceptance_criteria.
	tpl := `---
id: <% id %>
---

# <% id %>

## 1. Problem Statement           <!-- owner: pm -->

## 3. User Stories                <!-- owner: pm -->
`
	issues := ValidateTemplate(SpecTemplate, tpl)
	var missing []string
	for _, iss := range issues {
		if iss.Fatal && strings.Contains(iss.Message, "acceptance_criteria") {
			missing = append(missing, iss.Message)
		}
	}
	if len(missing) == 0 {
		t.Fatalf("expected fatal issue for missing acceptance_criteria; got %v", issues)
	}
}

func TestValidateTemplate_TriageSkipsGateCheck(t *testing.T) {
	// Triage templates have no gate-critical sections; a minimal valid triage
	// template must not report missing problem_statement/user_stories.
	tpl := `---
id: <% id %>
---

# <% id %>

## Context

## Notes
`
	issues := ValidateTemplate(TriageTemplate, tpl)
	for _, iss := range issues {
		if iss.Fatal {
			t.Errorf("triage template reported fatal issue unexpectedly: %s", iss.Message)
		}
	}
}

func TestResolveTemplate_EmptyRepoDirReturnsDefault(t *testing.T) {
	content, source := ResolveTemplate(SpecTemplate, "", "")
	if source != "default" || content != defaultSpecTpl {
		t.Errorf("empty repoDir must resolve to embedded default; source = %q", source)
	}
}

func TestResolveTemplate_FallsBackOnMissingGateSection(t *testing.T) {
	// Parses fine, but is missing the gate-critical acceptance_criteria
	// section — scaffolding from it would create specs that can never pass
	// pipeline gates, so resolution must fall back to the default.
	bad := "---\nid: <% id %>\n---\n\n# <% id %>\n\n## 1. Problem Statement           <!-- owner: pm -->\n\n## 3. User Stories                <!-- owner: pm -->\n"
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	content, source := ResolveTemplate(SpecTemplate, dir, "")
	if source != "default" {
		t.Errorf("gate-incomplete team template should fall back; source = %q", source)
	}
	if content != defaultSpecTpl {
		t.Error("should fall back to embedded default content")
	}
}

func TestReadTeamTemplate_RawContentEvenWhenInvalid(t *testing.T) {
	// 'spec template validate' must see the real (broken) file, not the
	// fallback — ReadTeamTemplate returns raw content without validation.
	bad := "---\nid: <% id %>\nbogus: <% nope %>\n---\n"
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "spec.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	content, path, ok := ReadTeamTemplate(SpecTemplate, dir, "")
	if !ok || content != bad {
		t.Errorf("raw team template not returned: ok=%v content=%q", ok, content)
	}
	if path != filepath.Join(dir, "templates", "spec.md") {
		t.Errorf("unexpected path %q", path)
	}
}

func TestRenderSpec_FrontmatterDefaultValuesAreYAMLSafe(t *testing.T) {
	tpl := "---\nid: <% id %>\n---\n\n# <% id %>\n"
	out, err := RenderSpec(tpl, SpecFields{ID: "SPEC-001"}, []KV{
		{Key: "note", Value: "watch out: colons"},
		{Key: "tag", Value: "#urgent"},
		{Key: "empty", Value: ""},
		{Key: "plain", Value: "payments"},
	})
	if err != nil {
		t.Fatalf("RenderSpec: %v", err)
	}
	for _, want := range []string{
		"note: \"watch out: colons\"",
		"tag: \"#urgent\"",
		"empty: \"\"",
		"plain: payments",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in frontmatter:\n%s", want, out)
		}
	}
}
