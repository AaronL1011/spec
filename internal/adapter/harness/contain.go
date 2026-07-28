// Package harness holds the shared machinery for running a coding harness as a
// contained one-shot completion.
//
// The completion plane's defining property is that it cannot touch the repo.
// That property is not free: a headless harness run inherits full tool access by
// default and can edit files, run shell commands, and spend minutes doing
// agentic work instead of returning a draft. So containment here is enforced by
// hard flags and asserted by contract test, never assumed — and a harness that
// cannot prove tool-disable does not advertise Capabilities.Generate at all.
package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds one contained completion.
//
// This is deliberately far longer than the 10s house budget for API calls: that
// budget describes a network round-trip, while this covers process startup plus
// model inference. A local model routinely needs 30-60s to draft a spec section,
// and a 10s cap would make the feature fail on exactly the air-gapped setups it
// is meant to serve. Callers show elapsed time, so a generous budget costs
// patience rather than confidence.
const DefaultTimeout = 120 * time.Second

// MaxRawTail bounds retained output when parsing yields no text, matching the
// InvokeResult convention.
const MaxRawTail = 2000

// Run executes a contained harness completion and returns its stdout.
//
// Containment is the whole contract:
//   - the caller supplies hard tool-disable flags in args (asserted per harness
//     by contract test);
//   - the process runs in a fresh empty temp directory, never a repo or the
//     specs clone, so even a tool that slipped through has nothing to edit and
//     no project instructions to discover;
//   - stdin is closed, so the harness cannot block waiting for input that will
//     never come;
//   - it runs in its own process group and cancellation signals the group, so
//     Esc actually kills descendants instead of orphaning them.
func Run(ctx context.Context, command string, args []string, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath(command); err != nil {
		return "", fmt.Errorf("%s not found in PATH: %w", command, err)
	}

	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// An empty temp dir, not the caller's cwd: a harness discovers project
	// instructions and MCP config by walking up from where it runs, so running
	// anywhere inside a repo would let unrelated context steer a spec draft.
	workDir, err := os.MkdirTemp("", "spec-generate-*")
	if err != nil {
		return "", fmt.Errorf("creating contained working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	cmd := exec.CommandContext(ctx, command, args...) //nolint:gosec // command comes from user config by design
	cmd.Dir = workDir
	cmd.Stdin = nil
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Own process group, so cancellation reaches the harness's children.
	setProcessGroup(cmd)
	// CommandContext kills only the direct child; both shipping harnesses spawn
	// descendants, so the group is signalled instead.
	cmd.Cancel = func() error { return killGroup(cmd) }

	runErr := cmd.Run()

	if ctxErr := ctx.Err(); errors.Is(ctxErr, context.DeadlineExceeded) {
		return "", fmt.Errorf("%s completion exceeded its %s budget — raise it with agent.generate.timeout, or use a faster model", command, timeout)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "", ctx.Err()
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(truncate(stdout.String(), MaxRawTail))
		}
		return "", fmt.Errorf("%s completion failed: %w: %s", command, runErr, detail)
	}

	return stdout.String(), nil
}

// Truncate bounds a string for use as a debugging tail.
func Truncate(s string, limit int) string { return truncate(s, limit) }

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Keep the tail: harness errors and final output land at the end.
	return "..." + s[len(s)-limit:]
}
