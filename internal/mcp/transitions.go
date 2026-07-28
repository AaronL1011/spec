package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/workflow"
)

// authoring-port/v1, transition tier.
//
// Stage transitions are opt-in and absent from tools/list unless
// preferences.agent_authoring.transitions is set. Section writes are on by
// default because they are recoverable from the specs-repo diff; a transition is
// not symmetric with them — it moves work through a human review pipeline and
// fires the pipeline's configured effects (Jira status sync, Slack posts), so an
// agent doing it unprompted would notify a team on the user's behalf.
//
// An absent tool is also self-documenting: a disabled capability is missing from
// tools/list rather than failing at call time, so a model discovers the boundary
// by looking rather than by trying.
//
// When enabled, transitions fire effects exactly as the CLI does (decision 008).
// Suppressing them for agent callers would create a second, quieter kind of
// advance whose side effects surface later and out of order, which is harder to
// reason about than a notification the user did not personally type. The
// authority question is answered by gating the tool, not by weakening the
// transition.

// transitionTools returns the opt-in transition tier.
func transitionTools() []Tool {
	return []Tool{
		{
			Name: "spec_advance",
			Description: "Advance a spec to the next pipeline stage. Runs the same gates as the CLI " +
				"and fires the pipeline's configured transition effects.",
			InputSchema: objectSchema(map[string]interface{}{
				"id": stringProp("Spec ID (e.g., 'SPEC-042')"),
			}, "id"),
		},
		{
			Name:        "spec_revert",
			Description: "Revert a spec to the previous pipeline stage with a reason.",
			InputSchema: objectSchema(map[string]interface{}{
				"id":     stringProp("Spec ID (e.g., 'SPEC-042')"),
				"reason": stringProp("Why the spec is being reverted"),
			}, "id", "reason"),
		},
	}
}

// transitionsEnabled reports whether the transition tier is available.
//
// Defaults to false, including when there is no config at all: an agent must
// never gain transition authority by virtue of a missing file.
func (h *GenericHandler) transitionsEnabled() bool {
	return h.config != nil && h.config.AgentTransitionsAllowed()
}

// transitionDeps assembles the workflow dependencies for an agent-initiated
// transition. Returns nil when the handler lacks what a transition needs.
func (h *GenericHandler) transitionDeps() (*workflow.Deps, error) {
	if h.config == nil || h.config.Team == nil {
		return nil, fmt.Errorf("no team config available — transitions need the pipeline definition")
	}
	if h.registry == nil {
		return nil, fmt.Errorf("no adapter registry available")
	}
	return &workflow.Deps{
		Config:   h.config,
		Registry: h.registry,
		DB:       h.db,
		Role:     h.config.OwnerRole(""),
	}, nil
}

// toolAdvance advances a spec through the same engine and gates as the CLI.
func (h *GenericHandler) toolAdvance(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if !h.transitionsEnabled() {
		return disabledTransitionResult(), nil
	}

	deps, err := h.transitionDeps()
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}
	specID := strings.ToUpper(params.ID)

	res, err := workflow.Advance(context.Background(), *deps, workflow.AdvanceInput{
		SpecID:   specID,
		SpecPath: h.specPath(specID),
		SpecDir:  h.specsDir,
	})
	if err != nil {
		// An unmet gate is an expected, structured outcome: it names what is
		// missing so the agent can act, rather than reading as a tool failure.
		return &ToolResult{Success: false, Message: fmt.Sprintf("GATE NOT MET: %v", err)}, nil
	}
	if len(res.GateFailures) > 0 {
		return &ToolResult{
			Success: false,
			Message: fmt.Sprintf("GATE NOT MET: %s", gateFailureSummary(res.GateFailures)),
		}, nil
	}

	h.logAgentWrite(specID, "advance", fmt.Sprintf("agent advanced %s to %s", specID, res.NewStage))
	h.publisher.Notify(specID)

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Advanced %s: %s → %s", specID, res.PreviousStage, res.NewStage),
	}, nil
}

// gateFailureSummary renders unmet gates so the message names what is missing
// rather than only that something was.
func gateFailureSummary(failures []pipeline.GateResult) string {
	parts := make([]string, 0, len(failures))
	for _, f := range failures {
		if f.Passed {
			continue
		}
		if f.Reason != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", f.Gate, f.Reason))
			continue
		}
		parts = append(parts, f.Gate)
	}
	return strings.Join(parts, "; ")
}

// toolRevert reverts a spec with a reason.
func (h *GenericHandler) toolRevert(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if !h.transitionsEnabled() {
		return disabledTransitionResult(), nil
	}
	if strings.TrimSpace(params.Reason) == "" {
		return &ToolResult{Success: false, Message: "reason is required for a revert"}, nil
	}

	deps, err := h.transitionDeps()
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}
	specID := strings.ToUpper(params.ID)

	res, err := workflow.Revert(context.Background(), *deps, workflow.RevertInput{
		SpecID:   specID,
		SpecPath: h.specPath(specID),
		SpecDir:  h.specsDir,
		Reason:   params.Reason,
	})
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	h.logAgentWrite(specID, "revert", fmt.Sprintf("agent reverted %s to %s: %s", specID, res.TargetStage, params.Reason))
	h.publisher.Notify(specID)

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Reverted %s: %s → %s", specID, res.PreviousStage, res.TargetStage),
	}, nil
}

// disabledTransitionResult names the preference that enables the tier.
//
// Reachable only when a client calls a tool that tools/list did not offer, so the
// message's job is to explain the boundary rather than to be discovered.
func disabledTransitionResult() *ToolResult {
	return &ToolResult{
		Success: false,
		Message: "stage transitions are disabled for agent sessions — set 'preferences.agent_authoring.transitions: true' in ~/.spec/config.yaml to enable",
	}
}
