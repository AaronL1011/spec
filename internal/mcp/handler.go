package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
)

// GenericHandler provides spec resources and tools without requiring a build session.
// It serves as the default MCP handler, providing access to specs, pipeline info,
// and common operations.
type GenericHandler struct {
	config   *config.ResolvedConfig
	specsDir string // Path to specs repo
}

// NewGenericHandler creates a handler that serves generic spec resources.
func NewGenericHandler(cfg *config.ResolvedConfig, specsDir string) *GenericHandler {
	return &GenericHandler{
		config:   cfg,
		specsDir: specsDir,
	}
}

// ListResources returns available resources.
func (h *GenericHandler) ListResources() []Resource {
	resources := []Resource{
		{
			URI:  "spec://pipeline",
			Name: "Pipeline Configuration",
		},
		{
			URI:  "spec://dashboard",
			Name: "Current Dashboard",
		},
	}

	// List all specs
	specs := h.listSpecs()
	for _, specID := range specs {
		resources = append(resources, Resource{
			URI:  fmt.Sprintf("spec://%s", specID),
			Name: fmt.Sprintf("Spec: %s", specID),
		})
	}

	return resources
}

// GetResource returns a specific resource by URI.
func (h *GenericHandler) GetResource(uri string) (*Resource, error) {
	switch {
	case uri == "spec://pipeline":
		return h.getPipelineResource()
	case uri == "spec://dashboard":
		return h.getDashboardResource()
	case strings.HasPrefix(uri, "spec://") && strings.Contains(uri, "/section/"):
		// spec://SPEC-042/section/problem_statement
		parts := strings.SplitN(strings.TrimPrefix(uri, "spec://"), "/section/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid section URI: %s", uri)
		}
		return h.getSpecSection(parts[0], parts[1])
	case strings.HasPrefix(uri, "spec://"):
		specID := strings.TrimPrefix(uri, "spec://")
		return h.getSpecResource(specID)
	default:
		return nil, fmt.Errorf("unknown resource: %s", uri)
	}
}

// ListTools returns available tools.
func (h *GenericHandler) ListTools() []Tool {
	return []Tool{
		{
			Name:        "spec_list",
			Description: "List all specs in the pipeline, optionally filtered by stage or owner",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"stage": map[string]interface{}{
						"type":        "string",
						"description": "Filter by pipeline stage (e.g., 'build', 'review')",
					},
					"owner": map[string]interface{}{
						"type":        "string",
						"description": "Filter by owner role (e.g., 'engineer', 'pm')",
					},
				},
			},
		},
		{
			Name:        "spec_read",
			Description: "Read the full content of a spec by ID",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Spec ID (e.g., 'SPEC-042')",
					},
					"section": map[string]interface{}{
						"type":        "string",
						"description": "Optional: specific section slug to read",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "spec_status",
			Description: "Get the pipeline status and metadata for a spec",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Spec ID (e.g., 'SPEC-042')",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "spec_decide",
			Description: "Record a question or decision to a spec's decision log",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Spec ID",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "The question or decision to record",
					},
				},
				"required": []string{"id", "question"},
			},
		},
		{
			Name:        "spec_decide_resolve",
			Description: "Resolve an existing decision in a spec's decision log",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":        map[string]interface{}{"type": "string", "description": "Spec ID"},
					"number":    map[string]interface{}{"type": "integer", "description": "Decision number to resolve"},
					"decision":  map[string]interface{}{"type": "string", "description": "The decision made"},
					"rationale": map[string]interface{}{"type": "string", "description": "Rationale for the decision"},
				},
				"required": []string{"id", "number", "decision"},
			},
		},
		{
			Name:        "spec_search",
			Description: "Search across all specs for matching content",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "spec_pipeline",
			Description: "Get pipeline configuration including stages, gates, and current stage owners",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "spec_validate",
			Description: "Validate if a spec can advance to the next stage (checks gates)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Spec ID to validate",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

// CallTool executes a tool.
func (h *GenericHandler) CallTool(name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "spec_list":
		return h.toolList(args)
	case "spec_read":
		return h.toolRead(args)
	case "spec_status":
		return h.toolStatus(args)
	case "spec_decide":
		return h.toolDecide(args)
	case "spec_decide_resolve":
		return h.toolDecideResolve(args)
	case "spec_search":
		return h.toolSearch(args)
	case "spec_pipeline":
		return h.toolPipeline()
	case "spec_validate":
		return h.toolValidate(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// Helper methods

func (h *GenericHandler) listSpecs() []string {
	var specs []string
	entries, err := os.ReadDir(h.specsDir)
	if err != nil {
		return specs
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "SPEC-") && strings.HasSuffix(e.Name(), ".md") {
			specs = append(specs, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return specs
}

func (h *GenericHandler) specPath(specID string) string {
	return filepath.Join(h.specsDir, specID+".md")
}

func (h *GenericHandler) readSpec(specID string) (string, error) {
	path := h.specPath(specID)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("spec %s not found", specID)
	}
	return string(data), nil
}

func (h *GenericHandler) getPipelineResource() (*Resource, error) {
	if h.config == nil || h.config.Team == nil {
		return &Resource{
			URI:     "spec://pipeline",
			Name:    "Pipeline Configuration",
			Content: "No pipeline configured. Run 'spec config init' to set up.",
		}, nil
	}

	resolved, err := pipeline.Resolve(h.config.Team.Pipeline)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("# Pipeline Configuration\n\n")
	if resolved.PresetName != "" {
		fmt.Fprintf(&sb, "**Preset:** %s\n\n", resolved.PresetName)
	}
	sb.WriteString("## Stages\n\n")
	for i, stage := range resolved.Stages {
		fmt.Fprintf(&sb, "%d. **%s** (owner: %s)\n", i+1, stage.Name, stage.GetOwner())
		if len(stage.Gates) > 0 {
			sb.WriteString("   Gates:\n")
			for _, g := range stage.Gates {
				fmt.Fprintf(&sb, "   - %s: %s\n", g.Type(), g.Value())
			}
		}
	}
	return &Resource{
		URI:     "spec://pipeline",
		Name:    "Pipeline Configuration",
		Content: sb.String(),
	}, nil
}

func (h *GenericHandler) getDashboardResource() (*Resource, error) {
	specs := h.listSpecs()
	var sb strings.Builder
	sb.WriteString("# Specs Dashboard\n\n")

	// Group by status
	byStatus := make(map[string][]string)
	for _, specID := range specs {
		content, err := h.readSpec(specID)
		if err != nil {
			continue
		}
		meta, err := markdown.ParseMeta(content)
		if err != nil {
			continue
		}
		byStatus[meta.Status] = append(byStatus[meta.Status], fmt.Sprintf("- **%s**: %s", specID, meta.Title))
	}

	for status, items := range byStatus {
		fmt.Fprintf(&sb, "## %s\n\n", status)
		for _, item := range items {
			sb.WriteString(item + "\n")
		}
		sb.WriteString("\n")
	}

	if len(specs) == 0 {
		sb.WriteString("No specs found.\n")
	}

	return &Resource{
		URI:     "spec://dashboard",
		Name:    "Current Dashboard",
		Content: sb.String(),
	}, nil
}

func (h *GenericHandler) getSpecResource(specID string) (*Resource, error) {
	content, err := h.readSpec(strings.ToUpper(specID))
	if err != nil {
		return nil, err
	}
	return &Resource{
		URI:     fmt.Sprintf("spec://%s", specID),
		Name:    fmt.Sprintf("Spec: %s", specID),
		Content: content,
	}, nil
}

func (h *GenericHandler) getSpecSection(specID, sectionSlug string) (*Resource, error) {
	content, err := h.readSpec(strings.ToUpper(specID))
	if err != nil {
		return nil, err
	}
	body := markdown.Body(content)
	sections := markdown.ExtractSections(body)
	sec := markdown.FindSection(sections, sectionSlug)
	if sec == nil {
		return nil, fmt.Errorf("section %q not found in %s", sectionSlug, specID)
	}
	return &Resource{
		URI:     fmt.Sprintf("spec://%s/section/%s", specID, sectionSlug),
		Name:    fmt.Sprintf("%s - %s", specID, sectionSlug),
		Content: sec.Content,
	}, nil
}

// Tool implementations

func (h *GenericHandler) toolList(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Stage string `json:"stage"`
		Owner string `json:"owner"`
	}
	_ = json.Unmarshal(args, &params) // Params are optional, ignore errors

	specs := h.listSpecs()
	var results []string

	for _, specID := range specs {
		content, err := h.readSpec(specID)
		if err != nil {
			continue
		}
		meta, err := markdown.ParseMeta(content)
		if err != nil {
			continue
		}

		// Filter by stage
		if params.Stage != "" && meta.Status != params.Stage {
			continue
		}

		// Filter by owner (would need pipeline config to resolve stage owner)
		if params.Owner != "" && h.config != nil && h.config.Team != nil {
			stageOwner := pipeline.StageOwner(h.config.Team.Pipeline, meta.Status)
			if stageOwner != params.Owner {
				continue
			}
		}

		results = append(results, fmt.Sprintf("- %s: %s [%s]", specID, meta.Title, meta.Status))
	}

	if len(results) == 0 {
		return &ToolResult{Success: true, Message: "No specs found matching criteria."}, nil
	}

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Found %d specs:\n%s", len(results), strings.Join(results, "\n")),
	}, nil
}

func (h *GenericHandler) toolRead(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID      string `json:"id"`
		Section string `json:"section"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	content, err := h.readSpec(strings.ToUpper(params.ID))
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	if params.Section != "" {
		body := markdown.Body(content)
		sections := markdown.ExtractSections(body)
		sec := markdown.FindSection(sections, params.Section)
		if sec == nil {
			return &ToolResult{Success: false, Message: fmt.Sprintf("section %q not found", params.Section)}, nil
		}
		return &ToolResult{Success: true, Message: sec.Content}, nil
	}

	return &ToolResult{Success: true, Message: content}, nil
}

func (h *GenericHandler) toolStatus(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	content, err := h.readSpec(strings.ToUpper(params.ID))
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	meta, err := markdown.ParseMeta(content)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Spec: %s\n", params.ID)
	fmt.Fprintf(&sb, "Title: %s\n", meta.Title)
	fmt.Fprintf(&sb, "Status: %s\n", meta.Status)
	fmt.Fprintf(&sb, "Author: %s\n", meta.Author)
	if meta.Cycle != "" {
		fmt.Fprintf(&sb, "Cycle: %s\n", meta.Cycle)
	}
	if len(meta.Repos) > 0 {
		fmt.Fprintf(&sb, "Repos: %s\n", strings.Join(meta.Repos, ", "))
	}

	// Get stage owner from pipeline
	if h.config != nil && h.config.Team != nil {
		owner := pipeline.StageOwner(h.config.Team.Pipeline, meta.Status)
		fmt.Fprintf(&sb, "Stage Owner: %s\n", owner)
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (h *GenericHandler) toolDecide(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	path := h.specPath(strings.ToUpper(params.ID))
	num, err := markdown.AppendDecision(path, params.Question, "agent")
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Decision #%03d recorded in %s: %s", num, params.ID, params.Question),
	}, nil
}

func (h *GenericHandler) toolDecideResolve(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID        string `json:"id"`
		Number    int    `json:"number"`
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	path := h.specPath(strings.ToUpper(params.ID))
	if err := markdown.ResolveDecision(path, params.Number, params.Decision, params.Rationale, "agent"); err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Decision #%03d resolved in %s: %s", params.Number, params.ID, params.Decision),
	}, nil
}

func (h *GenericHandler) toolSearch(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	query := strings.ToLower(params.Query)
	var matches []string

	for _, specID := range h.listSpecs() {
		content, err := h.readSpec(specID)
		if err != nil {
			continue
		}

		// Search in content
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), query) {
				preview := strings.TrimSpace(line)
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				matches = append(matches, fmt.Sprintf("%s (line %d): %s", specID, i+1, preview))
				if len(matches) >= 20 {
					break
				}
			}
		}
	}

	if len(matches) == 0 {
		return &ToolResult{Success: true, Message: "No matches found."}, nil
	}

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Found %d matches:\n%s", len(matches), strings.Join(matches, "\n")),
	}, nil
}

func (h *GenericHandler) toolPipeline() (*ToolResult, error) {
	if h.config == nil || h.config.Team == nil {
		return &ToolResult{Success: false, Message: "No pipeline configured"}, nil
	}

	resolved, err := pipeline.Resolve(h.config.Team.Pipeline)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	var sb strings.Builder
	if resolved.PresetName != "" {
		fmt.Fprintf(&sb, "Preset: %s\n\n", resolved.PresetName)
	}
	sb.WriteString("Stages:\n")
	for i, stage := range resolved.Stages {
		fmt.Fprintf(&sb, "  %d. %s (owner: %s)", i+1, stage.Name, stage.GetOwner())
		if stage.Optional {
			sb.WriteString(" [optional]")
		}
		sb.WriteString("\n")
		if len(stage.Gates) > 0 {
			for _, g := range stage.Gates {
				fmt.Fprintf(&sb, "     gate: %s = %s\n", g.Type(), g.Value())
			}
		}
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (h *GenericHandler) toolValidate(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	specID := strings.ToUpper(params.ID)
	content, err := h.readSpec(specID)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	meta, err := markdown.ParseMeta(content)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	if h.config == nil || h.config.Team == nil {
		return &ToolResult{Success: false, Message: "No pipeline configured"}, nil
	}

	// Get next stage
	next, err := pipeline.NextStage(h.config.Team.Pipeline, meta.Status, true)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Cannot advance: %v", err)}, nil
	}

	// Evaluate gates
	body := markdown.Body(content)
	sections := markdown.ExtractSections(body)
	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	results := pipeline.EvaluateGates(h.config.Team.Pipeline, next, sections, hasPRStack, false)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Validating %s: %s → %s\n\n", specID, meta.Status, next)

	allPassed := true
	for _, r := range results {
		if r.Passed {
			fmt.Fprintf(&sb, "✓ %s\n", r.Gate)
		} else {
			fmt.Fprintf(&sb, "✗ %s: %s\n", r.Gate, r.Reason)
			allPassed = false
		}
	}

	if allPassed {
		sb.WriteString("\nAll gates passed. Ready to advance.")
	} else {
		sb.WriteString("\nSome gates failed. Fix the issues above before advancing.")
	}

	return &ToolResult{Success: allPassed, Message: sb.String()}, nil
}
