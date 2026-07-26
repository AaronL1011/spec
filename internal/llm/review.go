package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The review gate is a loop, not a verdict.
//
// The most common reaction to an LLM draft is neither accept nor reject but
// "again, with a nudge". A verdict-only gate makes every near-miss cost a full
// restart with no way to steer, which is the difference between a feature tried
// once and one used daily.
//
// The loop stays bounded on purpose: the action set is fixed, every regeneration
// is an explicit keystroke (there is no auto-retry), nothing is written before
// accept, and anything richer than a one-line steer belongs in $EDITOR or an
// escalated interactive session.
//
// This file is the state machine. The CLI prompt and the TUI modal are two
// renderers of the same states, so the interaction is learned once.

// Action is a reviewer's decision about the shown draft.
type Action string

const (
	// ActionAccept writes the shown attempt.
	ActionAccept Action = "accept"
	// ActionEdit opens the shown attempt in $EDITOR, then returns to the gate.
	ActionEdit Action = "edit"
	// ActionRetry regenerates with identical inputs.
	ActionRetry Action = "retry"
	// ActionRetryNote regenerates with an added reviewer instruction.
	ActionRetryNote Action = "retry_note"
	// ActionEscalate hands off to an interactive session, carrying context.
	ActionEscalate Action = "escalate"
	// ActionSkip abandons the review, writing nothing.
	ActionSkip Action = "skip"
	// ActionPrev shows the previous attempt.
	ActionPrev Action = "prev"
	// ActionNext shows the next attempt.
	ActionNext Action = "next"
)

// Attempt is one generated draft within a review, retained so the reviewer can
// navigate between attempts and compare before accepting.
type Attempt struct {
	Content string
	Result  *Result
	// Note is the steer that produced this attempt, empty for the first.
	Note string
	// Edited marks an attempt modified in $EDITOR, so the status line can say
	// the content is no longer exactly what the model returned.
	Edited bool
}

// Outcome is how a review ended.
type Outcome struct {
	Action Action
	// Content is the accepted text. Empty unless Action is ActionAccept.
	Content string
	// Attempts is every draft generated during the review, for telemetry:
	// retry count is the signal that a task is underperforming.
	Attempts []Attempt
	// SteerNotes is every note the reviewer added, carried into an escalated
	// session so nothing is re-explained.
	SteerNotes []string
}

// RetryCount reports how many regenerations the reviewer asked for.
func (o Outcome) RetryCount() int {
	if len(o.Attempts) == 0 {
		return 0
	}
	return len(o.Attempts) - 1
}

// Prompter renders the gate and collects the reviewer's decision. The CLI
// implements it with a terminal prompt; the TUI implements it with a modal.
// Keeping it an interface is what stops the two surfaces from drifting into
// different action sets.
type Prompter interface {
	// Show presents an attempt and returns the chosen action. index and total
	// let the renderer show "attempt 2 of 3".
	Show(a Attempt, index, total int, caps GateCapabilities) (Action, error)
	// Note collects a one-line steer for ActionRetryNote.
	Note() (string, error)
	// Notify reports a non-fatal condition (an empty draft, a failed attempt)
	// without ending the review.
	Notify(message string)
}

// GateCapabilities tells a renderer which actions to offer. Escalation is only
// shown when the agent actually has a session plane, so the gate never advertises
// an action that would fail.
type GateCapabilities struct {
	// CanEscalate reports whether an interactive session is possible.
	CanEscalate bool
	// CanNavigate reports whether more than one attempt exists.
	CanNavigate bool
}

// Generator produces one draft. The gate calls it for the first attempt and
// again for every retry, passing accumulated steer notes.
type Generator func(ctx context.Context, notes []string, priorDraft string) (*Result, error)

// ErrNoDraft reports that generation produced nothing usable on the first
// attempt, so there is nothing to review.
var ErrNoDraft = errors.New("provider returned an empty draft")

// Review runs the gate loop until the reviewer accepts, skips, or escalates.
//
// Nothing is written here: Review returns the accepted content and the caller
// persists it. That separation is what makes the gate identical for a section
// write, a PR description, and a standup.
func Review(ctx context.Context, gen Generator, p Prompter, caps GateCapabilities) (*Outcome, error) {
	var (
		attempts []Attempt
		notes    []string
	)

	// First attempt.
	res, err := gen(ctx, nil, "")
	if err != nil {
		return nil, err
	}
	if res == nil || strings.TrimSpace(res.Text) == "" {
		return nil, ErrNoDraft
	}
	attempts = append(attempts, Attempt{Content: res.Text, Result: res})
	cursor := 0

	for {
		gateCaps := GateCapabilities{
			CanEscalate: caps.CanEscalate,
			CanNavigate: len(attempts) > 1,
		}
		action, err := p.Show(attempts[cursor], cursor, len(attempts), gateCaps)
		if err != nil {
			return nil, err
		}

		switch action {
		case ActionAccept:
			return &Outcome{
				Action:     ActionAccept,
				Content:    attempts[cursor].Content,
				Attempts:   attempts,
				SteerNotes: notes,
			}, nil

		case ActionSkip:
			return &Outcome{Action: ActionSkip, Attempts: attempts, SteerNotes: notes}, nil

		case ActionEscalate:
			if !caps.CanEscalate {
				p.Notify("this agent does not support sessions")
				continue
			}
			return &Outcome{
				Action: ActionEscalate,
				// Carry the rejected draft so the session starts informed.
				Content:    attempts[cursor].Content,
				Attempts:   attempts,
				SteerNotes: notes,
			}, nil

		case ActionEdit:
			edited, err := EditInEditor(attempts[cursor].Content, "")
			if err != nil {
				// A failed editor must not lose the draft: report and stay.
				p.Notify(fmt.Sprintf("editor failed: %v", err))
				continue
			}
			attempts[cursor].Content = edited
			attempts[cursor].Edited = true

		case ActionRetry, ActionRetryNote:
			if action == ActionRetryNote {
				note, err := p.Note()
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(note) != "" {
					notes = append(notes, note)
				}
			}
			// The rejected draft goes back to the model so a retry refines
			// rather than starting blind.
			next, err := gen(ctx, notes, attempts[cursor].Content)
			if err != nil {
				// A failed retry keeps the review alive: the reviewer still has
				// a usable earlier attempt.
				p.Notify(fmt.Sprintf("regeneration failed: %v", err))
				continue
			}
			if next == nil || strings.TrimSpace(next.Text) == "" {
				p.Notify("provider returned an empty draft; keeping the previous attempt")
				continue
			}
			lastNote := ""
			if len(notes) > 0 {
				lastNote = notes[len(notes)-1]
			}
			attempts = append(attempts, Attempt{Content: next.Text, Result: next, Note: lastNote})
			cursor = len(attempts) - 1

		case ActionPrev:
			if cursor > 0 {
				cursor--
			}

		case ActionNext:
			if cursor < len(attempts)-1 {
				cursor++
			}

		default:
			p.Notify(fmt.Sprintf("unknown action %q", action))
		}
	}
}
