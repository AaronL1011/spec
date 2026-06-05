package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Options configures a build engine beyond its adapters.
type Options struct {
	// Headless runs the agent autonomously (e.g. `spec fix --auto`).
	Headless bool
	// SkillRefs are explicit skill paths from config
	// (integrations.agent.settings.skill). They take precedence over
	// profile.yaml refs and the .spec/agent/skills/ directory. They are the
	// per-node worker fallback used when registry routing does not match.
	SkillRefs []string
	// ConductorSkills are the orchestrator-level skills handed to an MCP-capable
	// agent (integrations.agent.settings.conductor_skill). They are resolved
	// from the start dir only and are kept distinct from per-node worker skills,
	// which reach workers solely via spec_provision_node.
	ConductorSkills []string
	// TestCommand, when set, is run to populate FailingTests (best-effort).
	TestCommand string
	// Workspaces maps a PR-step repo name to a local directory. It is the
	// source-of-truth repo that worktrees are added to for each node.
	Workspaces map[string]string
	// MaxParallel bounds orchestrator fan-out across ready nodes. Surfaced to
	// the agent via the DAG resource; 0 means "use the default".
	MaxParallel int
	// Router selects the Tier-1 SkillRouter: "registry" (default), or
	// "none"/"discovery" to route nothing and let the harness discover skills.
	// Empty means the default.
	Router string
}

// agentDir is the reserved location for the agent skill/profile seam.
const agentDir = ".spec/agent"

// agentProfile is the OPTIONAL .spec/agent/profile.yaml. All fields are
// optional and read leniently; unknown keys are ignored.
type agentProfile struct {
	Model    string            `yaml:"model"`
	Thinking string            `yaml:"thinking"`
	Skill    map[string]string `yaml:"skill"`
}

// writeMCPConfig emits an ephemeral MCP config that points an agent at the
// spec MCP server focused on the active spec. Agents launched with this config
// can read spec://current/* resources and call the DAG node tools
// (spec_provision_node / spec_node_complete / …) with no manual .mcp.json setup.
func writeMCPConfig(specID, path string) error {
	bin := "spec"
	if exe, err := os.Executable(); err == nil && exe != "" {
		bin = exe
	}

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"spec": map[string]any{
				"command": bin,
				"args":    []string{"mcp-server", "--spec", specID},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling mcp config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating mcp config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing mcp config: %w", err)
	}
	return nil
}

// readProfile reads the OPTIONAL .spec/agent/profile.yaml. A missing or
// malformed profile yields a zero-value profile and no error — the engine
// falls back to defaults.
func readProfile(workDir string) agentProfile {
	var p agentProfile
	data, err := os.ReadFile(filepath.Join(workDir, agentDir, "profile.yaml"))
	if err != nil {
		return p
	}
	// Lenient: ignore parse errors so a malformed profile never breaks a build.
	_ = yaml.Unmarshal(data, &p)
	return p
}

// resolveSkills discovers skill paths in priority order: explicit config refs,
// then profile.yaml refs, then any entries under .spec/agent/skills/. Returns
// absolute paths. An empty result means no skills — the build proceeds normally.
func resolveSkills(workDir string, explicit []string, profile agentProfile) []string {
	var refs []string
	refs = append(refs, explicit...)
	for _, ref := range profile.Skill {
		refs = append(refs, ref)
	}

	var paths []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p == "" {
			return
		}
		p = expandTilde(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(workDir, p)
		}
		if seen[p] {
			return
		}
		if _, err := os.Stat(p); err != nil {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}

	for _, ref := range refs {
		add(strings.TrimSpace(ref))
	}

	// Fall back to discovering skill entries under the candidate skills dirs.
	if len(paths) == 0 {
		for _, skillsDir := range skillsDirsFor(workDir) {
			entries, err := os.ReadDir(skillsDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if isDiscoverableSkill(e) {
					add(filepath.Join(skillsDir, e.Name()))
				}
			}
		}
	}

	return paths
}

// isDiscoverableSkill reports whether a directory entry is an Agent Skill: a
// non-hidden directory (containing SKILL.md) or a single .md file. This excludes
// non-skill files such as registry.yaml from discovery.
func isDiscoverableSkill(e os.DirEntry) bool {
	if strings.HasPrefix(e.Name(), ".") {
		return false
	}
	return e.IsDir() || strings.HasSuffix(e.Name(), ".md")
}

// expandTilde resolves a leading ~/ (or bare ~) to the user's home directory so
// config values like "~/skills/spec-build" work. Other paths are unchanged.
func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// readSkillBody returns the markdown body of a skill path. A skill is either a
// single .md file or a directory containing SKILL.md (Agent Skills standard).
// Missing skills yield an empty string.
func readSkillBody(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	target := path
	if info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return ""
	}
	return string(data)
}
