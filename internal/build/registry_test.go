package build

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkill creates a skill directory with a SKILL.md body (and optional
// frontmatter) under the given skills dir.
func writeSkill(t *testing.T, skillsDir, name, body string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestSkillRegistry_RoutesByLayerWithModifiers covers the Item 4 acceptance:
// node [nexl360:rails-api] resolves the rails-api-service skill plus the
// cross-cutting modifiers.
func TestSkillRegistry_RoutesByLayerWithModifiers(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "rails-api-service", "# rails api")
	writeSkill(t, skillsDir, "react-web", "# react")
	writeSkill(t, skillsDir, "testing", "# testing")
	writeSkill(t, skillsDir, "security-review", "# security")

	registry := `skills:
  - name: rails-api-service
    path: rails-api-service
    applies_to:
      layers: [rails-api]
      repos: [nexl-ai-core]
  - name: react-web
    path: react-web
    applies_to:
      layers: [react-web]
modifiers:
  - testing
  - security-review
`
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	node := PRStep{Number: 1, Repo: "nexl360", Layer: "rails-api", Description: "mgmt screen"}
	got, ok := skillRegistryForNode(repo, node)
	if !ok {
		t.Fatal("expected registry routing to apply")
	}
	names := skillNames(got)
	for _, want := range []string{"rails-api-service", "testing", "security-review"} {
		if !contains(names, want) {
			t.Errorf("routed skills %v missing %q", names, want)
		}
	}
	if contains(names, "react-web") {
		t.Errorf("react-web should not route to a rails-api node: %v", names)
	}
}

// TestSkillRegistry_RoutesByRepo verifies repo-based matching independent of
// layer.
func TestSkillRegistry_RoutesByRepo(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "core-service", "# core")
	registry := `skills:
  - name: core-service
    path: core-service
    applies_to:
      repos: [nexl-ai-core]
`
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := skillRegistryForNode(repo, PRStep{Repo: "nexl-ai-core"})
	if !ok || len(got) != 1 || filepath.Base(got[0]) != "core-service" {
		t.Fatalf("expected core-service routed, got %v ok=%v", skillNames(got), ok)
	}
}

// TestSkillRegistry_UnmatchedDegradesToDiscovery verifies an unmatched layer
// returns false (so the caller falls back to discovery) without error.
func TestSkillRegistry_UnmatchedDegradesToDiscovery(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "rails-api-service", "# rails")
	registry := `skills:
  - name: rails-api-service
    path: rails-api-service
    applies_to:
      layers: [rails-api]
`
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	_, ok := skillRegistryForNode(repo, PRStep{Repo: "x", Layer: "go-grpc"})
	if ok {
		t.Error("unmatched node should degrade to discovery (ok=false)")
	}

	// And via skillsForNode the fallback discovery still finds the skill dir.
	got := skillsForNode(repo, PRStep{Repo: "x", Layer: "go-grpc"}, Options{})
	if len(got) == 0 {
		t.Error("expected discovery fallback to surface the skills dir entry")
	}
}

// TestSkillRegistry_FromFrontmatter verifies routing synthesised from per-skill
// SKILL.md frontmatter when no registry.yaml is present.
func TestSkillRegistry_FromFrontmatter(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "rails-api-service", "---\napplies_to:\n  layers: [rails-api]\n---\n# rails")
	writeSkill(t, skillsDir, "testing", "---\nmodifier: true\n---\n# testing")

	got, ok := skillRegistryForNode(repo, PRStep{Repo: "nexl360", Layer: "rails-api"})
	if !ok {
		t.Fatal("expected frontmatter-derived registry to apply")
	}
	names := skillNames(got)
	if !contains(names, "rails-api-service") || !contains(names, "testing") {
		t.Errorf("frontmatter routing = %v, want rails-api-service + testing modifier", names)
	}
}

// TestSkillRegistry_NoRegistryFallsBack verifies that with no registry and no
// frontmatter, routing reports false so discovery handles it.
func TestSkillRegistry_NoRegistryFallsBack(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "plain", "# plain skill, no routing")

	if _, ok := skillRegistryForNode(repo, PRStep{Repo: "x", Layer: "y"}); ok {
		t.Error("expected no registry routing without registry.yaml or frontmatter")
	}
}

// TestSkillRegistry_CanonicalShape covers the registry/v1 canonical form that
// the shipped ai-squad-skills registries use and that previously failed to
// parse: a flat prefixed applies_to, kind: layer|modifier, the .agents/skills
// location, and repo-root-relative paths. Routing must resolve correctly.
func TestSkillRegistry_CanonicalShape(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "hexagonal-architecture", "# hex")
	writeSkill(t, skillsDir, "deep-review", "# review")
	writeSkill(t, skillsDir, "test-driven-development", "# tdd")

	registry := `version: "1"
modifiers:
  - test-driven-development
  - deep-review
skills:
  - name: hexagonal-architecture
    kind: layer
    path: .agents/skills/hexagonal-architecture
    applies_to: ["nexl-ai-agent", "layer:hexagonal"]
    quality_gates: ["qlty check"]
  - name: deep-review
    kind: modifier
    path: .agents/skills/deep-review
  - name: test-driven-development
    kind: modifier
    path: .agents/skills/test-driven-development
  - name: context-glossary
    kind: modifier
    path: .agents/skills/context-glossary
`
	writeSkill(t, skillsDir, "context-glossary", "# glossary")
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := skillRegistryForNode(repo, PRStep{Repo: "nexl-ai-agent"})
	if !ok {
		t.Fatal("canonical registry should route (regression: flat applies_to once failed to parse)")
	}
	names := skillNames(got)
	for _, want := range []string{"hexagonal-architecture", "test-driven-development", "deep-review"} {
		if !contains(names, want) {
			t.Errorf("routed %v missing %q", names, want)
		}
	}
	// context-glossary is kind:modifier but NOT in the modifiers list — it is
	// declared-but-opt-in and must not auto-compose.
	if contains(names, "context-glossary") {
		t.Errorf("opt-in modifier context-glossary must not auto-compose: %v", names)
	}
	// Layer-tag matching also works.
	if _, ok := skillRegistryForNode(repo, PRStep{Layer: "hexagonal"}); !ok {
		t.Error("layer:hexagonal should match the layer skill")
	}
}

// TestNoneRouter_RoutesNothing verifies the passthrough router returns no skills
// even when a matching registry is present — skill discovery is left to the
// harness (the BYO "bring no routing model" path).
func TestNoneRouter_RoutesNothing(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "hex", "# hex")
	registry := "version: \"1\"\nskills:\n  - name: hex\n    kind: layer\n    path: .agents/skills/hex\n    applies_to: [\"svc\"]\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	node := PRStep{Repo: "svc"}
	if got := newSkillRouter(repo, Options{}).Route(node); len(got) == 0 {
		t.Fatalf("default registry router should route the canonical registry, got none")
	}
	if got := newSkillRouter(repo, Options{Router: "none"}).Route(node); len(got) != 0 {
		t.Errorf("none router must route nothing, got %v", skillNames(got))
	}
}

// TestSkillRegistry_LegacyModifierBool verifies the legacy `modifier: true` flag
// is still honoured alongside the new kind discriminator (compat shim).
func TestSkillRegistry_LegacyModifierBool(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, agentDir, "skills")
	writeSkill(t, skillsDir, "layer-skill", "# layer")
	writeSkill(t, skillsDir, "mod-skill", "# mod")
	registry := `skills:
  - name: layer-skill
    path: layer-skill
    applies_to:
      repos: [svc]
  - name: mod-skill
    path: mod-skill
    modifier: true
`
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := skillRegistryForNode(repo, PRStep{Repo: "svc"})
	if !ok {
		t.Fatal("expected routing")
	}
	names := skillNames(got)
	if !contains(names, "layer-skill") || !contains(names, "mod-skill") {
		t.Errorf("legacy modifier bool routing = %v", names)
	}
}
