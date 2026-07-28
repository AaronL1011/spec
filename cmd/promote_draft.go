package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
	"github.com/aaronl1011/spec/internal/markdown"
)

// `spec promote --draft` expands a triage item into the opening spec sections so
// a promoted spec does not start from a blank page.
//
// The draft is reviewed before anything is written, and a skip is not a failure:
// the spec is still promoted, just with those sections empty. That ordering is
// what keeps the agent optional — promotion is the user's intent, drafting is an
// assist.

// promotedSections is what a promote-triage draft can fill.
type promotedSections struct {
	Problem string
	Outcome string
}

// draftPromotedSections runs the promote-triage task through the review gate.
//
// It returns empty content (not an error) when no agent is configured, drafting
// is disabled, or the reviewer skipped — every one of those is a legitimate
// "promote without a draft", and failing promotion for them would make the
// assist a liability.
func draftPromotedSections(rc *config.ResolvedConfig, triageMeta *markdown.TriageMeta, triagePath string, autoAccept bool) (promotedSections, error) {
	if !rc.AgentDraftsEnabled() {
		return promotedSections{}, nil
	}
	svc := newLLMService(rc)
	if !svc.IsAvailable() {
		return promotedSections{}, nil
	}

	in := llm.Input{
		Meta: map[string]string{
			"title":    triageMeta.Title,
			"source":   triageMeta.Source,
			"priority": triageMeta.Priority,
			"reporter": triageMeta.ReportedBy,
		},
		Extra: map[string]string{},
	}
	// The triage body is the substance; frontmatter alone is a title and a
	// priority, which is not enough to write a problem statement from.
	if body := triageBody(triagePath); body != "" {
		in.Extra["body"] = body
	}

	content, err := reviewDraft(rc, svc, tasks.PromoteTriage, in, autoAccept)
	if err != nil {
		return promotedSections{}, err
	}
	if content == "" {
		return promotedSections{}, nil
	}

	problem, outcome := splitPromotedDraft(content)
	return promotedSections{Problem: problem, Outcome: outcome}, nil
}

// triageBody returns the prose under a triage item's frontmatter, or empty when
// it cannot be read. Best-effort: a missing body degrades the draft's quality,
// not the promotion.
func triageBody(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stripYAMLFrontmatter(string(data)))
}

// stripYAMLFrontmatter drops a leading `---` delimited block. Kept local rather
// than exported from internal/markdown, which has no such helper and does not
// need one for a single caller.
func stripYAMLFrontmatter(content string) string {
	const delim = "---"
	trimmed := strings.TrimLeft(content, "\r\n \t")
	if !strings.HasPrefix(trimmed, delim) {
		return content
	}
	// Skip the opening delimiter, then find the closing one.
	rest := trimmed[len(delim):]
	if idx := strings.Index(rest, "\n"+delim); idx >= 0 {
		after := rest[idx+len(delim)+1:]
		if nl := strings.Index(after, "\n"); nl >= 0 {
			return after[nl+1:]
		}
		return ""
	}
	return content
}

// splitPromotedDraft separates the two sections the task is asked to emit.
//
// The model is told to use exact headings, but a model is not a parser: an
// unexpected shape falls back to treating everything as the problem statement,
// which is recoverable by editing, rather than dropping the draft entirely.
func splitPromotedDraft(content string) (problem, outcome string) {
	lines := strings.Split(content, "\n")
	var current *strings.Builder
	problemBuf, outcomeBuf := &strings.Builder{}, &strings.Builder{}

	for _, line := range lines {
		heading := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "# ")))
		isHeading := strings.HasPrefix(strings.TrimSpace(line), "#")

		switch {
		case isHeading && strings.Contains(heading, "problem"):
			current = problemBuf
			continue
		case isHeading && (strings.Contains(heading, "outcome") || strings.Contains(heading, "goal")):
			current = outcomeBuf
			continue
		}
		if current == nil {
			current = problemBuf
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	return strings.TrimSpace(problemBuf.String()), strings.TrimSpace(outcomeBuf.String())
}

// applyPromotedSections writes drafted content into a freshly scaffolded spec.
//
// Writes go through the markdown engine so a drafted section is validated and
// formatted exactly like a hand-written one, and an unknown section slug is
// skipped rather than failing the promotion that already succeeded.
func applyPromotedSections(specPath string, sections promotedSections) error {
	writes := []struct {
		slug    string
		content string
	}{
		{"problem_statement", sections.Problem},
		{"goals_non_goals", sections.Outcome},
	}

	for _, w := range writes {
		if w.content == "" {
			continue
		}
		if !markdown.IsValidSectionSlug(w.slug) {
			continue
		}
		if err := markdown.ReplaceSection(specPath, w.slug, w.content); err != nil {
			return fmt.Errorf("writing %s: %w", w.slug, err)
		}
	}
	return nil
}
