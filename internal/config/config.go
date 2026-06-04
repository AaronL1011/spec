// Package config handles loading and resolution of team and user configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// TeamConfig represents the spec.config.yaml file committed to the specs repo.
type TeamConfig struct {
	Version string `yaml:"version"`

	Team struct {
		Name       string `yaml:"name"`
		CycleLabel string `yaml:"cycle_label"`
	} `yaml:"team"`

	SpecsRepo SpecsRepoConfig `yaml:"specs_repo"`

	Integrations IntegrationsConfig `yaml:"integrations"`

	Sync SyncConfig `yaml:"sync"`

	Archive struct {
		Directory string `yaml:"directory"`
	} `yaml:"archive"`

	Dashboard DashboardConfig `yaml:"dashboard"`

	Pipeline PipelineConfig `yaml:"pipeline"`

	// FastTrack configures engineer self-service for small bug fixes.
	FastTrack *FastTrackConfig `yaml:"fast_track,omitempty"`

	// Build configures the agentic build orchestration (DAG fan-out).
	Build BuildConfig `yaml:"build,omitempty"`
}

// BuildConfig tunes the build engine's DAG orchestration.
type BuildConfig struct {
	// MaxParallel bounds how many ready nodes the orchestrator fans out at
	// once. Surfaced to the agent via the DAG resource. Defaults to 4.
	MaxParallel int `yaml:"max_parallel,omitempty"`
}

// defaultMaxParallel is the fan-out bound when build.max_parallel is unset.
const defaultMaxParallel = 4

// GetMaxParallel returns the configured fan-out bound or the default.
func (b BuildConfig) GetMaxParallel() int {
	if b.MaxParallel <= 0 {
		return defaultMaxParallel
	}
	return b.MaxParallel
}

// FastTrackConfig configures the `spec fix` fast-track workflow.
type FastTrackConfig struct {
	// Enabled allows fast-track bug fixes. Defaults to false.
	Enabled bool `yaml:"enabled,omitempty"`

	// AllowedRoles lists roles that can create fast-track specs.
	// Defaults to ["engineer", "tl"].
	AllowedRoles []string `yaml:"allowed_roles,omitempty"`

	// MaxDuration is the maximum time before escalation (e.g., "2d", "48h").
	// If exceeded, notifies TL and/or PM.
	MaxDuration string `yaml:"max_duration,omitempty"`

	// RequireLabels requires fast-track specs to have specific labels (e.g., ["bug", "hotfix"]).
	RequireLabels []string `yaml:"require_labels,omitempty"`
}

// GetAllowedRoles returns allowed roles or default ["engineer", "tl"].
func (f *FastTrackConfig) GetAllowedRoles() []string {
	if f == nil || len(f.AllowedRoles) == 0 {
		return []string{"engineer", "tl"}
	}
	return f.AllowedRoles
}

// IsRoleAllowed checks if a role can create fast-track specs.
func (f *FastTrackConfig) IsRoleAllowed(role string) bool {
	for _, r := range f.GetAllowedRoles() {
		if r == role {
			return true
		}
	}
	return false
}

// IsEnabled returns whether fast-track is enabled.
func (f *FastTrackConfig) IsEnabled() bool {
	return f != nil && f.Enabled
}

// SpecsRepoConfig defines the specs repository location.
type SpecsRepoConfig struct {
	Provider string `yaml:"provider"`
	Owner    string `yaml:"owner"`
	Repo     string `yaml:"repo"`
	Branch   string `yaml:"branch"`
	Token    string `yaml:"token"`
}

// IntegrationsConfig holds all integration provider configs.
type IntegrationsConfig struct {
	Comms  ProviderConfig `yaml:"comms"`
	PM     ProviderConfig `yaml:"pm"`
	Docs   ProviderConfig `yaml:"docs"`
	Repo   ProviderConfig `yaml:"repo"`
	Agent  ProviderConfig `yaml:"agent"`
	AI     ProviderConfig `yaml:"ai"`
	Design ProviderConfig `yaml:"design"`
	Deploy DeployConfig   `yaml:"deploy"`
	Intake IntakeConfig   `yaml:"intake"`
}

// ProviderConfig is a generic integration config with a provider name and extra fields.
type ProviderConfig struct {
	Provider string            `yaml:"provider"`
	Extra    map[string]string `yaml:"-"`
	raw      map[string]interface{}
}

// Get returns an extra config value by key.
func (p ProviderConfig) Get(key string) string {
	if v, ok := p.Extra[key]; ok {
		return v
	}
	return ""
}

// UnmarshalYAML captures all keys into raw and extracts provider + extras.
func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	p.raw = raw
	if v, ok := raw["provider"]; ok {
		p.Provider = fmt.Sprintf("%v", v)
	}
	p.Extra = make(map[string]string)
	for k, v := range raw {
		if k != "provider" {
			p.Extra[k] = fmt.Sprintf("%v", v)
		}
	}
	return nil
}

// MarshalYAML emits provider plus all extra settings so a ProviderConfig
// round-trips through WriteUserConfig (e.g. a TUI settings save) without
// dropping keys like `command` or `skill`, which live in Extra (yaml:"-").
func (p ProviderConfig) MarshalYAML() (interface{}, error) {
	m := make(map[string]string, len(p.Extra)+1)
	if p.Provider != "" {
		m["provider"] = p.Provider
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	return m, nil
}

// DeployConfig holds deployment provider and environments.
type DeployConfig struct {
	Provider     string        `yaml:"provider"`
	Environments []Environment `yaml:"environments"`
}

// Environment defines a deployment target.
type Environment struct {
	Name string `yaml:"name"`
	Auto bool   `yaml:"auto"`
	Gate string `yaml:"gate,omitempty"`
}

// IntakeConfig holds intake source definitions.
type IntakeConfig struct {
	Sources []IntakeSource `yaml:"sources"`
}

// IntakeSource defines an external intake source.
type IntakeSource struct {
	Provider   string `yaml:"provider"`
	AutoCreate bool   `yaml:"auto_create"`
	Filter     string `yaml:"filter,omitempty"`
	Channel    string `yaml:"channel,omitempty"`
	Trigger    string `yaml:"trigger,omitempty"`
	Token      string `yaml:"token,omitempty"`
}

// SyncConfig defines sync behaviour.
type SyncConfig struct {
	OutboundOnAdvance bool   `yaml:"outbound_on_advance"`
	ConflictStrategy  string `yaml:"conflict_strategy"`
}

// DashboardConfig defines dashboard behaviour.
type DashboardConfig struct {
	StaleThreshold string `yaml:"stale_threshold"`
	RefreshTTL     int    `yaml:"refresh_ttl"`
}

// NOTE: PipelineConfig, StageConfig, GateConfig and related types are defined
// in pipeline.go to keep pipeline configuration concerns together.

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// interpolateEnvVars replaces ${VAR} patterns with environment variable values.
// Token variables support a spec-prefixed/legacy alias so that configs written
// against either naming convention resolve regardless of which variable the
// user exported (see lookupEnvWithAlias).
func interpolateEnvVars(data []byte) []byte {
	return envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(envVarPattern.FindSubmatch(match)[1])
		if val, ok := lookupEnvWithAlias(varName); ok {
			return []byte(val)
		}
		return match
	})
}

// lookupEnvWithAlias resolves an environment variable, falling back to a token
// alias when the requested variable is unset. spec migrated provider tokens
// from the legacy GITHUB_TOKEN style to the SPEC_GITHUB_TOKEN style; this lets
// configs that reference either spelling resolve against whichever variable the
// user has actually exported. The exact variable always wins; the alias is a
// fallback so existing (already-joined) configs keep working.
func lookupEnvWithAlias(varName string) (string, bool) {
	if val, ok := lookupNonEmptyEnv(varName); ok {
		return val, true
	}
	alias, ok := tokenEnvAlias(varName)
	if !ok {
		return "", false
	}
	if val, ok := lookupNonEmptyEnv(alias); ok {
		if strings.HasPrefix(varName, specTokenPrefix) {
			// Config asked for the new name but only the legacy var is set.
			fmt.Fprintf(os.Stderr, "warning: $%s is unset; falling back to deprecated $%s — export $%s instead\n",
				varName, alias, varName)
		}
		return val, true
	}
	return "", false
}

// lookupNonEmptyEnv reports an environment variable only when it is both set
// and non-empty. An exported-but-empty token (e.g. GITHUB_TOKEN= in CI) is
// treated as unset so the alias fallback can still resolve a usable value.
func lookupNonEmptyEnv(name string) (string, bool) {
	if val, ok := os.LookupEnv(name); ok && val != "" {
		return val, true
	}
	return "", false
}

const (
	specTokenPrefix = "SPEC_"
	tokenSuffix     = "_TOKEN"
)

// tokenEnvAlias returns the alternate spelling for a provider token variable.
// SPEC_GITHUB_TOKEN <-> GITHUB_TOKEN, and likewise for any *_TOKEN variable.
// Non-token variables have no alias.
func tokenEnvAlias(varName string) (string, bool) {
	if !strings.HasSuffix(varName, tokenSuffix) {
		return "", false
	}
	if strings.HasPrefix(varName, specTokenPrefix) {
		return strings.TrimPrefix(varName, specTokenPrefix), true
	}
	return specTokenPrefix + varName, true
}

// LoadTeamConfig reads and parses a spec.config.yaml file.
func LoadTeamConfig(path string) (*TeamConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading team config %s: %w", path, err)
	}
	data = interpolateEnvVars(data)

	var cfg TeamConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing team config %s: %w", path, err)
	}

	// Apply defaults
	if cfg.SpecsRepo.Branch == "" {
		cfg.SpecsRepo.Branch = "main"
	}
	if cfg.Archive.Directory == "" {
		cfg.Archive.Directory = "archive"
	}
	if cfg.Dashboard.RefreshTTL == 0 {
		cfg.Dashboard.RefreshTTL = 300
	}
	if cfg.Dashboard.StaleThreshold == "" {
		cfg.Dashboard.StaleThreshold = "48h"
	}
	if cfg.Sync.ConflictStrategy == "" {
		cfg.Sync.ConflictStrategy = "warn"
	}

	return &cfg, nil
}

// DefaultPipeline returns the default pipeline configuration when none is specified.
// This is the "product" preset - a full lifecycle pipeline.
func DefaultPipeline() PipelineConfig {
	t := true
	return PipelineConfig{
		Stages: []StageConfig{
			{Name: "triage", Owner: Owners{"pm"}, Icon: "📥"},
			{Name: "draft", Owner: Owners{"pm"}, Icon: "📝"},
			{Name: "tl-review", Owner: Owners{"tl"}, Icon: "👀", Gates: []GateConfig{
				{SectionNotEmpty: "problem_statement"},
			}},
			{Name: "design", Owner: Owners{"designer"}, Icon: "🎨", Gates: []GateConfig{
				{SectionNotEmpty: "user_stories"},
			}},
			{Name: "qa-expectations", Owner: Owners{"qa"}, Icon: "📋", Gates: []GateConfig{
				{SectionNotEmpty: "design_inputs"},
			}},
			{Name: "engineering", Owner: Owners{"engineer"}, Icon: "🔧", Gates: []GateConfig{
				{SectionNotEmpty: "acceptance_criteria"},
			}},
			{Name: "build", Owner: Owners{"engineer"}, Icon: "🏗️"},
			{Name: "pr-review", Owner: Owners{"engineer"}, Icon: "👁️", Gates: []GateConfig{
				{PRStackExists: &t},
			}},
			{Name: "qa-validation", Owner: Owners{"qa"}, Icon: "✅", Gates: []GateConfig{
				{PRsApproved: &t},
			}},
			{Name: "done", Owner: Owners{"tl"}, Icon: "🎉"},
			{Name: "deploying", Owner: Owners{"engineer"}, Icon: "🚀", Optional: true},
			{Name: "monitoring", Owner: Owners{"engineer"}, Icon: "📊", Optional: true},
			{Name: "closed", Owner: Owners{"tl"}, Icon: "📦", Optional: true, AutoArchive: true},
		},
	}
}

// FindTeamConfigPath searches for spec.config.yaml starting from dir, then up.
func FindTeamConfigPath(startDir string) (string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, "spec.config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Check .spec/ subdirectory (in service repos)
		candidate = filepath.Join(dir, ".spec", "spec.config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("spec.config.yaml not found — run 'spec config init' to set up")
}

// RequiredStages returns non-optional stages.
func (p PipelineConfig) RequiredStages() []StageConfig {
	var stages []StageConfig
	for _, s := range p.Stages {
		if !s.Optional {
			stages = append(stages, s)
		}
	}
	return stages
}

// NextStage returns the next stage after the given one.
func (p PipelineConfig) NextStage(current string) (string, bool) {
	idx := p.StageIndex(current)
	if idx < 0 || idx >= len(p.Stages)-1 {
		return "", false
	}
	return p.Stages[idx+1].Name, true
}

// EffectivePipeline returns the pipeline from team config, or default if empty.
func EffectivePipeline(tc *TeamConfig) PipelineConfig {
	if tc != nil && len(tc.Pipeline.Stages) > 0 {
		return tc.Pipeline
	}
	return DefaultPipeline()
}

// ArchiveDir returns the configured archive directory.
func ArchiveDir(tc *TeamConfig) string {
	if tc != nil && tc.Archive.Directory != "" {
		return tc.Archive.Directory
	}
	return "archive"
}

// ResolvedConfig holds the fully resolved team + user configuration.
type ResolvedConfig struct {
	Team *TeamConfig
	User *UserConfig

	// TeamConfigPath is the path to the team config file, if found.
	TeamConfigPath string
	// UserConfigPath is the path to the user config file.
	UserConfigPath string

	// SpecsRepoDir is the local path to the specs/ sub-directory within
	// the specs repo clone. All spec, triage, and archive content lives here.
	SpecsRepoDir string
}

// Pipeline returns the effective pipeline config.
func (r *ResolvedConfig) Pipeline() PipelineConfig {
	return EffectivePipeline(r.Team)
}

// EffectiveAgentConfig returns the coding-agent provider config to use,
// preferring the per-user override (~/.spec/config.yaml `agent:`) when its
// provider is set, then the team default (integrations.agent), then empty.
// This lets engineers pick their own harness while keeping a shared baseline.
func (r *ResolvedConfig) EffectiveAgentConfig() ProviderConfig {
	if r.User != nil && r.User.Agent != nil && r.User.Agent.Provider != "" {
		return *r.User.Agent
	}
	if r.Team != nil {
		return r.Team.Integrations.Agent
	}
	return ProviderConfig{}
}

// OwnerRole returns the user's owner role, with optional override.
func (r *ResolvedConfig) OwnerRole(override string) string {
	if override != "" {
		return strings.ToLower(override)
	}
	if r.User != nil {
		return strings.ToLower(r.User.User.OwnerRole)
	}
	return ""
}

// UserName returns the configured user name.
func (r *ResolvedConfig) UserName() string {
	if r.User != nil && r.User.User.Name != "" {
		return r.User.User.Name
	}
	return "unknown"
}

// UserHandle returns the configured user handle.
func (r *ResolvedConfig) UserHandle() string {
	if r.User != nil {
		return r.User.User.Handle
	}
	return ""
}

// CycleLabel returns the current cycle label.
func (r *ResolvedConfig) CycleLabel() string {
	if r.Team != nil {
		return r.Team.Team.CycleLabel
	}
	return ""
}

// TeamName returns the team name.
func (r *ResolvedConfig) TeamName() string {
	if r.Team != nil {
		return r.Team.Team.Name
	}
	return ""
}

// HasIntegration checks if a specific integration category has a non-empty provider.
func (r *ResolvedConfig) HasIntegration(category string) bool {
	if r.Team == nil {
		return false
	}
	switch category {
	case "comms":
		return r.Team.Integrations.Comms.Provider != "" && r.Team.Integrations.Comms.Provider != "none"
	case "pm":
		return r.Team.Integrations.PM.Provider != "" && r.Team.Integrations.PM.Provider != "none"
	case "docs":
		return r.Team.Integrations.Docs.Provider != "" && r.Team.Integrations.Docs.Provider != "none"
	case "repo":
		return r.Team.Integrations.Repo.Provider != "" && r.Team.Integrations.Repo.Provider != "none"
	case "agent":
		return r.Team.Integrations.Agent.Provider != "" && r.Team.Integrations.Agent.Provider != "none"
	case "ai":
		return r.Team.Integrations.AI.Provider != "" && r.Team.Integrations.AI.Provider != "none"
	case "design":
		return r.Team.Integrations.Design.Provider != "" && r.Team.Integrations.Design.Provider != "none"
	case "deploy":
		return r.Team.Integrations.Deploy.Provider != "" && r.Team.Integrations.Deploy.Provider != "none"
	default:
		return false
	}
}

// AIDraftsEnabled returns whether AI drafting is enabled for the user.
func (r *ResolvedConfig) AIDraftsEnabled() bool {
	if r.User != nil && !r.User.Preferences.AIDraftsEnabled() {
		return false
	}
	return r.HasIntegration("ai")
}
