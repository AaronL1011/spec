package mcp

import (
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
	"github.com/aaronl1011/spec/internal/pipeline/expr"
)

// specGraph builds the parent/child graph over the handler's specs directory.
func (h *GenericHandler) specGraph() (*hierarchy.Graph, error) {
	var team *config.TeamConfig
	if h.config != nil {
		team = h.config.Team
	}
	return hierarchy.Load(h.specsDir, config.ArchiveDir(team))
}

// childrenContext returns a spec's deliverable-slice rollup for gate
// evaluation, degrading to the zero value when the graph cannot be built. The
// MCP server must serve reads in a bare checkout with no config, so this can
// never be the thing that fails a tool call.
func (h *GenericHandler) childrenContext(specID string) expr.ChildrenContext {
	g, err := h.specGraph()
	if err != nil || h.config == nil {
		return expr.ChildrenContext{}
	}
	return g.Rollup(specID, h.config.Pipeline()).ExprContext()
}
