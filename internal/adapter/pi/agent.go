// Package pi implements AgentAdapter for the pi.dev coding agent.
// pi is MCP-native and maps cleanly onto the engine's InvokeRequest: the MCP
// config, skills, and system prompt all become native CLI flags.
package pi

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// invokeHeadless runs pi autonomously (`-p --mode json`) and tees its event
// stream: every line still reaches the user's stdout while a parser distils the
// session-level signal (exit reason, error class, token usage) into the result.
// Per-node progress is reconciled separately from the ledger by the engine.
//
// Parsing is best-effort: a stream we cannot interpret yields a zero-value
// result, never an error, so output-format drift never fails the build session.
func (a *Agent) invokeHeadless(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	args := append(a.args(req), "-p", "--mode", "json")
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	cmd := exec.CommandContext(ctx, a.Command, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	// Tee stdout: the user still sees the live stream while we parse a copy.
	pr, pw := io.Pipe()
	cmd.Stdout = io.MultiWriter(os.Stdout, pw)

	resultCh := make(chan adapter.InvokeResult, 1)
	go func() {
		resultCh <- parseHeadlessStream(pr)
	}()

	runErr := cmd.Run()
	_ = pw.Close() // unblock the parser; flushes any buffered tail.
	res := <-resultCh

	if runErr != nil {
		// A non-zero exit is authoritative over anything the stream implied.
		if res.ExitReason == "" || res.ExitReason == "completed" {
			res.ExitReason = "error"
		}
		if res.ErrorClass == "" {
			res.ErrorClass = "nonzero_exit"
		}
		if res.ErrorMessage == "" {
			res.ErrorMessage = runErr.Error()
		}
		return &res, fmt.Errorf("pi exited with error: %w", runErr)
	}
	if res.ExitReason == "" {
		res.ExitReason = "completed"
	}
	return &res, nil
}
