package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// preflightBuild validates that a spec can start a build before the TUI shows
// the confirm modal. It never spawns a process; an error is surfaced inline.
func (a *App) preflightBuild(specID string) error {
	specPath := resolveLocalSpecPath(a.rc, specID)
	meta, err := markdown.ReadMeta(specPath)
	if err != nil {
		return fmt.Errorf("%s not found locally — run 'spec pull %s'", specID, specID)
	}
	if meta.Status != "build" && meta.Status != "engineering" {
		return fmt.Errorf("%s is at %q — advance to 'build' first", specID, meta.Status)
	}
	return nil
}

// buildConfirmBody describes what launching a build does, naming the agent and
// the current step so the user makes a deliberate choice.
func (a *App) buildConfirmBody(specID string) string {
	step := ""
	if a.db != nil {
		if s, _ := build.LoadSession(a.db, specID); s != nil && !s.IsComplete() {
			step = fmt.Sprintf(" (step %d/%d)", s.CurrentStep, len(s.Steps))
		}
	}
	return fmt.Sprintf(
		"Launch the %s build agent for %s%s? It connects to the spec MCP server "+
			"and can edit files and record decisions.",
		a.agentName(), specID, step)
}

// agentName returns the effective agent provider name (per-user override or
// team default), or "agent" when none is configured.
func (a *App) agentName() string {
	if a.rc != nil {
		if p := a.rc.EffectiveAgentConfig().Provider; p != "" && p != "none" {
			return p
		}
	}
	return "agent"
}

// buildSpec suspends the TUI and launches the build engine in a new
// terminal pane (via the user's configured multiplexer) or falls back
// to running in the current terminal.
func buildSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	// Validate spec is in build stage before suspending.
	specPath := resolveLocalSpecPath(rc, specID)
	meta, err := markdown.ReadMeta(specPath)
	if err != nil {
		return cmdResult("build", specID, err)
	}
	if meta.Status != "build" && meta.Status != "engineering" {
		return cmdResult("build", specID,
			fmt.Errorf("%s is at %q — advance to 'build' first", specID, meta.Status))
	}

	mux := ""
	if rc.User != nil {
		mux = rc.User.Preferences.Multiplexer
	}

	// Run `spec build <id>` then hold the pane/terminal open so its output and
	// exit code stay readable. Without this, a fast exit (agent not found,
	// noop agent, immediate error) closes the pane before anything can be read
	// — "flashes up then vanishes". The id is passed as an argv parameter ($1),
	// never interpolated into the shell string, so it stays injection-safe.
	wrap := func(name string, pre ...string) tea.Cmd {
		args := make([]string, 0, len(pre)+4)
		args = append(args, pre...)
		args = append(args, "sh", "-c", buildHoldScript, "sh", specID)
		return spawnPane(name, args...)
	}

	switch mux {
	case "tmux":
		return wrap("tmux", "split-window", "-h")
	case "zellij":
		return wrap("zellij", "run", "--")
	case "wezterm":
		return wrap("wezterm", "cli", "split-pane", "--right", "--")
	case "iterm2":
		// iTerm2 doesn't have a clean CLI split; fall through to suspend.
	}

	// Fallback: suspend TUI and run in current terminal, pausing on exit so the
	// output survives the TUI resume.
	return tea.ExecProcess(
		exec.Command("sh", "-c", buildHoldScript, "sh", specID),
		func(err error) tea.Msg {
			return actionResultMsg{Action: "build", SpecID: specID, Err: err}
		},
	)
}

// buildHoldScript runs `spec build <id>` (id is $1) and then waits for Enter so
// the agent session's output and exit status remain visible after exit.
const buildHoldScript = `spec build "$1"; code=$?; printf '\n[spec build exited with status %s — press enter to close]\n' "$code"; read _`

// spawnPane launches a command in a new multiplexer pane without
// suspending the TUI.
func spawnPane(name string, args ...string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return actionResultMsg{Action: "build", Err: fmt.Errorf("spawn %s: %w", name, err)}
		}
		return actionResultMsg{Action: "build", Detail: fmt.Sprintf("launched in %s pane", name)}
	}
}

// cmdResult is a convenience for returning an immediate actionResultMsg.
func cmdResult(action, specID string, err error) tea.Cmd {
	return func() tea.Msg {
		return actionResultMsg{Action: action, SpecID: specID, Err: err}
	}
}
