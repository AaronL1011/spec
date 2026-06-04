package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/mcp"
	"github.com/aaronl1011/spec/internal/store"
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
  DAG build tools become available:

  spec_provision_node  Provision a node (branch + worktree), returns workDir
  spec_node_complete   Mark a node complete (captures its diff). Idempotent
  spec_node_failed     Record a node failure with a reason
  spec_push            Push a node's branch to origin
  spec_open_pr         Open a DRAFT PR for a node (stacked on its base)
  spec_link_prs        Re-chain the PR stack as parents merge

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
		defer func() { _ = db.Close() }()
		recent, _ := db.SessionMostRecent()
		if recent != "" {
			fmt.Fprintf(os.Stderr, "spec mcp: active session detected for %s, use --spec %s for build mode\n", recent, recent)
		}
	}

	// Attribute sync activity from this process to the MCP surface.
	gitpkg.SetReadSurface(store.SurfaceMCP)

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
	defer func() { _ = db.Close() }()

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

	buildServer := build.NewMCPServer(session, buildCtx, db, specPath, buildEngineOptions(rc, false)).
		WithRepo(buildRegistry(rc).Repo())
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

	// Add build-specific (DAG) tools.
	nodeIDProp := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{"type": "string", "description": "DAG node id (e.g. 'n3')"},
		},
		"required": []string{"node_id"},
	}
	tools = append(tools,
		mcp.Tool{
			Name:        "spec_provision_node",
			Description: "Provision a DAG node: compute its base ref, create its branch + worktree, and return { workDir, branch, baseRef, skillPaths }",
			InputSchema: nodeIDProp,
		},
		mcp.Tool{
			Name:        "spec_node_complete",
			Description: "Mark a DAG node complete, capturing its diff. Idempotent.",
			InputSchema: nodeIDProp,
		},
		mcp.Tool{
			Name:        "spec_node_failed",
			Description: "Record a DAG node failure with a reason so resume and reporting can surface it",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"node_id": map[string]interface{}{"type": "string", "description": "DAG node id"},
					"reason":  map[string]interface{}{"type": "string", "description": "Why the node failed"},
				},
				"required": []string{"node_id"},
			},
		},
		mcp.Tool{
			Name:        "spec_push",
			Description: "Push a provisioned node's branch to origin (from its worktree)",
			InputSchema: nodeIDProp,
		},
		mcp.Tool{
			Name:        "spec_open_pr",
			Description: "Open a DRAFT PR for a node (head=node branch, base=its stack base). Returns { number, url, base }. Idempotent.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"node_id": map[string]interface{}{"type": "string", "description": "DAG node id"},
					"title":   map[string]interface{}{"type": "string", "description": "Optional PR title"},
					"body":    map[string]interface{}{"type": "string", "description": "Optional PR body"},
				},
				"required": []string{"node_id"},
			},
		},
		mcp.Tool{
			Name:        "spec_link_prs",
			Description: "Re-chain the PR stack by retargeting each node's PR to its stack base. With {node_id, base} retargets a single PR (e.g. to the default branch once a parent merges).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"node_id": map[string]interface{}{"type": "string", "description": "Optional: retarget only this node's PR"},
					"base":    map[string]interface{}{"type": "string", "description": "Optional: explicit base branch"},
				},
			},
		},
	)

	return tools
}

func (h *combinedHandler) CallTool(name string, args json.RawMessage) (*mcp.ToolResult, error) {
	// Build-specific (DAG) tools route to the build MCP server.
	switch name {
	case "spec_provision_node", "spec_node_complete", "spec_node_failed",
		"spec_push", "spec_open_pr", "spec_link_prs":
		r, err := h.build.CallTool(name, args)
		if err != nil {
			return nil, err
		}
		return &mcp.ToolResult{Success: r.Success, Message: r.Message}, nil
	}

	// Generic tools
	return h.generic.CallTool(name, args)
}
