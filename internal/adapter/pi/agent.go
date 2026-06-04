// Package pi implements AgentAdapter for the pi.dev coding agent.
// pi is MCP-native and maps cleanly onto the engine's InvokeRequest: the MCP
// config, skills, and system prompt all become native CLI flags.
package pi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Agent implements adapter.AgentAdapter for pi.dev.
type Agent struct {
	// Command is the CLI executable name. Defaults to "pi".
	Command string
}

// NewAgent creates a pi.dev AgentAdapter.
// command overrides the CLI binary name (default: "pi").
func NewAgent(command string) *Agent {
	if command == "" {
		command = "pi"
	}
	return &Agent{Command: command}
}

// Capabilities reports pi's supported features: MCP, headless autonomous runs,
// repeatable skills, and an appended system prompt.
func (a *Agent) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{MCP: true, Headless: true, Skills: true, SystemPrompt: true}
}

// Invoke spawns pi for a build session. Interactive (default) inherits stdio
// and blocks until exit; headless runs `-p --mode json` autonomously. Progress
// is reconciled by spec-cli from the node ledger after pi exits, so the result
// is empty either way.
func (a *Agent) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	if _, err := exec.LookPath(a.Command); err != nil {
		return nil, fmt.Errorf("%s not found in PATH — install pi: https://pi.dev", a.Command)
	}

	if req.Headless {
		return a.invokeHeadless(ctx, req)
	}
	return a.invokeInteractive(ctx, req)
}

// args builds the shared pi flag set for a request.
func (a *Agent) args(req adapter.InvokeRequest) []string {
	var args []string
	if req.MCPConfigPath != "" {
		args = append(args, "--mcp-config", req.MCPConfigPath)
	}
	for _, skill := range req.SkillPaths {
		args = append(args, "--skill", skill)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.SpecID != "" {
		args = append(args, "--session-id", "spec-"+req.SpecID)
	}
	return args
}

// invokeInteractive runs pi attached to the terminal.
func (a *Agent) invokeInteractive(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	args := a.args(req)
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	cmd := exec.CommandContext(ctx, a.Command, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 130 || exitErr.ExitCode() == 2 {
				// SIGINT / Ctrl-C — not an error.
				return &adapter.InvokeResult{}, nil
			}
		}
		return nil, fmt.Errorf("pi exited with error: %w", err)
	}
	return &adapter.InvokeResult{}, nil
}

// invokeHeadless runs pi autonomously (`-p --mode json`) and streams its output
// through. Completion is reconciled from the node ledger by the engine after pi
// exits, so there is nothing to parse out of the stream here.
func (a *Agent) invokeHeadless(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	args := append(a.args(req), "-p", "--mode", "json")
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	cmd := exec.CommandContext(ctx, a.Command, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pi exited with error: %w", err)
	}
	return &adapter.InvokeResult{}, nil
}
