// Package build orchestrates the coding agent integration.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/markdown"
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
}

// PRStep represents one step in the PR stack plan.
type PRStep struct {
	Number      int    `yaml:"number" json:"number"`
	Repo        string `yaml:"repo" json:"repo"`
	Description string `yaml:"description" json:"description"`
	Branch      string `yaml:"branch" json:"branch"`
	Status      string `yaml:"status" json:"status"` // "pending", "in-progress", "complete"
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

var prStepPattern = regexp.MustCompile(`^\s*(\d+)\.\s*\[([^\]]+)\]\s*(.+)$`)

func parsePRSteps(content string) ([]PRStep, error) {
	lines := strings.Split(content, "\n")
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

	return steps, nil
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

	// System prompt
	sb.WriteString("## Instructions\n\n")
	sb.WriteString(ctx.SystemPrompt)

	return os.WriteFile(outputPath, []byte(sb.String()), 0o644)
}

func buildSystemPrompt(ctx *BuildContext) string {
	var sb strings.Builder
	sb.WriteString("You are implementing a feature based on the spec above. ")
	if ctx.CurrentStep.Number > 0 {
		fmt.Fprintf(&sb, "You are on step %d: [%s] %s. ",
			ctx.CurrentStep.Number, ctx.CurrentStep.Repo, ctx.CurrentStep.Description)
	}
	sb.WriteString("Follow the acceptance criteria in §6. ")
	sb.WriteString("Record any decisions using the spec_decide tool. ")
	sb.WriteString("When the step is complete, use spec_step_complete to advance.")
	return sb.String()
}
