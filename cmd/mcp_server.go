package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nexl/spec-cli/internal/build"
	"github.com/nexl/spec-cli/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Run spec as a standalone MCP server (stdio transport)",
	Long: `Starts an MCP (Model Context Protocol) server on stdio, serving spec
context and tools to any MCP-compatible agent. Configure your agent to
connect by adding to .mcp.json:

  {"mcpServers": {"spec": {"command": "spec", "args": ["mcp-server"]}}}

The server requires an active build session (run 'spec build <id>' first)
or an explicit --spec flag.`,
	RunE:          runMCPServer,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	mcpServerCmd.Flags().String("spec", "", "spec ID to serve context for (defaults to most recent session)")
	rootCmd.AddCommand(mcpServerCmd)
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	specIDFlag, _ := cmd.Flags().GetString("spec")

	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}
	defer db.Close()

	specID, err := resolveSpecID(specIDFlag, db)
	if err != nil {
		return err
	}

	session, err := build.LoadSession(db, specID)
	if err != nil {
		return fmt.Errorf("loading session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("no build session for %s — run 'spec build %s' first", specID, specID)
	}

	specPath, err := resolveLocalSpecPath(specID)
	if err != nil {
		specPath, err = resolveSpecPath(rc, specID)
		if err != nil {
			return fmt.Errorf("spec %s not found locally or in specs repo — run 'spec pull %s'", specID, specID)
		}
	}

	buildCtx, err := build.AssembleContext(specPath, session, "")
	if err != nil {
		return fmt.Errorf("assembling build context: %w", err)
	}

	buildServer := build.NewMCPServer(session, buildCtx, db, specPath)
	handler := &buildHandler{server: buildServer}

	return mcp.Serve(ctx(), handler, os.Stdin, os.Stdout, os.Stderr)
}

// resolveSpecID determines the spec ID from a flag or the most recent session.
func resolveSpecID(flag string, db interface{ SessionMostRecent() (string, error) }) (string, error) {
	if flag != "" {
		return strings.ToUpper(flag), nil
	}
	recent, err := db.SessionMostRecent()
	if err != nil || recent == "" {
		return "", fmt.Errorf("no active session — use --spec to specify a spec ID")
	}
	return recent, nil
}

// buildHandler adapts build.MCPServer to the mcp.Handler interface.
type buildHandler struct {
	server *build.MCPServer
}

func (h *buildHandler) ListResources() []mcp.Resource {
	brs := h.server.ListResources()
	out := make([]mcp.Resource, len(brs))
	for i, r := range brs {
		out[i] = mcp.Resource{URI: r.URI, Name: r.Name, Content: r.Content}
	}
	return out
}

func (h *buildHandler) GetResource(uri string) (*mcp.Resource, error) {
	r, err := h.server.GetResource(uri)
	if err != nil {
		return nil, err
	}
	return &mcp.Resource{URI: r.URI, Name: r.Name, Content: r.Content}, nil
}

func (h *buildHandler) ListTools() []mcp.Tool {
	defs := build.ToolDefinitions()
	out := make([]mcp.Tool, len(defs))
	for i, d := range defs {
		out[i] = mcp.Tool{
			Name:        d["name"].(string),
			Description: d["description"].(string),
			InputSchema: d["inputSchema"].(map[string]interface{}),
		}
	}
	return out
}

func (h *buildHandler) CallTool(name string, args json.RawMessage) (*mcp.ToolResult, error) {
	r, err := h.server.CallTool(name, args)
	if err != nil {
		return nil, err
	}
	return &mcp.ToolResult{Success: r.Success, Message: r.Message}, nil
}
