// Package build orchestrates the coding agent integration.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
)

// BuildContext is the assembled context payload passed to an agent.
type BuildContext struct {
	SpecPath     string
	SpecContent  string
	PriorDiffs   []string
	FailingTests string
	Conventions  string
	CurrentStep  PRStep
	SystemPrompt string
	// Skills holds the resolved skill bodies (Agent Skills markdown). Empty
	// when no skills are present under .spec/agent/skills/ or in config.
	Skills []string
}

// PRStep represents one step in the PR stack plan.
type PRStep struct {
	Number      int    `yaml:"number" json:"number"`
	Repo        string `yaml:"repo" json:"repo"`
	Description string `yaml:"description" json:"description"`
	Branch      string `yaml:"branch" json:"branch"`
	Status      string `yaml:"status" json:"status"` // "pending", "in-progress", "complete"
	// BaseRef is the commit the step branch was created from. Used to capture
	// the step's diff for cumulative cross-step context.
	BaseRef string `yaml:"base_ref" json:"base_ref,omitempty"`
}

// ParsePRStack extracts PR steps from the §7.3 PR Stack Plan section.
func ParsePRStack(content string) ([]PRStep, error) {
	body := markdown.Body(content)
	sections := markdown.ExtractSections(body)
	prSection := markdown.FindSection(sections, "pr_stack_plan")
	if prSection == nil {
		return nil, nil
	}

	return parsePRSteps(prSection.Content)
}

// ParsePRStackFromFile reads a spec file and extracts PR steps.
func ParsePRStackFromFile(path string) ([]PRStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ParsePRStack(string(data))
}

// prStepPattern matches the compact list form: `1. [repo] description`.
var prStepPattern = regexp.MustCompile(`^\s*(\d+)\.\s*\[([^\]]+)\]\s*(.+)$`)

// partHeaderPattern matches the prose form header: `**Part 1 — \x60repo\x60: title**`.
var partHeaderPattern = regexp.MustCompile("(?i)^\\*\\*\\s*part\\s+(\\d+)\\s*[\u2014\u2013:-]\\s*(.+?)\\s*\\*\\*\\s*$")

// repoLinePattern matches a standalone repo declaration: `Repo: \x60repo\x60`.
var repoLinePattern = regexp.MustCompile("(?i)^\\s*repo:\\s*`?([A-Za-z0-9._/-]+)`?\\s*$")

// backtickToken extracts the first \x60backtick\x60-quoted token.
var backtickToken = regexp.MustCompile("`([^`]+)`")

// parsePRSteps extracts PR steps from the PR Stack Plan section. It supports two
// authoring styles: the compact list (`1. [repo] desc`) and the prose form
// (`**Part 1 — \x60repo\x60: title**` with optional `Repo: \x60repo\x60` lines).
func parsePRSteps(content string) ([]PRStep, error) {
	lines := strings.Split(content, "\n")

	if steps := parseListSteps(lines); len(steps) > 0 {
		return steps, nil
	}
	return parsePartSteps(lines), nil
}

// parseListSteps handles the compact `1. [repo] description` form.
func parseListSteps(lines []string) []PRStep {
	var steps []PRStep
	for _, line := range lines {
		matches := prStepPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		num := 0
		_, _ = fmt.Sscanf(matches[1], "%d", &num)
		steps = append(steps, PRStep{
			Number:      num,
			Repo:        strings.TrimSpace(matches[2]),
			Description: strings.TrimSpace(matches[3]),
			Status:      "pending",
		})
	}
	return steps
}

// parsePartSteps handles the prose `**Part N — \x60repo\x60: title**` form, using a
// following `Repo: \x60repo\x60` line to set or correct the repo when present.
func parsePartSteps(lines []string) []PRStep {
	var steps []PRStep
	for _, line := range lines {
		if m := partHeaderPattern.FindStringSubmatch(line); m != nil {
			num := 0
			_, _ = fmt.Sscanf(m[1], "%d", &num)
			repo, desc := splitPartTitle(m[2])
			steps = append(steps, PRStep{
				Number:      num,
				Repo:        repo,
				Description: desc,
				Status:      "pending",
			})
			continue
		}
		// A `Repo:` line refines the most recent part's repo.
		if m := repoLinePattern.FindStringSubmatch(line); m != nil && len(steps) > 0 {
			steps[len(steps)-1].Repo = strings.TrimSpace(m[1])
		}
	}
	return steps
}

// splitPartTitle pulls the repo (first backtick token) and a human description
// out of a part header's text, e.g. "\x60nexl-ai-core\x60: Full Implementation".
func splitPartTitle(s string) (repo, desc string) {
	if m := backtickToken.FindStringSubmatch(s); m != nil {
		repo = strings.TrimSpace(m[1])
	}
	desc = backtickToken.ReplaceAllString(s, "")
	desc = strings.TrimLeft(desc, " :\u2014\u2013-")
	desc = strings.TrimSpace(desc)
	return repo, desc
}

// AssembleContext builds the full context payload for an agent.
func AssembleContext(specPath string, session *SessionState, conventions string) (*BuildContext, error) {
	specContent, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	ctx := &BuildContext{
		SpecPath:    specPath,
		SpecContent: string(specContent),
		Conventions: conventions,
	}

	if session != nil && session.CurrentStep > 0 && session.CurrentStep <= len(session.Steps) {
		ctx.CurrentStep = session.Steps[session.CurrentStep-1]
	}

	// Load prior diffs from session directory
	if session != nil {
		sessionDir := SessionDir(session.SpecID)
		for i := 1; i < session.CurrentStep; i++ {
			diffPath := filepath.Join(sessionDir, fmt.Sprintf("step-%d.diff", i))
			if data, err := os.ReadFile(diffPath); err == nil {
				ctx.PriorDiffs = append(ctx.PriorDiffs, string(data))
			}
		}
	}

	ctx.SystemPrompt = buildSystemPrompt(ctx)
	return ctx, nil
}

// WriteContextFile writes a consolidated context markdown file for non-MCP agents.
func WriteContextFile(ctx *BuildContext, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# Build Context\n\n")

	// Current step
	if ctx.CurrentStep.Number > 0 {
		fmt.Fprintf(&sb, "## Current Step: %d. [%s] %s\n\n",
			ctx.CurrentStep.Number, ctx.CurrentStep.Repo, ctx.CurrentStep.Description)
	}

	// Full spec
	sb.WriteString("## Spec\n\n")
	sb.WriteString(ctx.SpecContent)
	sb.WriteString("\n\n")

	// Conventions
	if ctx.Conventions != "" {
		sb.WriteString("## Project Conventions\n\n")
		sb.WriteString(ctx.Conventions)
		sb.WriteString("\n\n")
	}

	// Prior diffs
	if len(ctx.PriorDiffs) > 0 {
		sb.WriteString("## Prior Step Diffs\n\n")
		for i, diff := range ctx.PriorDiffs {
			fmt.Fprintf(&sb, "### Step %d\n\n```diff\n%s\n```\n\n", i+1, diff)
		}
	}

	// Failing tests
	if strings.TrimSpace(ctx.FailingTests) != "" {
		sb.WriteString("## Failing Tests\n\n")
		fmt.Fprintf(&sb, "```\n%s\n```\n\n", ctx.FailingTests)
	}

	// Reproducibility skills (Agent Skills bodies). Included so non-skill
	// agents still follow the playbook via the consolidated context file.
	if len(ctx.Skills) > 0 {
		sb.WriteString("## Agent Skills\n\n")
		for _, body := range ctx.Skills {
			sb.WriteString(strings.TrimSpace(body))
			sb.WriteString("\n\n")
		}
	}

	// System prompt
	sb.WriteString("## Instructions\n\n")
	sb.WriteString(ctx.SystemPrompt)

	return os.WriteFile(outputPath, []byte(sb.String()), 0o644)
}

// buildKickoffPrompt is the initial user message that tells the agent to start
// working on the current step. Without it an interactive agent opens an idle
// session and waits for input; with it the agent begins immediately.
func buildKickoffPrompt(ctx *BuildContext) string {
	var sb strings.Builder
	if ctx.CurrentStep.Number > 0 {
		fmt.Fprintf(&sb, "Begin step %d", ctx.CurrentStep.Number)
		if ctx.CurrentStep.Repo != "" {
			fmt.Fprintf(&sb, " [%s]", ctx.CurrentStep.Repo)
		}
		if ctx.CurrentStep.Description != "" {
			fmt.Fprintf(&sb, ": %s", ctx.CurrentStep.Description)
		}
		sb.WriteString(". ")
	} else {
		sb.WriteString("Begin implementing this spec. ")
	}
	sb.WriteString("Read spec://current/full and spec://current/acceptance-criteria via the spec MCP server, ")
	sb.WriteString("implement this step following the project conventions, record decisions with spec_decide, ")
	sb.WriteString("and call spec_step_complete when the step is implemented and verified.")
	return sb.String()
}

// buildSystemPrompt assembles the minimal base build instruction plus the
// current-step scope. It stays intentionally thin: the behavioural playbook is
// what a separately-authored skill provides via .spec/agent/skills/.
func buildSystemPrompt(ctx *BuildContext) string {
	var sb strings.Builder
	sb.WriteString("You are implementing a feature based on the active spec. ")
	sb.WriteString("The spec is available via the spec MCP server (spec://current/full) ")
	sb.WriteString("and its sections, conventions, and prior-step diffs. ")
	if ctx.CurrentStep.Number > 0 {
		fmt.Fprintf(&sb, "You are on step %d: [%s] %s. ",
			ctx.CurrentStep.Number, ctx.CurrentStep.Repo, ctx.CurrentStep.Description)
	}
	sb.WriteString("Follow the acceptance criteria in §6 and the project conventions. ")
	sb.WriteString("Record any decisions using the spec_decide tool. ")
	sb.WriteString("When the step is complete, use spec_step_complete to advance.")
	return sb.String()
}
