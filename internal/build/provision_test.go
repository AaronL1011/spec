package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMCPConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-config.json")

	if err := writeMCPConfig("SPEC-042", path); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing config: %v", err)
	}

	spec, ok := cfg.MCPServers["spec"]
	if !ok {
		t.Fatal("missing spec server entry")
	}
	want := []string{"mcp-server", "--spec", "SPEC-042"}
	if len(spec.Args) != len(want) {
		t.Fatalf("args = %v, want %v", spec.Args, want)
	}
	for i, a := range want {
		if spec.Args[i] != a {
			t.Errorf("args[%d] = %q, want %q", i, spec.Args[i], a)
		}
	}
}

func TestResolveSkills_EmptyDir(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, agentDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveSkills(workDir, nil, agentProfile{})
	if len(got) != 0 {
		t.Errorf("expected no skills for empty dir, got %v", got)
	}
}

func TestResolveSkills_DiscoversDirEntries(t *testing.T) {
	workDir := t.TempDir()
	skillsDir := filepath.Join(workDir, agentDir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "spec-build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "spec-build", "SKILL.md"), []byte("# Build"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hidden entries (e.g. .gitkeep) must be ignored.
	if err := os.WriteFile(filepath.Join(skillsDir, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveSkills(workDir, nil, agentProfile{})
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %v", got)
	}
	if filepath.Base(got[0]) != "spec-build" {
		t.Errorf("skill = %q, want spec-build", got[0])
	}
}

func TestResolveSkills_ExplicitTakesPrecedence(t *testing.T) {
	workDir := t.TempDir()
	skillsDir := filepath.Join(workDir, agentDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A discoverable dir entry that must be ignored when explicit refs exist.
	if err := os.MkdirAll(filepath.Join(skillsDir, "discovered"), 0o755); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(workDir, "custom-skill.md")
	if err := os.WriteFile(explicit, []byte("# Custom"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveSkills(workDir, []string{"custom-skill.md"}, agentProfile{})
	if len(got) != 1 || filepath.Base(got[0]) != "custom-skill.md" {
		t.Fatalf("expected explicit skill only, got %v", got)
	}
}

func TestReadProfile_MissingIsGraceful(t *testing.T) {
	got := readProfile(t.TempDir())
	if got.Model != "" || got.Thinking != "" || len(got.Skill) != 0 {
		t.Errorf("expected zero-value profile, got %+v", got)
	}
}

func TestReadProfile_MalformedIsGraceful(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, agentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(workDir, agentDir, "profile.yaml")
	if err := os.WriteFile(bad, []byte("model: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Must not panic or error; returns a zero-value profile.
	got := readProfile(workDir)
	if got.Model != "" {
		t.Errorf("expected empty model for malformed profile, got %q", got.Model)
	}
}

func TestReadProfile_ParsesFields(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, agentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model: anthropic/claude-sonnet-4\nthinking: medium\nskill:\n  build: .spec/agent/skills/spec-build\n"
	if err := os.WriteFile(filepath.Join(workDir, agentDir, "profile.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readProfile(workDir)
	if got.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Skill["build"] != ".spec/agent/skills/spec-build" {
		t.Errorf("skill.build = %q", got.Skill["build"])
	}
}

func TestReadSkillBody_DirAndFile(t *testing.T) {
	dir := t.TempDir()

	// Directory form with SKILL.md.
	skillDir := filepath.Join(dir, "spec-build")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Build playbook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if body := readSkillBody(skillDir); body != "# Build playbook" {
		t.Errorf("dir body = %q", body)
	}

	// Single-file form.
	file := filepath.Join(dir, "fix.md")
	if err := os.WriteFile(file, []byte("# Fix playbook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if body := readSkillBody(file); body != "# Fix playbook" {
		t.Errorf("file body = %q", body)
	}

	// Missing path.
	if body := readSkillBody(filepath.Join(dir, "nope")); body != "" {
		t.Errorf("missing body = %q, want empty", body)
	}
}

func TestResolveSkills_ExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillDir := filepath.Join(home, "skills", "spec-build")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Build"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveSkills(t.TempDir(), []string{"~/skills/spec-build"}, agentProfile{})
	if len(got) != 1 || got[0] != skillDir {
		t.Fatalf("expected %q, got %v", skillDir, got)
	}
}
