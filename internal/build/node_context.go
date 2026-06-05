package build

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nodeContextDoc is the deterministic, per-node slice returned by
// spec_node_context. It is everything a worker needs to build one node without
// re-reading the whole spec: identity, dependencies, routed skills, the
// acceptance criteria the node must satisfy, and the gates it must pass. It is a
// projection of data the kernel already owns — no agent/model concerns.
type nodeContextDoc struct {
	SchemaVersion      string   `json:"schemaVersion"`
	NodeID             string   `json:"nodeId"`
	Number             int      `json:"number"`
	Repo               string   `json:"repo,omitempty"`
	Layer              string   `json:"layer,omitempty"`
	Description        string   `json:"description"`
	Status             string   `json:"status"`
	DependsOn          []string `json:"dependsOn"`
	Branch             string   `json:"branch,omitempty"`
	SkillPaths         []string `json:"skillPaths"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	QualityGates       []string `json:"qualityGates"`
}

// toolNodeContext returns the deterministic per-node context document. An
// unknown node id yields an actionable error rather than a panic.
func (s *MCPServer) toolNodeContext(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.NodeID) == "" {
		return &MCPToolResult{Success: false, Message: "node_id is required"}, nil
	}
	doc, ok := s.nodeContextJSON(params.NodeID)
	if !ok {
		return &MCPToolResult{Success: false, Message: fmt.Sprintf("node %q not found in the DAG", params.NodeID)}, nil
	}
	return &MCPToolResult{Success: true, Message: doc}, nil
}

// nodeContextJSON renders the per-node context document for a node id. It
// degrades gracefully: an unknown id yields an error result the caller surfaces,
// not a panic, and missing optional inputs (no registry, no AC mapping) simply
// produce empty arrays.
func (s *MCPServer) nodeContextJSON(nodeID string) (string, bool) {
	if s.graph == nil {
		return "", false
	}
	var node PRStep
	found := false
	for _, n := range s.graph.Nodes {
		if n.NodeID() == nodeID {
			node, found = n, true
			break
		}
	}
	if !found {
		return "", false
	}

	deps := s.graph.Dependencies(nodeID)
	if deps == nil {
		deps = []string{}
	}
	skills := s.skillsForNode(node)
	if skills == nil {
		skills = []string{}
	}
	doc := nodeContextDoc{
		SchemaVersion:      DAGSchemaVersion,
		NodeID:             nodeID,
		Number:             node.Number,
		Repo:               node.Repo,
		Layer:              node.Layer,
		Description:        node.Description,
		Status:             s.session.NodeStatus(nodeID),
		DependsOn:          deps,
		Branch:             s.session.node(nodeID).Branch,
		SkillPaths:         skills,
		AcceptanceCriteria: emptyIfNil(s.acTextForNode(node)),
		QualityGates:       emptyIfNil(s.gatesForNode(node)),
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// acTextForNode resolves a node's `(ac: …)` indices to the acceptance-criteria
// text from the spec. An out-of-range index is rendered as a clear marker rather
// than silently dropped, so a stale mapping is visible.
func (s *MCPServer) acTextForNode(node PRStep) []string {
	if len(node.ACs) == 0 {
		return nil
	}
	items := acItems(s.getSection("acceptance_criteria"))
	var out []string
	for _, idx := range node.ACs {
		if idx >= 1 && idx <= len(items) {
			out = append(out, items[idx-1])
		} else {
			out = append(out, fmt.Sprintf("(ac %d: out of range — §6 has %d criteria)", idx, len(items)))
		}
	}
	return out
}

// gatesForNode resolves the registry quality gates for a node via the node's
// resolved repo, mirroring how skills are routed.
func (s *MCPServer) gatesForNode(node PRStep) []string {
	repoPath, err := resolveRepoPath(node.Repo, s.session.WorkDir, s.opts.Workspaces)
	if err != nil || repoPath == "" {
		repoPath = s.session.WorkDir
	}
	return qualityGatesForNode(repoPath, node)
}

// acItems extracts the ordered acceptance-criteria items from an AC section,
// stripping the leading checkbox/bullet marker so indices line up with `(ac: N)`
// references in §7.3.
func acItems(content string) []string {
	var items []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"- [ ]", "- [x]", "- [X]"} {
			if rest, ok := strings.CutPrefix(trimmed, prefix); ok {
				items = append(items, strings.TrimSpace(rest))
				break
			}
		}
	}
	return items
}

// emptyIfNil normalises a nil slice to an empty one for stable JSON output.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
