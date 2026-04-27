package pipeline

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline/expr"
)

var linkPattern = regexp.MustCompile(`https?://[^\s\)\]>]+`)

// GateResult represents the result of a gate check.
type GateResult struct {
	Gate   string
	Passed bool
	Reason string
}

// EvaluateGates checks all gates for the current stage by building a context from raw parameters.
func EvaluateGates(pipeline config.PipelineConfig, currentStage string, sections []markdown.Section, hasPRStack bool, prsApproved bool, meta *markdown.SpecMeta) []GateResult {
	var timeInStage time.Duration
	var revertCount int
	var specID, specTitle, specStatus string
	if meta != nil {
		// Parse updated timestamp to compute time-in-stage for duration gates.
		// Empty or malformed dates result in timeInStage=0, which may incorrectly pass
		// gates checking for minimum dwell time — only acceptable because such
		// gates should reject incomplete/corrupted specs as a safety measure anyway.
		if meta.Updated != "" {
			if updated, err := time.Parse("2006-01-02", meta.Updated); err == nil {
				timeInStage = time.Since(updated)
			}
		}
		revertCount = meta.RevertCount
		specID = meta.ID
		specTitle = meta.Title
		specStatus = meta.Status
	}

	// Build expression context from available data
	ctx := expr.NewContextBuilder().
		WithSpec(specID, specTitle, specStatus, nil, 0, timeInStage, revertCount).
		WithPRStack(hasPRStack, 0, 0, false, false).
		WithPRs(0, 0, prsApproved).
		Build()

	// Add sections to context
	for _, sec := range sections {
		ctx.Sections[sec.Slug] = expr.SectionContext{
			Empty:     strings.TrimSpace(sec.Content) == "",
			WordCount: len(strings.Fields(sec.Content)),
		}
	}

	return evaluateGatesWithBuiltContext(pipeline, currentStage, sections, ctx)
}

// EvaluateGatesWithContext checks all gates using a pre-built expression context.
func EvaluateGatesWithContext(pipeline config.PipelineConfig, currentStage string, ctx expr.Context) []GateResult {
	// Convert context sections to markdown.Section for compatibility
	var sections []markdown.Section
	for slug, sec := range ctx.Sections {
		content := ""
		if !sec.Empty {
			content = "non-empty" // placeholder for section check
		}
		sections = append(sections, markdown.Section{Slug: slug, Content: content})
	}

	return evaluateGatesWithBuiltContext(pipeline, currentStage, sections, ctx)
}

// evaluateGatesWithBuiltContext is the single source of truth for evaluating all gates on a stage.
func evaluateGatesWithBuiltContext(pipeline config.PipelineConfig, currentStage string, sections []markdown.Section, ctx expr.Context) []GateResult {
	stage := pipeline.StageByName(currentStage)
	if stage == nil {
		return nil
	}

	var results []GateResult
	for _, gate := range stage.Gates {
		result := evaluateGateWithContext(gate, sections, ctx.PRStack.Exists, ctx.PRs.Approved == ctx.PRs.Open, ctx)
		results = append(results, result)
	}
	return results
}

// AllGatesPassed returns true if all gates passed.
func AllGatesPassed(results []GateResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// FailedGates returns only the gates that did not pass.
func FailedGates(results []GateResult) []GateResult {
	var failed []GateResult
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r)
		}
	}
	return failed
}

func evaluateGateWithContext(gate config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool, ctx expr.Context) GateResult {
	// Handle logical operators first
	if len(gate.All) > 0 {
		return evaluateAllGateWithContext(gate.All, sections, hasPRStack, prsApproved, ctx)
	}
	if len(gate.Any) > 0 {
		return evaluateAnyGateWithContext(gate.Any, sections, hasPRStack, prsApproved, ctx)
	}
	if gate.Not != nil {
		return evaluateNotGateWithContext(*gate.Not, sections, hasPRStack, prsApproved, ctx)
	}

	// Handle simple gates
	if slug := gate.GetSectionNotEmpty(); slug != "" {
		gateType := gate.Type() // preserves "section_complete" vs "section_not_empty"
		if markdown.IsSectionNonEmpty(sections, slug) {
			return GateResult{Gate: fmt.Sprintf("%s: %s", gateType, slug), Passed: true}
		}
		return GateResult{
			Gate:   fmt.Sprintf("%s: %s", gateType, slug),
			Passed: false,
			Reason: fmt.Sprintf("section %q is empty — it must have content before advancing", humanizeSlug(slug)),
		}
	}

	if gate.PRStackExists != nil && *gate.PRStackExists {
		if hasPRStack {
			return GateResult{Gate: "pr_stack_exists", Passed: true}
		}
		return GateResult{
			Gate:   "pr_stack_exists",
			Passed: false,
			Reason: "PR stack plan (§7.3) is required — add the PR stack with 'spec edit' or 'spec draft --pr-stack'",
		}
	}

	if gate.PRsApproved != nil && *gate.PRsApproved {
		if prsApproved {
			return GateResult{Gate: "prs_approved", Passed: true}
		}
		return GateResult{
			Gate:   "prs_approved",
			Passed: false,
			Reason: "all PRs must be approved before advancing to QA validation",
		}
	}

	if gate.Duration != "" {
		// Duration gate: require spec to spend minimum time in current stage before advancing.
		// Computed from spec.Updated (when last modified) to now. If no valid updated date,
		// timeInStage will be 0, which will fail the gate — intentional safety measure.
		gateName := fmt.Sprintf("duration: %s", gate.Duration)
		d, err := time.ParseDuration(gate.Duration)
		if err != nil {
			return GateResult{Gate: gateName, Passed: false, Reason: fmt.Sprintf("invalid duration %q: %v", gate.Duration, err)}
		}
		if ctx.Spec.TimeInStage >= d {
			return GateResult{Gate: gateName, Passed: true}
		}
		remaining := d - ctx.Spec.TimeInStage
		return GateResult{
			Gate:   gateName,
			Passed: false,
			Reason: fmt.Sprintf("spec must remain in stage for %s (%s remaining)", gate.Duration, formatDuration(remaining)),
		}
	}

	if gate.Expr != "" {
		// Evaluate expression gate
		passed, err := expr.Evaluate(gate.Expr, ctx)
		if err != nil {
			return GateResult{
				Gate:   fmt.Sprintf("expr: %s", gate.Expr),
				Passed: false,
				Reason: fmt.Sprintf("expression error: %v", err),
			}
		}
		if passed {
			return GateResult{Gate: fmt.Sprintf("expr: %s", gate.Expr), Passed: true}
		}
		message := gate.Message
		if message == "" {
			message = fmt.Sprintf("expression %q evaluated to false", gate.Expr)
		}
		return GateResult{
			Gate:   fmt.Sprintf("expr: %s", gate.Expr),
			Passed: false,
			Reason: message,
		}
	}

	if gate.LinkExists != nil {
		gateName := fmt.Sprintf("link_exists: %s", gate.LinkExists.Section)
		section := markdown.FindSection(sections, gate.LinkExists.Section)
		if section == nil || strings.TrimSpace(section.Content) == "" {
			return GateResult{
				Gate:   gateName,
				Passed: false,
				Reason: fmt.Sprintf("section %q is empty or missing — add content with a link before advancing", humanizeSlug(gate.LinkExists.Section)),
			}
		}
		links := linkPattern.FindAllString(section.Content, -1)
		if len(links) == 0 {
			return GateResult{
				Gate:   gateName,
				Passed: false,
				Reason: fmt.Sprintf("no links found in section %q — add a URL before advancing", humanizeSlug(gate.LinkExists.Section)),
			}
		}
		if gate.LinkExists.Type != "" {
			domain := linkTypeToDomain(gate.LinkExists.Type)
			found := false
			for _, link := range links {
				if strings.Contains(strings.ToLower(link), domain) {
					found = true
					break
				}
			}
			if !found {
				return GateResult{
					Gate:   fmt.Sprintf("link_exists: %s (type: %s)", gate.LinkExists.Section, gate.LinkExists.Type),
					Passed: false,
					Reason: fmt.Sprintf("no %s link found in section %q", gate.LinkExists.Type, humanizeSlug(gate.LinkExists.Section)),
				}
			}
		}
		return GateResult{Gate: gateName, Passed: true}
	}

	// Unknown or empty gate
	return GateResult{
		Gate:   gate.Type(),
		Passed: true,
		Reason: fmt.Sprintf("unknown gate type %q — skipping", gate.Type()),
	}
}

// evaluateAllGateWithContext returns true only if ALL nested gates pass.
func evaluateAllGateWithContext(gates []config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool, ctx expr.Context) GateResult {
	var failedGates []string
	for _, g := range gates {
		result := evaluateGateWithContext(g, sections, hasPRStack, prsApproved, ctx)
		if !result.Passed {
			failedGates = append(failedGates, result.Gate)
		}
	}
	if len(failedGates) == 0 {
		return GateResult{Gate: "all", Passed: true}
	}
	return GateResult{
		Gate:   "all",
		Passed: false,
		Reason: fmt.Sprintf("failed gates: %s", strings.Join(failedGates, ", ")),
	}
}

// evaluateAnyGateWithContext returns true if ANY nested gate passes.
func evaluateAnyGateWithContext(gates []config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool, ctx expr.Context) GateResult {
	var allReasons []string
	for _, g := range gates {
		result := evaluateGateWithContext(g, sections, hasPRStack, prsApproved, ctx)
		if result.Passed {
			return GateResult{Gate: "any", Passed: true}
		}
		allReasons = append(allReasons, result.Reason)
	}
	return GateResult{
		Gate:   "any",
		Passed: false,
		Reason: fmt.Sprintf("none of the alternatives passed: %s", strings.Join(allReasons, "; ")),
	}
}

// evaluateNotGateWithContext returns true if the nested gate FAILS.
func evaluateNotGateWithContext(gate config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool, ctx expr.Context) GateResult {
	result := evaluateGateWithContext(gate, sections, hasPRStack, prsApproved, ctx)
	if !result.Passed {
		return GateResult{Gate: "not", Passed: true}
	}
	return GateResult{
		Gate:   "not",
		Passed: false,
		Reason: fmt.Sprintf("gate should not pass but did: %s", result.Gate),
	}
}

func humanizeSlug(slug string) string {
	return strings.ReplaceAll(slug, "_", " ")
}

func linkTypeToDomain(linkType string) string {
	switch strings.ToLower(linkType) {
	case "figma":
		return "figma.com"
	case "github":
		return "github.com"
	case "confluence":
		return "atlassian.net"
	case "jira":
		return "atlassian.net"
	default:
		return strings.ToLower(linkType)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}
	days := hours / 24
	remainHours := hours % 24
	if remainHours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, remainHours)
}
