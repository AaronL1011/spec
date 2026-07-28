package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/markdown"
)

// These tests exercise the draft keys through the real dispatch path
// (App.Update → handleKey → updateDetail), not the helpers in isolation. The
// original wiring bug lived exactly in that gap: startSectionDraft and the
// kickoff builders were unit-tested and green while `d`/`D` were unreachable
// from the reader and always targetless from the overview.

// draftDispatchSpecBody is the on-disk fixture the fallback logic reads:
// §1 filled, §2 and §4 empty owner sections.
const draftDispatchSpecBody = `---
id: SPEC-001
title: Dispatch
---

## 1. Problem Statement

filled

## 2. Goals Non Goals

## 4. Proposed Solution
`

// draftDispatchApp returns an App with a session-capable agent, the fixture
// spec on disk, and its detail view open.
func draftDispatchApp(t *testing.T) App {
	t.Helper()

	a := testApp()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SPEC-001.md"), []byte(draftDispatchSpecBody), 0o644); err != nil {
		t.Fatal(err)
	}

	rc := rcWithAgent("pi", true)
	rc.SpecsRepoDir = dir
	a.rc = rc

	a.showDetail = true
	a.detail = newSpecDetail(rc, "SPEC-001", a.styles, a.keys, a.theme)
	a.detail.loading = false
	a.detail.agentCanDraft = true
	a.detail.meta = &markdown.SpecMeta{ID: "SPEC-001", Title: "Dispatch", Status: "draft"}
	a.detail.sections = []markdown.Section{
		{Slug: "problem_statement", Heading: "## 1. Problem Statement", Level: 2, Content: "filled"},
		{Slug: "goals_non_goals", Heading: "## 2. Goals Non Goals", Level: 2, Content: ""},
		{Slug: "proposed_solution", Heading: "## 4. Proposed Solution", Level: 2, Content: ""},
	}
	return a
}

func pressKey(t *testing.T, a App, key string) App {
	t.Helper()
	model, _ := a.Update(keyMsg(key))
	got, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned %T, want App", model)
	}
	return got
}

// --- d: reader dispatch ---

// The reader is where the section cursor lives, so it is the one surface where
// `d` has an exact target. It must start a draft for the section under the
// cursor.
func TestDraftKey_ReaderDraftsSectionUnderCursor(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 1 // goals_non_goals, empty

	got := pressKey(t, a, "d")

	if !got.draft.active() {
		t.Fatal("d in the reader should start a draft flow")
	}
	if got.draft.slug != "goals_non_goals" {
		t.Errorf("draft slug = %q, want the section under the cursor", got.draft.slug)
	}
}

// A cursor on a filled section must not silently overwrite it: `d` falls
// forward to the next empty owner section, which is what the docs promise
// ("draft the next empty section").
func TestDraftKey_ReaderOnFilledSectionFallsForward(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 0 // problem_statement, filled

	got := pressKey(t, a, "d")

	if !got.draft.active() {
		t.Fatal("d on a filled section should fall forward to an empty one")
	}
	if got.draft.slug != "goals_non_goals" {
		t.Errorf("draft slug = %q, want the next empty owner section", got.draft.slug)
	}
}

// Typing in a thread prompt must never trigger a draft: 'd' is text there.
func TestDraftKey_InactiveWhileThreadInputCaptures(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 1
	a.detail.input = threadInput{kind: "ask", area: newThreadArea(a.theme)}

	got := pressKey(t, a, "d")

	if got.draft.active() {
		t.Error("d while a thread prompt captures input should stay text, not start a draft")
	}
}

// --- d: overview and list fallback ---

// The overview has no section cursor, so `d` targets the first empty owner
// section instead of erroring "no section selected".
func TestDraftKey_OverviewFallsBackToFirstEmptySection(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = false

	got := pressKey(t, a, "d")

	if !got.draft.active() {
		t.Fatal("d in the overview should start a draft for the first empty owner section")
	}
	if got.draft.slug != "goals_non_goals" {
		t.Errorf("draft slug = %q, want the first empty owner section", got.draft.slug)
	}
}

// With every owner section filled, `d` explains rather than drafting nothing.
func TestDraftKey_NoEmptySectionsExplains(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = false

	full := `---
id: SPEC-001
title: Dispatch
---

## 1. Problem Statement

filled

## 2. Goals Non Goals

also filled
`
	if err := os.WriteFile(filepath.Join(a.rc.SpecsRepoDir, "SPEC-001.md"), []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	got := pressKey(t, a, "d")

	if got.draft.active() {
		t.Error("d with no empty owner sections should not start a draft")
	}
}

// --- D: intent input ---

// D opens an intent input before launching, so the user can say what the
// session is for instead of being handed a generic kickoff.
func TestDraftSessionKey_OpensIntentInput(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 1

	got := pressKey(t, a, "D")

	if !got.modal.Visible {
		t.Fatal("D should open the intent input modal before launching a session")
	}
	if got.pendingAction != "draft-session:goals_non_goals" {
		t.Errorf("pendingAction = %q, want draft-session carrying the cursor section", got.pendingAction)
	}
	if got.pendingSpecID != "SPEC-001" {
		t.Errorf("pendingSpecID = %q, want SPEC-001", got.pendingSpecID)
	}
	if got.draft.active() {
		t.Error("D should not start a one-shot draft flow")
	}
}

// From the overview there is no cursor; the intent modal still opens, with no
// section pinned.
func TestDraftSessionKey_OverviewOpensIntentInputWithoutSection(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = false

	got := pressKey(t, a, "D")

	if !got.modal.Visible {
		t.Fatal("D from the overview should open the intent input modal")
	}
	if got.pendingAction != "draft-session:" {
		t.Errorf("pendingAction = %q, want draft-session with no section", got.pendingAction)
	}
}

// Submitting the intent launches the session; the intent must reach the
// kickoff prompt.
func TestDraftSessionIntent_SubmitLaunches(t *testing.T) {
	a := draftDispatchApp(t)
	a.pendingAction = "draft-session:goals_non_goals"
	a.pendingSpecID = "SPEC-001"

	cmd := a.executeActionWithInput("focus on the failure modes")
	if cmd == nil {
		t.Fatal("submitting an intent should launch the session")
	}
}

// A blank submit is a valid "just open it": the session launches with the
// default kickoff rather than the modal refusing to close.
func TestDraftSessionIntent_BlankSubmitLaunches(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 1

	a = pressKey(t, a, "D")
	if !a.modal.Visible {
		t.Fatal("intent modal should be open")
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := model.(App)
	if got.modal.Visible {
		t.Error("blank Enter should close the intent modal and launch")
	}
	if cmd == nil {
		t.Error("blank Enter should still launch the session with the default kickoff")
	}
}

// --- overview footer hints ---

// The overview footer must not advertise stale keys: archive is `g a` (and
// restore `g r`), never bare `d`/`r`, which now mean draft and nothing.
func TestOverviewFooter_ArchiveHintMatchesRealKeys(t *testing.T) {
	a := draftDispatchApp(t)

	plain := xansi.Strip(strings.Join(a.detail.overviewLines(), "\n"))
	if strings.Contains(plain, "d archive") {
		t.Error("overview footer still advertises 'd' for archive; archive is 'g a'")
	}
	if !strings.Contains(plain, "g a archive") {
		t.Error("overview footer should advertise 'g a' for archive")
	}

	a.detail.isArchived = true
	plain = xansi.Strip(strings.Join(a.detail.overviewLines(), "\n"))
	if strings.Contains(plain, "r restore") && !strings.Contains(plain, "g r restore") {
		t.Error("overview footer still advertises 'r' for restore; restore is 'g r'")
	}
}

// With a draft-capable agent the footer names the draft key, so the overview
// teaches the same affordance the reader hints at.
func TestOverviewFooter_NamesDraftKeyWhenAgentCanDraft(t *testing.T) {
	a := draftDispatchApp(t)

	plain := xansi.Strip(strings.Join(a.detail.overviewLines(), "\n"))
	if !strings.Contains(plain, "d draft") {
		t.Error("overview footer should advertise 'd' for drafting when the agent supports it")
	}

	a.detail.agentCanDraft = false
	plain = xansi.Strip(strings.Join(a.detail.overviewLines(), "\n"))
	if strings.Contains(plain, "d draft") {
		t.Error("overview footer should not advertise drafting without a capable agent")
	}
}

// --- thread reply submit through the full dispatch path ---

// Ctrl+S must submit a thread reply when routed through the real key path
// (App.Update → handleKey → updateDetail → updateReader → handleThreadInputKey),
// not just when the component is called directly.
func TestThreadReplyCtrlS_DispatchesThroughApp(t *testing.T) {
	a := draftDispatchApp(t)
	a.detail.readerMode = true
	a.detail.sectionIdx = 1

	input := threadInput{kind: "reply", threadID: "T-1", area: newThreadArea(a.theme)}
	input.area.SetValue("agreed — capping at three")
	a.detail.input = input

	model, cmd := a.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	got := model.(App)

	if got.detail.input.active() {
		t.Error("ctrl+s should submit and close the reply prompt")
	}
	if cmd == nil {
		t.Error("ctrl+s with text should produce a reply command")
	}
}

// --- editor suspend ---

// The modal's `e` must hand the editor to the runtime via tea.ExecProcess so
// Bubble Tea releases the terminal first and restores raw mode after. Running
// a terminal editor from a background goroutine while the TUI holds the screen
// leaves the tty in the editor's cooked state on exit — IXON comes back on and
// Ctrl+S turns into flow control, which presents as "keys stopped working".
func TestEditDraft_HandsEditorToTheRuntime(t *testing.T) {
	a := draftDispatchApp(t)
	a.rc.User.Preferences.Editor = "true" // harmless no-op "editor"
	a.draft = draftSession{
		state:    draftReviewing,
		specID:   "SPEC-001",
		slug:     "goals_non_goals",
		attempts: []llm.Attempt{{Content: "draft body"}},
	}

	_, cmd := a.editDraft()
	if cmd == nil {
		t.Fatal("e should produce a command")
	}

	// Executing an ExecProcess command yields the runtime's internal exec
	// message; the buggy path ran the editor right here and produced a
	// draftEditedMsg synchronously.
	msg := cmd()
	if _, ranInline := msg.(draftEditedMsg); ranInline {
		t.Fatal("editDraft ran the editor inside a tea.Cmd goroutine; it must use tea.ExecProcess so the terminal is released and restored")
	}
}
