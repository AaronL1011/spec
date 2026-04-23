package ai

import (
	"fmt"
	"strings"
)

// SectionDraftPrompt generates a prompt for drafting a spec section.
func SectionDraftPrompt(sectionSlug string, existingContext map[string]string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Draft the %q section for a software specification.\n\n", humanizeSlug(sectionSlug))
	sb.WriteString("Use the following existing context from the spec:\n\n")

	for section, content := range existingContext {
		if content != "" {
			fmt.Fprintf(&sb, "### %s\n%s\n\n", humanizeSlug(section), content)
		}
	}

	sb.WriteString("Write clear, actionable content appropriate for the section. ")
	sb.WriteString("Do not include the heading — just the section body content.")

	return sb.String()
}

// PRDescriptionPrompt generates a prompt for drafting a PR description.
func PRDescriptionPrompt(diff, specContext, stackPosition string) string {
	var sb strings.Builder

	sb.WriteString("Generate a pull request description for the following change.\n\n")
	sb.WriteString("## Diff\n\n```diff\n")
	sb.WriteString(diff)
	sb.WriteString("\n```\n\n")

	if specContext != "" {
		sb.WriteString("## Spec Context\n\n")
		sb.WriteString(specContext)
		sb.WriteString("\n\n")
	}

	if stackPosition != "" {
		sb.WriteString("## Stack Position\n\n")
		sb.WriteString(stackPosition)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Write a clear PR description with: Summary, Changes, Testing notes. ")
	sb.WriteString("Use markdown formatting.")

	return sb.String()
}

// PRStackPrompt generates a prompt for proposing a PR stack plan.
func PRStackPrompt(solution, architectureNotes string, repos []string) string {
	var sb strings.Builder

	sb.WriteString("Propose a PR stack plan for implementing the following feature.\n\n")
	sb.WriteString("## Solution\n\n")
	sb.WriteString(solution)
	sb.WriteString("\n\n")

	if architectureNotes != "" {
		sb.WriteString("## Architecture Notes\n\n")
		sb.WriteString(architectureNotes)
		sb.WriteString("\n\n")
	}

	if len(repos) > 0 {
		sb.WriteString("## Target Repos\n\n")
		sb.WriteString(strings.Join(repos, ", "))
		sb.WriteString("\n\n")
	}

	sb.WriteString("Output a numbered list in the format:\n")
	sb.WriteString("1. [repo-name] Description of PR\n")
	sb.WriteString("2. [repo-name] Description of PR\n\n")
	sb.WriteString("Order by dependency — earlier PRs should be merged before later ones. ")
	sb.WriteString("Each PR should be reviewable independently.")

	return sb.String()
}

// TriageSummarisePrompt generates a prompt for summarising a triage source.
func TriageSummarisePrompt(sourceContent string) string {
	return fmt.Sprintf(`Summarise the following alert/report into a concise triage description.
Include: what happened, what's the impact, and any initial context.
Keep it to 2-3 sentences.

Source:
%s`, sourceContent)
}

func humanizeSlug(slug string) string {
	return strings.ReplaceAll(slug, "_", " ")
}
