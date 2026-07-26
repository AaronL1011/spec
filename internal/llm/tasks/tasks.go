// Package tasks declares spec's LLM tasks, one file per concern.
//
// A task is data plus a pure Build function, so its assembled prompt is
// golden-testable and a wording change is a reviewable diff. Registration is a
// map lookup: no plugin machinery for what is a handful of prompts.
package tasks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/markdown"
)

// Task IDs. Stable strings: they appear in the activity log and in telemetry, so
// they are part of the observable surface.
const (
	// DraftSection drafts one spec section from the rest of the spec.
	DraftSection = "draft-section"
	// DraftAcceptance proposes testable acceptance criteria.
	DraftAcceptance = "draft-acceptance"
	// DraftPR writes a PR description from a diff and spec context.
	DraftPR = "draft-pr"
	// DraftPRStack proposes the PR stack DAG.
	DraftPRStack = "draft-pr-stack"
	// PromoteTriage expands a triage item into problem and outcome sections.
	PromoteTriage = "promote-triage"
)

// Default context budgets, in approximate tokens. Generous enough that trimming
// is rare on a real spec, small enough that a pathological spec cannot blow up a
// local model's context window.
const (
	sectionBudget   = 6000
	prBudget        = 8000
	planningBudget  = 6000
	promotionBudget = 2000
)

// registry maps task id to task. Unexported so lookups go through Get, which
// returns an actionable error for an unknown id.
var registry = map[string]llm.Task{
	DraftSection:    draftSectionTask,
	DraftAcceptance: draftAcceptanceTask,
	DraftPR:         draftPRTask,
	DraftPRStack:    draftPRStackTask,
	PromoteTriage:   promoteTriageTask,

	// Fast-follow set: same gate, same budgeting, no new code paths.
	ReviseSection:     reviseSectionTask,
	ReviewPlan:        reviewPlanTask,
	SummariseActivity: summariseActivityTask,
	ExtractDecision:   extractDecisionTask,
}

// Get returns a task by id.
func Get(id string) (llm.Task, error) {
	t, ok := registry[id]
	if !ok {
		return llm.Task{}, fmt.Errorf("unknown task %q — known tasks: %s", id, strings.Join(IDs(), ", "))
	}
	return t, nil
}

// IDs returns every registered task id in stable order.
func IDs() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// --- draft-section ---

var draftSectionTask = llm.Task{
	ID:    DraftSection,
	Title: "Section draft",
	System: "You are a senior engineer drafting a section of a software specification. " +
		"Write only the section body: no heading, no preamble, no meta-commentary about being an AI. " +
		"Be concrete and specific to the spec you are given; prefer plain language over jargon, " +
		"and do not invent facts, names, or numbers that the provided context does not support. " +
		"If the context is too thin to write the section responsibly, say what is missing in one line instead of padding.",
	TokenBudget: sectionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		label := humanizeSlug(in.Section)
		prompt := fmt.Sprintf(
			"Draft the %q section for %s.\n\nWrite the section body only, in markdown.",
			label, specLabel(in))

		parts := specContextParts(in, in.Section)
		return prompt, parts
	},
}

// --- draft-acceptance ---

var draftAcceptanceTask = llm.Task{
	ID:    DraftAcceptance,
	Title: "Acceptance criteria",
	System: "You are a QA engineer writing acceptance criteria for a software specification. " +
		"Every criterion must be independently verifiable by someone who did not write the code: " +
		"name the observable behaviour, not the implementation. " +
		"Prefer many small checks over few broad ones, and cover failure and degradation paths, " +
		"not only the happy path. Do not invent requirements the spec does not imply.",
	TokenBudget: sectionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		prompt := fmt.Sprintf(
			"Propose acceptance criteria for %s.\n\n"+
				"Output a markdown checklist using '- [ ] ' items. "+
				"Group them under '### ' subheadings by user story when the spec defines stories.",
			specLabel(in))

		parts := specContextParts(in, "acceptance_criteria")
		return prompt, parts
	},
}

// --- draft-pr ---

var draftPRTask = llm.Task{
	ID:    DraftPR,
	Title: "PR description",
	System: "You are an engineer writing a pull request description for reviewers. " +
		"Lead with why the change exists, then what it does. " +
		"Describe only what the diff actually shows — never claim tests, benchmarks, or behaviour " +
		"that is not visible in the provided changes.",
	TokenBudget: prBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		prompt := fmt.Sprintf(
			"Write a pull request description for the change below, implementing %s.\n\n"+
				"Use these sections: a one-paragraph summary, '## Changes' as a short bullet list, "+
				"and '## Testing' describing how it was verified.",
			specLabel(in))

		var parts []llm.ContextPart
		if diff := strings.TrimSpace(in.Diff); diff != "" {
			parts = append(parts, llm.ContextPart{
				Label:   "Diff",
				Content: fencedDiff(diff),
				Weight:  llm.WeightPrimary,
			})
		}
		parts = append(parts, sectionPart(in, "problem_statement", llm.WeightSupporting)...)
		parts = append(parts, sectionPart(in, "proposed_solution", llm.WeightSupporting)...)
		if pos := in.Extra["stack_position"]; strings.TrimSpace(pos) != "" {
			parts = append(parts, llm.ContextPart{
				Label:   "Position in the PR stack",
				Content: pos,
				Weight:  llm.WeightSupporting,
			})
		}
		return prompt, parts
	},
}

// --- draft-pr-stack ---

var draftPRStackTask = llm.Task{
	ID:    DraftPRStack,
	Title: "PR stack plan",
	System: "You are a tech lead decomposing a specification into a stack of pull requests. " +
		"Each node must be independently reviewable and leave the system working when merged. " +
		"Order strictly by dependency, and only declare a dependency that genuinely exists — " +
		"nodes with no unmet dependency run in parallel, so spurious edges serialise work needlessly.",
	TokenBudget: planningBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Propose a PR stack plan for %s.\n\n", specLabel(in))
		sb.WriteString("Output one node per line, numbered, in exactly this format:\n")
		sb.WriteString("    N. [repo:layer] Description (after: A, B)\n\n")
		sb.WriteString("Rules: ':layer' is optional and routes skills; '(after: ...)' lists dependency ")
		sb.WriteString("edges to earlier node numbers and is omitted for root nodes. ")
		sb.WriteString("Do not author draft-PR URLs. Output the list only, with no commentary.")

		var parts []llm.ContextPart
		if len(in.Repos) > 0 {
			parts = append(parts, llm.ContextPart{
				Label:   "Target repos (use only these)",
				Content: strings.Join(in.Repos, ", "),
				Weight:  llm.WeightPrimary,
			})
		}
		parts = append(parts, sectionPart(in, "proposed_solution", llm.WeightPrimary)...)
		parts = append(parts, sectionPart(in, "architecture_notes", llm.WeightSupporting)...)
		parts = append(parts, sectionPart(in, "problem_statement", llm.WeightBackground)...)
		return sb.String(), parts
	},
}

// --- promote-triage ---

var promoteTriageTask = llm.Task{
	ID:    PromoteTriage,
	Title: "Promoted spec draft",
	System: "You are a product manager turning a triage item into the opening sections of a " +
		"specification. Write from the reported problem: state the user-visible impact before the " +
		"mechanism, and quantify only what the input actually supports. " +
		"Do not propose a solution — that is a later section written by someone else.",
	TokenBudget: promotionBudget,
	Build: func(in llm.Input) (string, []llm.ContextPart) {
		var sb strings.Builder
		sb.WriteString("Expand the triage item below into two spec sections.\n\n")
		sb.WriteString("Output exactly two markdown blocks, in this order and with these headings:\n")
		sb.WriteString("    ## Problem Statement\n    ## Desired Outcome\n\n")
		sb.WriteString("Keep each to a short paragraph or a few bullets.")

		var parts []llm.ContextPart
		for _, key := range []string{"title", "source", "priority", "reporter"} {
			if v := strings.TrimSpace(in.Meta[key]); v != "" {
				parts = append(parts, llm.ContextPart{
					Label:   humanizeSlug(key),
					Content: v,
					Weight:  llm.WeightPrimary,
				})
			}
		}
		if body := strings.TrimSpace(in.Extra["body"]); body != "" {
			parts = append(parts, llm.ContextPart{
				Label:   "Reported detail",
				Content: body,
				Weight:  llm.WeightPrimary,
			})
		}
		return sb.String(), parts
	},
}

// --- shared helpers ---

// sectionWeights encodes editorial judgement about which spec sections matter
// most as context: the problem and goals anchor everything, the solution and
// decisions shape it, and anything unlisted is background a tight budget may
// drop first.
//
// Keys must be real slugs from markdown.ValidSectionSlugs. An invented slug
// would silently fall through to WeightBackground and sort into the alphabetical
// tail, so a test asserts every key is canonical.
var sectionWeights = map[string]int{
	"problem_statement":        llm.WeightPrimary,
	"goals_non_goals":          llm.WeightPrimary,
	"tl_dr":                    llm.WeightSupporting,
	"user_stories":             llm.WeightSupporting,
	"proposed_solution":        llm.WeightSupporting,
	"concept_overview":         llm.WeightSupporting,
	"architecture_approach":    llm.WeightSupporting,
	"decision_log":             llm.WeightSupporting,
	"acceptance_criteria":      llm.WeightSupporting,
	"technical_implementation": llm.WeightSupporting,
}

// specContextParts renders the spec's populated sections as context, excluding
// the one being drafted (feeding a section its own stale content invites the
// model to echo it back).
func specContextParts(in llm.Input, exclude string) []llm.ContextPart {
	weights := sectionWeights

	slugs := make([]string, 0, len(in.Sections))
	for slug := range in.Sections {
		slugs = append(slugs, slug)
	}
	// Deterministic order: canonical section order when known, then alphabetical
	// for anything else, so assembly is reproducible for golden tests.
	canonical := markdown.ValidSectionSlugs()
	rank := make(map[string]int, len(canonical))
	for i, slug := range canonical {
		rank[slug] = i
	}
	sort.SliceStable(slugs, func(a, b int) bool {
		ra, oka := rank[slugs[a]]
		rb, okb := rank[slugs[b]]
		switch {
		case oka && okb:
			return ra < rb
		case oka:
			return true
		case okb:
			return false
		default:
			return slugs[a] < slugs[b]
		}
	})

	var parts []llm.ContextPart
	for _, slug := range slugs {
		if slug == exclude {
			continue
		}
		content := strings.TrimSpace(in.Sections[slug])
		if content == "" {
			continue
		}
		weight, ok := weights[slug]
		if !ok {
			weight = llm.WeightBackground
		}
		parts = append(parts, llm.ContextPart{
			Label:   humanizeSlug(slug),
			Content: content,
			Weight:  weight,
		})
	}
	return parts
}

// sectionPart returns one named section as a context part, or nothing when the
// section is absent or empty.
func sectionPart(in llm.Input, slug string, weight int) []llm.ContextPart {
	content := strings.TrimSpace(in.Sections[slug])
	if content == "" {
		return nil
	}
	return []llm.ContextPart{{
		Label:   humanizeSlug(slug),
		Content: content,
		Weight:  weight,
	}}
}

// specLabel names the spec in a prompt, preferring its title.
func specLabel(in llm.Input) string {
	title := strings.TrimSpace(in.Meta["title"])
	switch {
	case in.SpecID != "" && title != "":
		return fmt.Sprintf("%s (%q)", in.SpecID, title)
	case in.SpecID != "":
		return in.SpecID
	case title != "":
		return fmt.Sprintf("%q", title)
	default:
		return "this specification"
	}
}

// fencedDiff wraps a diff in a fenced block so a model does not read diff
// markers as markdown.
func fencedDiff(diff string) string {
	return "```diff\n" + diff + "\n```"
}

// humanizeSlug turns a section slug into a readable label.
func humanizeSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "_", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
