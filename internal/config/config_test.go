package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTeamConfig(t *testing.T) {
	content := `
version: "1"
team:
  name: "Test Team"
  cycle_label: "Cycle 1"
specs_repo:
  provider: github
  owner: test-org
  repo: specs
  branch: main
integrations:
  comms:
    provider: slack
  pm:
    provider: jira
    base_url: https://jira.example.com
  docs:
    provider: none
  repo:
    provider: github
  agent:
    provider: claude-code
  ai:
    provider: anthropic
    model: claude-sonnet-4-20250514
pipeline:
  stages:
    - name: triage
      owner_role: pm
    - name: draft
      owner_role: pm
    - name: build
      owner_role: engineer
      gates:
        - section_complete: acceptance_criteria
    - name: done
      owner_role: tl
      optional: true
      auto_archive: true
`

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadTeamConfig(path)
	if err != nil {
		t.Fatalf("LoadTeamConfig: %v", err)
	}

	if cfg.Team.Name != "Test Team" {
		t.Errorf("team.name = %q, want %q", cfg.Team.Name, "Test Team")
	}
	if cfg.SpecsRepo.Provider != "github" {
		t.Errorf("specs_repo.provider = %q, want %q", cfg.SpecsRepo.Provider, "github")
	}
	if cfg.Integrations.PM.Provider != "jira" {
		t.Errorf("integrations.pm.provider = %q, want %q", cfg.Integrations.PM.Provider, "jira")
	}
	if cfg.Integrations.AI.Provider != "anthropic" {
		t.Errorf("integrations.ai.provider = %q, want %q", cfg.Integrations.AI.Provider, "anthropic")
	}
	if len(cfg.Pipeline.Stages) != 4 {
		t.Fatalf("pipeline.stages length = %d, want 4", len(cfg.Pipeline.Stages))
	}
	if cfg.Pipeline.Stages[2].Name != "build" {
		t.Errorf("stages[2].name = %q, want %q", cfg.Pipeline.Stages[2].Name, "build")
	}
	if len(cfg.Pipeline.Stages[2].Gates) != 1 {
		t.Fatalf("stages[2].gates length = %d, want 1", len(cfg.Pipeline.Stages[2].Gates))
	}
	if cfg.Pipeline.Stages[2].Gates[0].Type() != "section_complete" {
		t.Errorf("gate type = %q, want %q", cfg.Pipeline.Stages[2].Gates[0].Type(), "section_complete")
	}
	if !cfg.Pipeline.Stages[3].Optional {
		t.Error("stages[3].optional = false, want true")
	}
}

func TestEnvVarInterpolation(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret123")
	content := `
version: "1"
team:
  name: "Test"
specs_repo:
  provider: github
  owner: org
  repo: specs
  token: ${TEST_TOKEN}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadTeamConfig(path)
	if err != nil {
		t.Fatalf("LoadTeamConfig: %v", err)
	}

	if cfg.SpecsRepo.Token != "secret123" {
		t.Errorf("token = %q, want %q", cfg.SpecsRepo.Token, "secret123")
	}
}

func TestPipelineStageIndex(t *testing.T) {
	p := DefaultPipeline()

	tests := []struct {
		name string
		want int
	}{
		{"triage", 0},
		{"draft", 1},
		{"build", 6},
		{"done", 9},
		{"nonexistent", -1},
	}

	for _, tt := range tests {
		got := p.StageIndex(tt.name)
		if got != tt.want {
			t.Errorf("StageIndex(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestPipelineTransitions(t *testing.T) {
	p := DefaultPipeline()

	if !p.IsValidTransition("draft", "tl-review") {
		t.Error("draft → tl-review should be valid")
	}
	if p.IsValidTransition("build", "draft") {
		t.Error("build → draft should be invalid (backward)")
	}
	if !p.IsValidReversion("build", "draft") {
		t.Error("build → draft reversion should be valid")
	}
	if p.IsValidReversion("draft", "build") {
		t.Error("draft → build reversion should be invalid (forward)")
	}
}

func TestFindTeamConfigPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: '1'"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory and search from there
	subDir := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	found, err := FindTeamConfigPath(subDir)
	if err != nil {
		t.Fatalf("FindTeamConfigPath: %v", err)
	}
	if found != cfgPath {
		t.Errorf("found = %q, want %q", found, cfgPath)
	}
}

func TestUserConfig_AIDrafts_DefaultTrue(t *testing.T) {
	content := `
user:
  owner_role: engineer
  name: Test
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	// When ai_drafts is not set, defaults to true
	if !cfg.Preferences.AIDraftsEnabled() {
		t.Error("AIDraftsEnabled() should default to true when not set")
	}
}

func TestUserConfig_AIDrafts_ExplicitFalse(t *testing.T) {
	content := `
user:
  owner_role: engineer
  name: Test
preferences:
  ai_drafts: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	// When ai_drafts is explicitly false, should be false
	if cfg.Preferences.AIDraftsEnabled() {
		t.Error("AIDraftsEnabled() should be false when explicitly set to false")
	}
}

func TestUserConfig_AIDrafts_ExplicitTrue(t *testing.T) {
	content := `
user:
  owner_role: engineer
preferences:
  ai_drafts: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	if !cfg.Preferences.AIDraftsEnabled() {
		t.Error("AIDraftsEnabled() should be true when explicitly set")
	}
}

func TestDefaultPipelineHasAllStages(t *testing.T) {
	p := DefaultPipeline()
	expected := []string{
		"triage", "draft", "tl-review", "design", "qa-expectations",
		"engineering", "build", "pr-review", "qa-validation", "done",
		"deploying", "monitoring", "closed",
	}

	names := p.StageNames()
	if len(names) != len(expected) {
		t.Fatalf("stage count = %d, want %d", len(names), len(expected))
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("stage[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestUserConfig_AutoNavigate_DefaultTrue(t *testing.T) {
	content := `
user:
  owner_role: engineer
  name: Test User
preferences:
  editor: vim
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	if !cfg.Preferences.AutoNavigateEnabled() {
		t.Error("AutoNavigateEnabled should default to true")
	}
}

func TestUserConfig_AutoNavigate_ExplicitFalse(t *testing.T) {
	content := `
user:
  owner_role: engineer
preferences:
  auto_navigate: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	if cfg.Preferences.AutoNavigateEnabled() {
		t.Error("AutoNavigateEnabled should be false when explicitly set")
	}
}

func TestUserConfig_Workspaces(t *testing.T) {
	content := `
user:
  owner_role: engineer
workspaces:
  auth-service: ~/code/auth-service
  api-gateway: /home/user/code/api-gateway
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	if len(cfg.Workspaces) != 2 {
		t.Fatalf("Workspaces count = %d, want 2", len(cfg.Workspaces))
	}

	if cfg.GetWorkspacePath("auth-service") != "~/code/auth-service" {
		t.Errorf("auth-service path = %q", cfg.GetWorkspacePath("auth-service"))
	}

	if cfg.GetWorkspacePath("unknown-repo") != "" {
		t.Error("unknown repo should return empty string")
	}
}

func TestUserConfig_Multiplexer(t *testing.T) {
	content := `
user:
  owner_role: engineer
preferences:
  multiplexer: tmux
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	if cfg.Preferences.Multiplexer != MultiplexerTmux {
		t.Errorf("Multiplexer = %q, want tmux", cfg.Preferences.Multiplexer)
	}
}

func TestUserConfig_PassiveAwareness(t *testing.T) {
	content := `
user:
  owner_role: engineer
preferences:
  passive_awareness:
    show: [review_requests, spec_owned]
    hide: [triage]
    during_build: true
    dismiss_duration: "4h"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	pa := cfg.Preferences.PassiveAwareness
	if pa == nil {
		t.Fatal("PassiveAwareness should not be nil")
	}

	if len(pa.Show) != 2 {
		t.Errorf("Show count = %d, want 2", len(pa.Show))
	}
	if len(pa.Hide) != 1 {
		t.Errorf("Hide count = %d, want 1", len(pa.Hide))
	}
	if !pa.DuringBuild {
		t.Error("DuringBuild should be true")
	}
	if cfg.Preferences.GetDismissDuration() != "4h" {
		t.Errorf("DismissDuration = %q, want 4h", cfg.Preferences.GetDismissDuration())
	}
}

func TestUserConfig_PassiveAwareness_Defaults(t *testing.T) {
	content := `
user:
  owner_role: engineer
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}

	// Should not show during build by default
	if cfg.Preferences.ShowPassiveAwarenessDuringBuild() {
		t.Error("ShowPassiveAwarenessDuringBuild should default to false")
	}

	// Dismiss duration should default to 2h
	if cfg.Preferences.GetDismissDuration() != "2h" {
		t.Errorf("default DismissDuration = %q, want 2h", cfg.Preferences.GetDismissDuration())
	}
}

func TestValidMultiplexers(t *testing.T) {
	valid := ValidMultiplexers()
	if len(valid) != 5 {
		t.Errorf("ValidMultiplexers count = %d, want 5", len(valid))
	}

	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"tmux", true},
		{"zellij", true},
		{"wezterm", true},
		{"iterm2", true},
		{"none", true},
		{"invalid", false},
		{"screen", false},
	}

	for _, tt := range tests {
		got := IsValidMultiplexer(tt.input)
		if got != tt.want {
			t.Errorf("IsValidMultiplexer(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestFastTrackConfig(t *testing.T) {
	content := `
version: "1"
fast_track:
  enabled: true
  allowed_roles: [engineer, tl, senior]
  max_duration: "2d"
  require_labels: [bug, hotfix]
  pipeline_variant: bug
  excluded_stages: [design, qa-expectations]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadTeamConfig(path)
	if err != nil {
		t.Fatalf("LoadTeamConfig: %v", err)
	}

	ft := cfg.FastTrack
	if ft == nil {
		t.Fatal("FastTrack should not be nil")
	}

	if !ft.IsEnabled() {
		t.Error("IsEnabled should be true")
	}

	if len(ft.AllowedRoles) != 3 {
		t.Errorf("AllowedRoles count = %d, want 3", len(ft.AllowedRoles))
	}

	if !ft.IsRoleAllowed("engineer") {
		t.Error("engineer should be allowed")
	}
	if !ft.IsRoleAllowed("senior") {
		t.Error("senior should be allowed")
	}
	if ft.IsRoleAllowed("pm") {
		t.Error("pm should not be allowed")
	}

	if ft.MaxDuration != "2d" {
		t.Errorf("MaxDuration = %q, want 2d", ft.MaxDuration)
	}

	if len(ft.RequireLabels) != 2 {
		t.Errorf("RequireLabels count = %d, want 2", len(ft.RequireLabels))
	}

	if ft.PipelineVariant != "bug" {
		t.Errorf("PipelineVariant = %q, want bug", ft.PipelineVariant)
	}

	if len(ft.ExcludedStages) != 2 {
		t.Errorf("ExcludedStages count = %d, want 2", len(ft.ExcludedStages))
	}
}

func TestFastTrackConfig_Defaults(t *testing.T) {
	// nil config
	var cfg *FastTrackConfig
	if cfg.IsEnabled() {
		t.Error("nil config should not be enabled")
	}
	roles := cfg.GetAllowedRoles()
	if len(roles) != 2 || roles[0] != "engineer" || roles[1] != "tl" {
		t.Errorf("default roles = %v, want [engineer, tl]", roles)
	}
	if cfg.IsRoleAllowed("pm") {
		t.Error("pm should not be allowed by default")
	}
	if !cfg.IsRoleAllowed("engineer") {
		t.Error("engineer should be allowed by default")
	}
}

func TestFastTrackConfig_Disabled(t *testing.T) {
	content := `
version: "1"
fast_track:
  enabled: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadTeamConfig(path)
	if err != nil {
		t.Fatalf("LoadTeamConfig: %v", err)
	}

	if cfg.FastTrack.IsEnabled() {
		t.Error("FastTrack should not be enabled")
	}
}
