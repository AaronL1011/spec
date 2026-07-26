package tasks

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/llm"
)

// The fast-follow task set. These exist to prove the registry's claim: adding a
// capability is a table row and a golden file, not a new code path. Each one
// reuses the same review gate, budgeting, and telemetry as the shipped tasks.

const (
	// ReviseSection rewrites an existing section against reviewer feedback,
	// rather than drafting an empty one from scratch.
	ReviseSection = "revise-section"
	// ReviewPlan critiques a PR stack plan before anyone builds it.
	ReviewPlan = "review-plan"
	// SummariseActivity turns an activity log into a readable progress note.
	SummariseActivity = "summarise-activity"
	// ExtractDecision recovers a decision record from discussion prose.
	ExtractDecision = "extract-decision"
)

// --- revise-section ---

// Revision is distinct from drafting: the existing text is the subject, not
// context to be balanced against everything else. A reviewer asking for "one
// less paragraph" wants the paragraph they have edited, not a fresh attempt.
var reviseSectionTask = llm.Task{
	ID:    ReviseSection,
	Title: "Section revision",
	System: "You are a senior engineer revising a section of a software specification. " +
		"Preserve the author's voice, structure, and any decisions already recorded; " +
		"change only what the feedback asks for. " +
		"Do not rewrite from scratch, do not add meta-commentary, and do not silently drop content " +
		"the feedback did not mention. Output the full revised section body in markdown.",
	TokenBudget: sectionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		label := humanizeSlug(in.Section)
		prompt := fmt.Sprintf(
			"Revise the %q section of %s according to the feedback.\n\n"+
				"Return the complete revised section body, not a diff or a description of changes.",
			label, specLabel(in))

		// The current text is critical: a revision that loses it has failed,
		// so it must never be dropped by budgeting.
		parts := []llm.ContextPart{{
			Label:   "Current section (revise this)",
			Content: in.Sections[in.Section],
			Weight:  llm.WeightCritical,
		}}
		// Surrounding sections stay useful but are droppable.
		parts = append(parts, specContextParts(in, in.Section)...)
		return prompt, parts
	},
}

// --- review-plan ---

// Reviewing a plan before building is cheaper than discovering a bad split
// halfway through a stack, which is the failure this targets.
var reviewPlanTask = llm.Task{
	ID:    ReviewPlan,
	Title: "Plan review",
	System: "You are a staff engineer reviewing a proposed PR stack plan before any code is written. " +
		"Look for dependency mistakes, PRs that cannot compile or pass tests on their own, " +
		"steps that bundle unrelated changes, and missing work the spec implies. " +
		"Be specific about which step is wrong and why. " +
		"If the plan is sound, say so briefly rather than inventing objections.",
	TokenBudget: planningBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		prompt := fmt.Sprintf(
			"Review the PR stack plan for %s.\n\n"+
				"Output '## Risks' as a bullet list of concrete problems, each naming the step it applies to, "+
				"then '## Suggestions' with the smallest changes that would fix them. "+
				"Do not restate the plan.",
			specLabel(in))

		parts := []llm.ContextPart{{
			Label:   "Proposed plan (review this)",
			Content: in.Sections["pr_stack_plan"],
			Weight:  llm.WeightCritical,
		}}
		if len(in.Repos) > 0 {
			parts = append(parts, llm.ContextPart{
				Label:   "Target repositories",
				Content: strings.Join(in.Repos, "\n"),
				Weight:  llm.WeightSupporting,
			})
		}
		parts = append(parts, specContextParts(in, "pr_stack_plan")...)
		return prompt, parts
	},
}

// --- summarise-activity ---

// The activity log is complete but unreadable at standup length; this is the
// summarisation that makes it usable without losing what actually happened.
var summariseActivityTask = llm.Task{
	ID:    SummariseActivity,
	Title: "Activity summary",
	System: "You are summarising a project activity log for a team update. " +
		"Report only what the log shows: no speculation about intent, no invented progress, " +
		"and no claims about work that is not recorded. " +
		"Prefer specifics (what moved, what blocked) over adjectives. " +
		"If the log shows little activity, say that plainly.",
	TokenBudget: promotionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		scope := "the recent activity"
		if in.SpecID != "" {
			scope = specLabel(in)
		}
		prompt := fmt.Sprintf(
			"Summarise %s as a short progress note.\n\n"+
				"Write two to four sentences of prose, then '### Blocked' as a bullet list "+
				"if and only if the log records blockers.",
			scope)

		// The log is the whole subject, so it outranks spec context.
		parts := []llm.ContextPart{{
			Label:   "Activity log",
			Content: in.Extra["activity"],
			Weight:  llm.WeightCritical,
		}}
		parts = append(parts, specContextParts(in, "")...)
		return prompt, parts
	},
}

// --- extract-decision ---

// Decisions get made in discussion threads and then lost. Extraction is a
// mechanical recovery of something already agreed, not a judgement call, which
// is what makes it safe to automate behind the same review gate.
var extractDecisionTask = llm.Task{
	ID:    ExtractDecision,
	Title: "Decision record",
	System: "You are recording a technical decision that has already been made in discussion. " +
		"Capture the decision, the alternatives that were actually considered, and the stated reasoning. " +
		"Do not invent alternatives nobody raised, do not add reasoning nobody gave, " +
		"and do not soften or second-guess the decision. " +
		"If the discussion did not reach a decision, say so in one line instead of inventing one.",
	TokenBudget: promotionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		prompt := "Extract the decision from the discussion below.\n\n" +
			"Output exactly these fields, each on its own line:\n" +
			"Decision: <one sentence, in the past tense>\n" +
			"Alternatives: <semicolon-separated, only those actually discussed>\n" +
			"Rationale: <one or two sentences of the reasoning given>"

		parts := []llm.ContextPart{{
			Label:   "Discussion",
			Content: in.Extra["discussion"],
			Weight:  llm.WeightCritical,
		}}
		parts = append(parts, specContextParts(in, "decision_log")...)
		return prompt, parts
	},
}
