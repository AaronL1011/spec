package pipeline

import (
	"fmt"
	"strings"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
)

// GateResult represents the result of a gate check.
type GateResult struct {
	Gate   string
	Passed bool
	Reason string
}

// EvaluateGates checks all gates for the current stage.
func EvaluateGates(pipeline config.PipelineConfig, currentStage string, sections []markdown.Section, hasPRStack bool, prsApproved bool) []GateResult {
	stage := pipeline.StageByName(currentStage)
	if stage == nil {
		return nil
	}

	var results []GateResult
	for _, gate := range stage.Gates {
		result := evaluateGate(gate, sections, hasPRStack, prsApproved)
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

func evaluateGate(gate config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool) GateResult {
	// Handle logical operators first
	if len(gate.All) > 0 {
		return evaluateAllGate(gate.All, sections, hasPRStack, prsApproved)
	}
	if len(gate.Any) > 0 {
		return evaluateAnyGate(gate.Any, sections, hasPRStack, prsApproved)
	}
	if gate.Not != nil {
		return evaluateNotGate(*gate.Not, sections, hasPRStack, prsApproved)
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
		// Duration gates are checked elsewhere (requires timestamp)
		// For now, pass them in validate mode
		return GateResult{Gate: fmt.Sprintf("duration: %s", gate.Duration), Passed: true}
	}

	if gate.Expr != "" {
		// Expression gates will be implemented in Phase 2
		// For now, pass them with a note
		return GateResult{
			Gate:   fmt.Sprintf("expr: %s", gate.Expr),
			Passed: true,
			Reason: "expression evaluation not yet implemented",
		}
	}

	if gate.LinkExists != nil {
		// Link exists gates will be implemented later
		return GateResult{
			Gate:   fmt.Sprintf("link_exists: %s", gate.LinkExists.Section),
			Passed: true,
			Reason: "link_exists gate not yet implemented",
		}
	}

	// Unknown or empty gate
	return GateResult{
		Gate:   gate.Type(),
		Passed: true,
		Reason: fmt.Sprintf("unknown gate type %q — skipping", gate.Type()),
	}
}

// evaluateAllGate returns true only if ALL nested gates pass.
func evaluateAllGate(gates []config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool) GateResult {
	var failedGates []string
	for _, g := range gates {
		result := evaluateGate(g, sections, hasPRStack, prsApproved)
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

// evaluateAnyGate returns true if ANY nested gate passes.
func evaluateAnyGate(gates []config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool) GateResult {
	var allReasons []string
	for _, g := range gates {
		result := evaluateGate(g, sections, hasPRStack, prsApproved)
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

// evaluateNotGate returns true if the nested gate FAILS.
func evaluateNotGate(gate config.GateConfig, sections []markdown.Section, hasPRStack bool, prsApproved bool) GateResult {
	result := evaluateGate(gate, sections, hasPRStack, prsApproved)
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
