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
	got, ok := skillRegistryForNode(repo, node, Options{})
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

	got, ok := skillRegistryForNode(repo, PRStep{Repo: "nexl-ai-core"}, Options{})
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

	_, ok := skillRegistryForNode(repo, PRStep{Repo: "x", Layer: "go-grpc"}, Options{})
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

	got, ok := skillRegistryForNode(repo, PRStep{Repo: "nexl360", Layer: "rails-api"}, Options{})
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

	if _, ok := skillRegistryForNode(repo, PRStep{Repo: "x", Layer: "y"}, Options{}); ok {
		t.Error("expected no registry routing without registry.yaml or frontmatter")
	}
}
