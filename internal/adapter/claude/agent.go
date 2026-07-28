// Package claude implements AgentAdapter for Claude Code.
// Claude Code is MCP-native — it discovers the spec MCP server via .mcp.json
// in the workspace. The adapter writes that file from the engine-generated MCP
// config (restoring any prior file on exit) and spawns the claude subprocess.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/harness"
)

// Agent implements adapter.AgentAdapter for Claude Code.
type Agent struct {
	// Command is the CLI executable name. Defaults to "claude".
	Command string
	// Model overrides the harness default for completions, in Claude Code's own
	// spelling. Empty uses whatever the harness is configured for.
	Model string
	// Timeout bounds one contained completion. Zero uses harness.DefaultTimeout.
	Timeout time.Duration
}

// NewAgent creates a Claude Code AgentAdapter.
// command overrides the CLI binary name (default: "claude").
func NewAgent(command string) *Agent {
	if command == "" {
		command = "claude"
	}
	return &Agent{Command: command}
}

// Invoke spawns Claude Code as a subprocess in the request's working directory.
// It first installs the engine-generated MCP config as <workDir>/.mcp.json so
// Claude discovers the spec server, restoring any prior file on exit.
func (a *Agent) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	if _, err := exec.LookPath(a.Command); err != nil {
		return nil, fmt.Errorf("%s not found in PATH — install Claude Code: https://docs.anthropic.com/en/docs/claude-code", a.Command)
	}

	if req.MCPConfigPath != "" && req.WorkDir != "" {
		restore, err := harness.InstallMCPConfig(req.MCPConfigPath, req.WorkDir)
		if err != nil {
			return nil, err
		}
		defer restore()
	}

	args := []string{}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	cmd := exec.CommandContext(ctx, a.Command, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inherit the user's full environment so Claude picks up
	// auth tokens, git config, and MCP server configuration.
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		// Exit code 0 is normal (user quit). 130/2 are SIGINT / Ctrl-C.
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 130 || exitErr.ExitCode() == 2 {
				return &adapter.InvokeResult{}, nil
			}
		}
		return nil, fmt.Errorf("claude exited with error: %w", err)
	}
	return &adapter.InvokeResult{}, nil
}
