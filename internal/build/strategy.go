package build

import (
	"fmt"
	"strings"
)

// BuildStrategy is the Tier-2 port: it owns the VCS/review workflow layered on
// top of the kernel's DAG traversal — which finishing tools the harness may
// call, and what "done" means for a build. The kernel always provisions a
// branch + worktree per node (a mechanism it owns); a strategy decides whether
// those branches become a stack of draft PRs, stay local, or something else.
// Strategy is pluggable policy: bringing a different one needs zero kernel
// change, and bringing none (local-only) is a first-class choice.
type BuildStrategy interface {
	// Name identifies the strategy (advertised via spec://current/capabilities).
	Name() string
	// FinishingTools lists the finishing tool names this strategy exposes, a
	// subset of {spec_push, spec_open_pr, spec_link_prs}. Empty means local-only:
	// the kernel commits to per-node branches and no remote/PR tools are offered.
	FinishingTools() []string
	// Complete reports terminal status given the node ledger and the DAG. It is
	// consulted once every node is complete to decide whether the build is fully
	// done (e.g. draft PRs present on stack leaves) or still needs a finishing
	// pass.
	Complete(session *SessionState, graph *Graph) Completion
}

// Completion is a strategy's verdict on a build whose nodes are all complete.
type Completion struct {
	// Done reports whether the build is fully finished under the strategy.
	Done bool
	// Summary is a human-readable one-line status.
	Summary string
	// Hint is an optional next action shown when the build is not yet done.
	Hint string
}

// allFinishingTools is the full finishing tool set the stacked-draft-pr strategy
// exposes; other strategies expose a subset (or none).
var allFinishingTools = []string{"spec_push", "spec_open_pr", "spec_link_prs"}

// newBuildStrategy selects a strategy from Options. The default ("" or
// "stacked-draft-pr") reproduces spec-cli's historical behaviour: a stack of
// draft PRs with completion defined by PRs on the stack leaves. "none"/"local"
// keeps work on local branches and exposes no finishing tools.
func newBuildStrategy(opts Options) BuildStrategy {
	switch strings.ToLower(strings.TrimSpace(opts.Strategy)) {
	case "none", "local", "local-branches":
		return noneStrategy{}
	default:
		return stackedDraftPRStrategy{}
	}
}

// stackedDraftPRStrategy is the default: each node becomes a branch stacked on
// its parent, finished as a draft PR retargeted along the stack. A build is done
// when every stack leaf carries a draft PR.
type stackedDraftPRStrategy struct{}

// Name implements BuildStrategy.
func (stackedDraftPRStrategy) Name() string { return "stacked-draft-pr" }

// FinishingTools implements BuildStrategy.
func (stackedDraftPRStrategy) FinishingTools() []string { return allFinishingTools }

// Complete implements BuildStrategy.
func (stackedDraftPRStrategy) Complete(session *SessionState, graph *Graph) Completion {
	n := len(session.Steps)
	applicable, withPR, missing := leafPRStatus(session, graph)
	switch {
	case applicable == 0:
		return Completion{Done: true, Summary: fmt.Sprintf("all %d node(s) complete", n)}
	case len(missing) == 0:
		return Completion{Done: true, Summary: fmt.Sprintf("%d node(s), %d draft PR(s) on stack leaves", n, withPR)}
	default:
		return Completion{
			Summary: fmt.Sprintf("nodes complete, but draft PRs are missing on stack leaves: %s", strings.Join(missing, ", ")),
			Hint:    "Run the PR finisher to push and open them (spec_push / spec_open_pr)",
		}
	}
}

// noneStrategy keeps every node on a local branch and exposes no finishing
// tools. A build is done once all nodes are complete; pushing and opening PRs is
// left entirely to the team's own workflow.
type noneStrategy struct{}

// Name implements BuildStrategy.
func (noneStrategy) Name() string { return "none" }

// FinishingTools implements BuildStrategy.
func (noneStrategy) FinishingTools() []string { return nil }

// Complete implements BuildStrategy.
func (noneStrategy) Complete(session *SessionState, _ *Graph) Completion {
	return Completion{Done: true, Summary: fmt.Sprintf("all %d node(s) complete (local branches, no PRs)", len(session.Steps))}
}

// exposesFinishingTool reports whether a strategy offers a given finishing tool.
func exposesFinishingTool(strategy BuildStrategy, tool string) bool {
	for _, t := range strategy.FinishingTools() {
		if t == tool {
			return true
		}
	}
	return false
}
