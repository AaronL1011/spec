package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// scriptedPrompter replays a fixed sequence of actions, standing in for a human
// at the gate. It records what it was shown so tests can assert the reviewer saw
// the attempt they accepted.
type scriptedPrompter struct {
	actions []Action
	notes   []string
	cursor  int
	noteIdx int

	shown     []string
	shownIdx  []int
	shownTot  []int
	shownCaps []GateCapabilities
	notified  []string
}

func (s *scriptedPrompter) Show(a Attempt, index, total int, caps GateCapabilities) (Action, error) {
	s.shown = append(s.shown, a.Content)
	s.shownIdx = append(s.shownIdx, index)
	s.shownTot = append(s.shownTot, total)
	s.shownCaps = append(s.shownCaps, caps)
	if s.cursor >= len(s.actions) {
		return ActionSkip, nil
	}
	a2 := s.actions[s.cursor]
	s.cursor++
	return a2, nil
}

func (s *scriptedPrompter) Note() (string, error) {
	if s.noteIdx >= len(s.notes) {
		return "", nil
	}
	n := s.notes[s.noteIdx]
	s.noteIdx++
	return n, nil
}

func (s *scriptedPrompter) Notify(msg string) { s.notified = append(s.notified, msg) }

// counting generator returns a distinct draft per call and records the notes and
// prior draft it was given, which is how the tests verify steering.
type countingGen struct {
	calls      int
	gotNotes   [][]string
	gotPrior   []string
	failOnCall int
	emptyOn    int
}

func (g *countingGen) fn(_ context.Context, notes []string, prior string) (*Result, error) {
	g.calls++
	g.gotNotes = append(g.gotNotes, append([]string(nil), notes...))
	g.gotPrior = append(g.gotPrior, prior)
	if g.calls == g.failOnCall {
		return nil, errors.New("provider exploded")
	}
	if g.calls == g.emptyOn {
		return &Result{Text: "  "}, nil
	}
	return &Result{
		Text:   fmt.Sprintf("draft %d", g.calls),
		Model:  "test-model",
		Tokens: adapter.TokenUsage{Total: 100 * g.calls},
	}, nil
}

func TestReview_AcceptFirstDraft(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Action != ActionAccept || out.Content != "draft 1" {
		t.Errorf("outcome = %+v, want accept of draft 1", out)
	}
	if out.RetryCount() != 0 {
		t.Errorf("RetryCount = %d, want 0", out.RetryCount())
	}
}

// Retry re-runs with identical inputs and replaces the shown draft, which is the
// whole point of the loop: a near-miss costs one keystroke.
func TestReview_RetryRegeneratesAndShowsNewDraft(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionRetry, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if gen.calls != 2 {
		t.Errorf("generator calls = %d, want 2", gen.calls)
	}
	if out.Content != "draft 2" {
		t.Errorf("accepted %q, want the regenerated draft", out.Content)
	}
	if out.RetryCount() != 1 {
		t.Errorf("RetryCount = %d, want 1", out.RetryCount())
	}
	if len(p.shown) != 2 || p.shown[1] != "draft 2" {
		t.Errorf("reviewer was shown %v, want the new draft second", p.shown)
	}
}

// The steer note must reach the generator, or retry-with-note is decorative.
func TestReview_RetryWithNotePassesSteer(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{
		actions: []Action{ActionRetryNote, ActionAccept},
		notes:   []string{"lead with the revenue impact"},
	}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(gen.gotNotes) < 2 {
		t.Fatalf("generator called %d times, want a retry", len(gen.gotNotes))
	}
	notes := gen.gotNotes[1]
	if len(notes) != 1 || notes[0] != "lead with the revenue impact" {
		t.Errorf("retry notes = %v, want the reviewer's steer", notes)
	}
	if len(out.SteerNotes) != 1 {
		t.Errorf("outcome should retain steer notes, got %v", out.SteerNotes)
	}
	// The rejected draft goes back too, so a retry refines rather than restarts.
	if gen.gotPrior[1] != "draft 1" {
		t.Errorf("prior draft = %q, want the rejected attempt", gen.gotPrior[1])
	}
}

// Notes accumulate: a second nudge does not erase the first.
func TestReview_NotesAccumulateAcrossRetries(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{
		actions: []Action{ActionRetryNote, ActionRetryNote, ActionAccept},
		notes:   []string{"be terser", "mention the queue"},
	}

	if _, err := Review(context.Background(), gen.fn, p, GateCapabilities{}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	last := gen.gotNotes[len(gen.gotNotes)-1]
	if len(last) != 2 {
		t.Fatalf("final notes = %v, want both steers", last)
	}
}

// Attempt navigation must show the right draft, and accept must write the shown
// one rather than the latest.
func TestReview_NavigateAttemptsAndAcceptShown(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionRetry, ActionPrev, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Content != "draft 1" {
		t.Errorf("accepted %q, want the attempt on screen after navigating back", out.Content)
	}
	if len(out.Attempts) != 2 {
		t.Errorf("attempts retained = %d, want 2", len(out.Attempts))
	}
}

func TestReview_NavigationClampsAtEnds(t *testing.T) {
	gen := &countingGen{}
	// Prev at the first attempt and next at the last must be no-ops, not panics.
	p := &scriptedPrompter{actions: []Action{ActionPrev, ActionNext, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Content != "draft 1" {
		t.Errorf("content = %q", out.Content)
	}
}

// Navigation is only offered once there is more than one attempt.
func TestReview_NavigationCapabilityReflectsAttemptCount(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionRetry, ActionAccept}}

	if _, err := Review(context.Background(), gen.fn, p, GateCapabilities{}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if p.shownCaps[0].CanNavigate {
		t.Error("navigation should be hidden for a single attempt")
	}
	if !p.shownCaps[1].CanNavigate {
		t.Error("navigation should be offered once a second attempt exists")
	}
}

func TestReview_SkipWritesNothing(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionSkip}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Action != ActionSkip || out.Content != "" {
		t.Errorf("skip must yield no content, got %+v", out)
	}
}

// A failed retry must not destroy the review: the reviewer still has a usable
// earlier attempt.
func TestReview_FailedRetryKeepsPreviousAttempt(t *testing.T) {
	gen := &countingGen{failOnCall: 2}
	p := &scriptedPrompter{actions: []Action{ActionRetry, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("a failed retry should not end the review: %v", err)
	}
	if out.Content != "draft 1" {
		t.Errorf("content = %q, want the surviving first draft", out.Content)
	}
	if len(p.notified) == 0 {
		t.Error("the reviewer should be told the retry failed")
	}
}

func TestReview_EmptyRetryKeepsPreviousAttempt(t *testing.T) {
	gen := &countingGen{emptyOn: 2}
	p := &scriptedPrompter{actions: []Action{ActionRetry, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Content != "draft 1" {
		t.Errorf("content = %q, want the previous attempt kept", out.Content)
	}
}

// An empty first draft has nothing to review, which is a distinct condition from
// a reviewer skipping.
func TestReview_EmptyFirstDraftIsAnError(t *testing.T) {
	gen := &countingGen{emptyOn: 1}
	p := &scriptedPrompter{actions: []Action{ActionAccept}}

	_, err := Review(context.Background(), gen.fn, p, GateCapabilities{})
	if !errors.Is(err, ErrNoDraft) {
		t.Errorf("err = %v, want ErrNoDraft", err)
	}
}

// Escalation carries the rejected draft and every steer note, so the session
// starts informed and nothing is re-explained.
func TestReview_EscalationCarriesContext(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{
		actions: []Action{ActionRetryNote, ActionEscalate},
		notes:   []string{"try a queue-based approach"},
	}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{CanEscalate: true})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Action != ActionEscalate {
		t.Fatalf("action = %v, want escalate", out.Action)
	}
	if out.Content != "draft 2" {
		t.Errorf("escalation should carry the rejected draft, got %q", out.Content)
	}
	if len(out.SteerNotes) != 1 || !strings.Contains(out.SteerNotes[0], "queue-based") {
		t.Errorf("escalation should carry steer notes, got %v", out.SteerNotes)
	}
}

// With a completion-only agent the gate must not act on an escalation it cannot
// perform.
func TestReview_EscalationRefusedWithoutSessionPlane(t *testing.T) {
	gen := &countingGen{}
	p := &scriptedPrompter{actions: []Action{ActionEscalate, ActionAccept}}

	out, err := Review(context.Background(), gen.fn, p, GateCapabilities{CanEscalate: false})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if out.Action != ActionAccept {
		t.Errorf("action = %v, want the review to continue", out.Action)
	}
	if len(p.notified) == 0 {
		t.Error("the reviewer should be told sessions are unsupported")
	}
	for _, caps := range p.shownCaps {
		if caps.CanEscalate {
			t.Error("escalation should not be advertised without a session plane")
		}
	}
}
