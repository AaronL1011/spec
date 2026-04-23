package build

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/store"
)

// MCPServer serves spec context to MCP-compatible agents.
// This is a simplified implementation that handles the core resource/tool protocol.
type MCPServer struct {
	session  *SessionState
	ctx      *BuildContext
	db       *store.DB
	specPath string
}

// NewMCPServer creates a new MCP server for a build session.
func NewMCPServer(session *SessionState, buildCtx *BuildContext, db *store.DB, specPath string) *MCPServer {
	return &MCPServer{
		session:  session,
		ctx:      buildCtx,
		db:       db,
		specPath: specPath,
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

	return resources
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
	case "spec_step_complete":
		return s.toolStepComplete()
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

func (s *MCPServer) toolStepComplete() (*MCPToolResult, error) {
	if s.session == nil || s.db == nil {
		return &MCPToolResult{Success: false, Message: "no active session"}, nil
	}

	step := s.session.CurrentPRStep()
	if step == nil {
		return &MCPToolResult{Success: false, Message: "no current step"}, nil
	}

	_ = LogActivity(s.session.SpecID, fmt.Sprintf("Step %d completed via MCP: %s", step.Number, step.Description))

	if err := AdvanceStep(s.db, s.session); err != nil {
		return &MCPToolResult{Success: false, Message: err.Error()}, nil
	}

	msg := fmt.Sprintf("Step %d completed.", step.Number)
	if !s.session.IsComplete() {
		next := s.session.CurrentPRStep()
		msg += fmt.Sprintf(" Next: Step %d — [%s] %s", next.Number, next.Repo, next.Description)
	} else {
		msg += " All steps complete!"
	}

	return &MCPToolResult{Success: true, Message: msg}, nil
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

// ToolDefinitions returns the MCP tool definitions for advertising.
func ToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "spec_decide",
			"description": "Record a question or decision to the spec's decision log",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{
						"type":        "string",
						"description": "The question or decision to record",
					},
				},
				"required": []string{"question"},
			},
		},
		{
			"name":        "spec_decide_resolve",
			"description": "Resolve an existing decision in the decision log",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"number":    map[string]interface{}{"type": "integer", "description": "Decision number to resolve"},
					"decision":  map[string]interface{}{"type": "string", "description": "The decision made"},
					"rationale": map[string]interface{}{"type": "string", "description": "Rationale for the decision"},
				},
				"required": []string{"number", "decision"},
			},
		},
		{
			"name":        "spec_step_complete",
			"description": "Mark the current PR stack step as complete and advance to the next",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "spec_status",
			"description": "Check current pipeline status and build progress",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "spec_search",
			"description": "Search the spec and knowledge base for context",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query"},
				},
				"required": []string{"query"},
			},
		},
	}
}
