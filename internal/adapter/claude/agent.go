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
	"path/filepath"

	"github.com/aaronl1011/spec/internal/adapter"
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

// Invoke spawns Claude Code as a subprocess in the request's working directory.
// It first installs the engine-generated MCP config as <workDir>/.mcp.json so
// Claude discovers the spec server, restoring any prior file on exit.
func (a *Agent) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	if _, err := exec.LookPath(a.Command); err != nil {
		return nil, fmt.Errorf("%s not found in PATH — install Claude Code: https://docs.anthropic.com/en/docs/claude-code", a.Command)
	}

	if req.MCPConfigPath != "" && req.WorkDir != "" {
		restore, err := installMCPConfig(req.MCPConfigPath, req.WorkDir)
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

// Capabilities reports Claude Code's supported features. Claude Code is
// MCP-native and accepts an appended system prompt; skill-dir mapping is not
// handled here, so skill bodies are folded into the system prompt by the engine.
func (a *Agent) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{MCP: true, SystemPrompt: true}
}

// installMCPConfig writes the engine-generated MCP config to <workDir>/.mcp.json
// and returns a restore function that puts back any pre-existing file (or
// removes the one we created) when the session ends.
func installMCPConfig(mcpConfigPath, workDir string) (func(), error) {
	src, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return nil, fmt.Errorf("reading mcp config: %w", err)
	}

	dest := filepath.Join(workDir, ".mcp.json")
	prev, prevErr := os.ReadFile(dest)
	hadPrev := prevErr == nil

	if err := os.WriteFile(dest, src, 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", dest, err)
	}

	return func() {
		if hadPrev {
			_ = os.WriteFile(dest, prev, 0o644)
		} else {
			_ = os.Remove(dest)
		}
	}, nil
}
