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
	// SkillPaths is the deduplicated union of skill paths across all DAG nodes,
	// passed to skill-capable agents as the set the orchestrator may dispatch.
	SkillPaths []string
}

// PRStep represents one node in the §7.3 PR stack plan. A plan is a DAG: each
// step has a stable ID, an optional repo and layer (which drive worktree
// placement and skill routing), and zero or more dependencies on earlier steps.
type PRStep struct {
	Number      int    `yaml:"number" json:"number"`
	ID          string `yaml:"id" json:"id"`
	Repo        string `yaml:"repo" json:"repo"`
	Layer       string `yaml:"layer" json:"layer,omitempty"`
	Description string `yaml:"description" json:"description"`
	Branch      string `yaml:"branch" json:"branch"`
	Status      string `yaml:"status" json:"status"` // "pending", "in-progress", "complete", "failed"
	// DependsOn holds the step numbers this node depends on (parsed from the
	// `(after: 1,2)` edge annotation). Empty for a root node.
	DependsOn []int `yaml:"depends_on" json:"depends_on,omitempty"`
	// PRURL is the draft PR recorded for this node in §7.3 via a trailing
	// `<!-- pr: <url> -->` annotation. Empty until the finisher opens a PR.
	PRURL string `yaml:"pr_url" json:"pr_url,omitempty"`
	// BaseRef is the commit the step branch was created from. Used to capture
	// the step's diff for cumulative cross-step context.
	BaseRef string `yaml:"base_ref" json:"base_ref,omitempty"`
}

// NodeID returns the stable node identifier, deriving it from the step number
// when an explicit ID was not parsed (e.g. "n3").
func (s PRStep) NodeID() string {
	if s.ID != "" {
		return s.ID
	}
	return fmt.Sprintf("n%d", s.Number)
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

// afterEdgePattern matches a trailing dependency annotation: `(after: 1,2)`.
var afterEdgePattern = regexp.MustCompile(`(?i)\(\s*after:\s*([0-9,\s]+)\)`)

// prAnnotationPattern matches a recorded draft-PR annotation: `<!-- pr: <url> -->`.
var prAnnotationPattern = regexp.MustCompile(`(?i)<!--\s*pr:\s*(\S+)\s*-->`)

// parsePRSteps extracts PR steps from the PR Stack Plan section. It supports two
// authoring styles: the compact list (`1. [repo:layer] desc (after: 1,2)`) and
// the prose form (`**Part 1 — \x60repo\x60: title**` with optional `Repo:` lines).
// Stable node IDs are assigned after parsing so callers always get an ID.
func parsePRSteps(content string) ([]PRStep, error) {
	lines := strings.Split(content, "\n")

	steps := parseListSteps(lines)
	if len(steps) == 0 {
		steps = parsePartSteps(lines)
	}
	assignNodeIDs(steps)
	return steps, nil
}

// assignNodeIDs gives every step a stable ID derived from its number when the
// author did not provide one explicitly.
func assignNodeIDs(steps []PRStep) {
	for i := range steps {
		if steps[i].ID == "" {
			steps[i].ID = fmt.Sprintf("n%d", steps[i].Number)
		}
	}
}

// splitRepoLayer splits a bracket token `repo:layer` into its parts. Both sides
// are optional: `[repo]`, `[:layer]`, and `[repo:layer]` all parse.
func splitRepoLayer(token string) (repo, layer string) {
	repo = strings.TrimSpace(token)
	if i := strings.Index(token, ":"); i >= 0 {
		repo = strings.TrimSpace(token[:i])
		layer = strings.TrimSpace(token[i+1:])
	}
	return repo, layer
}

// extractAfterEdges pulls a trailing `(after: 1,2)` annotation out of a step
// description, returning the cleaned description and the parsed dependency
// step numbers (nil when there is no annotation).
func extractAfterEdges(desc string) (string, []int) {
	m := afterEdgePattern.FindStringSubmatch(desc)
	if m == nil {
		return strings.TrimSpace(desc), nil
	}
	var deps []int
	for _, field := range strings.Split(m[1], ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(field, "%d", &n); err == nil {
			deps = append(deps, n)
		}
	}
	cleaned := strings.TrimSpace(afterEdgePattern.ReplaceAllString(desc, ""))
	return cleaned, deps
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
		repo, layer := splitRepoLayer(matches[2])
		prURL, rest := extractPRAnnotation(matches[3])
		desc, deps := extractAfterEdges(rest)
		steps = append(steps, PRStep{
			Number:      num,
			Repo:        repo,
			Layer:       layer,
			Description: desc,
			DependsOn:   deps,
			PRURL:       prURL,
			Status:      "pending",
		})
	}
	return steps
}

// extractPRAnnotation pulls a trailing `<!-- pr: <url> -->` annotation out of a
// step line, returning the URL (empty when absent) and the line with the
// annotation removed so downstream parsing is unaffected.
func extractPRAnnotation(line string) (url, rest string) {
	m := prAnnotationPattern.FindStringSubmatch(line)
	if m == nil {
		return "", line
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(prAnnotationPattern.ReplaceAllString(line, ""))
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

	// Load prior diffs for completed nodes. DAG builds key diffs by node id;
	// the legacy sequential walk keyed them by step number, so fall back to
	// that when no node diff is present.
	if session != nil {
		ctx.PriorDiffs = loadPriorDiffs(session)
	}

	ctx.SystemPrompt = soloSystemPrompt()
	return ctx, nil
}

// WriteContextFile writes a consolidated context markdown file for non-MCP agents.
func WriteContextFile(ctx *BuildContext, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# Build Context\n\n")
	sb.WriteString("This spec is built as a DAG of nodes (see §7.3 PR Stack Plan). ")
	sb.WriteString("MCP-capable agents should read spec://current/dag and drive it with the node tools; ")
	sb.WriteString("the sections below are the consolidated fallback for agents without MCP.\n\n")

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

// loadPriorDiffs returns the captured diffs of completed nodes, preferring the
// per-node diff (node-<id>.diff) and falling back to the legacy per-step diff.
func loadPriorDiffs(session *SessionState) []string {
	sessionDir := SessionDir(session.SpecID)
	var diffs []string
	for _, step := range session.Steps {
		if session.NodeStatus(step.NodeID()) != NodeComplete {
			continue
		}
		nodePath := filepath.Join(sessionDir, fmt.Sprintf("node-%s.diff", step.NodeID()))
		if data, err := os.ReadFile(nodePath); err == nil {
			diffs = append(diffs, string(data))
			continue
		}
		legacyPath := filepath.Join(sessionDir, fmt.Sprintf("step-%d.diff", step.Number))
		if data, err := os.ReadFile(legacyPath); err == nil {
			diffs = append(diffs, string(data))
		}
	}
	return diffs
}

// conductorKickoff is the initial user message handed to an MCP-capable agent.
// It frames the whole build as a traversal of the Build Integration Port: the
// agent conducts, spec-cli owns the DAG, ledger, and git/PR mechanics. Per-node
// worker skills are delivered through spec_provision_node, not this prompt.
func conductorKickoff() string {
	var sb strings.Builder
	sb.WriteString("Conduct this spec's build via the Build Integration Port. ")
	sb.WriteString("Read spec://current/dag for the node graph and waves, and spec://current/full for the spec. ")
	sb.WriteString("Process waves in order; nodes within a wave are independent and may run in parallel up to maxParallel. ")
	sb.WriteString("For each ready node: call spec_provision_node(node_id) to get its workDir, branch, and skillPaths, ")
	sb.WriteString("dispatch one worker into that workDir following the node's skillPaths and the project conventions, ")
	sb.WriteString("then checkpoint with spec_node_complete(node_id) — or spec_node_failed(node_id, reason) if it cannot be finished. ")
	sb.WriteString("When every node is complete, push and open stacked DRAFT PRs with spec_push, spec_open_pr, and spec_link_prs. ")
	sb.WriteString("Record decisions with spec_decide. Do not mark PRs ready or merge — humans own that.")
	return sb.String()
}

// conductorSystemPrompt is the base instruction for an MCP-capable agent. It
// stays thin and names the contract rather than describing a do-it-yourself
// loop: the orchestration playbook is a separately-authored skill, and spec-cli
// supplies the deterministic DAG + tools, not the reasoning.
func conductorSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString("You are the build conductor for the active spec, driving it through spec-cli's Build Integration Port over MCP. ")
	sb.WriteString("spec-cli owns the dependency graph, the durable node ledger, and all git/worktree/PR mechanics; you conduct execution and never design. ")
	sb.WriteString("Read spec://current/dag for the node graph and waves and spec://current/full for the spec, with its sections, conventions, and prior-node diffs. ")
	sb.WriteString("Walk waves in order: provision each ready node with spec_provision_node, dispatch a worker into the returned workDir following its skillPaths, ")
	sb.WriteString("and checkpoint it with spec_node_complete/spec_node_failed. When all nodes are complete, finish the stacked draft PRs with spec_push/spec_open_pr/spec_link_prs. ")
	sb.WriteString("Follow the acceptance criteria in §6 and the project conventions, record decisions with spec_decide, and stop and report on any gap or spec/reality mismatch rather than improvising. ")
	sb.WriteString("If a conductor playbook skill is configured, follow it.")
	return sb.String()
}

// soloKickoff is the initial user message for a non-MCP agent that implements
// the whole spec itself from the consolidated context file.
func soloKickoff(_ *BuildContext) string {
	var sb strings.Builder
	sb.WriteString("Implement this spec from the consolidated build context. ")
	sb.WriteString("Work through the §7.3 PR stack in order, satisfy the acceptance criteria in §6, and follow the project conventions. ")
	sb.WriteString("Keep changes scoped to the spec and stop and report on any gap rather than guessing.")
	return sb.String()
}

// soloSystemPrompt assembles the base instruction for a non-MCP agent. Such an
// agent has no node tools, so it implements the spec end to end from the
// consolidated context (full spec, conventions, prior diffs, folded skills).
func soloSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString("You are implementing the active spec end to end from the consolidated build context below. ")
	sb.WriteString("It contains the full spec, its acceptance criteria, project conventions, prior diffs, and any capability playbooks. ")
	sb.WriteString("Implement each node of the §7.3 plan in order, follow the acceptance criteria in §6 and the project conventions, ")
	sb.WriteString("keep commits compartmentalised and semver-conventional, and stop and report on any gap or spec/reality mismatch rather than guessing.")
	return sb.String()
}
