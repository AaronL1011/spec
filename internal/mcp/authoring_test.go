package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

const authoringFixture = `---
id: SPEC-001
title: Test Spec
status: draft
version: 0.1.0
author: aaron
cycle: Cycle 0
revert_count: 0
created: 2026-01-01
updated: 2026-01-01
---

# SPEC-001 - Test Spec

## 1. Problem Statement

The original problem.

## 6. Acceptance Criteria

- [ ] existing criterion

## 7. Technical Implementation

Notes.
`

func authoringHandler(t *testing.T) (*GenericHandler, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SPEC-001.md")
	if err := os.WriteFile(path, []byte(authoringFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewGenericHandler(nil, dir), path
}

func callJSON(t *testing.T, h *GenericHandler, name string, args map[string]interface{}) *ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.CallTool(name, raw)
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

// A write must round-trip: the hash a read hands out is the hash a write accepts.
func TestSectionRead_ReturnsHashThatWriteAccepts(t *testing.T) {
	h, _ := authoringHandler(t)

	read := callJSON(t, h, "spec_section_read", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement",
	})
	if !read.Success {
		t.Fatalf("read failed: %s", read.Message)
	}
	if !strings.Contains(read.Message, "content_hash:") {
		t.Fatalf("read should report a content_hash, got:\n%s", read.Message)
	}
	hash := extractHash(t, read.Message)

	write := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement",
		"content": "A rewritten problem.", "base_hash": hash,
	})
	if !write.Success {
		t.Fatalf("write with a fresh hash should succeed: %s", write.Message)
	}
	// The new hash comes back so a second write needs no re-read.
	if !strings.Contains(write.Message, "content_hash:") {
		t.Errorf("write should return the new content_hash, got:\n%s", write.Message)
	}
}

// The written content must round-trip through the markdown engine, so an agent's
// edit is structurally identical to a human's.
func TestSectionWrite_PersistsThroughMarkdownEngine(t *testing.T) {
	h, path := authoringHandler(t)

	res := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement", "content": "Agent-authored problem.",
	})
	if !res.Success {
		t.Fatalf("write failed: %s", res.Message)
	}

	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := markdown.FindSection(sections, "problem_statement")
	if s == nil {
		t.Fatal("section missing after write — the engine lost the heading")
	}
	if !strings.Contains(s.Content, "Agent-authored problem.") {
		t.Errorf("content = %q", s.Content)
	}
	// The rest of the document must be intact.
	if markdown.FindSection(sections, "technical_implementation") == nil {
		t.Error("a section write must not disturb sibling sections")
	}
}

// The concurrency guarantee: a stale hash is refused and the file is untouched,
// so an agent cannot silently clobber a human edit.
func TestSectionWrite_StaleHashConflictsWithoutWriting(t *testing.T) {
	h, path := authoringHandler(t)

	read := callJSON(t, h, "spec_section_read", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement",
	})
	staleHash := extractHash(t, read.Message)

	// A human edits the same section in the meantime.
	if err := markdown.ReplaceSection(path, "problem_statement", "Human edit wins."); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement",
		"content": "Agent clobber.", "base_hash": staleHash,
	})
	if res.Success {
		t.Fatal("a stale base_hash must not write")
	}
	if !strings.Contains(res.Message, "CONFLICT") {
		t.Errorf("error should be a structured conflict, got: %s", res.Message)
	}
	if !strings.Contains(res.Message, "problem_statement") {
		t.Errorf("conflict should name the section, got: %s", res.Message)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a conflicting write must leave the file byte-identical")
	}
}

func TestSectionWrite_UnknownSlugIsRejected(t *testing.T) {
	h, _ := authoringHandler(t)
	res := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "not_a_section", "content": "x",
	})
	if res.Success {
		t.Fatal("an unknown section slug must be rejected")
	}
	if !strings.Contains(res.Message, "valid slugs") {
		t.Errorf("error should list valid slugs, got: %s", res.Message)
	}
}

// An agent that loses track mid-conversation will re-run this, so a duplicate
// must be a no-op rather than noise a human then cleans up.
func TestAcceptanceAdd_IsIdempotent(t *testing.T) {
	h, path := authoringHandler(t)

	first := callJSON(t, h, "spec_acceptance_add", map[string]interface{}{
		"id": "SPEC-001", "criterion": "Draft review offers retry",
	})
	if !first.Success {
		t.Fatalf("add failed: %s", first.Message)
	}

	second := callJSON(t, h, "spec_acceptance_add", map[string]interface{}{
		"id": "SPEC-001", "criterion": "Draft review offers retry",
	})
	if !second.Success {
		t.Fatalf("a repeat add should succeed as a no-op: %s", second.Message)
	}
	if !strings.Contains(second.Message, "already present") {
		t.Errorf("a repeat should report no change, got: %s", second.Message)
	}

	body, _, err := markdown.SectionContentHash(path, "acceptance_criteria")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(body, "Draft review offers retry"); n != 1 {
		t.Errorf("criterion appears %d times, want exactly 1", n)
	}
}

// A criterion the user has already ticked is still present, so re-adding it must
// not create an unchecked duplicate beside the completed one.
func TestAcceptanceAdd_RecognisesTickedCriterion(t *testing.T) {
	h, path := authoringHandler(t)
	if err := markdown.ReplaceSection(path, "acceptance_criteria", "- [x] Already done"); err != nil {
		t.Fatal(err)
	}

	res := callJSON(t, h, "spec_acceptance_add", map[string]interface{}{
		"id": "SPEC-001", "criterion": "Already done",
	})
	if !res.Success || !strings.Contains(res.Message, "already present") {
		t.Errorf("a ticked criterion should count as present, got: %s", res.Message)
	}

	body, _, err := markdown.SectionContentHash(path, "acceptance_criteria")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "- [ ] Already done") {
		t.Error("must not add an unchecked duplicate of a completed criterion")
	}
}

func TestAcceptanceAdd_AppendsToExistingList(t *testing.T) {
	h, path := authoringHandler(t)
	res := callJSON(t, h, "spec_acceptance_add", map[string]interface{}{
		"id": "SPEC-001", "criterion": "New criterion",
	})
	if !res.Success {
		t.Fatalf("add failed: %s", res.Message)
	}
	body, _, err := markdown.SectionContentHash(path, "acceptance_criteria")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "existing criterion") {
		t.Error("appending must preserve existing criteria")
	}
	if !strings.Contains(body, "- [ ] New criterion") {
		t.Errorf("new criterion should be a checkbox item, got:\n%s", body)
	}
}

// Stage must not be reachable through the metadata tool, or it would be a way
// around the gates that the transition tier exists to enforce.
func TestMetaUpdate_RefusesStageAndStatus(t *testing.T) {
	h, _ := authoringHandler(t)

	for _, field := range []string{"stage", "status"} {
		res := callJSON(t, h, "spec_meta_update", map[string]interface{}{
			"id": "SPEC-001", field: "engineering",
		})
		if res.Success {
			t.Errorf("%s must not be settable through spec_meta_update", field)
		}
		if !strings.Contains(res.Message, "spec_advance") {
			t.Errorf("refusal should point at the transition tools, got: %s", res.Message)
		}
	}
}

func TestMetaUpdate_AppliesAllowedFields(t *testing.T) {
	h, path := authoringHandler(t)

	res := callJSON(t, h, "spec_meta_update", map[string]interface{}{
		"id": "SPEC-001", "title": "Renamed Spec", "assignees": "aaron, blake",
	})
	if !res.Success {
		t.Fatalf("update failed: %s", res.Message)
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Renamed Spec" {
		t.Errorf("title = %q", meta.Title)
	}
	if len(meta.Assignees) != 2 || meta.Assignees[0] != "aaron" {
		t.Errorf("assignees = %v", meta.Assignees)
	}
	// Status must be untouched by a metadata update.
	if meta.Status != "draft" {
		t.Errorf("status = %q, want it unchanged", meta.Status)
	}
}

func TestMetaUpdate_NoFieldsIsANoOp(t *testing.T) {
	h, _ := authoringHandler(t)
	res := callJSON(t, h, "spec_meta_update", map[string]interface{}{"id": "SPEC-001"})
	if !res.Success {
		t.Errorf("an empty update should succeed as a no-op: %s", res.Message)
	}
}

// Agent writes are attributed distinctly from human CLI actions, which is the
// whole point of the actor-kind column.
func TestAuthoringWrite_IsLoggedAsAgentActor(t *testing.T) {
	h, _ := authoringHandler(t)

	db, err := store.Open(filepath.Join(t.TempDir(), "spec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	h.WithDB(db)

	res := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement", "content": "Agent wrote this.",
	})
	if !res.Success {
		t.Fatalf("write failed: %s", res.Message)
	}

	entries, err := db.ActivityForSpec("SPEC-001", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d activity entries, want 1", len(entries))
	}
	if entries[0].ActorKind != store.ActorAgent {
		t.Errorf("actor kind = %q, want agent", entries[0].ActorKind)
	}

	// A CLI-equivalent write is human, so the two are distinguishable.
	if err := db.ActivityLog("SPEC-001", "section_write", "human edit", "", "aaron"); err != nil {
		t.Fatal(err)
	}
	entries, err = db.ActivityForSpec("SPEC-001", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sawAgent, sawHuman bool
	for _, e := range entries {
		switch e.ActorKind {
		case store.ActorAgent:
			sawAgent = true
		case store.ActorHuman:
			sawHuman = true
		}
	}
	if !sawAgent || !sawHuman {
		t.Error("agent and human writes must be distinguishable in the log")
	}
}

// The MCP server must serve a bare checkout: no config, no specs repo, no
// database. Attribution degrades silently rather than failing the write.
func TestAuthoringPort_SurvivesBareCheckout(t *testing.T) {
	h, _ := authoringHandler(t) // nil config, no DB

	// Reads work.
	if res := callJSON(t, h, "spec_section_read", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement",
	}); !res.Success {
		t.Errorf("read should work without config or DB: %s", res.Message)
	}

	// Writes work, just unlogged.
	if res := callJSON(t, h, "spec_section_write", map[string]interface{}{
		"id": "SPEC-001", "slug": "problem_statement", "content": "written",
	}); !res.Success {
		t.Errorf("write should work without config or DB: %s", res.Message)
	}

	// Listing tools must not panic with no config.
	if len(h.ListTools()) == 0 {
		t.Error("tools should be listed without config")
	}
}

// A missing spec is a clean tool error, not a panic.
func TestAuthoringPort_MissingSpecIsAnError(t *testing.T) {
	h, _ := authoringHandler(t)
	res := callJSON(t, h, "spec_section_read", map[string]interface{}{
		"id": "SPEC-404", "slug": "problem_statement",
	})
	if res.Success {
		t.Error("reading a nonexistent spec should fail cleanly")
	}
}

// --- transition tier gating ---

func TestTransitionTier_AbsentByDefault(t *testing.T) {
	h, _ := authoringHandler(t)

	names := toolNameSet(h.ListTools())
	for _, name := range []string{"spec_advance", "spec_revert"} {
		if names[name] {
			t.Errorf("%q must be absent from tools/list unless enabled", name)
		}
	}
	// The authoring tier is present in the same session, which is the point of
	// tiering rather than gating the whole port.
	if !names["spec_section_write"] {
		t.Error("spec_section_write should be available by default")
	}
}

func TestTransitionTier_PresentWhenEnabled(t *testing.T) {
	h, _ := authoringHandler(t)
	h.config = configWithTransitions(true)

	names := toolNameSet(h.ListTools())
	for _, name := range []string{"spec_advance", "spec_revert"} {
		if !names[name] {
			t.Errorf("%q should appear once transitions are enabled", name)
		}
	}
}

// Calling a disabled tool must explain the boundary rather than failing opaquely.
func TestTransitionTier_DisabledCallNamesThePreference(t *testing.T) {
	h, _ := authoringHandler(t)

	res := callJSON(t, h, "spec_advance", map[string]interface{}{"id": "SPEC-001"})
	if res.Success {
		t.Fatal("advance must not run while the tier is disabled")
	}
	if !strings.Contains(res.Message, "agent_authoring") || !strings.Contains(res.Message, "transitions") {
		t.Errorf("message should name the enabling preference, got: %s", res.Message)
	}
}

func TestTransitionTier_RevertRequiresReason(t *testing.T) {
	h, _ := authoringHandler(t)
	h.config = configWithTransitions(true)

	res := callJSON(t, h, "spec_revert", map[string]interface{}{"id": "SPEC-001", "reason": ""})
	if res.Success {
		t.Error("a revert without a reason should be rejected")
	}
}

// --- helpers ---

func configWithTransitions(enabled bool) *config.ResolvedConfig {
	user := &config.UserConfig{}
	user.Preferences.AgentAuthoring.Transitions = enabled
	return &config.ResolvedConfig{User: user}
}

func toolNameSet(tools []Tool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, tool := range tools {
		out[tool.Name] = true
	}
	return out
}

func extractHash(t *testing.T, message string) string {
	t.Helper()
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "content_hash:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "content_hash:"))
		}
	}
	t.Fatalf("no content_hash in message:\n%s", message)
	return ""
}
