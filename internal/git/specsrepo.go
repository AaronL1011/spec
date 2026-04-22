package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nexl/spec-cli/internal/config"
)

const (
	maxPushRetries = 3
)

// SpecsRepoDir returns the local path for the specs repo clone.
func SpecsRepoDir(cfg *config.SpecsRepoConfig) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".spec", "repos", cfg.Owner, cfg.Repo)
}

// SpecsRepoURL returns the clone URL for the specs repo.
// If a token is configured, it is embedded in the URL for passwordless auth.
func SpecsRepoURL(cfg *config.SpecsRepoConfig) string {
	var host string
	switch cfg.Provider {
	case "gitlab":
		host = "gitlab.com"
	case "bitbucket":
		host = "bitbucket.org"
	default:
		host = "github.com"
	}

	if cfg.Token != "" {
		return fmt.Sprintf("https://x-access-token:%s@%s/%s/%s.git", cfg.Token, host, cfg.Owner, cfg.Repo)
	}
	return fmt.Sprintf("https://%s/%s/%s.git", host, cfg.Owner, cfg.Repo)
}

// EnsureSpecsRepo clones the specs repo if not present, otherwise fetches latest.
func EnsureSpecsRepo(ctx context.Context, cfg *config.SpecsRepoConfig) (string, error) {
	if err := validateToken(cfg); err != nil {
		return "", err
	}

	dir := SpecsRepoDir(cfg)

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		// Clone
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("creating repos directory: %w", err)
		}
		url := SpecsRepoURL(cfg)
		if err := Clone(ctx, url, dir); err != nil {
			return "", fmt.Errorf("cloning specs repo %s/%s: %w", cfg.Owner, cfg.Repo, redactToken(err))
		}
		return dir, nil
	}

	// Guard against nuking unpushed local edits
	if err := guardUnpushedChanges(ctx, dir); err != nil {
		return dir, err
	}

	// Ensure the remote URL has the current token
	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return dir, fmt.Errorf("updating remote URL: %w", err)
	}

	// Fetch and reset to latest
	if err := Fetch(ctx, dir); err != nil {
		return dir, fmt.Errorf("fetching specs repo: %w", redactToken(err))
	}
	ref := fmt.Sprintf("origin/%s", cfg.Branch)
	if err := ResetHard(ctx, dir, ref); err != nil {
		return dir, fmt.Errorf("resetting specs repo: %w", redactToken(err))
	}

	return dir, nil
}

// WithSpecsRepo fetches the latest, calls the mutator function, then commits and pushes.
// If the push fails due to a conflict, it retries up to maxPushRetries times.
func WithSpecsRepo(ctx context.Context, cfg *config.SpecsRepoConfig, mutate func(repoPath string) (commitMsg string, err error)) error {
	dir := SpecsRepoDir(cfg)

	// Ensure the remote URL has the current token
	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return fmt.Errorf("updating remote URL: %w", err)
	}

	// Guard against nuking unpushed local edits (checked once before first attempt)
	if err := guardUnpushedChanges(ctx, dir); err != nil {
		return err
	}

	for attempt := 0; attempt <= maxPushRetries; attempt++ {
		// Fetch latest
		if err := Fetch(ctx, dir); err != nil {
			return fmt.Errorf("fetching specs repo: %w", redactToken(err))
		}
		ref := fmt.Sprintf("origin/%s", cfg.Branch)
		if err := ResetHard(ctx, dir, ref); err != nil {
			return fmt.Errorf("resetting specs repo: %w", redactToken(err))
		}

		// Apply mutation
		commitMsg, err := mutate(dir)
		if err != nil {
			return fmt.Errorf("mutation failed: %w", err)
		}

		// Check if there are changes to commit
		hasChanges, err := HasChanges(ctx, dir)
		if err != nil {
			return fmt.Errorf("checking changes: %w", err)
		}
		if !hasChanges {
			return nil // Nothing to do
		}

		// Commit
		if err := Commit(ctx, dir, commitMsg); err != nil {
			return fmt.Errorf("committing: %w", err)
		}

		// Push
		if err := Push(ctx, dir, cfg.Branch); err != nil {
			if attempt < maxPushRetries {
				// Retry: concurrent push may have advanced the ref
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return fmt.Errorf("push failed after %d retries — another user may have modified the specs repo: %w", maxPushRetries, redactToken(err))
		}

		return nil
	}

	return fmt.Errorf("push failed after %d retries", maxPushRetries)
}

// PushLocalEdits commits any uncommitted changes in the specs repo and pushes them.
// Unlike WithSpecsRepo, which resets to remote state before applying a mutation,
// PushLocalEdits preserves existing local edits — it is the backing implementation
// for `spec push`. Returns true if changes were found and pushed.
// On a push conflict it fetches and rebases rather than hard-resetting, preserving
// the committed local work.
func PushLocalEdits(ctx context.Context, cfg *config.SpecsRepoConfig, commitMsg string) (bool, error) {
	if err := validateToken(cfg); err != nil {
		return false, err
	}

	dir := SpecsRepoDir(cfg)

	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return false, fmt.Errorf("updating remote URL: %w", err)
	}

	hasChanges, err := HasChanges(ctx, dir)
	if err != nil {
		return false, fmt.Errorf("checking local changes: %w", err)
	}
	if !hasChanges {
		return false, nil
	}

	if err := Commit(ctx, dir, commitMsg); err != nil {
		return false, fmt.Errorf("committing local edits: %w", err)
	}

	for attempt := 0; attempt <= maxPushRetries; attempt++ {
		pushErr := Push(ctx, dir, cfg.Branch)
		if pushErr == nil {
			return true, nil
		}
		if attempt >= maxPushRetries {
			return false, fmt.Errorf("push failed after %d retries — another user may have modified the specs repo: %w", maxPushRetries, redactToken(pushErr))
		}
		if err := Fetch(ctx, dir); err != nil {
			return false, fmt.Errorf("fetching after push conflict: %w", redactToken(err))
		}
		ref := fmt.Sprintf("origin/%s", cfg.Branch)
		if err := Rebase(ctx, dir, ref); err != nil {
			return false, fmt.Errorf("rebasing after push conflict — resolve manually in %s: %w", dir, err)
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}

	return false, fmt.Errorf("push failed after %d retries", maxPushRetries)
}

// ReadSpecFile reads a spec file from the specs repo.
func ReadSpecFile(cfg *config.SpecsRepoConfig, filename string) ([]byte, error) {
	dir := SpecsRepoDir(cfg)
	path := filepath.Join(dir, filename)
	return os.ReadFile(path)
}

// ListSpecFiles returns all spec files in the specs repo root.
func ListSpecFiles(cfg *config.SpecsRepoConfig) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	return listMarkdownFiles(dir)
}

// ListTriageFiles returns all triage files in the triage/ directory.
func ListTriageFiles(cfg *config.SpecsRepoConfig) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	triageDir := filepath.Join(dir, "triage")
	if _, err := os.Stat(triageDir); os.IsNotExist(err) {
		return nil, nil
	}
	return listMarkdownFiles(triageDir)
}

// ListArchiveFiles returns all archived spec files.
func ListArchiveFiles(cfg *config.SpecsRepoConfig, archiveDir string) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	archivePath := filepath.Join(dir, archiveDir)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return nil, nil
	}
	return listMarkdownFiles(archivePath)
}

func listMarkdownFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// SpecFilePath returns the absolute path to a spec file in the specs repo.
func SpecFilePath(cfg *config.SpecsRepoConfig, filename string) string {
	return filepath.Join(SpecsRepoDir(cfg), filename)
}

// TriageFilePath returns the absolute path to a triage file.
func TriageFilePath(cfg *config.SpecsRepoConfig, filename string) string {
	return filepath.Join(SpecsRepoDir(cfg), "triage", filename)
}

// guardUnpushedChanges checks for uncommitted changes in the specs repo
// and returns an actionable error if any are found. This prevents
// hard-reset operations from silently discarding local edits.
func guardUnpushedChanges(ctx context.Context, dir string) error {
	if os.Getenv("SPEC_FORCE") != "" {
		return nil
	}

	has, err := HasChanges(ctx, dir)
	if err != nil {
		// If we can't check, don't block — the reset will proceed.
		return nil
	}
	if !has {
		return nil
	}

	// List the changed files for a helpful message.
	status, _ := Status(ctx, dir)
	return fmt.Errorf(
		"specs repo has unpushed local changes that would be overwritten:\n%s\n\n"+
			"Run 'spec push' to save them, or discard with 'git -C %s checkout .'\n"+
			"To force this operation, set SPEC_FORCE=1",
		indentStatus(status), dir,
	)
}

// indentStatus prefixes each line of git status output for readability.
func indentStatus(status string) string {
	if status == "" {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(status, "\n"), "\n") {
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// validateToken checks that the specs repo token is usable.
// Returns an actionable error if the token is missing or looks like an
// unresolved environment variable reference.
func validateToken(cfg *config.SpecsRepoConfig) error {
	token := cfg.Token
	if token == "" {
		return fmt.Errorf("specs repo token not configured — set GITHUB_TOKEN in your environment or add 'token' to specs_repo in spec.config.yaml")
	}
	if strings.HasPrefix(token, "${") {
		return fmt.Errorf("specs repo token %s is not set in your environment — export it before running spec", token)
	}
	return nil
}

// redactToken removes tokens from error messages to avoid leaking credentials.
func redactToken(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Redact x-access-token:TOKEN@ patterns
	redacted := tokenRedactPattern.ReplaceAllString(msg, "x-access-token:***@")
	return fmt.Errorf("%s", redacted)
}

var tokenRedactPattern = regexp.MustCompile(`x-access-token:[^@]+@`)

// ensureRemoteURL updates the origin remote URL if the token has changed
// since the repo was cloned. This ensures fetch/push use the current token.
func ensureRemoteURL(ctx context.Context, dir string, cfg *config.SpecsRepoConfig) error {
	expected := SpecsRepoURL(cfg)
	current, err := Run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("getting current remote URL: %w", err)
	}
	if current != expected {
		if _, err := Run(ctx, dir, "remote", "set-url", "origin", expected); err != nil {
			return fmt.Errorf("setting remote URL: %w", err)
		}
	}
	return nil
}
