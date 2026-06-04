package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
// and the MCP server. Registry-driven routing takes precedence; the existing
// precedence (config refs → profile → discovery) remains the fallback when no
// registry is present or the node matches no registered skill.
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

// skillRegistryEntry is one routable skill in registry.yaml. applies_to matches
// a node by repo or layer; modifier entries are cross-cutting and always ride
// along once at least one primary skill matches.
type skillRegistryEntry struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path"`
	Modifier  bool   `yaml:"modifier"`
	AppliesTo struct {
		Layers []string `yaml:"layers"`
		Repos  []string `yaml:"repos"`
	} `yaml:"applies_to"`
}

// skillRegistryFile is the parsed .spec/agent/skills/registry.yaml. modifiers
// lists the names (or paths) of cross-cutting skills always appended to a match.
type skillRegistryFile struct {
	Skills    []skillRegistryEntry `yaml:"skills"`
	Modifiers []string             `yaml:"modifiers"`
}

// skillRegistryForNode performs registry-driven, per-node skill routing. It
// returns (paths, true) when a registry exists and the node matches at least
// one primary skill; otherwise (nil, false) so the caller falls back to
// discovery. An unmatched layer therefore degrades gracefully without error.
func skillRegistryForNode(repoPath string, node PRStep, _ Options) ([]string, bool) {
	skillsDir := filepath.Join(repoPath, agentDir, "skills")
	reg, ok := loadSkillRegistry(skillsDir)
	if !ok {
		return nil, false
	}

	seen := make(map[string]bool)
	var paths []string
	add := func(ref string) {
		p := resolveSkillRef(ref, skillsDir)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}

	matched := false
	for _, e := range reg.Skills {
		if e.Modifier {
			continue
		}
		if entryMatchesNode(e, node) {
			matched = true
			add(skillRef(e))
		}
	}
	if !matched {
		return nil, false
	}

	// Cross-cutting modifiers ride along once a primary skill matched.
	for _, e := range reg.Skills {
		if e.Modifier {
			add(skillRef(e))
		}
	}
	for _, m := range reg.Modifiers {
		add(m)
	}

	return paths, true
}

// entryMatchesNode reports whether a registry entry applies to a node by layer
// or repo. An entry with neither layers nor repos matches nothing (it must be a
// modifier to be included).
func entryMatchesNode(e skillRegistryEntry, node PRStep) bool {
	if node.Layer != "" && containsFold(e.AppliesTo.Layers, node.Layer) {
		return true
	}
	if node.Repo != "" && containsFold(e.AppliesTo.Repos, node.Repo) {
		return true
	}
	return false
}

// skillRef returns the reference used to resolve an entry's path, preferring an
// explicit path and falling back to the skill name.
func skillRef(e skillRegistryEntry) string {
	if e.Path != "" {
		return e.Path
	}
	return e.Name
}

// loadSkillRegistry reads registry.yaml from the skills dir. When absent, it
// synthesises a registry from any SKILL.md frontmatter carrying applies_to, so
// teams can route via either mechanism. Returns false when neither is present.
func loadSkillRegistry(skillsDir string) (*skillRegistryFile, bool) {
	data, err := os.ReadFile(filepath.Join(skillsDir, "registry.yaml"))
	if err == nil {
		var reg skillRegistryFile
		if yaml.Unmarshal(data, &reg) == nil && len(reg.Skills) > 0 {
			return &reg, true
		}
	}
	return loadRegistryFromFrontmatter(skillsDir)
}

// loadRegistryFromFrontmatter builds a registry from per-skill SKILL.md
// frontmatter (applies_to). Returns false when no skill declares routing.
func loadRegistryFromFrontmatter(skillsDir string) (*skillRegistryFile, bool) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, false
	}
	var reg skillRegistryFile
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		fm, ok := readSkillFrontmatter(filepath.Join(skillsDir, ent.Name()))
		if !ok {
			continue
		}
		e := skillRegistryEntry{Name: ent.Name(), Path: ent.Name(), Modifier: fm.Modifier}
		e.AppliesTo.Layers = fm.AppliesTo.Layers
		e.AppliesTo.Repos = fm.AppliesTo.Repos
		reg.Skills = append(reg.Skills, e)
	}
	if len(reg.Skills) == 0 {
		return nil, false
	}
	return &reg, true
}

// skillFrontmatter is the routing subset of a SKILL.md YAML frontmatter block.
type skillFrontmatter struct {
	Modifier  bool `yaml:"modifier"`
	AppliesTo struct {
		Layers []string `yaml:"layers"`
		Repos  []string `yaml:"repos"`
	} `yaml:"applies_to"`
}

// readSkillFrontmatter parses the leading --- YAML block of a skill's SKILL.md.
// Returns false when the skill has no frontmatter or no applies_to/modifier.
func readSkillFrontmatter(skillDir string) (skillFrontmatter, bool) {
	var fm skillFrontmatter
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return fm, false
	}
	block, ok := frontmatterBlock(string(data))
	if !ok {
		return fm, false
	}
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return fm, false
	}
	if len(fm.AppliesTo.Layers) == 0 && len(fm.AppliesTo.Repos) == 0 && !fm.Modifier {
		return fm, false
	}
	return fm, true
}

// containsFold reports whether values contains target, case-insensitively.
func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), target) {
			return true
		}
	}
	return false
}

// frontmatterBlock returns the YAML between the leading --- fences of a markdown
// document, or false when there is no frontmatter block.
func frontmatterBlock(content string) (string, bool) {
	trimmed := strings.TrimLeft(content, "\ufeff\n\r ")
	if !strings.HasPrefix(trimmed, "---") {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// resolveSkillRef turns a registry ref (path or name) into an absolute skill
// path, expanding ~ and resolving relative refs against the skills dir. Returns
// "" when the target does not exist.
func resolveSkillRef(ref, skillsDir string) string {
	p := expandTilde(ref)
	if !filepath.IsAbs(p) {
		p = filepath.Join(skillsDir, p)
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
