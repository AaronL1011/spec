package build

import (
	"fmt"
	"path/filepath"
)

// resolveRepoPath maps a node's repo to the local source repo its worktrees are
// added to. Resolution is shared by the engine (workspace validation) and the
// MCP server (provisioning) so they never diverge:
//   - no repo            → baseDir (the session/start directory);
//   - repo == basename(baseDir) → baseDir (you launched inside the repo);
//   - workspaces[repo]   → that path (tilde-expanded, made absolute);
//   - otherwise          → an actionable error naming the repo and config key.
func resolveRepoPath(repo, baseDir string, workspaces map[string]string) (string, error) {
	if repo == "" || repo == filepath.Base(baseDir) {
		return baseDir, nil
	}
	ws := expandTilde(workspaces[repo])
	if ws == "" {
		return "", fmt.Errorf(
			"repo %q has no configured workspace — add it under workspaces.%s in ~/.spec/config.yaml:\n  workspaces:\n    %s: /path/to/%s",
			repo, repo, repo, repo)
	}
	if !filepath.IsAbs(ws) {
		ws = filepath.Join(baseDir, ws)
	}
	return ws, nil
}

// skillsForNode selects the skill paths for a node, identically for the engine
// and the MCP server. Registry-driven routing layers on top of the existing
// precedence (config refs → profile → discovery), which remains the fallback.
func skillsForNode(baseDir string, node PRStep, opts Options) []string {
	repoPath, err := resolveRepoPath(node.Repo, baseDir, opts.Workspaces)
	if err != nil || repoPath == "" {
		repoPath = baseDir
	}
	if routed, ok := skillRegistryForNode(repoPath, node, opts); ok {
		return routed
	}
	return resolveSkills(repoPath, opts.SkillRefs, readProfile(repoPath))
}

// skillRegistryForNode performs registry-driven, per-node skill routing.
//
// Placeholder for Item 4: until the registry parser lands it always reports
// "no registry", so skillsForNode falls back to the existing precedence.
func skillRegistryForNode(_ string, _ PRStep, _ Options) ([]string, bool) {
	return nil, false
}
