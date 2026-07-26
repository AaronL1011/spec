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
func EditInEditorContext(ctx context.Context, content, editor string) (string, error) {
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "spec-draft-*.md")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("writing draft: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("closing draft: %w", err)
	}

	// Split so EDITOR="code -w" and similar work rather than being treated as
	// one impossible binary name.
	fields := strings.Fields(editor)
	cmd := exec.CommandContext(ctx, fields[0], append(fields[1:], tmpFile.Name())...) //nolint:gosec // the editor command comes from user config by design
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running %s: %w", editor, err)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("reading edited draft: %w", err)
	}
	return string(data), nil
}
