package build

import (
	"context"
	"encoding/json"
	"fmt"

	gitpkg "github.com/aaronl1011/spec/internal/git"
)

// prResult is the JSON payload returned by spec_open_pr.
type prResult struct {
	NodeID string `json:"nodeId"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	Base   string `json:"base"`
}

// toolPush pushes a node's branch to origin from its worktree. Push stays in
// internal/git per the architecture rule; the finisher skill calls this tool
// instead of shelling out.
func (s *MCPServer) toolPush(args json.RawMessage) (*MCPToolResult, error) {
	var p struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	node := s.session.node(p.NodeID)
	if node.Branch == "" || node.Worktree == "" {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf(
			"node %s is not provisioned — call spec_provision_node first", p.NodeID)}, nil
	}
	if err := gitpkg.Push(context.Background(), node.Worktree, node.Branch); err != nil {
		return nil, fmt.Errorf("pushing node %s branch %s: %w", p.NodeID, node.Branch, err)
	}
	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Node %s pushed (%s)", p.NodeID, node.Branch))
	return &MCPToolResult{Success: true, Message: fmt.Sprintf("Pushed %s.", node.Branch)}, nil
}

// toolOpenPR opens a DRAFT pull request for a node: head is the node branch,
// base is its recorded BaseRef (parent branch / integration branch / default),
// which yields correct stack chaining. The PR number and URL are recorded in
// the ledger. Idempotent: a node that already has a PR returns it unchanged.
func (s *MCPServer) toolOpenPR(args json.RawMessage) (*MCPToolResult, error) {
	var p struct {
		NodeID string `json:"node_id"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return &MCPToolResult{Success: false, Message: "no repo adapter configured — set integrations.repo to open PRs"}, nil
	}
	node, ok := s.graphNode(p.NodeID)
	if !ok {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf("unknown node %q", p.NodeID)}, nil
	}
	if node.Repo == "" {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf("node %s has no repo — cannot open a PR", p.NodeID)}, nil
	}
	ledger := s.session.node(p.NodeID)
	if ledger.Branch == "" || ledger.BaseRef == "" {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf(
			"node %s is not provisioned — call spec_provision_node first", p.NodeID)}, nil
	}
	if ledger.PRURL != "" {
		payload, _ := json.MarshalIndent(prResult{NodeID: p.NodeID, Number: ledger.PRNumber, URL: ledger.PRURL, Base: ledger.BaseRef}, "", "  ")
		return &MCPToolResult{Success: true, Message: string(payload)}, nil
	}

	title := p.Title
	if title == "" {
		title = fmt.Sprintf("%s %s: %s", s.session.SpecID, p.NodeID, node.Description)
	}
	number, url, err := s.repo.OpenDraftPR(context.Background(), node.Repo, ledger.Branch, ledger.BaseRef, title, p.Body)
	if err != nil {
		return nil, fmt.Errorf("opening draft PR for node %s: %w", p.NodeID, err)
	}

	ledger.PRNumber = number
	ledger.PRURL = url
	if err := SaveSession(s.db, s.session); err != nil {
		return nil, fmt.Errorf("saving session after opening PR for %s: %w", p.NodeID, err)
	}
	// Record the PR into §7.3 so the pr_stack_exists gate can verify coverage
	// from the spec. Best-effort: the ledger already holds the authoritative URL.
	if s.specPath != "" {
		if err := recordPRInSpec(s.specPath, node.Number, url); err != nil {
			_ = LogActivity(s.session.SpecID, fmt.Sprintf("Warning: could not record PR for %s in spec: %v", p.NodeID, err))
		}
	}
	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Node %s draft PR #%d opened: %s", p.NodeID, number, url))

	payload, _ := json.MarshalIndent(prResult{NodeID: p.NodeID, Number: number, URL: url, Base: ledger.BaseRef}, "", "  ")
	return &MCPToolResult{Success: true, Message: string(payload)}, nil
}

// toolLinkPRs re-chains the PR stack by retargeting each node's open PR to its
// recorded base ref. With {node_id, base} it retargets a single PR (used to
// retarget a child to the default branch once its parent merges). Idempotent.
func (s *MCPServer) toolLinkPRs(args json.RawMessage) (*MCPToolResult, error) {
	var p struct {
		NodeID string `json:"node_id"`
		Base   string `json:"base"`
	}
	_ = json.Unmarshal(args, &p) // all fields optional
	if s.repo == nil {
		return &MCPToolResult{Success: false, Message: "no repo adapter configured — set integrations.repo to link PRs"}, nil
	}

	if p.NodeID != "" {
		node, ok := s.graphNode(p.NodeID)
		if !ok {
			return &MCPToolResult{Success: false, Message: fmt.Sprintf("unknown node %q", p.NodeID)}, nil
		}
		ledger := s.session.node(p.NodeID)
		if ledger.PRNumber == 0 {
			return &MCPToolResult{Success: false, Message: fmt.Sprintf("node %s has no open PR to retarget", p.NodeID)}, nil
		}
		base := p.Base
		if base == "" {
			base = ledger.BaseRef
		}
		if err := s.repo.SetPRBase(context.Background(), node.Repo, ledger.PRNumber, base); err != nil {
			return nil, fmt.Errorf("retargeting PR for node %s: %w", p.NodeID, err)
		}
		return &MCPToolResult{Success: true, Message: fmt.Sprintf("PR #%d (node %s) retargeted to %s.", ledger.PRNumber, p.NodeID, base)}, nil
	}

	count := 0
	for _, step := range s.session.Steps {
		ledger := s.session.node(step.NodeID())
		if ledger.PRNumber == 0 || ledger.BaseRef == "" {
			continue
		}
		if err := s.repo.SetPRBase(context.Background(), step.Repo, ledger.PRNumber, ledger.BaseRef); err != nil {
			return nil, fmt.Errorf("linking PR for node %s: %w", step.NodeID(), err)
		}
		count++
	}
	return &MCPToolResult{Success: true, Message: fmt.Sprintf("Linked %d PR(s) to their stack bases.", count)}, nil
}

// graphNode returns a node from the graph, falling back to a linear scan of the
// session steps when the graph is unavailable.
func (s *MCPServer) graphNode(id string) (PRStep, bool) {
	if s.graph != nil {
		return s.graph.Node(id)
	}
	for _, step := range s.session.Steps {
		if step.NodeID() == id {
			return step, true
		}
	}
	return PRStep{}, false
}
