// Package llm owns every LLM-backed feature in spec: the declared task
// registry, deterministic prompt assembly, and the human review gate.
//
// Two rules shape this package. First, it never writes to stdout or stderr —
// the service returns errors and callers decide how to degrade, which is what
// makes the same task usable from the CLI, the TUI, and a test. Second, prompt
// assembly is pure: a Task turns inputs into a request with no I/O, so every
// task is golden-testable and a prompt change is visible in a diff.
package llm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Task declares one LLM feature: what to ask, how to assemble context, and how
// much of it the model may receive. Adding a feature is one Task plus one golden
// test, not a bespoke prompt-and-plumbing stack.
//
// Tasks are plain data and pure functions. There is deliberately no plugin
// machinery: a handful of prompts does not need a framework.
type Task struct {
	// ID is the stable task identifier used in telemetry and budgets
	// (e.g. "draft-section"). It appears in the activity log, so it is part of
	// the tool's observable surface and should not change casually.
	ID string
	// Title is the human label shown in the review gate ("Problem Statement").
	Title string
	// System is the system prompt. Task-specific, because "draft a section" and
	// "propose a PR stack" want different personas.
	System string
	// TokenBudget caps assembled context in approximate tokens. 0 means no cap.
	TokenBudget int
	// Format selects markdown (default) or JSON output.
	Format adapter.OutputFormat
	// Build turns typed inputs into the user prompt and labelled context parts.
	// It must be pure: no file reads, no network, no clock.
	Build func(in Input) (prompt string, parts []ContextPart)
}

// Input carries everything a task's Build may read. It is a single struct rather
// than per-task types so the registry stays uniform and callers assemble inputs
// the same way for every task.
type Input struct {
	// SpecID is the target spec (e.g. "SPEC-034"), when there is one.
	SpecID string
	// Section is the target section slug, for section-scoped tasks.
	Section string
	// Sections holds existing spec content keyed by slug, used as context.
	Sections map[string]string
	// Meta carries frontmatter-derived values (title, repos, priority).
	Meta map[string]string
	// Repos lists target repositories, for planning tasks.
	Repos []string
	// Diff is a unified diff, for PR-description tasks.
	Diff string
	// Extra carries task-specific free-form values.
	Extra map[string]string
	// SteerNotes are reviewer instructions from retry-with-note. They are
	// appended as a maximum-weight context part so budgeting never drops the
	// one thing the user explicitly asked for.
	SteerNotes []string
	// PriorDraft is the rejected attempt, included when escalating or retrying
	// so the model can see what missed rather than starting blind.
	PriorDraft string
}

// ContextPart is a labelled block of context with a trimming weight. It mirrors
// adapter.ContextPart but is declared here because weight is an assembly
// concern: adapters render what they are given and do no trimming.
type ContextPart struct {
	Label   string
	Content string
	// Weight orders trimming. Lower weights are dropped first; ties break by
	// declaration order, so trimming is deterministic and testable.
	Weight int
}

// Context part weights. Named rather than inline so a task author picks an
// intent, not a magic number.
const (
	// WeightBackground is nice-to-have context, dropped first.
	WeightBackground = 10
	// WeightSupporting is useful context: related sections, metadata.
	WeightSupporting = 20
	// WeightPrimary is the material the task is fundamentally about.
	WeightPrimary = 30
	// WeightCritical never trims in practice. Reserved for reviewer steer
	// notes: dropping the user's explicit instruction to fit a budget would be
	// the one trim that is never acceptable.
	WeightCritical = 100
)

// SteerLabel is the context-part label carrying reviewer feedback. Asserted by
// golden test, so it is a constant rather than a string literal per task.
const SteerLabel = "Reviewer feedback"

// PriorDraftLabel is the context-part label carrying a rejected attempt.
const PriorDraftLabel = "Previous attempt (rejected)"

// Request is a fully assembled, provider-agnostic generation request. It is the
// golden-test surface: given fixed inputs, a task must produce a byte-identical
// Request.
type Request struct {
	Task    string
	Title   string
	System  string
	Prompt  string
	Parts   []ContextPart
	Format  adapter.OutputFormat
	Trimmed []string
}

// Assemble turns a task and its inputs into a Request, applying the task's
// context budget deterministically.
//
// Steer notes and the prior draft are appended here rather than in each task's
// Build, so every task supports retry-with-note identically and no task author
// can forget to honour it.
func Assemble(t Task, in Input) Request {
	prompt, parts := "", []ContextPart(nil)
	if t.Build != nil {
		prompt, parts = t.Build(in)
	}

	if draft := strings.TrimSpace(in.PriorDraft); draft != "" {
		parts = append(parts, ContextPart{
			Label:   PriorDraftLabel,
			Content: draft,
			Weight:  WeightPrimary,
		})
	}
	// Steer notes carry maximum weight: the reviewer asked for this explicitly,
	// so it must outlive every other part under a tight budget.
	if note := joinNotes(in.SteerNotes); note != "" {
		parts = append(parts, ContextPart{
			Label:   SteerLabel,
			Content: note,
			Weight:  WeightCritical,
		})
	}

	kept, trimmed := applyBudget(parts, t.TokenBudget)

	return Request{
		Task:    t.ID,
		Title:   t.Title,
		System:  t.System,
		Prompt:  prompt,
		Parts:   kept,
		Format:  t.Format,
		Trimmed: trimmed,
	}
}

// joinNotes renders accumulated steer notes as a numbered list when there is
// more than one, so a sequence of nudges reads as a history rather than a blur.
func joinNotes(notes []string) string {
	cleaned := make([]string, 0, len(notes))
	for _, n := range notes {
		if s := strings.TrimSpace(n); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	switch len(cleaned) {
	case 0:
		return ""
	case 1:
		return cleaned[0]
	default:
		var sb strings.Builder
		for i, n := range cleaned {
			if i > 0 {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "%d. %s", i+1, n)
		}
		return sb.String()
	}
}

// applyBudget drops the lowest-weight parts until the estimate fits, returning
// the kept parts in their original declaration order plus the labels dropped.
//
// Determinism is the point: ascending weight is dropped first and ties break by
// declaration order, so the same inputs always trim the same way and a golden
// test can assert it.
func applyBudget(parts []ContextPart, budget int) (kept []ContextPart, trimmed []string) {
	if budget <= 0 || len(parts) == 0 {
		return parts, nil
	}

	total := 0
	for _, p := range parts {
		total += estimateTokens(p.Content)
	}
	if total <= budget {
		return parts, nil
	}

	// Order candidates for removal: lowest weight first, then latest declared,
	// so a tie removes the part a task author added last.
	order := make([]int, len(parts))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := parts[order[a]], parts[order[b]]
		if pa.Weight != pb.Weight {
			return pa.Weight < pb.Weight
		}
		return order[a] > order[b]
	})

	drop := make(map[int]bool)
	for _, idx := range order {
		if total <= budget {
			break
		}
		// WeightCritical parts are never dropped, even when the budget cannot be
		// met. Reviewer steer notes live here: silently discarding the one
		// instruction the user typed, to satisfy a size estimate, would be worse
		// than overshooting the budget. Since candidates are ordered by ascending
		// weight, reaching a critical part means everything droppable is gone.
		if parts[idx].Weight >= WeightCritical {
			break
		}
		drop[idx] = true
		trimmed = append(trimmed, parts[idx].Label)
		total -= estimateTokens(parts[idx].Content)
	}

	for i, p := range parts {
		if !drop[i] {
			kept = append(kept, p)
		}
	}
	// Report trimming in declaration order for a stable, readable notice.
	sort.Strings(trimmed)
	return kept, trimmed
}

// charsPerToken approximates tokenisation. A real tokeniser would vary per
// provider and pull a dependency for a budget that only needs to be roughly
// right; four characters per token is the standard rule of thumb.
const charsPerToken = 4

// estimateTokens approximates the token cost of a string.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + charsPerToken - 1) / charsPerToken
}

// GenerateRequest converts an assembled Request into the adapter-level request.
//
// When context was trimmed, the prompt says so: a model that silently received
// less than the task promised would be misled about what it was given, and a
// user reading a thin draft deserves to know why.
func (r Request) GenerateRequest(maxTokens int) adapter.GenerateRequest {
	prompt := r.Prompt
	if len(r.Trimmed) > 0 {
		prompt += fmt.Sprintf(
			"\n\nNote: some context was omitted to fit the size budget (%s). Work from what is provided and say so if something essential is missing.",
			strings.Join(r.Trimmed, ", "))
	}

	parts := make([]adapter.ContextPart, 0, len(r.Parts))
	for _, p := range r.Parts {
		parts = append(parts, adapter.ContextPart{
			Label:   p.Label,
			Content: p.Content,
			Weight:  p.Weight,
		})
	}

	return adapter.GenerateRequest{
		Task:      r.Task,
		System:    r.System,
		Prompt:    prompt,
		Context:   parts,
		MaxTokens: maxTokens,
		Format:    r.Format,
	}
}

// String renders the assembled request as the exact text a provider receives.
// Golden tests compare against this, so a prompt change shows up as a reviewable
// diff rather than an invisible behaviour change.
func (r Request) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TASK: %s\n", r.Task)
	fmt.Fprintf(&sb, "SYSTEM: %s\n", r.System)
	sb.WriteString("---\n")
	sb.WriteString(r.GenerateRequest(0).Prompt)
	for _, p := range r.Parts {
		fmt.Fprintf(&sb, "\n\n## %s\n\n%s", p.Label, p.Content)
	}
	sb.WriteString("\n")
	return sb.String()
}
