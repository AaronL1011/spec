package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
)

// The draft-review modal is the TUI renderer of the same review-gate states the
// CLI prompt renders, so the loop is learned once. Its action set is fixed at
// {accept, edit, retry, retry+note, escalate, skip} plus attempt navigation —
// anything richer belongs in $EDITOR or an escalated session, which is what keeps
// this from growing into a mini-editor.
//
// Two properties matter more than the rendering:
//   - nothing is written before accept, so a skip at any point leaves the spec
//     untouched;
//   - a pending generation shows elapsed time and is genuinely cancellable,
//     because a local model can take tens of seconds and the user must be able
//     to tell progress from a hang.

// draftState is the modal's lifecycle.
type draftState int

const (
	// draftIdle means no draft flow is active.
	draftIdle draftState = iota
	// draftGenerating means a completion is in flight.
	draftGenerating
	// draftReviewing means an attempt is on screen awaiting a decision.
	draftReviewing
	// draftSteering means the reviewer is typing a steer note.
	draftSteering
)

// draftSession holds one review's state, including every attempt so the reviewer
// can navigate between them before accepting.
type draftSession struct {
	state    draftState
	specID   string
	slug     string
	taskID   string
	title    string
	attempts []llm.Attempt
	cursor   int
	notes    []string

	// startedAt drives the elapsed-time counter. A bare spinner cannot
	// distinguish a slow local model from a hung one.
	startedAt time.Time
	elapsed   time.Duration

	// cancel aborts the in-flight generation. Esc must actually kill the work
	// (and its process group), not merely stop showing it.
	cancel context.CancelFunc

	// steerInput accumulates the note being typed.
	steerInput string

	// lastErr is a non-fatal failure shown in the modal, so a failed retry does
	// not discard the attempts already gathered.
	lastErr string
}

// active reports whether a draft flow owns the keyboard.
func (d draftSession) active() bool { return d.state != draftIdle }

// current returns the attempt on screen.
func (d draftSession) current() (llm.Attempt, bool) {
	if d.cursor < 0 || d.cursor >= len(d.attempts) {
		return llm.Attempt{}, false
	}
	return d.attempts[d.cursor], true
}

// --- messages ---

// draftResultMsg carries a finished generation back to the update loop.
type draftResultMsg struct {
	specID string
	slug   string
	result *llm.Result
	err    error
	// note is the steer that produced this attempt, empty for a first attempt.
	note string
}

// draftTickMsg advances the elapsed-time counter while generating.
type draftTickMsg time.Time

// draftWrittenMsg reports the outcome of persisting an accepted draft.
type draftWrittenMsg struct {
	specID string
	slug   string
	err    error
	// nextEmpty names the next empty owner section, so the modal can offer
	// chaining without a batch mode's loss of per-section review.
	nextEmpty string
}

// draftTickInterval is how often the elapsed counter refreshes. Fast enough to
// read as live, slow enough not to churn the render loop.
const draftTickInterval = 250 * time.Millisecond

func draftTick() tea.Cmd {
	return tea.Tick(draftTickInterval, func(t time.Time) tea.Msg { return draftTickMsg(t) })
}

// --- starting a draft ---

// startSectionDraft begins a draft for one section, or returns an explanatory
// command when the configured agent cannot do it.
//
// Capability is checked before anything is shown: offering an action that then
// fails is worse than not offering it, so this is the same gate that hides the
// `d` hint.
func (a *App) startSectionDraft(specID, slug string) tea.Cmd {
	svc := a.llmService()
	if svc == nil || !svc.IsAvailable() {
		a.statusBar.SetStatusError("Drafting unavailable", a.agentPlaneExplanation())
		return nil
	}
	slug = a.resolveDraftTarget(specID, slug)
	if slug == "" {
		a.statusBar.SetStatusError("Nothing to draft", "every owner section already has content — open the reader (o) to work on one")
		return nil
	}

	task, err := tasks.Get(tasks.DraftSection)
	if err != nil {
		a.statusBar.SetStatusError("Drafting unavailable", err.Error())
		return nil
	}

	a.draft = draftSession{
		state:     draftGenerating,
		specID:    specID,
		slug:      slug,
		taskID:    task.ID,
		title:     sectionTitle(slug),
		startedAt: time.Now(),
	}
	return tea.Batch(a.generateDraftCmd(nil, ""), draftTick())
}

// generateDraftCmd runs one generation off the update loop.
//
// The context is stored on the session so Esc can cancel it; cancellation
// propagates to the harness process group, so stopping the modal actually stops
// the work rather than orphaning it.
func (a *App) generateDraftCmd(notes []string, prior string) tea.Cmd {
	svc := a.llmService()
	if svc == nil {
		return nil
	}
	task, err := tasks.Get(a.draft.taskID)
	if err != nil {
		return func() tea.Msg {
			return draftResultMsg{specID: a.draft.specID, slug: a.draft.slug, err: err}
		}
	}

	in, err := a.draftInputFor(a.draft.specID, a.draft.slug)
	if err != nil {
		return func() tea.Msg {
			return draftResultMsg{specID: a.draft.specID, slug: a.draft.slug, err: err}
		}
	}
	in.SteerNotes = notes
	in.PriorDraft = prior

	ctx, cancel := context.WithCancel(context.Background())
	a.draft.cancel = cancel

	specID, slug := a.draft.specID, a.draft.slug
	note := ""
	if len(notes) > 0 {
		note = notes[len(notes)-1]
	}

	return func() tea.Msg {
		res, err := svc.Run(ctx, task, in)
		return draftResultMsg{specID: specID, slug: slug, result: res, err: err, note: note}
	}
}

// --- update ---

// updateDraft handles keys while a draft flow is active.
func (a App) updateDraft(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch a.draft.state {
	case draftGenerating:
		// Only cancellation is meaningful mid-flight; every other key would
		// imply acting on a draft that does not exist yet.
		if msg.String() == "esc" {
			a.cancelDraft()
			a.statusBar.SetStatusSuccess("Draft cancelled", 2*time.Second)
			return a, nil
		}
		return a, nil

	case draftSteering:
		return a.updateDraftSteer(msg)

	case draftReviewing:
		return a.updateDraftReview(msg)
	}
	return a, nil
}

// updateDraftSteer collects a one-line steer note.
func (a App) updateDraftSteer(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Abandoning the note returns to the draft rather than discarding it.
		a.draft.state = draftReviewing
		a.draft.steerInput = ""
		return a, nil
	case "enter":
		note := strings.TrimSpace(a.draft.steerInput)
		a.draft.steerInput = ""
		if note == "" {
			a.draft.state = draftReviewing
			return a, nil
		}
		a.draft.notes = append(a.draft.notes, note)
		return a.regenerate()
	case "backspace":
		if n := len(a.draft.steerInput); n > 0 {
			a.draft.steerInput = a.draft.steerInput[:n-1]
		}
		return a, nil
	default:
		if msg.Text != "" {
			a.draft.steerInput += msg.Text
		}
		return a, nil
	}
}

// updateDraftReview handles the fixed action set on a shown attempt.
func (a App) updateDraftReview(msg tea.KeyPressMsg) (App, tea.Cmd) {
	// Case matters: r and R are different actions, mirroring the CLI gate.
	switch msg.String() {
	case "esc":
		a.closeDraft()
		a.statusBar.SetStatusSuccess("Draft skipped", 2*time.Second)
		return a, nil
	case "[":
		if a.draft.cursor > 0 {
			a.draft.cursor--
		}
		return a, nil
	case "]":
		if a.draft.cursor < len(a.draft.attempts)-1 {
			a.draft.cursor++
		}
		return a, nil
	}

	switch msg.Text {
	case "a", "y":
		return a.acceptDraft()
	case "s":
		a.closeDraft()
		a.statusBar.SetStatusSuccess("Draft skipped", 2*time.Second)
		return a, nil
	case "r":
		return a.regenerate()
	case "R":
		a.draft.state = draftSteering
		return a, nil
	case "e":
		return a.editDraft()
	case "i":
		return a.escalateDraft()
	}
	return a, nil
}

// regenerate re-runs the task, carrying accumulated notes and the rejected draft.
func (a App) regenerate() (App, tea.Cmd) {
	prior := ""
	if att, ok := a.draft.current(); ok {
		prior = att.Content
	}
	a.draft.state = draftGenerating
	a.draft.startedAt = time.Now()
	a.draft.elapsed = 0
	a.draft.lastErr = ""
	return a, tea.Batch(a.generateDraftCmd(a.draft.notes, prior), draftTick())
}

// editDraft suspends the TUI into $EDITOR and returns with the edited content.
func (a App) editDraft() (App, tea.Cmd) {
	att, ok := a.draft.current()
	if !ok {
		return a, nil
	}
	editor := ""
	if a.rc != nil && a.rc.User != nil {
		editor = a.rc.User.Preferences.Editor
	}
	cursor := a.draft.cursor

	return a, func() tea.Msg {
		edited, err := llm.EditInEditor(att.Content, editor)
		if err != nil {
			return draftEditedMsg{cursor: cursor, err: err}
		}
		return draftEditedMsg{cursor: cursor, content: edited}
	}
}

// draftEditedMsg returns an $EDITOR round-trip to the update loop.
type draftEditedMsg struct {
	cursor  int
	content string
	err     error
}

// acceptDraft writes the shown attempt through the markdown engine.
func (a App) acceptDraft() (App, tea.Cmd) {
	att, ok := a.draft.current()
	if !ok {
		return a, nil
	}
	specID, slug := a.draft.specID, a.draft.slug
	content := att.Content

	// Telemetry records the review before the write, so a skip-after-retries is
	// counted even if persistence then fails.
	a.recordDraftOutcome(llm.ActionAccept)

	a.closeDraft()
	return a, a.writeDraftCmd(specID, slug, content)
}

// escalateDraft hands off to an interactive session carrying the rejected draft
// and steer notes, so nothing is re-explained.
func (a App) escalateDraft() (App, tea.Cmd) {
	svc := a.llmService()
	if svc == nil || !svc.Capabilities().MCP {
		a.statusBar.SetStatusError("Interactive unavailable", "this agent does not support sessions")
		return a, nil
	}

	att, _ := a.draft.current()
	kickoff := interactiveKickoff(a.draft.specID, a.draft.slug, att.Content, a.draft.notes)
	specID := a.draft.specID

	a.recordDraftOutcome(llm.ActionEscalate)
	a.closeDraft()
	return a, interactiveDraftSession(a.rc, specID, kickoff)
}

// cancelDraft aborts an in-flight generation and closes the modal.
func (a *App) cancelDraft() {
	if a.draft.cancel != nil {
		a.draft.cancel()
	}
	a.draft = draftSession{}
}

// closeDraft ends the flow, releasing any generation context.
func (a *App) closeDraft() {
	if a.draft.cancel != nil {
		a.draft.cancel()
	}
	a.draft = draftSession{}
}

// handleDraftResult folds a finished generation into the session.
func (a App) handleDraftResult(msg draftResultMsg) (App, tea.Cmd) {
	// A result for a different spec/section is stale (the user moved on).
	if !a.draft.active() || msg.specID != a.draft.specID || msg.slug != a.draft.slug {
		return a, nil
	}
	a.draft.cancel = nil

	if msg.err != nil {
		// A failed first attempt has nothing to fall back to; a failed retry
		// keeps the attempts already gathered.
		if len(a.draft.attempts) == 0 {
			a.closeDraft()
			a.statusBar.SetStatusError("Draft failed", msg.err.Error())
			return a, nil
		}
		a.draft.state = draftReviewing
		a.draft.lastErr = msg.err.Error()
		return a, nil
	}

	if msg.result == nil || strings.TrimSpace(msg.result.Text) == "" {
		if len(a.draft.attempts) == 0 {
			a.closeDraft()
			a.statusBar.SetStatusError("Draft failed", "the agent returned an empty draft")
			return a, nil
		}
		a.draft.state = draftReviewing
		a.draft.lastErr = "the agent returned an empty draft; keeping the previous attempt"
		return a, nil
	}

	a.draft.attempts = append(a.draft.attempts, llm.Attempt{
		Content: msg.result.Text,
		Result:  msg.result,
		Note:    msg.note,
	})
	a.draft.cursor = len(a.draft.attempts) - 1
	a.draft.state = draftReviewing
	a.draft.lastErr = ""
	return a, nil
}

// handleDraftTick advances the elapsed counter while a generation is in flight.
func (a App) handleDraftTick() (App, tea.Cmd) {
	if a.draft.state != draftGenerating {
		return a, nil
	}
	a.draft.elapsed = time.Since(a.draft.startedAt)
	return a, draftTick()
}

// handleDraftEdited applies an $EDITOR round-trip.
func (a App) handleDraftEdited(msg draftEditedMsg) (App, tea.Cmd) {
	if !a.draft.active() {
		return a, nil
	}
	if msg.err != nil {
		// A failed editor must not lose the draft.
		a.draft.lastErr = fmt.Sprintf("editor failed: %v", msg.err)
		a.draft.state = draftReviewing
		return a, nil
	}
	if msg.cursor >= 0 && msg.cursor < len(a.draft.attempts) {
		a.draft.attempts[msg.cursor].Content = msg.content
		a.draft.attempts[msg.cursor].Edited = true
	}
	a.draft.state = draftReviewing
	return a, nil
}

// sectionTitle renders a slug as a modal title.
func sectionTitle(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "_", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
