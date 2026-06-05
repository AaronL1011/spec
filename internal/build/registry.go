package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillRouter is the Tier-1 port: it maps a DAG node to the capability skill
// paths its worker should load. Routing is opaque to the kernel — a router is
// pluggable policy, and bringing none is a first-class choice. spec-cli ships a
// registry router (the default) and a passthrough "none" router; teams may
// provide their own by populating skill paths another way.
type SkillRouter interface {
	// Route returns absolute skill paths for a node, or nil when it does not
	// route (the harness/model then discovers skills itself).
	Route(node PRStep) []string
}

// newSkillRouter selects a router from Options. The default ("" or "registry")
// is the registry router, which itself degrades to config/profile/discovery and
// finally to nothing when no registry matches. "none"/"discovery" is an explicit
// passthrough that leaves skill selection entirely to the harness.
func newSkillRouter(baseDir string, opts Options) SkillRouter {
	switch strings.ToLower(strings.TrimSpace(opts.Router)) {
	case "none", "discovery", "passthrough":
		return noneRouter{}
	default:
		return registryRouter{baseDir: baseDir, opts: opts}
	}
}

// registryRouter is the default SkillRouter: registry.yaml routing with the
// config-refs → profile → discovery fallback.
type registryRouter struct {
	baseDir string
	opts    Options
}

// Route implements SkillRouter for the registry router.
func (r registryRouter) Route(node PRStep) []string {
	return skillsForNode(r.baseDir, node, r.opts)
}

// noneRouter is the passthrough SkillRouter: it routes nothing, leaving skill
// discovery to the harness (e.g. pi/Claude scanning .agents/skills).
type noneRouter struct{}

// Route implements SkillRouter for the passthrough router.
func (noneRouter) Route(PRStep) []string { return nil }

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

// skillsSubdirs lists the per-repo locations a registry/skills set may live in,
// in priority order: the cross-harness Agent Skills location first, then the
// legacy spec-cli location. Keeping both lets teams adopt the standard layout
// without breaking existing checkouts.
var skillsSubdirs = []string{
	filepath.Join(".agents", "skills"),
	filepath.Join(agentDir, "skills"), // .spec/agent/skills (legacy)
}

// skillsDirsFor returns the candidate skills directories for a repo root.
func skillsDirsFor(repoPath string) []string {
	dirs := make([]string, len(skillsSubdirs))
	for i, sub := range skillsSubdirs {
		dirs[i] = filepath.Join(repoPath, sub)
	}
	return dirs
}

// skillsForNode is the registry router's core: registry-driven routing takes
// precedence; the config refs → profile → discovery chain is the fallback when
// no registry is present or the node matches no registered skill.
func skillsForNode(baseDir string, node PRStep, opts Options) []string {
	repoPath, err := resolveRepoPath(node.Repo, baseDir, opts.Workspaces)
	if err != nil || repoPath == "" {
		repoPath = baseDir
	}
	if routed, ok := skillRegistryForNode(repoPath, node); ok {
		return routed
	}
	return resolveSkills(repoPath, opts.SkillRefs, readProfile(repoPath))
}

// appliesTo is the routing match set for a skill. It accepts both the canonical
// flat prefixed list form (`["repo", "layer:proto"]`) and the legacy nested
// form (`{layers: [...], repos: [...]}`) so registries written to either shape
// parse identically.
type appliesTo struct {
	Layers []string
	Repos  []string
}

// UnmarshalYAML accepts the flat-prefixed-sequence or nested-mapping form.
func (a *appliesTo) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode {
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		for _, it := range items {
			it = strings.TrimSpace(it)
			if rest, ok := strings.CutPrefix(it, "layer:"); ok {
				a.Layers = append(a.Layers, rest)
			} else if it != "" {
				a.Repos = append(a.Repos, it)
			}
		}
		return nil
	}
	var nested struct {
		Layers []string `yaml:"layers"`
		Repos  []string `yaml:"repos"`
	}
	if err := value.Decode(&nested); err != nil {
		return err
	}
	a.Layers, a.Repos = nested.Layers, nested.Repos
	return nil
}

// skillRegistryEntry is one routable skill in registry.yaml. A skill is a
// modifier (cross-cutting, always composed) when kind is "modifier" or the
// legacy `modifier: true` flag is set; otherwise it is a layer skill matched by
// applies_to.
type skillRegistryEntry struct {
	Name         string    `yaml:"name"`
	Kind         string    `yaml:"kind"`
	Path         string    `yaml:"path"`
	ModifierFlag bool      `yaml:"modifier"`
	AppliesTo    appliesTo `yaml:"applies_to"`
	// QualityGates are verification commands a node satisfying this skill must
	// pass (e.g. "qlty check"). Surfaced to the harness via spec_node_context.
	QualityGates []string `yaml:"quality_gates"`
}

// isModifier reports whether the entry is a cross-cutting modifier.
func (e skillRegistryEntry) isModifier() bool {
	return e.ModifierFlag || strings.EqualFold(strings.TrimSpace(e.Kind), "modifier")
}

// skillRegistryFile is the parsed registry.yaml. modifiers lists the names (or
// paths) of cross-cutting skills always appended once a layer skill matches.
type skillRegistryFile struct {
	Version     string               `yaml:"version"`
	Skills      []skillRegistryEntry `yaml:"skills"`
	Modifiers   []string             `yaml:"modifiers"`
	Conventions registryConventions  `yaml:"conventions"`
}

// registryConventions holds repo-level conventions spec-cli applies
// deterministically, independent of skill routing. pr_title is a template with
// {type}, {epic}, and {desc} placeholders, e.g. "[{epic}] {desc}" or
// "{type}: {epic} {desc}".
type registryConventions struct {
	PRTitle string `yaml:"pr_title"`
}

// prTitleFormatForRepo returns the repo-level PR title template from its
// registry, or "" when no registry or no convention is declared. It is
// independent of the active skill router — a convention is repo data, not
// routing policy.
func prTitleFormatForRepo(repoPath string) string {
	reg, _, ok := loadSkillRegistry(repoPath)
	if !ok {
		return ""
	}
	return strings.TrimSpace(reg.Conventions.PRTitle)
}

// skillRegistryForNode performs registry-driven, per-node skill routing. It
// returns (paths, true) when a registry exists and the node matches at least
// one layer skill; otherwise (nil, false) so the caller falls back to
// discovery. An unmatched layer therefore degrades gracefully without error.
func skillRegistryForNode(repoPath string, node PRStep) ([]string, bool) {
	c, ok := composeRegistry(repoPath, node)
	if !ok {
		return nil, false
	}
	return c.skills, true
}

// qualityGatesForNode returns the verification commands a node must pass,
// composed from the registry the same way skills are. Returns nil when no
// registry matches the node.
func qualityGatesForNode(repoPath string, node PRStep) []string {
	c, ok := composeRegistry(repoPath, node)
	if !ok {
		return nil
	}
	return c.gates
}

// composition is the result of resolving a node against a registry: the routed
// skill paths and the quality gates it must satisfy.
type composition struct {
	skills []string
	gates  []string
}

// composeRegistry resolves a node against the repo's registry, collecting both
// the routed skill paths and the quality gates from the matched layer skill plus
// any auto-composed modifiers. It returns ok=false when no registry is present
// or the node matches no layer skill (so the caller falls back to discovery).
func composeRegistry(repoPath string, node PRStep) (composition, bool) {
	reg, skillsDir, ok := loadSkillRegistry(repoPath)
	if !ok {
		return composition{}, false
	}

	var c composition
	seenSkill := make(map[string]bool)
	seenGate := make(map[string]bool)
	addGates := func(gs []string) {
		for _, g := range gs {
			g = strings.TrimSpace(g)
			if g != "" && !seenGate[g] {
				seenGate[g] = true
				c.gates = append(c.gates, g)
			}
		}
	}
	addSkill := func(ref string) {
		p := resolveSkillRef(ref, repoPath, skillsDir)
		if p == "" || seenSkill[p] {
			return
		}
		seenSkill[p] = true
		c.skills = append(c.skills, p)
	}

	matched := false
	for _, e := range reg.Skills {
		if e.isModifier() {
			continue // a modifier is never matched as a layer
		}
		if entryMatchesNode(e, node) {
			matched = true
			addSkill(skillRef(e))
			addGates(e.QualityGates)
		}
	}
	if !matched {
		return composition{}, false
	}

	// Auto-composed modifiers ride along once a layer skill matched. Only the
	// top-level `modifiers:` list (and legacy `modifier: true` entries) are
	// auto-on; a bare `kind: modifier` entry is declared-but-opt-in (e.g. a
	// glossary a human invokes), so it is not composed automatically.
	byName := make(map[string]skillRegistryEntry, len(reg.Skills))
	for _, e := range reg.Skills {
		byName[e.Name] = e
		if e.ModifierFlag {
			addSkill(skillRef(e))
			addGates(e.QualityGates)
		}
	}
	for _, m := range reg.Modifiers {
		addSkill(m)
		if e, ok := byName[m]; ok {
			addGates(e.QualityGates)
		}
	}

	return c, true
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

// loadSkillRegistry finds and parses a registry for a repo. It searches the
// candidate skills dirs for a registry.yaml, then falls back to synthesising a
// registry from per-skill SKILL.md frontmatter. It returns the registry and the
// directory it was found in (used to resolve name-only refs). Returns false when
// neither is present.
func loadSkillRegistry(repoPath string) (*skillRegistryFile, string, bool) {
	for _, dir := range skillsDirsFor(repoPath) {
		data, err := os.ReadFile(filepath.Join(dir, "registry.yaml"))
		if err != nil {
			continue
		}
		var reg skillRegistryFile
		if yaml.Unmarshal(data, &reg) == nil && len(reg.Skills) > 0 {
			return &reg, dir, true
		}
	}
	for _, dir := range skillsDirsFor(repoPath) {
		if reg, ok := loadRegistryFromFrontmatter(dir); ok {
			return reg, dir, true
		}
	}
	return nil, "", false
}

// loadRegistryFromFrontmatter builds a registry from per-skill SKILL.md routing
// frontmatter. Returns false when no skill declares routing.
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
		r, ok := readSkillFrontmatter(filepath.Join(skillsDir, ent.Name()))
		if !ok {
			continue
		}
		e := skillRegistryEntry{Name: ent.Name(), Path: ent.Name(), Kind: r.Kind, ModifierFlag: r.Modifier, AppliesTo: r.AppliesTo, QualityGates: r.QualityGates}
		reg.Skills = append(reg.Skills, e)
	}
	if len(reg.Skills) == 0 {
		return nil, false
	}
	return &reg, true
}

// skillRouting is the routing subset of a SKILL.md frontmatter, usable either at
// the top level or under `metadata:` (the Agent Skills standard location).
type skillRouting struct {
	Kind         string    `yaml:"kind"`
	Modifier     bool      `yaml:"modifier"`
	AppliesTo    appliesTo `yaml:"applies_to"`
	QualityGates []string  `yaml:"quality_gates"`
}

// hasRouting reports whether the block carries any routing information.
func (r skillRouting) hasRouting() bool {
	return len(r.AppliesTo.Layers) > 0 || len(r.AppliesTo.Repos) > 0 || r.Modifier || strings.TrimSpace(r.Kind) != ""
}

// skillFrontmatter captures routing both at the top level and under metadata.
type skillFrontmatter struct {
	skillRouting `yaml:",inline"`
	Metadata     skillRouting `yaml:"metadata"`
}

// readSkillFrontmatter parses the leading --- YAML block of a skill's SKILL.md,
// preferring routing under `metadata:` and falling back to top-level keys.
// Returns false when the skill has no frontmatter or no routing.
func readSkillFrontmatter(skillDir string) (skillRouting, bool) {
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return skillRouting{}, false
	}
	block, ok := frontmatterBlock(string(data))
	if !ok {
		return skillRouting{}, false
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return skillRouting{}, false
	}
	if fm.Metadata.hasRouting() {
		return fm.Metadata, true
	}
	if fm.hasRouting() {
		return fm.skillRouting, true
	}
	return skillRouting{}, false
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

// resolveSkillRef turns a registry ref into an absolute skill path. An absolute
// ref (or ~) is used as-is. A relative ref is resolved repo-root-relative first
// (the canonical `path: .agents/skills/<name>` form), then against the registry
// directory (a bare skill name). Returns "" when the target does not exist.
func resolveSkillRef(ref, repoRoot, skillsDir string) string {
	p := expandTilde(ref)
	if filepath.IsAbs(p) {
		return statOrEmpty(p)
	}
	if cand := statOrEmpty(filepath.Join(repoRoot, p)); cand != "" {
		return cand
	}
	return statOrEmpty(filepath.Join(skillsDir, p))
}

// statOrEmpty returns path if it exists, else "".
func statOrEmpty(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
