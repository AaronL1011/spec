package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSpec = `---
id: SPEC-042
title: Auth refactor
status: build
version: 0.2.0
author: Aaron Lewis
cycle: Cycle 7
epic_key: PLAT-123
repos:
    - auth-service
    - api-gateway
revert_count: 1
source: TRIAGE-088
created: "2026-04-01"
updated: "2026-04-21"
---

# SPEC-042 - Auth refactor

## 1. Problem Statement           <!-- owner: pm -->
Auth tokens are expiring prematurely for EU users.

## 2. Goals & Non-Goals           <!-- owner: pm -->
### Goals
- Fix token expiration
### Non-Goals
- Rewrite auth from scratch

## 3. User Stories                <!-- owner: pm -->
As an EU user, I want my session to persist.

## 4. Proposed Solution           <!-- owner: pm -->

### 4.1 Concept Overview
Use Redis for token storage.

### 4.2 Architecture / Approach
Sidecar pattern.

## 5. Design Inputs               <!-- owner: designer -->

## 6. Acceptance Criteria         <!-- owner: qa -->
- [ ] EU sessions last 24h
- [ ] No regression in US sessions

## 7. Technical Implementation    <!-- owner: engineer -->

### 7.1 Architecture Notes
Redis cluster with 3 nodes.

### 7.2 Dependencies & Risks
Redis dependency.

### 7.3 PR Stack Plan
1. [auth-service] Add token bucket rate limiter
2. [auth-service] Integrate Redis backend
3. [api-gateway] Add rate limit middleware

## 8. Escape Hatch Log            <!-- auto: spec eject -->

## 9. QA Validation Notes         <!-- owner: qa -->

## 10. Deployment Notes           <!-- owner: engineer -->

## 11. Retrospective              <!-- auto: spec retro -->

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | REST vs gRPC? | REST, gRPC | **gRPC** | Lower latency | Aaron | 2026-04-10 |
`

func TestParseMeta(t *testing.T) {
	meta, err := ParseMeta(testSpec)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"ID", meta.ID, "SPEC-042"},
		{"Title", meta.Title, "Auth refactor"},
		{"Status", meta.Status, "build"},
		{"Author", meta.Author, "Aaron Lewis"},
		{"Cycle", meta.Cycle, "Cycle 7"},
		{"EpicKey", meta.EpicKey, "PLAT-123"},
		{"RevertCount", meta.RevertCount, 1},
		{"Source", meta.Source, "TRIAGE-088"},
		{"Repos length", len(meta.Repos), 2},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestExtractSections(t *testing.T) {
	body := Body(testSpec)
	sections := ExtractSections(body)

	// Find all top-level sections
	topLevel := 0
	for _, s := range sections {
		if s.Level == 2 {
			topLevel++
		}
	}
	if topLevel != 12 { // including the title heading
		t.Errorf("top-level sections = %d, want 12", topLevel)
	}

	// Check specific sections
	tests := []struct {
		slug  string
		owner string
		empty bool
	}{
		{"problem_statement", "pm", false},
		{"goals_non_goals", "pm", false},
		{"user_stories", "pm", false},
		{"design_inputs", "designer", true},
		{"acceptance_criteria", "qa", false},
		{"technical_implementation", "engineer", false},
		{"escape_hatch_log", "auto", true},
		{"qa_validation_notes", "qa", true},
		{"retrospective", "auto", true},
	}

	for _, tt := range tests {
		s := FindSection(sections, tt.slug)
		if s == nil {
			t.Errorf("section %q not found", tt.slug)
			continue
		}
		if s.Owner != tt.owner {
			t.Errorf("section %q owner = %q, want %q", tt.slug, s.Owner, tt.owner)
		}
		isEmpty := strings.TrimSpace(s.Content) == ""
		if isEmpty != tt.empty {
			t.Errorf("section %q empty = %v, want %v", tt.slug, isEmpty, tt.empty)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"## TL;DR                             <!-- owner: anyone -->", "tl_dr"},
		{"## 1. Problem Statement           <!-- owner: pm -->", "problem_statement"},
		{"### 7.3 PR Stack Plan", "pr_stack_plan"},
		{"## Decision Log", "decision_log"},
		{"## 5. Design Inputs               <!-- owner: designer -->", "design_inputs"},
		{"### 4.1 Concept Overview", "concept_overview"},
		{"## 11. Retrospective              <!-- auto: spec retro -->", "retrospective"},
	}

	for _, tt := range tests {
		// Extract the heading text (after ##)
		heading := strings.TrimLeft(tt.input, "# ")
		got := slugify(heading)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsSectionNonEmpty(t *testing.T) {
	body := Body(testSpec)
	sections := ExtractSections(body)

	if !IsSectionNonEmpty(sections, "problem_statement") {
		t.Error("problem_statement should be non-empty")
	}
	if IsSectionNonEmpty(sections, "design_inputs") {
		t.Error("design_inputs should be empty")
	}
}

func TestReplaceSectionContent(t *testing.T) {
	result, err := ReplaceSectionContent(testSpec, "problem_statement", "New problem statement content.\n")
	if err != nil {
		t.Fatalf("ReplaceSectionContent: %v", err)
	}

	// Parse the result and check the section was replaced
	body := Body(result)
	sections := ExtractSections(body)
	s := FindSection(sections, "problem_statement")
	if s == nil {
		t.Fatal("section not found after replacement")
	}
	if !strings.Contains(s.Content, "New problem statement content.") {
		t.Errorf("section content = %q, want to contain 'New problem statement content.'", s.Content)
	}

	// Verify other sections are untouched
	gs := FindSection(sections, "goals_non_goals")
	if gs == nil || !strings.Contains(gs.Content, "Fix token expiration") {
		t.Error("goals section should be untouched")
	}
}

func TestDecisionLog(t *testing.T) {
	body := Body(testSpec)
	sections := ExtractSections(body)
	dl := FindSection(sections, "decision_log")
	if dl == nil {
		t.Fatal("decision log not found")
	}

	entries, err := ParseDecisionLog(dl.Content)
	if err != nil {
		t.Fatalf("ParseDecisionLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Number != 1 {
		t.Errorf("entry number = %d, want 1", entries[0].Number)
	}
	if entries[0].Decision != "**gRPC**" {
		t.Errorf("decision = %q, want %q", entries[0].Decision, "**gRPC**")
	}
}

func TestAppendAndResolveDecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-042.md")
	if err := os.WriteFile(path, []byte(testSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	// Append a decision
	num, err := AppendDecision(path, "Token bucket or sliding window?", "Aaron")
	if err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	if num != 2 {
		t.Errorf("new decision number = %d, want 2", num)
	}

	// Verify it was appended
	entries, err := ParseDecisionLogFromFile(path)
	if err != nil {
		t.Fatalf("ParseDecisionLogFromFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// Resolve the decision
	if err := ResolveDecision(path, 2, "Token bucket", "Simpler to implement", "Aaron"); err != nil {
		t.Fatalf("ResolveDecision: %v", err)
	}

	entries, _ = ParseDecisionLogFromFile(path)
	if len(entries) != 2 {
		t.Fatalf("entries after resolve = %d, want 2", len(entries))
	}
	if !strings.Contains(entries[1].Decision, "Token bucket") {
		t.Errorf("resolved decision = %q, want to contain 'Token bucket'", entries[1].Decision)
	}
}

func TestResolveNonExistentDecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-042.md")
	if err := os.WriteFile(path, []byte(testSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ResolveDecision(path, 999, "Test", "Test", "Aaron")
	if err == nil {
		t.Error("expected error for non-existent decision")
	}
}

func TestMaxSpecNum(t *testing.T) {
	tests := []struct {
		files []string
		want  int
	}{
		{nil, 0},
		{[]string{"SPEC-001.md"}, 1},
		{[]string{"SPEC-001.md", "SPEC-042.md", "SPEC-003.md"}, 42},
		{[]string{"README.md"}, 0},
	}

	for _, tt := range tests {
		got := MaxSpecNum(tt.files)
		if got != tt.want {
			t.Errorf("MaxSpecNum(%v) = %d, want %d", tt.files, got, tt.want)
		}
	}
}

func TestMaxTriageNum(t *testing.T) {
	tests := []struct {
		files []string
		want  int
	}{
		{nil, 0},
		{[]string{"TRIAGE-001.md", "TRIAGE-088.md"}, 88},
	}

	for _, tt := range tests {
		got := MaxTriageNum(tt.files)
		if got != tt.want {
			t.Errorf("MaxTriageNum(%v) = %d, want %d", tt.files, got, tt.want)
		}
	}
}

func TestScaffoldSpec(t *testing.T) {
	content := ScaffoldSpec("SPEC-042", "Auth refactor", "Aaron", "Cycle 7", "direct")

	meta, err := ParseMeta(content)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	if meta.ID != "SPEC-042" {
		t.Errorf("ID = %q, want SPEC-042", meta.ID)
	}
	if meta.Status != "draft" {
		t.Errorf("Status = %q, want draft", meta.Status)
	}

	sections := ExtractSections(Body(content))
	requiredSlugs := []string{
		"tl_dr", "decision_log", "problem_statement", "goals_non_goals",
		"acceptance_criteria", "technical_implementation", "pr_stack_plan",
	}
	for _, slug := range requiredSlugs {
		if FindSection(sections, slug) == nil {
			t.Errorf("scaffolded spec missing section %q", slug)
		}
	}
}

func TestBody(t *testing.T) {
	body := Body(testSpec)
	if strings.Contains(body, "---") && strings.HasPrefix(body, "---") {
		t.Error("Body should not start with frontmatter delimiter")
	}
	if !strings.HasPrefix(body, "# SPEC-042") {
		t.Errorf("Body should start with title heading, got: %q", body[:30])
	}
}

func TestParseTriageMeta(t *testing.T) {
	content := ScaffoldTriage("TRIAGE-001", "Bug report", "high", "support", "#8821", "Aaron")
	meta, err := ParseTriageMeta(content)
	if err != nil {
		t.Fatalf("ParseTriageMeta: %v", err)
	}
	if meta.ID != "TRIAGE-001" {
		t.Errorf("ID = %q, want TRIAGE-001", meta.ID)
	}
	if meta.Priority != "high" {
		t.Errorf("Priority = %q, want high", meta.Priority)
	}
}

const testSpecWithSteps = `---
id: SPEC-042
title: Auth refactor
status: build
version: 0.2.0
author: Aaron
cycle: Cycle 7
repos:
    - auth-service
    - api-gateway
revert_count: 0
created: "2026-04-01"
updated: "2026-04-21"
steps:
    - repo: auth-service
      description: Add token bucket rate limiter
      branch: spec-042/step-1-rate-limiter
      pr: 415
      status: complete
    - repo: auth-service
      description: Integrate Redis backend
      branch: spec-042/step-2-redis
      pr: 418
      status: in-progress
    - repo: api-gateway
      description: Add rate limit middleware
      status: pending
    - repo: frontend
      description: Handle 429 errors
      status: pending
review:
    requested_at: "2026-04-20"
    reviewers:
        - "@mike"
    approvals:
        - reviewer: "@mike"
          approved_at: "2026-04-21"
    status: approved
---

# SPEC-042 - Auth refactor

Body content here.
`

func TestParseMetaWithSteps(t *testing.T) {
	meta, err := ParseMeta(testSpecWithSteps)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}

	// Check steps were parsed
	if len(meta.Steps) != 4 {
		t.Fatalf("Steps length = %d, want 4", len(meta.Steps))
	}

	// Check first step
	step1 := meta.Steps[0]
	if step1.Repo != "auth-service" {
		t.Errorf("Step 1 Repo = %q, want auth-service", step1.Repo)
	}
	if step1.Description != "Add token bucket rate limiter" {
		t.Errorf("Step 1 Description = %q", step1.Description)
	}
	if step1.Branch != "spec-042/step-1-rate-limiter" {
		t.Errorf("Step 1 Branch = %q", step1.Branch)
	}
	if step1.PR != 415 {
		t.Errorf("Step 1 PR = %d, want 415", step1.PR)
	}
	if step1.Status != StepStatusComplete {
		t.Errorf("Step 1 Status = %q, want complete", step1.Status)
	}

	// Check third step (no branch/PR yet)
	step3 := meta.Steps[2]
	if step3.Status != StepStatusPending {
		t.Errorf("Step 3 Status = %q, want pending", step3.Status)
	}
	if step3.Branch != "" {
		t.Errorf("Step 3 Branch should be empty, got %q", step3.Branch)
	}

	// Check review state
	if meta.Review == nil {
		t.Fatal("Review should not be nil")
	}
	if meta.Review.Status != ReviewStatusApproved {
		t.Errorf("Review Status = %q, want approved", meta.Review.Status)
	}
	if len(meta.Review.Approvals) != 1 {
		t.Errorf("Review Approvals = %d, want 1", len(meta.Review.Approvals))
	}
	if meta.Review.Approvals[0].Reviewer != "@mike" {
		t.Errorf("Approval Reviewer = %q, want @mike", meta.Review.Approvals[0].Reviewer)
	}
}

func TestCurrentStep(t *testing.T) {
	meta, _ := ParseMeta(testSpecWithSteps)

	// Current step should be index 1 (second step, first non-complete)
	current := meta.CurrentStep()
	if current != 1 {
		t.Errorf("CurrentStep = %d, want 1", current)
	}
}

func TestCurrentStep_AllComplete(t *testing.T) {
	meta := &SpecMeta{
		Steps: []BuildStep{
			{Status: StepStatusComplete},
			{Status: StepStatusComplete},
		},
	}
	current := meta.CurrentStep()
	if current != -1 {
		t.Errorf("CurrentStep when all complete = %d, want -1", current)
	}
}

func TestCurrentStep_NoSteps(t *testing.T) {
	meta := &SpecMeta{}
	current := meta.CurrentStep()
	if current != -1 {
		t.Errorf("CurrentStep with no steps = %d, want -1", current)
	}
}

func TestAllStepsComplete(t *testing.T) {
	tests := []struct {
		name  string
		steps []BuildStep
		want  bool
	}{
		{"no steps", nil, false},
		{"all complete", []BuildStep{{Status: StepStatusComplete}, {Status: StepStatusComplete}}, true},
		{"some pending", []BuildStep{{Status: StepStatusComplete}, {Status: StepStatusPending}}, false},
		{"all pending", []BuildStep{{Status: StepStatusPending}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &SpecMeta{Steps: tt.steps}
			got := meta.AllStepsComplete()
			if got != tt.want {
				t.Errorf("AllStepsComplete = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStepsExist(t *testing.T) {
	tests := []struct {
		name  string
		steps []BuildStep
		want  bool
	}{
		{"no steps", nil, false},
		{"empty slice", []BuildStep{}, false},
		{"has steps", []BuildStep{{Repo: "test"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &SpecMeta{Steps: tt.steps}
			got := meta.StepsExist()
			if got != tt.want {
				t.Errorf("StepsExist = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReviewStateHelpers(t *testing.T) {
	tests := []struct {
		name           string
		review         *ReviewState
		wantApproved   bool
		wantPending    bool
		wantChangesReq bool
	}{
		{"nil review", nil, false, false, false},
		{"approved", &ReviewState{Status: ReviewStatusApproved}, true, false, false},
		{"pending", &ReviewState{Status: ReviewStatusPending}, false, true, false},
		{"changes requested", &ReviewState{Status: ReviewStatusChangesRequested}, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &SpecMeta{Review: tt.review}

			if got := meta.IsReviewApproved(); got != tt.wantApproved {
				t.Errorf("IsReviewApproved = %v, want %v", got, tt.wantApproved)
			}
			if got := meta.IsReviewPending(); got != tt.wantPending {
				t.Errorf("IsReviewPending = %v, want %v", got, tt.wantPending)
			}
			if got := meta.IsReviewChangesRequested(); got != tt.wantChangesReq {
				t.Errorf("IsReviewChangesRequested = %v, want %v", got, tt.wantChangesReq)
			}
		})
	}
}

func TestFastTrackField(t *testing.T) {
	content := `---
id: SPEC-048
title: Fix null pointer
status: build
version: 0.1.0
author: Aaron
cycle: Cycle 7
revert_count: 0
created: "2026-04-23"
updated: "2026-04-23"
fast_track: true
---

# SPEC-048 - Fix null pointer
`

	meta, err := ParseMeta(content)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	if !meta.FastTrack {
		t.Error("FastTrack should be true")
	}
}

func TestParseTriageMeta_NewFields(t *testing.T) {
	content := `---
id: TRIAGE-007
title: Severity test
status: open
priority: high
severity: urgent
linked_spec: SPEC-009
reported_by: alice
created: "2026-06-01"
resolved_at: ""
comments:
  - actor: bob
    message: reproduced on staging
    at: "2026-06-01T10:00:00Z"
---

Body text here.
`
	meta, err := ParseTriageMeta(content)
	if err != nil {
		t.Fatalf("ParseTriageMeta: %v", err)
	}
	if meta.Severity != "urgent" {
		t.Errorf("Severity = %q, want urgent", meta.Severity)
	}
	if meta.LinkedSpec != "SPEC-009" {
		t.Errorf("LinkedSpec = %q, want SPEC-009", meta.LinkedSpec)
	}
	if len(meta.Comments) != 1 {
		t.Fatalf("Comments len = %d, want 1", len(meta.Comments))
	}
	if meta.Comments[0].Actor != "bob" {
		t.Errorf("Comments[0].Actor = %q, want bob", meta.Comments[0].Actor)
	}
}

func TestWriteTriageMeta_PreservesBody(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/TRIAGE-001.md"
	content := `---
id: TRIAGE-001
title: Original title
status: open
priority: medium
created: "2026-06-01"
---

Body content preserved here.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatalf("ReadTriageMeta: %v", err)
	}
	meta.Severity = "urgent"
	if err := WriteTriageMeta(path, meta); err != nil {
		t.Fatalf("WriteTriageMeta: %v", err)
	}
	data, _ := os.ReadFile(path)
	updated := string(data)
	if !strings.Contains(updated, "severity: urgent") {
		t.Error("severity field should be written")
	}
	if !strings.Contains(updated, "Body content preserved here.") {
		t.Error("body should be preserved after WriteTriageMeta")
	}
}

func TestAppendTriageComment(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/TRIAGE-002.md"
	content := `---
id: TRIAGE-002
title: Comment test
status: open
priority: low
created: "2026-06-01"
---

Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := AppendTriageComment(path, "alice", "first note"); err != nil {
		t.Fatalf("AppendTriageComment: %v", err)
	}
	if err := AppendTriageComment(path, "bob", "second note"); err != nil {
		t.Fatalf("AppendTriageComment: %v", err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatalf("ReadTriageMeta after append: %v", err)
	}
	if len(meta.Comments) != 2 {
		t.Fatalf("Comments len = %d, want 2", len(meta.Comments))
	}
	if meta.Comments[0].Actor != "alice" || meta.Comments[0].Message != "first note" {
		t.Errorf("first comment = %+v, want alice/first note", meta.Comments[0])
	}
	if meta.Comments[1].Actor != "bob" {
		t.Errorf("second comment actor = %q, want bob", meta.Comments[1].Actor)
	}
}

func TestUpdateTriageFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TRIAGE-010.md")
	content := `---
id: TRIAGE-010
title: Original
status: open
priority: low
source: email
created: "2026-06-01"
---

Original body here.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTriageFields(path, "Updated Title", "high", "slack", "New body text"); err != nil {
		t.Fatalf("UpdateTriageFields: %v", err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Updated Title" {
		t.Errorf("Title = %q, want Updated Title", meta.Title)
	}
	if meta.Priority != "high" {
		t.Errorf("Priority = %q, want high", meta.Priority)
	}
	if meta.Source != "slack" {
		t.Errorf("Source = %q, want slack", meta.Source)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "New body text") {
		t.Error("body should be updated")
	}
	if strings.Contains(string(data), "Original body here") {
		t.Error("old body should be replaced")
	}
}

func TestArchiveTriageItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TRIAGE-011.md")
	content := `---
id: TRIAGE-011
title: To be closed
status: open
priority: medium
created: "2026-06-01"
---

Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveTriageItem(path, "resolved", "fixed in v2", "alice"); err != nil {
		t.Fatalf("ArchiveTriageItem: %v", err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "archived" {
		t.Errorf("Status = %q, want archived", meta.Status)
	}
	if meta.ResolvedAt == "" {
		t.Error("ResolvedAt should be set")
	}
	if len(meta.Comments) != 1 {
		t.Fatalf("Comments = %d, want 1", len(meta.Comments))
	}
	if meta.Comments[0].Actor != "alice" {
		t.Errorf("Comment actor = %q, want alice", meta.Comments[0].Actor)
	}
	if !strings.Contains(meta.Comments[0].Message, "resolved") {
		t.Error("comment should contain the reason")
	}
	if !strings.Contains(meta.Comments[0].Message, "fixed in v2") {
		t.Error("comment should contain the note")
	}
}

func TestEscalateTriageItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TRIAGE-012.md")
	content := `---
id: TRIAGE-012
title: Escalation test
status: open
priority: high
created: "2026-06-01"
---

Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EscalateTriageItem(path, false, "bob"); err != nil {
		t.Fatalf("EscalateTriageItem (escalate): %v", err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Severity != "urgent" {
		t.Errorf("Severity after escalate = %q, want urgent", meta.Severity)
	}
	if len(meta.Comments) != 1 {
		t.Fatalf("Comments = %d, want 1", len(meta.Comments))
	}
	if !strings.Contains(meta.Comments[0].Message, "escalated to urgent") {
		t.Errorf("escalation comment = %q, want 'escalated to urgent'", meta.Comments[0].Message)
	}

	if err := EscalateTriageItem(path, true, "charlie"); err != nil {
		t.Fatalf("EscalateTriageItem (de-escalate): %v", err)
	}
	meta, _ = ReadTriageMeta(path)
	if meta.Severity != "" {
		t.Errorf("Severity after de-escalate = %q, want empty", meta.Severity)
	}
	if len(meta.Comments) != 2 {
		t.Fatalf("Comments = %d, want 2", len(meta.Comments))
	}
	if !strings.Contains(meta.Comments[1].Message, "de-escalated") {
		t.Errorf("de-escalation comment = %q", meta.Comments[1].Message)
	}
}

func TestExtractFrontmatterBlock(t *testing.T) {
	content := "---\nid: TRIAGE-001\ntitle: Test\n---\n\nBody here."
	block := extractFrontmatterBlock(content)
	if !strings.HasPrefix(block, "---") {
		t.Error("block should start with ---")
	}
	if !strings.Contains(block, "TRIAGE-001") {
		t.Error("block should contain frontmatter content")
	}
	if strings.Contains(block, "Body here") {
		t.Error("block should not contain the body")
	}
}

func TestUpdateTriageFields_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TRIAGE-013.md")
	content := `---
id: TRIAGE-013
title: Has history
status: open
priority: low
created: "2026-06-01"
comments:
    - actor: alice
      message: initial note
      at: "2026-06-01T10:00:00Z"
---

Original body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTriageFields(path, "Edited", "high", "", "New body"); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadTriageMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Comments) != 1 {
		t.Fatalf("existing comments should be preserved, got %d", len(meta.Comments))
	}
	if meta.Comments[0].Actor != "alice" {
		t.Errorf("preserved comment actor = %q, want alice", meta.Comments[0].Actor)
	}
}
