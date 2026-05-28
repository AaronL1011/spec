package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

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

	buildCmd := fmt.Sprintf("spec build %s", specID)

	switch mux {
	case "tmux":
		return spawnPane("tmux", "split-window", "-h", buildCmd)
	case "zellij":
		return spawnPane("zellij", "run", "--", "sh", "-c", buildCmd)
	case "wezterm":
		return spawnPane("wezterm", "cli", "split-pane", "--right", "--", "sh", "-c", buildCmd)
	case "iterm2":
		// iTerm2 doesn't have a clean CLI split; fall through to suspend.
	}

	// Fallback: suspend TUI and run in current terminal.
	return tea.ExecProcess(
		exec.Command("spec", "build", specID),
		func(err error) tea.Msg {
			return actionResultMsg{Action: "build", SpecID: specID, Err: err}
		},
	)
}

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
