// Package git wraps all git CLI interactions. No other package shells out to git.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
)

// Run executes a git command in the given directory with a timeout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Clone clones a repository to the given directory.
func Clone(ctx context.Context, url, dir string) error {
	_, err := Run(ctx, ".", "clone", url, dir)
	return err
}

// Fetch fetches from origin in the given repo directory.
func Fetch(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "fetch", "origin")
	return err
}

// ResetHard resets the working tree to a specific ref.
func ResetHard(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "reset", "--hard", ref)
	return err
}

// Rebase rebases the current branch onto the given ref.
func Rebase(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "rebase", ref)
	return err
}

// Commit stages all changes and creates a commit.
func Commit(ctx context.Context, dir, message string) error {
	if _, err := Run(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	_, err := Run(ctx, dir, "commit", "-m", message)
	return err
}

// Push pushes to origin for the given branch.
func Push(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "push", "origin", branch)
	return err
}

// CurrentBranch returns the current branch name.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// Status returns the short status output.
func Status(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "status", "--porcelain")
}

// HasChanges returns true if there are uncommitted changes.
func HasChanges(ctx context.Context, dir string) (bool, error) {
	status, err := Status(ctx, dir)
	if err != nil {
		return false, err
	}
	return status != "", nil
}

// Log returns recent commit messages.
func Log(ctx context.Context, dir string, n int, format string) (string, error) {
	if format == "" {
		format = "%h %s"
	}
	return Run(ctx, dir, "log", fmt.Sprintf("-n%d", n), fmt.Sprintf("--format=%s", format))
}

// ConfigGet returns a git config value.
func ConfigGet(ctx context.Context, dir, key string) (string, error) {
	return Run(ctx, dir, "config", "--get", key)
}

// UserName returns the configured git user.name.
func UserName(ctx context.Context) string {
	name, err := ConfigGet(ctx, ".", "user.name")
	if err != nil {
		return "unknown"
	}
	return name
}

// UserEmail returns the configured git user.email.
func UserEmail(ctx context.Context) string {
	email, err := ConfigGet(ctx, ".", "user.email")
	if err != nil {
		return ""
	}
	return email
}

// IsGitRepo checks if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	_, err := Run(context.Background(), dir, "rev-parse", "--git-dir")
	return err == nil
}
