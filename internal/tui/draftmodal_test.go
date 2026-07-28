package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/llm"
)

// The modal is the TUI renderer of the same review-gate states the CLI renders,
// so these assert the shared properties: nothing is written before accept, the
// action set is fixed, and an unsupported action is never offered.

func draftingApp(t *testing.T) App {
	t.Helper()
	a := testApp()
	a.width = 100
	a.height = 40
	return a
}

// reviewingSession builds an App parked on a reviewable attempt, without needing
// a provider.
func reviewingSession(t *testing.T, attempts ...string) App {
	t.Helper()
	a := draftingApp(t)
	a.draft = draftSession{
		state:  draftReviewing,
		specID: "SPEC-001",
		slug:   "problem_statement",
		taskID: "draft-section",
		title:  "Problem Statement",
	}
	for i, content := range attempts {
		a.draft.attempts = append(a.draft.attempts, llm.Attempt{
			Content: content,
			Result: &llm.Result{
				Model:    "test-model",
				Duration: time.Duration(i+1) * time.Second,
				Tokens:   adapter.TokenUsage{Total: 100 * (i + 1)},
			},
		})
	}
	a.draft.cursor = len(a.draft.attempts) - 1
	return a
}

func press(a App, key string) (App, tea.Cmd) {
	return a.updateDraft(tea.KeyPressMsg{Code: keyCodeFor(key), Text: key})
}

// keyCodeFor maps a printable key to its rune code; esc and brackets are handled
// via their literal text.
func keyCodeFor(key string) rune {
	if key == "" {
		return 0
	}
	return []rune(key)[0]
}

func TestDraftFlow_SkipWritesNothing(t *testing.T) {
	a := reviewingSession(t, "a draft")

	a, cmd := press(a, "s")
	if a.draft.active() {
		t.Error("skip should end the draft flow")
	}
	if cmd != nil {
		// A skip must not produce a write command.
		if msg := cmd(); msg != nil {
			if _, isWrite := msg.(draftWrittenMsg); isWrite {
				t.Error("skip must not write")
			}
		}
	}
}

func TestDraftFlow_EscapeSkips(t *testing.T) {
	a := reviewingSession(t, "a draft")
	a, _ = a.updateDraft(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.draft.active() {
		t.Error("esc should end the draft flow")
	}
}

// Cancelling mid-generation must stop the work, not just hide it.
func TestDraftFlow_EscapeDuringGenerationCancels(t *testing.T) {
	a := draftingApp(t)
	cancelled := false
	a.draft = draftSession{
		state:     draftGenerating,
		specID:    "SPEC-001",
		slug:      "problem_statement",
		startedAt: time.Now(),
		cancel:    func() { cancelled = true },
	}

	a, _ = a.updateDraft(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !cancelled {
		t.Error("esc must cancel the in-flight generation, not merely close the modal")
	}
	if a.draft.active() {
		t.Error("the flow should end after cancellation")
	}
}

// Keys that act on a draft must be inert while one is still generating.
func TestDraftFlow_ReviewKeysIgnoredWhileGenerating(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{state: draftGenerating, specID: "SPEC-001", slug: "tl_dr", startedAt: time.Now()}

	for _, key := range []string{"a", "r", "e", "i"} {
		var cmd tea.Cmd
		a, cmd = press(a, key)
		if a.draft.state != draftGenerating {
			t.Fatalf("key %q changed state while generating", key)
		}
		if cmd != nil {
			t.Errorf("key %q produced a command while generating", key)
		}
	}
}

// Case matters: r and R are different actions, as in the CLI gate.
func TestDraftFlow_UppercaseROpensSteerInput(t *testing.T) {
	a := reviewingSession(t, "a draft")

	a, _ = press(a, "R")
	if a.draft.state != draftSteering {
		t.Fatalf("R should open the steer input, state = %v", a.draft.state)
	}

	// Typing accumulates, backspace deletes.
	a, _ = press(a, "b")
	a, _ = press(a, "e")
	if a.draft.steerInput != "be" {
		t.Errorf("steer input = %q, want %q", a.draft.steerInput, "be")
	}
	a, _ = a.updateDraft(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if a.draft.steerInput != "b" {
		t.Errorf("backspace should delete, got %q", a.draft.steerInput)
	}
}

// Abandoning a note must keep the draft rather than discarding it.
func TestDraftFlow_EscapeFromSteerKeepsDraft(t *testing.T) {
	a := reviewingSession(t, "a draft")
	a, _ = press(a, "R")
	a, _ = a.updateDraft(tea.KeyPressMsg{Code: tea.KeyEscape})

	if a.draft.state != draftReviewing {
		t.Errorf("esc from the steer input should return to the draft, state = %v", a.draft.state)
	}
	if !a.draft.active() {
		t.Error("the draft must not be discarded")
	}
	if a.draft.steerInput != "" {
		t.Error("the abandoned note should be cleared")
	}
}

// An empty note is not a steer, so it must not trigger a regeneration.
func TestDraftFlow_EmptySteerReturnsWithoutRegenerating(t *testing.T) {
	a := reviewingSession(t, "a draft")
	a, _ = press(a, "R")
	a, _ = a.updateDraft(tea.KeyPressMsg{Code: tea.KeyEnter})

	if a.draft.state != draftReviewing {
		t.Errorf("an empty note should return to review, state = %v", a.draft.state)
	}
	if len(a.draft.notes) != 0 {
		t.Errorf("an empty note should not be recorded, got %v", a.draft.notes)
	}
}

func TestDraftFlow_AttemptNavigation(t *testing.T) {
	a := reviewingSession(t, "first", "second", "third")
	if a.draft.cursor != 2 {
		t.Fatalf("cursor should start on the newest attempt, got %d", a.draft.cursor)
	}

	a, _ = a.updateDraft(tea.KeyPressMsg{Text: "["})
	if a.draft.cursor != 1 {
		t.Errorf("[ should step back, cursor = %d", a.draft.cursor)
	}
	att, _ := a.draft.current()
	if att.Content != "second" {
		t.Errorf("shown attempt = %q, want the previous one", att.Content)
	}

	a, _ = a.updateDraft(tea.KeyPressMsg{Text: "]"})
	if a.draft.cursor != 2 {
		t.Errorf("] should step forward, cursor = %d", a.draft.cursor)
	}
}

// Navigation must clamp rather than panic at the ends.
func TestDraftFlow_NavigationClamps(t *testing.T) {
	a := reviewingSession(t, "only")

	a, _ = a.updateDraft(tea.KeyPressMsg{Text: "["})
	a, _ = a.updateDraft(tea.KeyPressMsg{Text: "]"})
	if a.draft.cursor != 0 {
		t.Errorf("cursor should stay at 0 with one attempt, got %d", a.draft.cursor)
	}
}

// Accept must write the attempt on screen, not the newest one.
func TestDraftFlow_AcceptWritesShownAttempt(t *testing.T) {
	a := reviewingSession(t, "first", "second")
	a, _ = a.updateDraft(tea.KeyPressMsg{Text: "["}) // back to "first"

	att, ok := a.draft.current()
	if !ok || att.Content != "first" {
		t.Fatalf("precondition: shown attempt = %q", att.Content)
	}

	a, cmd := press(a, "a")
	if a.draft.active() {
		t.Error("accept should end the flow")
	}
	if cmd == nil {
		t.Fatal("accept should produce a write command")
	}
}

// A failed retry keeps the attempts already gathered: the reviewer still has
// something usable.
func TestDraftFlow_FailedRetryKeepsPreviousAttempt(t *testing.T) {
	a := reviewingSession(t, "good draft")
	a.draft.state = draftGenerating

	a, _ = a.handleDraftResult(draftResultMsg{
		specID: "SPEC-001", slug: "problem_statement",
		err: errFake("provider exploded"),
	})

	if !a.draft.active() {
		t.Fatal("a failed retry must not end the review")
	}
	if a.draft.state != draftReviewing {
		t.Errorf("state = %v, want reviewing", a.draft.state)
	}
	att, _ := a.draft.current()
	if att.Content != "good draft" {
		t.Errorf("previous attempt lost, got %q", att.Content)
	}
	if a.draft.lastErr == "" {
		t.Error("the failure should be surfaced in the modal")
	}
}

// A failed *first* attempt has nothing to fall back to, so the flow ends.
func TestDraftFlow_FailedFirstAttemptEndsFlow(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{state: draftGenerating, specID: "SPEC-001", slug: "tl_dr", startedAt: time.Now()}

	a, _ = a.handleDraftResult(draftResultMsg{
		specID: "SPEC-001", slug: "tl_dr", err: errFake("no provider"),
	})
	if a.draft.active() {
		t.Error("a failed first attempt should end the flow with an error")
	}
}

// An empty draft is a failure to report, not content to review.
func TestDraftFlow_EmptyResultIsRejected(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{state: draftGenerating, specID: "SPEC-001", slug: "tl_dr", startedAt: time.Now()}

	a, _ = a.handleDraftResult(draftResultMsg{
		specID: "SPEC-001", slug: "tl_dr", result: &llm.Result{Text: "   "},
	})
	if a.draft.active() {
		t.Error("an empty first draft should end the flow")
	}
}

// A result for a section the user has moved on from must be ignored, or a slow
// provider could overwrite the current view.
func TestDraftFlow_StaleResultIgnored(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{state: draftGenerating, specID: "SPEC-001", slug: "tl_dr", startedAt: time.Now()}

	a, _ = a.handleDraftResult(draftResultMsg{
		specID: "SPEC-002", slug: "problem_statement",
		result: &llm.Result{Text: "from another spec"},
	})
	if len(a.draft.attempts) != 0 {
		t.Error("a result for a different spec/section must be ignored")
	}
}

func TestDraftFlow_ResultAppendsAttemptAndCarriesNote(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{state: draftGenerating, specID: "SPEC-001", slug: "tl_dr", startedAt: time.Now()}

	a, _ = a.handleDraftResult(draftResultMsg{
		specID: "SPEC-001", slug: "tl_dr",
		result: &llm.Result{Text: "drafted", Model: "m", Tokens: adapter.TokenUsage{Total: 10}},
		note:   "be terser",
	})

	if len(a.draft.attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(a.draft.attempts))
	}
	if a.draft.attempts[0].Note != "be terser" {
		t.Errorf("the steer that produced an attempt should be retained, got %q", a.draft.attempts[0].Note)
	}
	if a.draft.state != draftReviewing {
		t.Errorf("state = %v, want reviewing", a.draft.state)
	}
}

// A failed editor must not lose the draft.
func TestDraftFlow_EditorFailureKeepsDraft(t *testing.T) {
	a := reviewingSession(t, "original")

	a, _ = a.handleDraftEdited(draftEditedMsg{cursor: 0, err: errFake("editor exploded")})
	if !a.draft.active() {
		t.Fatal("an editor failure must not discard the draft")
	}
	att, _ := a.draft.current()
	if att.Content != "original" {
		t.Errorf("content changed after a failed edit: %q", att.Content)
	}
	if a.draft.lastErr == "" {
		t.Error("the editor failure should be surfaced")
	}
}

func TestDraftFlow_EditorSuccessMarksEdited(t *testing.T) {
	a := reviewingSession(t, "original")

	a, _ = a.handleDraftEdited(draftEditedMsg{cursor: 0, content: "hand-tuned"})
	att, _ := a.draft.current()
	if att.Content != "hand-tuned" {
		t.Errorf("content = %q, want the edited text", att.Content)
	}
	if !att.Edited {
		t.Error("an edited attempt should be marked, so the status line can say so")
	}
}

// The elapsed counter is what distinguishes a slow local model from a hang.
func TestDraftFlow_TickAdvancesElapsed(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{
		state:     draftGenerating,
		specID:    "SPEC-001",
		startedAt: time.Now().Add(-3 * time.Second),
	}

	a, cmd := a.handleDraftTick()
	if a.draft.elapsed < 2*time.Second {
		t.Errorf("elapsed = %v, want roughly 3s", a.draft.elapsed)
	}
	if cmd == nil {
		t.Error("the tick should reschedule itself while generating")
	}
}

func TestDraftFlow_TickStopsAfterGeneration(t *testing.T) {
	a := reviewingSession(t, "draft")
	_, cmd := a.handleDraftTick()
	if cmd != nil {
		t.Error("the tick should not reschedule once a draft is on screen")
	}
}

// --- rendering ---

func TestViewDraft_ShowsElapsedAndCancelHintWhileGenerating(t *testing.T) {
	a := draftingApp(t)
	a.draft = draftSession{
		state:     draftGenerating,
		specID:    "SPEC-001",
		slug:      "problem_statement",
		taskID:    "draft-section",
		title:     "Problem Statement",
		startedAt: time.Now(),
		elapsed:   4200 * time.Millisecond,
	}

	out := a.viewDraft()
	if !strings.Contains(out, "4.2s") {
		t.Errorf("a pending generation should show elapsed time:\n%s", out)
	}
	if !strings.Contains(out, "draft-section") {
		t.Errorf("a pending generation should name the task:\n%s", out)
	}
	if !strings.Contains(out, "esc cancels") {
		t.Errorf("cancellation should be advertised:\n%s", out)
	}
}

func TestViewDraft_ShowsProviderCostAndActions(t *testing.T) {
	a := reviewingSession(t, "the drafted body")

	out := a.viewDraft()
	for _, want := range []string{"test-model", "100 tokens", "[a]ccept", "[r]etry", "[R]etry+note", "[s]kip"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q:\n%s", want, out)
		}
	}
}

func TestViewDraft_ShowsAttemptCounter(t *testing.T) {
	a := reviewingSession(t, "first", "second")
	out := a.viewDraft()
	if !strings.Contains(out, "attempt 2/2") {
		t.Errorf("expected an attempt counter:\n%s", out)
	}
}

// Escalation must not be offered when the agent has no session plane: a key that
// then fails is worse than one that is absent.
func TestViewDraft_HidesInteractiveWithoutSessionPlane(t *testing.T) {
	a := reviewingSession(t, "draft")
	// No agent configured, so no MCP capability.
	a.rc = &config.ResolvedConfig{}

	out := a.viewDraft()
	if strings.Contains(out, "[i]nteractive") {
		t.Errorf("escalation should be hidden without a session plane:\n%s", out)
	}
}

// A long draft must be visibly truncated rather than silently cut.
func TestViewDraft_MarksTruncation(t *testing.T) {
	long := strings.Repeat("line of draft text\n", 60)
	a := reviewingSession(t, long)

	out := a.viewDraft()
	if !strings.Contains(out, "more lines") {
		t.Errorf("truncation should be marked so a cut draft is not mistaken for a short one:\n%s", out)
	}
}

// --- helpers ---

type errFake string

func (e errFake) Error() string { return string(e) }
