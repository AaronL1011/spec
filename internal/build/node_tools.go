package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gitpkg "github.com/aaronl1011/spec/internal/git"
)

// provisionResult is the JSON payload returned by spec_provision_node. pi reads
// it and dispatches a worker with cwd=WorkDir; all git mechanics stayed here.
type provisionResult struct {
	NodeID     string   `json:"nodeId"`
	WorkDir    string   `json:"workDir"`
	Branch     string   `json:"branch"`
	BaseRef    string   `json:"baseRef"`
	SkillPaths []string `json:"skillPaths"`
}

// repoPathForNode resolves the source repo a node's worktree is added to,
// delegating to the shared resolver so engine validation and provisioning agree.
func (s *MCPServer) repoPathForNode(node PRStep) (string, error) {
	return resolveRepoPath(node.Repo, s.session.WorkDir, s.opts.Workspaces)
}

// skillsForNode selects the skill paths for a node via the configured router.
func (s *MCPServer) skillsForNode(node PRStep) []string {
	return newSkillRouter(s.session.WorkDir, s.opts).Route(node)
}

// toolProvisionNode computes a node's base ref, creates its branch + worktree,
// records the placement in the ledger, and returns the worker's working dir.
func (s *MCPServer) toolProvisionNode(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if s.graph == nil {
		return &MCPToolResult{Success: false, Message: "no valid DAG for this session"}, nil
	}
	node, ok := s.graph.Node(params.NodeID)
	if !ok {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf("unknown node %q", params.NodeID)}, nil
	}

	repoPath, err := s.repoPathForNode(node)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	parentBranches, err := s.parentBranchesInRepo(node)
	if err != nil {
		return nil, err
	}

	branch := gitpkg.SpecBranchName(s.session.SpecID, node.Number, node.Description)
	integrationName := fmt.Sprintf("%s-integrate", branch)
	baseRef, err := gitpkg.ComputeBaseRef(ctx, repoPath, "", parentBranches, integrationName)
	if err != nil {
		return nil, fmt.Errorf("computing base ref for node %s: %w", node.NodeID(), err)
	}

	workDir, err := gitpkg.AddWorktree(ctx, repoPath, branch, baseRef)
	if err != nil {
		return nil, err
	}

	// Record placement in the ledger so resume, diff capture, and PR stacking
	// can find the node's branch/base/worktree later.
	ledger := s.session.node(node.NodeID())
	ledger.Branch = branch
	ledger.BaseRef = baseRef
	ledger.Worktree = workDir
	if ledger.Status == NodePending || ledger.Status == NodeFailed {
		ledger.Status = NodeInProgress
	}
	if err := SaveSession(s.db, s.session); err != nil {
		return nil, fmt.Errorf("saving session after provisioning %s: %w", node.NodeID(), err)
	}
	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Node %s provisioned on %s (base %s)", node.NodeID(), branch, baseRef))

	payload, _ := json.MarshalIndent(provisionResult{
		NodeID:     node.NodeID(),
		WorkDir:    workDir,
		Branch:     branch,
		BaseRef:    baseRef,
		SkillPaths: s.skillsForNode(node),
	}, "", "  ")
	return &MCPToolResult{Success: true, Message: string(payload)}, nil
}

// parentBranchesInRepo returns the recorded branches of a node's parents that
// live in the same repo (cross-repo parents only gate readiness, they can't be
// a git base). An unprovisioned same-repo parent is an actionable error.
func (s *MCPServer) parentBranchesInRepo(node PRStep) ([]string, error) {
	var branches []string
	for _, depID := range s.graph.Dependencies(node.NodeID()) {
		parent, ok := s.graph.Node(depID)
		if !ok || parent.Repo != node.Repo {
			continue
		}
		branch := s.session.node(depID).Branch
		if branch == "" {
			return nil, fmt.Errorf(
				"node %s depends on %s in the same repo, but %s has not been provisioned yet — provision parents first",
				node.NodeID(), depID, depID)
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

// toolNodeComplete marks a node complete and captures its diff keyed by node id.
// It is idempotent: completing an already-complete node re-affirms success.
func (s *MCPServer) toolNodeComplete(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if s.session == nil || s.db == nil {
		return &MCPToolResult{Success: false, Message: "no active session"}, nil
	}
	if _, ok := s.session.Nodes[params.NodeID]; !ok && s.graph != nil {
		if _, exists := s.graph.Node(params.NodeID); !exists {
			return &MCPToolResult{Success: false, Message: fmt.Sprintf("unknown node %q", params.NodeID)}, nil
		}
	}

	ledger := s.session.node(params.NodeID)
	already := ledger.Status == NodeComplete
	s.captureNodeDiff(params.NodeID, ledger)
	s.session.MarkNodeComplete(params.NodeID)
	if err := SaveSession(s.db, s.session); err != nil {
		return nil, fmt.Errorf("saving session after completing %s: %w", params.NodeID, err)
	}

	verb := "completed"
	if already {
		verb = "already complete"
	}
	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Node %s %s via MCP", params.NodeID, verb))
	msg := fmt.Sprintf("Node %s %s.", params.NodeID, verb)
	if s.session.NodesComplete() {
		msg += " All nodes complete!"
	}
	return &MCPToolResult{Success: true, Message: msg}, nil
}

// captureNodeDiff writes the node's diff (base ref → worktree HEAD) so later
// nodes and reporting see it. Best-effort: missing placement is skipped.
func (s *MCPServer) captureNodeDiff(nodeID string, ledger *NodeState) {
	if ledger.BaseRef == "" || ledger.Worktree == "" {
		return
	}
	diff, err := gitpkg.Diff(context.Background(), ledger.Worktree, ledger.BaseRef)
	if err != nil || diff == "" {
		return
	}
	path := filepath.Join(SessionDir(s.session.SpecID), fmt.Sprintf("node-%s.diff", nodeID))
	_ = os.WriteFile(path, []byte(diff), 0o644)
}

// toolNodeFailed records a node failure with a reason so resume and reporting
// can surface it.
func (s *MCPServer) toolNodeFailed(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if s.session == nil || s.db == nil {
		return &MCPToolResult{Success: false, Message: "no active session"}, nil
	}
	s.session.MarkNodeFailed(params.NodeID, params.Reason)
	if err := SaveSession(s.db, s.session); err != nil {
		return nil, fmt.Errorf("saving session after failing %s: %w", params.NodeID, err)
	}
	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Node %s failed: %s", params.NodeID, params.Reason))
	return &MCPToolResult{Success: true, Message: fmt.Sprintf("Node %s marked failed.", params.NodeID)}, nil
}
