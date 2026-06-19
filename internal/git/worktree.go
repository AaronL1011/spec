package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// worktreeRoot returns the directory under which spec-cli places worktrees for
// a given source repo. It sits beside the repo (not inside the working tree) so
// worktree files never pollute the source checkout.
func worktreeRoot(repoPath string) string {
	clean := filepath.Clean(repoPath)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+".spec-worktrees")
}

// worktreeSlug sanitises a branch name into a single path segment.
func worktreeSlug(branch string) string {
	s := strings.ReplaceAll(branch, "/", "-")
	var clean []byte
	for _, c := range []byte(strings.ToLower(s)) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			clean = append(clean, c)
		}
	}
	return string(clean)
}

// DefaultBranch returns the repo's default branch, preferring the remote HEAD
// (origin/HEAD) and falling back to a local main/master. Returns "main" when
// nothing can be determined so callers always have a usable base.
func DefaultBranch(ctx context.Context, repoPath string) string {
	if out, err := Run(ctx, repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, name, ok := strings.Cut(out, "/"); ok && name != "" {
			return name
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if BranchExists(ctx, repoPath, candidate) {
			return candidate
		}
	}
	return "main"
}

// HasOriginRemote reports whether repoPath has an `origin` remote configured.
// Purely-local repos (no origin) cannot be stale relative to a remote, so
// callers skip the freshness fetch for them.
func HasOriginRemote(ctx context.Context, repoPath string) bool {
	out, err := Run(ctx, repoPath, "remote", "get-url", "origin")
	return err == nil && strings.TrimSpace(out) != ""
}

// AddWorktree creates (or re-attaches) a git worktree for branch in repoPath and
// returns its directory. When the branch does not yet exist it is created at
// baseRef; an empty baseRef defaults to HEAD. If a worktree for the branch
// already exists, its existing directory is returned (idempotent resume).
func AddWorktree(ctx context.Context, repoPath, branch, baseRef string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("add worktree in %s: branch name is required", repoPath)
	}
	dir := filepath.Join(worktreeRoot(repoPath), worktreeSlug(branch))

	// Resume: if the directory is already a worktree for this branch, reuse it.
	if dirExists(dir) {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creating worktree root for %s: %w", repoPath, err)
	}

	var err error
	if BranchExists(ctx, repoPath, branch) {
		_, err = Run(ctx, repoPath, "worktree", "add", dir, branch)
	} else {
		ref := baseRef
		if ref == "" {
			ref = "HEAD"
		}
		_, err = Run(ctx, repoPath, "worktree", "add", "-b", branch, dir, ref)
	}
	if err != nil {
		return "", fmt.Errorf("adding worktree for %s in %s — ensure the base ref exists and the branch is free: %w", branch, repoPath, err)
	}
	return dir, nil
}

// RemoveWorktree removes a worktree directory and prunes its administrative
// metadata. It is best-effort cleanup: a missing worktree is not an error.
func RemoveWorktree(ctx context.Context, worktreeDir string) error {
	if worktreeDir == "" {
		return nil
	}
	if !dirExists(worktreeDir) {
		return nil // already gone — nothing to clean up
	}
	if _, err := Run(ctx, worktreeDir, "worktree", "remove", "--force", worktreeDir); err != nil {
		return fmt.Errorf("removing worktree %s: %w", worktreeDir, err)
	}
	return nil
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsRepo reports whether dir is inside a git working tree. Used to validate
// configured workspaces before a build starts.
func IsRepo(ctx context.Context, dir string) bool {
	if !dirExists(dir) {
		return false
	}
	out, err := Run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// ComputeBaseRef determines the ref a node's branch should be cut from, given
// the branches of its DAG parents:
//   - no parents  → the repo default branch;
//   - one parent  → that parent's branch (linear stacking);
//   - many parents → a freshly-created integration branch (named integrationName)
//     that merges every parent branch, so the node sees all upstream work.
func ComputeBaseRef(ctx context.Context, repoPath, defaultBranch string, parentBranches []string, integrationName string) (string, error) {
	switch len(parentBranches) {
	case 0:
		if defaultBranch == "" {
			defaultBranch = DefaultBranch(ctx, repoPath)
		}
		return defaultBranch, nil
	case 1:
		return parentBranches[0], nil
	default:
		return IntegrationBranch(ctx, repoPath, integrationName, parentBranches)
	}
}

// IntegrationBranch creates a branch named name based on the first parent and
// merges the remaining parents into it via a temporary worktree, returning the
// branch name. A merge conflict surfaces as an actionable error naming the
// branches so the engineer can resolve the integration manually.
func IntegrationBranch(ctx context.Context, repoPath, name string, parents []string) (string, error) {
	if len(parents) < 2 {
		return "", fmt.Errorf("integration branch %q needs at least two parents, got %d", name, len(parents))
	}

	// (Re)create the integration branch at the first parent.
	if _, err := Run(ctx, repoPath, "branch", "-f", name, parents[0]); err != nil {
		return "", fmt.Errorf("creating integration branch %s from %s: %w", name, parents[0], err)
	}

	dir := filepath.Join(worktreeRoot(repoPath), worktreeSlug(name)+"-integrate")
	_ = RemoveWorktree(ctx, dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creating integration worktree root: %w", err)
	}
	if _, err := Run(ctx, repoPath, "worktree", "add", dir, name); err != nil {
		return "", fmt.Errorf("adding integration worktree for %s: %w", name, err)
	}
	defer func() { _ = RemoveWorktree(ctx, dir) }()

	args := append([]string{"merge", "--no-edit"}, parents[1:]...)
	if _, err := Run(ctx, dir, args...); err != nil {
		_, _ = Run(ctx, dir, "merge", "--abort") // leave the worktree clean for retry
		return "", fmt.Errorf(
			"merging parents %s into integration branch %s failed (likely a conflict) — resolve the integration manually then re-run: %w",
			strings.Join(parents, ", "), name, err)
	}
	return name, nil
}
