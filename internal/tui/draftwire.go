package tui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter/resolve"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// Wiring between the TUI and the llm package: service construction, capability
// explanations, spec input assembly, persistence, and the interactive escalation
// path.

// llmService builds the task-running service from the resolved personal agent.
//
// Constructed per call rather than cached because it is cheap and the user can
// change providers in Settings mid-session; a cached service would keep serving
// the old provider until restart.
func (a *App) llmService() *llm.Service {
	if a.rc == nil {
		return nil
	}
	agentCfg := a.rc.EffectiveAgentConfig()
	if agentCfg.Provider == "" || agentCfg.Provider == "none" {
		return nil
	}
	// resolve.Agent always returns a usable adapter (noop for an unknown or
	// unconfigured provider), so there is no nil to check — the service's
	// IsAvailable, which asks the adapter for its capabilities, is the real gate.
	agent, _ := resolve.Agent(agentCfg)
	return llm.NewService(agent, a.rc.AgentDraftsEnabled()).
		WithMaxTokens(agentCfg.Generate.MaxTokens)
}

// agentCapabilities reports what the configured agent supports, so affordances
// render only when they would work.
func (a *App) agentCapabilities() (canGenerate, canSession bool) {
	svc := a.llmService()
	if svc == nil {
		return false, false
	}
	caps := svc.Capabilities()
	return caps.Generate && a.rc.AgentDraftsEnabled(), caps.MCP
}

// agentPlaneExplanation says why drafting is unavailable, in one line.
//
// An unsupported action gets an explanation rather than silence: a key that does
// nothing is indistinguishable from a broken one.
func (a *App) agentPlaneExplanation() string {
	if a.rc == nil {
		return "no configuration loaded"
	}
	provider := a.rc.EffectiveAgentConfig().Provider
	switch {
	case provider == "" || provider == "none":
		return "no agent configured — set 'agent:' in ~/.spec/config.yaml"
	case !a.rc.AgentDraftsEnabled():
		return "agent drafting is disabled in your preferences (agent_drafts)"
	default:
		return fmt.Sprintf("%q does not support one-shot completions", provider)
	}
}

// draftInputFor assembles task input from the spec on disk.
func (a *App) draftInputFor(specID, slug string) (llm.Input, error) {
	path := resolveLocalSpecPath(a.rc, specID)
	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return llm.Input{}, fmt.Errorf("reading %s: %w", specID, err)
	}
	byslug := make(map[string]string, len(sections))
	for _, s := range sections {
		byslug[s.Slug] = s.Content
	}

	in := llm.Input{
		SpecID:   specID,
		Section:  slug,
		Sections: byslug,
		Meta:     map[string]string{},
	}
	if meta, err := markdown.ReadMeta(path); err == nil && meta != nil {
		in.Meta["title"] = meta.Title
		in.Repos = meta.Repos
	}
	return in, nil
}

// writeDraftCmd persists an accepted draft through the specs-repo sync wrapper,
// so an agent-drafted section is committed exactly like a hand-edited one.
func (a *App) writeDraftCmd(specID, slug, content string) tea.Cmd {
	rc := a.rc
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo,
			gitpkg.SyncOptions{Surface: store.SurfaceTUI, Trigger: "draft", SpecID: specID},
			func(repoPath string) (string, error) {
				specPath := filepath.Join(repoPath, gitpkg.SpecsSubDir, specID+".md")
				if err := markdown.ReplaceSection(specPath, slug, content); err != nil {
					return "", err
				}
				return fmt.Sprintf("docs: %s — agent-drafted %s", specID, slug), nil
			})

		msg := draftWrittenMsg{specID: specID, slug: slug, err: err}
		if err == nil {
			// Offer the next empty owner section so a fresh spec can be worked
			// through in a flow, without a batch mode's loss of per-section
			// review.
			msg.nextEmpty = nextEmptyOwnerSection(rc, specID, slug)
		}
		return msg
	}
}

// nextEmptyOwnerSection returns the next empty section a human is expected to
// author, or empty when none remain.
//
// Auto-maintained sections (§8 escape hatch, §11 retrospective) and the decision
// log are excluded: they are filled by commands, not by drafting, so offering
// them would be offering to write something the tool owns.
func nextEmptyOwnerSection(rc *config.ResolvedConfig, specID, after string) string {
	path := resolveLocalSpecPath(rc, specID)
	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return ""
	}

	// Must match draftHintFor's exclusions, or chaining would offer a section
	// whose hint the reader deliberately suppresses. §6 follows the QA policy of
	// being drafted only when targeted explicitly; the PR stack has its own task.
	skip := map[string]bool{
		"escape_hatch_log":    true,
		"retrospective":       true,
		"decision_log":        true,
		"pr_stack_plan":       true,
		"acceptance_criteria": true,
	}

	seenCurrent := false
	var firstEmpty string
	for _, s := range sections {
		if skip[s.Slug] {
			continue
		}
		empty := strings.TrimSpace(s.Content) == ""
		if s.Slug == after {
			seenCurrent = true
			continue
		}
		if !empty {
			continue
		}
		// Prefer the next empty section after the one just written, so drafting
		// moves forward through the document rather than jumping back.
		if seenCurrent {
			return s.Slug
		}
		if firstEmpty == "" {
			firstEmpty = s.Slug
		}
	}
	return firstEmpty
}

// recordDraftOutcome logs one review to the activity log.
//
// Best-effort: telemetry must never interfere with a draft the user accepted.
func (a *App) recordDraftOutcome(action llm.Action) {
	if a.db == nil || a.draft.specID == "" {
		return
	}
	outcome := &llm.Outcome{
		Action:     action,
		Attempts:   a.draft.attempts,
		SteerNotes: a.draft.notes,
	}
	summary := fmt.Sprintf("agent draft %s: %s", a.draft.taskID, action)
	if r := outcome.RetryCount(); r > 0 {
		summary += fmt.Sprintf(" after %d retries", r)
	}
	user := ""
	if a.rc != nil {
		user = a.rc.UserName()
	}
	_ = a.db.ActivityLogAs(a.draft.specID, "agent_generate", summary, "", user, store.ActorHuman)
}

// interactiveKickoff builds the prompt that opens an escalated session.
//
// It carries the spec, the target section, the rejected draft, and every steer
// note, because the first thing a user would otherwise type at a blank prompt is
// something spec already knew.
func interactiveKickoff(specID, slug, rejected string, notes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on %s", specID)
	if slug != "" {
		fmt.Fprintf(&b, ", section §%s", slug)
	}
	b.WriteString(".\n\n")
	b.WriteString("Read the spec through the MCP tools, then write the section with spec_section_write ")
	b.WriteString("(pass the content_hash from spec_section_read so a concurrent edit is not clobbered).\n")

	if strings.TrimSpace(rejected) != "" {
		b.WriteString("\nA one-shot draft was rejected. Do not simply repeat it:\n\n")
		b.WriteString(rejected)
		b.WriteString("\n")
	}
	if len(notes) > 0 {
		b.WriteString("\nReviewer feedback on that draft:\n")
		for i, n := range notes {
			fmt.Fprintf(&b, "%d. %s\n", i+1, n)
		}
	}
	return b.String()
}

// interactiveDraftSession suspends the TUI and launches an authoring session.
//
// It reuses the build path's suspend-and-hold approach: the session inherits the
// terminal, and the shell pauses on exit so a fast failure (missing binary,
// immediate error) stays readable instead of flashing past as the TUI resumes.
func interactiveDraftSession(rc *config.ResolvedConfig, specID, kickoff string) tea.Cmd {
	if rc == nil {
		return cmdResult("draft-interactive", specID, fmt.Errorf("no configuration loaded"))
	}
	return tea.ExecProcess(
		exec.CommandContext(context.Background(), "sh", "-c", interactiveHoldScript, "sh", specID, kickoff),
		func(err error) tea.Msg {
			return actionResultMsg{Action: "draft-interactive", SpecID: specID, Err: err}
		},
	)
}

// interactiveHoldScript runs an interactive authoring session and waits for Enter
// so its output survives the TUI resume. The spec id and kickoff prompt are argv
// parameters ($1, $2), never interpolated into the shell string, so they stay
// injection-safe.
const interactiveHoldScript = `spec draft "$1" --interactive --kickoff "$2"; code=$?; printf '\n[exit %s] press Enter to return to spec…' "$code"; read _`

// draftTargetSection returns the section the draft keys act on: the reader
// cursor when the reader is open, otherwise empty so the caller can explain
// rather than guess.
func (a *App) draftTargetSection() string {
	if a.showDetail && a.detail.readerMode {
		return a.detail.currentSectionSlug()
	}
	return ""
}

// handleDraftWritten reports the outcome of persisting an accepted draft and
// refreshes the reader so the new content is visible immediately.
//
// On success it offers the next empty owner section, which is what turns a fresh
// spec into a flow rather than a series of separate invocations — while keeping
// per-section review, unlike a batch mode.
func (a App) handleDraftWritten(msg draftWrittenMsg) (App, tea.Cmd) {
	if msg.err != nil {
		a.statusBar.SetStatusError("Draft write failed", msg.err.Error())
		return a, nil
	}

	a.statusBar.SetStatusSuccess("Wrote §"+msg.slug, 2*time.Second)

	cmds := []tea.Cmd{a.detail.fetchData()}
	if msg.nextEmpty != "" {
		// The slug travels in pendingSpecID's companion field via the action
		// name, so no new App field is needed for a one-shot confirmation.
		a.pendingAction = "draft-next:" + msg.nextEmpty
		a.pendingSpecID = msg.specID
		a.modal.ShowConfirm("Draft next section",
			fmt.Sprintf("§%s is still empty. Draft it now?", msg.nextEmpty))
		a.modal.SetSize(a.width, a.contentHeight())
	}
	return a, tea.Batch(cmds...)
}
