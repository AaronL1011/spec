package build

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// MCPServer serves spec context to MCP-compatible agents. It owns the DAG, the
// node ledger, and every git/worktree operation: the orchestrator reads the DAG
// once and checkpoints each node back through the node tools, while spec-cli
// keeps all branch/worktree mechanics on this side of the contract.
type MCPServer struct {
	session  *SessionState
	ctx      *BuildContext
	db       *store.DB
	specPath string
	opts     Options
	graph    *Graph
	// repo is the GitHub (or noop) adapter used by the PR tools. It may be nil
	// in contexts that never call them; the tools guard against that.
	repo adapter.RepoAdapter
}

// WithRepo injects the repo adapter used by the draft-PR tools and returns the
// server for chaining. Kept separate from the constructor so call sites that
// never open PRs need not thread an adapter through.
func (s *MCPServer) WithRepo(r adapter.RepoAdapter) *MCPServer {
	s.repo = r
	return s
}

// NewMCPServer creates a new MCP server for a build session. opts carries the
// workspace map (source repos for worktrees) and skill routing inputs. The DAG
// is built from the session's steps; a malformed plan yields a nil graph and a
// descriptive DAG resource rather than a panic.
func NewMCPServer(session *SessionState, buildCtx *BuildContext, db *store.DB, specPath string, opts Options) *MCPServer {
	s := &MCPServer{
		session:  session,
		ctx:      buildCtx,
		db:       db,
		specPath: specPath,
		opts:     opts,
	}
	if session != nil {
		if g, err := BuildGraph(session.Steps); err == nil {
			s.graph = g
		}
	}
	return s
}

// NodeToolNames lists the build-session tools this server adds on top of the
// generic spec tools. Exposed so the combined MCP handler can advertise them.
func NodeToolNames() []string {
	return []string{
		"spec_provision_node", "spec_node_complete", "spec_node_failed",
		"spec_push", "spec_open_pr", "spec_link_prs",
	}
}

// MCPResource represents a resource served by the MCP server.
type MCPResource struct {
	URI     string `json:"uri"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ListResources returns all available resources.
func (s *MCPServer) ListResources() []MCPResource {
	resources := []MCPResource{
		{
			URI:     "spec://current/full",
			Name:    fmt.Sprintf("Full spec: %s", s.session.SpecID),
			Content: s.ctx.SpecContent,
		},
		{
			URI:     "spec://current/decisions",
			Name:    "Decision log",
			Content: s.getDecisionLog(),
		},
		{
			URI:     "spec://current/acceptance-criteria",
			Name:    "Acceptance criteria",
			Content: s.getSection("acceptance_criteria"),
		},
	}

	if s.ctx.Conventions != "" {
		resources = append(resources, MCPResource{
			URI:     "spec://current/conventions",
			Name:    "Project conventions",
			Content: s.ctx.Conventions,
		})
	}

	if len(s.ctx.PriorDiffs) > 0 {
		var diffs strings.Builder
		for i, diff := range s.ctx.PriorDiffs {
			fmt.Fprintf(&diffs, "## Step %d\n\n```diff\n%s\n```\n\n", i+1, diff)
		}
		resources = append(resources, MCPResource{
			URI:     "spec://current/prior-diffs",
			Name:    "Prior step diffs",
			Content: diffs.String(),
		})
	}

	resources = append(resources, MCPResource{
		URI:     "spec://current/dag",
		Name:    "Build DAG",
		Content: s.dagJSON(),
	})

	return resources
}

// dagNode is the JSON shape of a node in the spec://current/dag resource.
type dagNode struct {
	ID         string   `json:"id"`
	Number     int      `json:"number"`
	Repo       string   `json:"repo,omitempty"`
	Layer      string   `json:"layer,omitempty"`
	DependsOn  []string `json:"dependsOn"`
	Status     string   `json:"status"`
	Branch     string   `json:"branch,omitempty"`
	SkillPaths []string `json:"skillPaths"`
}

// dagDocument is the top-level JSON shape of spec://current/dag.
type dagDocument struct {
	SpecID      string     `json:"specId"`
	MaxParallel int        `json:"maxParallel"`
	Nodes       []dagNode  `json:"nodes"`
	Waves       [][]string `json:"waves"`
	Error       string     `json:"error,omitempty"`
}

// dagJSON renders the DAG resource the orchestrator reads once to plan its walk.
func (s *MCPServer) dagJSON() string {
	doc := dagDocument{SpecID: s.session.SpecID, MaxParallel: s.opts.MaxParallel, Nodes: []dagNode{}, Waves: [][]string{}}
	if s.graph == nil {
		doc.Error = "PR stack did not form a valid DAG — check §7.3 (after: ...) edges"
		b, _ := json.MarshalIndent(doc, "", "  ")
		return string(b)
	}

	for _, n := range s.graph.Nodes {
		id := n.NodeID()
		deps := s.graph.Dependencies(id)
		if deps == nil {
			deps = []string{}
		}
		skills := s.skillsForNode(n)
		if skills == nil {
			skills = []string{}
		}
		doc.Nodes = append(doc.Nodes, dagNode{
			ID:         id,
			Number:     n.Number,
			Repo:       n.Repo,
			Layer:      n.Layer,
			DependsOn:  deps,
			Status:     s.session.NodeStatus(id),
			Branch:     s.session.node(id).Branch,
			SkillPaths: skills,
		})
	}
	for _, wave := range s.graph.Waves() {
		var ids []string
		for _, n := range wave {
			ids = append(ids, n.NodeID())
		}
		doc.Waves = append(doc.Waves, ids)
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"specId":%q,"error":"marshalling dag: %v"}`, s.session.SpecID, err)
	}
	return string(b)
}

// GetResource returns a specific resource by URI.
func (s *MCPServer) GetResource(uri string) (*MCPResource, error) {
	// Handle section resources
	if strings.HasPrefix(uri, "spec://current/section/") {
		slug := strings.TrimPrefix(uri, "spec://current/section/")
		content := s.getSection(slug)
		if content == "" {
			return nil, fmt.Errorf("section %q not found", slug)
		}
		return &MCPResource{URI: uri, Name: slug, Content: content}, nil
	}

	for _, r := range s.ListResources() {
		if r.URI == uri {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("resource %q not found", uri)
}

// MCPToolResult represents the result of an MCP tool call.
type MCPToolResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// CallTool executes an MCP tool.
func (s *MCPServer) CallTool(name string, args json.RawMessage) (*MCPToolResult, error) {
	switch name {
	case "spec_decide":
		return s.toolDecide(args)
	case "spec_decide_resolve":
		return s.toolDecideResolve(args)
	case "spec_provision_node":
		return s.toolProvisionNode(args)
	case "spec_node_complete":
		return s.toolNodeComplete(args)
	case "spec_node_failed":
		return s.toolNodeFailed(args)
	case "spec_push":
		return s.toolPush(args)
	case "spec_open_pr":
		return s.toolOpenPR(args)
	case "spec_link_prs":
		return s.toolLinkPRs(args)
	case "spec_status":
		return s.toolStatus()
	case "spec_search":
		return s.toolSearch(args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *MCPServer) toolDecide(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	num, err := markdown.AppendDecision(s.specPath, params.Question, "agent")
	if err != nil {
		return &MCPToolResult{Success: false, Message: err.Error()}, nil
	}

	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Decision #%03d: %s", num, params.Question))

	return &MCPToolResult{
		Success: true,
		Message: fmt.Sprintf("Decision #%03d recorded: %s", num, params.Question),
	}, nil
}

func (s *MCPServer) toolDecideResolve(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		Number    int    `json:"number"`
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	if err := markdown.ResolveDecision(s.specPath, params.Number, params.Decision, params.Rationale, "agent"); err != nil {
		return &MCPToolResult{Success: false, Message: err.Error()}, nil
	}

	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Decision #%03d resolved: %s", params.Number, params.Decision))

	return &MCPToolResult{
		Success: true,
		Message: fmt.Sprintf("Decision #%03d resolved: %s", params.Number, params.Decision),
	}, nil
}

func (s *MCPServer) toolStatus() (*MCPToolResult, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Spec: %s\n", s.session.SpecID)
	fmt.Fprintf(&sb, "Step: %d/%d\n", s.session.CurrentStep, len(s.session.Steps))
	for _, step := range s.session.Steps {
		var marker string
		switch step.Status {
		case "in-progress":
			marker = "▶ "
		case "complete":
			marker = "✓ "
		default:
			marker = "  "
		}
		fmt.Fprintf(&sb, "%s%d. [%s] %s (%s)\n", marker, step.Number, step.Repo, step.Description, step.Status)
	}

	return &MCPToolResult{Success: true, Message: sb.String()}, nil
}

func (s *MCPServer) toolSearch(args json.RawMessage) (*MCPToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Simple keyword search in the spec itself for now
	// Full knowledge engine search will be added in Phase 4
	var matches []string
	for _, line := range strings.Split(s.ctx.SpecContent, "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(params.Query)) {
			matches = append(matches, strings.TrimSpace(line))
		}
	}

	if len(matches) == 0 {
		return &MCPToolResult{Success: true, Message: "No matches found."}, nil
	}

	result := fmt.Sprintf("Found %d matches:\n", len(matches))
	for _, m := range matches {
		if len(m) > 200 {
			m = m[:200] + "..."
		}
		result += "  • " + m + "\n"
	}

	return &MCPToolResult{Success: true, Message: result}, nil
}

func (s *MCPServer) getSection(slug string) string {
	body := markdown.Body(s.ctx.SpecContent)
	sections := markdown.ExtractSections(body)
	sec := markdown.FindSection(sections, slug)
	if sec == nil {
		return ""
	}
	return sec.Content
}

func (s *MCPServer) getDecisionLog() string {
	body := markdown.Body(s.ctx.SpecContent)
	sections := markdown.ExtractSections(body)
	dl := markdown.FindSection(sections, "decision_log")
	if dl == nil {
		return ""
	}
	return dl.Content
}
