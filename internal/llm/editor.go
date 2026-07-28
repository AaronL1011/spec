package llm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Editing a draft is an interactive, terminal-bound action, so it lives beside
// the CLI renderer rather than in the service or prompt-assembly path: those
// must stay free of subprocesses and stdio (see no_print_test.go).
// EditInEditor opens content in the user's editor and returns the saved result.
// Exported because the TUI needs the same suspend-and-edit behaviour as the CLI.
func EditInEditor(content, editor string) (string, error) {
	return EditInEditorContext(context.Background(), content, editor)
}

// EditInEditorContext is EditInEditor bound to a context, so a cancelled review
// tears down the editor rather than leaving it holding the terminal.
//
// It attaches the editor to the process's own stdio and blocks, which is only
// correct for callers that own the terminal in cooked mode (the CLI gate). A
// TUI must use EditorSession with tea.ExecProcess instead.
func EditInEditorContext(ctx context.Context, content, editor string) (string, error) {
	cmd, result, err := EditorSession(ctx, content, editor)
	if err != nil {
		return "", err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_, _ = result() // removes the temp file
		return "", fmt.Errorf("running %s: %w", cmd.Args[0], err)
	}
	return result()
}

// EditorSession prepares an editor invocation over content without running it.
//
// The returned command has no stdio attached: the caller decides how it meets
// the terminal. That split exists because running a terminal editor from a
// background goroutine while a TUI holds the screen corrupts the tty — the
// editor restores its own cooked termios on exit (IXON back on turns Ctrl+S
// into flow control) and the TUI never knows it must re-enter raw mode. The
// TUI therefore hands this command to tea.ExecProcess, which releases the
// terminal first and restores it after.
//
// result reads the edited content and removes the temp file; call it exactly
// once after the command finishes, on both the success and failure paths.
func EditorSession(ctx context.Context, content, editor string) (cmd *exec.Cmd, result func() (string, error), err error) {
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "spec-draft-*.md")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp file: %w", err)
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return nil, nil, fmt.Errorf("writing draft: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, nil, fmt.Errorf("closing draft: %w", err)
	}

	// Split so EDITOR="code -w" and similar work rather than being treated as
	// one impossible binary name.
	fields := strings.Fields(editor)
	cmd = exec.CommandContext(ctx, fields[0], append(fields[1:], tmpFile.Name())...) //nolint:gosec // the editor command comes from user config by design

	result = func() (string, error) {
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		data, err := os.ReadFile(tmpFile.Name())
		if err != nil {
			return "", fmt.Errorf("reading edited draft: %w", err)
		}
		return string(data), nil
	}
	return cmd, result, nil
}
