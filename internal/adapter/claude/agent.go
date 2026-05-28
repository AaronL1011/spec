// Package claude implements AgentAdapter for Claude Code.
// Claude Code is MCP-native — it discovers the spec MCP server via .mcp.json
// in the workspace. The adapter simply spawns the claude subprocess.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Agent implements adapter.AgentAdapter for Claude Code.
type Agent struct {
	// Command is the CLI executable name. Defaults to "claude".
	Command string
}

// NewAgent creates a Claude Code AgentAdapter.
// command overrides the CLI binary name (default: "claude").
func NewAgent(command string) *Agent {
	if command == "" {
		command = "claude"
	}
	return &Agent{Command: command}
}

// Invoke spawns Claude Code as a subprocess in the given working directory.
// The MCP server is already running (started by the build engine); Claude
// discovers it through the .mcp.json configuration in the workspace.
// contextFile is ignored for MCP-native agents but available for reference.
func (a *Agent) Invoke(ctx context.Context, contextFile string, workDir string) error {
	if _, err := exec.LookPath(a.Command); err != nil {
		return fmt.Errorf("%s not found in PATH — install Claude Code: https://docs.anthropic.com/en/docs/claude-code", a.Command)
	}

	cmd := exec.CommandContext(ctx, a.Command)
	cmd.Dir = workDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inherit the user's full environment so Claude picks up
	// auth tokens, git config, and MCP server configuration.
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		// Exit code 0 is normal (user quit). Non-zero may be a real error
		// or the user pressing Ctrl-C — both are acceptable.
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 130 || exitErr.ExitCode() == 2 {
				// SIGINT / Ctrl-C — not an error.
				return nil
			}
		}
		return fmt.Errorf("claude exited with error: %w", err)
	}
	return nil
}

// SupportsMCP returns true. Claude Code natively connects to MCP servers
// configured in .mcp.json in the workspace root.
func (a *Agent) SupportsMCP() bool { return true }
