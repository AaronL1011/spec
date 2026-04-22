package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexl/spec-cli/internal/build"
	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Run spec as an MCP server (stdio transport)",
	Long: `Starts an MCP (Model Context Protocol) server on stdio, serving spec
resources and tools to any MCP-compatible agent.

Configure your agent by adding to .mcp.json:

  {"mcpServers": {"spec": {"command": "spec", "args": ["mcp-server"]}}}

RESOURCES:
  spec://pipeline          Pipeline configuration
  spec://dashboard         All specs grouped by status  
  spec://SPEC-042          Full spec content
  spec://SPEC-042/section/problem_statement   Specific section

TOOLS:
  spec_list       List specs (filter by stage/owner)
  spec_read       Read a spec or section
  spec_status     Get spec metadata and pipeline position
  spec_decide     Add a decision to the decision log
  spec_decide_resolve   Resolve a decision
  spec_search     Search across all specs
  spec_pipeline   Get pipeline configuration
  spec_validate   Check if a spec can advance

BUILD MODE:
  If --spec is provided or there's an active build session, additional
  build-specific tools become available:
  
  spec_step_complete   Mark current PR step as complete
  
Use --spec to focus on a specific spec during a build session.`,
	RunE:          runMCPServer,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	mcpServerCmd.Flags().String("spec", "", "focus on a specific spec (enables build mode if session exists)")
	rootCmd.AddCommand(mcpServerCmd)
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	specIDFlag, _ := cmd.Flags().GetString("spec")

	rc, err := resolveConfig()
	if err != nil {
		// Even without config, we can serve limited functionality
		fmt.Fprintf(os.Stderr, "spec mcp: warning: no config found, limited functionality\n")
		rc = nil
	}

	// Determine specs directory
	specsDir := "."
	if rc != nil && rc.Team != nil {
		// Try to find specs repo
		specsRepoPath := filepath.Join(os.Getenv("HOME"), ".spec", "repos",
			rc.Team.SpecsRepo.Owner, rc.Team.SpecsRepo.Repo)
		if _, err := os.Stat(specsRepoPath); err == nil {
			specsDir = specsRepoPath
		}
	}

	// Check for build session mode
	if specIDFlag != "" {
		return runBuildMCPServer(cmd, specIDFlag, rc)
	}

	// Try to detect active session
	db, err := openDB()
	if err == nil {
		defer db.Close()
		recent, _ := db.SessionMostRecent()
		if recent != "" {
			fmt.Fprintf(os.Stderr, "spec mcp: active session detected for %s, use --spec %s for build mode\n", recent, recent)
		}
	}

	// Generic mode - serve all specs
	handler := mcp.NewGenericHandler(rc, specsDir)
	return mcp.Serve(context.Background(), handler, os.Stdin, os.Stdout, os.Stderr)
}

// runBuildMCPServer runs in build mode with session-specific tools
func runBuildMCPServer(cmd *cobra.Command, specID string, rc *config.ResolvedConfig) error {
	specID = strings.ToUpper(specID)

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}
	defer db.Close()

	session, err := build.LoadSession(db, specID)
	if err != nil {
		return fmt.Errorf("loading session: %w", err)
	}

	// If no session, fall back to generic mode with spec focus
	if session == nil {
		fmt.Fprintf(os.Stderr, "spec mcp: no build session for %s, serving in generic mode\n", specID)
		specsDir := "."
		if rc != nil && rc.Team != nil {
			specsRepoPath := filepath.Join(os.Getenv("HOME"), ".spec", "repos",
				rc.Team.SpecsRepo.Owner, rc.Team.SpecsRepo.Repo)
			if _, err := os.Stat(specsRepoPath); err == nil {
				specsDir = specsRepoPath
			}
		}
		handler := mcp.NewGenericHandler(rc, specsDir)
		return mcp.Serve(context.Background(), handler, os.Stdin, os.Stdout, os.Stderr)
	}

	// Build mode with session
	specPath, err := resolveLocalSpecPath(specID)
	if err != nil {
		if rc != nil {
			specPath, err = resolveSpecPath(rc, specID)
		}
		if err != nil {
			return fmt.Errorf("spec %s not found — run 'spec pull %s'", specID, specID)
		}
	}

	buildCtx, err := build.AssembleContext(specPath, session, "")
	if err != nil {
		return fmt.Errorf("assembling build context: %w", err)
	}

	buildServer := build.NewMCPServer(session, buildCtx, db, specPath)
	handler := &combinedHandler{
		generic: mcp.NewGenericHandler(rc, filepath.Dir(specPath)),
		build:   buildServer,
		specID:  specID,
	}

	return mcp.Serve(context.Background(), handler, os.Stdin, os.Stdout, os.Stderr)
}

// combinedHandler merges generic and build handlers
type combinedHandler struct {
	generic *mcp.GenericHandler
	build   *build.MCPServer
	specID  string
}

func (h *combinedHandler) ListResources() []mcp.Resource {
	// Combine resources from both handlers
	resources := h.generic.ListResources()

	// Add build-specific resources
	for _, r := range h.build.ListResources() {
		resources = append(resources, mcp.Resource{
			URI:     r.URI,
			Name:    r.Name,
			Content: r.Content,
		})
	}

	return resources
}

func (h *combinedHandler) GetResource(uri string) (*mcp.Resource, error) {
	// Try build handler first for spec:// URIs
	if strings.HasPrefix(uri, "spec://current/") {
		r, err := h.build.GetResource(uri)
		if err == nil {
			return &mcp.Resource{URI: r.URI, Name: r.Name, Content: r.Content}, nil
		}
	}

	// Fall back to generic
	return h.generic.GetResource(uri)
}

func (h *combinedHandler) ListTools() []mcp.Tool {
	// Start with generic tools
	tools := h.generic.ListTools()

	// Add build-specific tools
	tools = append(tools, mcp.Tool{
		Name:        "spec_step_complete",
		Description: "Mark the current PR stack step as complete and advance to the next",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	})

	return tools
}

func (h *combinedHandler) CallTool(name string, args json.RawMessage) (*mcp.ToolResult, error) {
	// Build-specific tools
	switch name {
	case "spec_step_complete":
		r, err := h.build.CallTool(name, args)
		if err != nil {
			return nil, err
		}
		return &mcp.ToolResult{Success: r.Success, Message: r.Message}, nil
	}

	// Generic tools
	return h.generic.CallTool(name, args)
}

