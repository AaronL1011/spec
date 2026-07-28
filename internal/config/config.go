// Package config handles loading and resolution of team and user configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/urgency"
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

	// Bounty configures spec bounties: a scarce, role-gated pull signal on
	// claimable work. Absent means bounties are off.
	Bounty *BountyConfig `yaml:"bounty,omitempty"`

	// Build configures the agentic build orchestration (DAG fan-out).
	Build BuildConfig `yaml:"build,omitempty"`

	// Templates configures custom spec/triage skeleton templates committed
	// to the specs repo (SPEC-025). Empty falls back to embedded defaults.
	Templates TemplatesConfig `yaml:"templates,omitempty"`

	// removedIntegrationKeys records which keys this release ignores were still
	// present in the file. The corresponding struct fields are gone, so an
	// ignored key can only be recognised from the source YAML — without this,
	// "key absent" and "key removed" are indistinguishable and the cutover is
	// silent.
	removedIntegrationKeys map[string]bool
}

// UnmarshalYAML decodes a team config and records any removed integration keys
// still present, so resolve can warn and lint can error on them.
func (c *TeamConfig) UnmarshalYAML(value *yaml.Node) error {
	// A type alias sheds this method, so the default decoding applies and this
	// does not recurse.
	type plain TeamConfig
	var out plain
	if err := value.Decode(&out); err != nil {
		return err
	}
	*c = TeamConfig(out)

	if integrations := mappingValue(value, "integrations"); integrations != nil {
		for _, removed := range removedTeamKeys {
			if mappingValue(integrations, removed.key) != nil {
				if c.removedIntegrationKeys == nil {
					c.removedIntegrationKeys = make(map[string]bool, len(removedTeamKeys))
				}
				c.removedIntegrationKeys[removed.key] = true
			}
		}
	}
	return nil
}

// TemplatesConfig names the spec/triage template paths inside the specs repo
// and optional frontmatter defaults seeded into every scaffolded spec.
//
// Paths default to the conventional locations (templates/spec.md,
// templates/triage.md) when left empty, so a team only commits the file to
// opt in — no config block required.
//
// FrontmatterDefaults is an ordered list parsed from a YAML mapping so seeded
// keys land in config declaration order, keeping frontmatter diffs stable
// (no YAML round-trip). Computed scaffold fields (id, title, dates, …) always
// win: a default whose key already appears in the rendered frontmatter is
// skipped.
type TemplatesConfig struct {
	SpecPath   string `yaml:"spec,omitempty"`
	TriagePath string `yaml:"triage,omitempty"`

	FrontmatterDefaults FrontmatterDefaults `yaml:"frontmatter_defaults,omitempty"`
}

// KV is an ordered key/value pair. It mirrors markdown.KV without importing
// the markdown package (config must not depend on internal/markdown), so
// callers convert at the boundary.
type KV struct {
	Key   string
	Value string
}

// FrontmatterDefaults is an ordered list of key/value pairs parsed from a YAML
// mapping, preserving the config's declaration order.
type FrontmatterDefaults []KV

// UnmarshalYAML parses a YAML mapping into an ordered slice of KV pairs,
// preserving key order so seeded frontmatter keys land deterministically.
func (f *FrontmatterDefaults) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("frontmatter_defaults must be a YAML mapping")
	}
	out := make(FrontmatterDefaults, 0, len(value.Content))
	for i := 0; i+1 < len(value.Content); i += 2 {
		var k, v string
		if err := value.Content[i].Decode(&k); err != nil {
			return err
		}
		if err := value.Content[i+1].Decode(&v); err != nil {
			return err
		}
		out = append(out, KV{Key: k, Value: v})
	}
	*f = out
	return nil
}

// EffectiveSpecPath returns the configured spec template path or the default.
func (t TemplatesConfig) EffectiveSpecPath() string {
	if t.SpecPath != "" {
		return t.SpecPath
	}
	return "templates/spec.md"
}

// EffectiveTriagePath returns the configured triage template path or the default.
func (t TemplatesConfig) EffectiveTriagePath() string {
	if t.TriagePath != "" {
		return t.TriagePath
	}
	return "templates/triage.md"
}

// BuildConfig tunes the build engine's DAG orchestration.
type BuildConfig struct {
	// MaxParallel bounds how many ready nodes the orchestrator fans out at
	// once. Surfaced to the agent via the DAG resource. Defaults to 4.
	MaxParallel int `yaml:"max_parallel,omitempty"`

	// Router selects the skill-routing model handed to the build engine:
	// "registry" (default) routes per-node from .agents/skills/registry.yaml;
	// "none" routes nothing and lets the harness discover skills. Empty = default.
	Router string `yaml:"router,omitempty"`

	// Strategy selects the VCS/review workflow: "stacked-draft-pr" (default)
	// stacks a draft PR per node; "none" keeps work on local branches with no
	// finishing tools. Empty = default.
	Strategy string `yaml:"strategy,omitempty"`
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
//
// The agent is deliberately absent: a coding harness and its auth are personal
// tools, so `agent:` lives only in ~/.spec/config.yaml (see UserConfig.Agent).
// Shared agent-adjacent *policy* (build.router, build.strategy) stays in team
// config, because policy is a team concern even though the harness is not.
type IntegrationsConfig struct {
	Comms  ProviderConfig `yaml:"comms"`
	PM     ProviderConfig `yaml:"pm"`
	Docs   ProviderConfig `yaml:"docs"`
	Repo   ProviderConfig `yaml:"repo"`
	Design ProviderConfig `yaml:"design"`
	Deploy DeployConfig   `yaml:"deploy"`
	Intake IntakeConfig   `yaml:"intake"`
}

// GenerateConfig holds completion-plane settings for an agent provider.
//
// It is an explicit struct rather than another Extra key because a nested YAML
// mapping cannot survive ProviderConfig's flattening unmarshal: it would be
// stringified into Extra as "map[model:x ...]".
type GenerateConfig struct {
	// Model is passed through verbatim in the provider's own spelling. spec does
	// not translate model names; `spec agent check` reports which model actually
	// answered, which is how a wrong spelling surfaces.
	Model string `yaml:"model,omitempty"`
	// MaxTokens caps the response. Honoured by completion-API providers only —
	// neither `claude -p` nor `pi -p` exposes a token cap, so it is inert for
	// harness providers and reported as such by lint and `spec agent check`.
	MaxTokens int `yaml:"max_tokens,omitempty"`
	// BaseURL is the OpenAI-compatible endpoint. Required for the generic
	// openai-compatible provider; vendor presets supply a default.
	BaseURL string `yaml:"base_url,omitempty"`
	// Token is an optional bearer credential for gateways that require one.
	// Write it as ${SPEC_LLM_TOKEN}: see tokenSource for why.
	Token string `yaml:"token,omitempty"`
	// Timeout bounds a single completion (e.g. "120s"). Empty uses the adapter
	// default, which is deliberately generous because local models are slow.
	Timeout string `yaml:"timeout,omitempty"`

	// tokenSource retains the literal pre-interpolation spelling of Token (e.g.
	// "${SPEC_LLM_TOKEN}") so a config round-trip re-emits the reference rather
	// than the resolved secret. Config loading expands ${VAR} before unmarshal,
	// so without this a TUI settings save would persist a plaintext API key into
	// ~/.spec/config.yaml — likely a dotfiles repo.
	tokenSource string
}

// TokenSource returns the literal spelling of the token as written in config,
// which is the env-var reference when one was used. Empty when the token was
// absent or written literally.
func (g GenerateConfig) TokenSource() string { return g.tokenSource }

// IsZero reports whether any completion setting is present, so marshalling can
// omit an empty generate block entirely.
func (g GenerateConfig) IsZero() bool {
	return g.Model == "" && g.MaxTokens == 0 && g.BaseURL == "" &&
		g.Token == "" && g.Timeout == ""
}

// ProviderConfig is a generic integration config with a provider name and extra fields.
type ProviderConfig struct {
	Provider string            `yaml:"provider"`
	Extra    map[string]string `yaml:"-"`
	// Generate holds completion-plane settings. Only meaningful for the agent.
	Generate GenerateConfig `yaml:"-"`
	raw      map[string]interface{}
}

// generateKey is the nested mapping ProviderConfig decodes into Generate rather
// than flattening into Extra.
const generateKey = "generate"

// Get returns an extra config value by key.
func (p ProviderConfig) Get(key string) string {
	if v, ok := p.Extra[key]; ok {
		return v
	}
	return ""
}

// UnmarshalYAML captures all keys into raw and extracts provider + extras.
//
// The `generate` key is decoded into the typed Generate field and excluded from
// Extra: flattening it with fmt.Sprintf would turn a nested mapping into the
// string "map[model:x]", which no caller can use.
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
		if k == "provider" || k == generateKey {
			continue
		}
		p.Extra[k] = fmt.Sprintf("%v", v)
	}

	// Decode the nested generate mapping through the YAML node so types (int
	// max_tokens) survive rather than being stringified.
	p.Generate = GenerateConfig{}
	if node := mappingValue(value, generateKey); node != nil {
		if err := node.Decode(&p.Generate); err != nil {
			return fmt.Errorf("parsing agent generate config: %w", err)
		}
	}
	return nil
}

// MarshalYAML emits provider plus all extra settings so a ProviderConfig
// round-trips through WriteUserConfig (e.g. a TUI settings save) without
// dropping keys like `command` or `skill`, which live in Extra (yaml:"-").
//
// The generate block is emitted as a nested mapping, and its token is written
// back as the original ${VAR} reference so a resolved credential is never
// persisted to disk.
func (p ProviderConfig) MarshalYAML() (interface{}, error) {
	m := make(map[string]interface{}, len(p.Extra)+2)
	if p.Provider != "" {
		m["provider"] = p.Provider
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	if !p.Generate.IsZero() {
		gen, err := p.Generate.forMarshal()
		if err != nil {
			return nil, err
		}
		m[generateKey] = gen
	}
	return m, nil
}

// forMarshal renders the generate block for writing, substituting the env-var
// reference for a resolved token. A token that was written literally cannot be
// re-emitted safely, so the write is refused with the env-var form named — spec
// never writes a resolved credential to ~/.spec/config.yaml.
func (g GenerateConfig) forMarshal() (map[string]interface{}, error) {
	out := make(map[string]interface{}, 5)
	if g.Model != "" {
		out["model"] = g.Model
	}
	if g.MaxTokens != 0 {
		out["max_tokens"] = g.MaxTokens
	}
	if g.BaseURL != "" {
		out["base_url"] = g.BaseURL
	}
	if g.Timeout != "" {
		out["timeout"] = g.Timeout
	}
	if g.Token != "" {
		switch {
		case g.tokenSource != "":
			out["token"] = g.tokenSource
		default:
			return nil, fmt.Errorf("refusing to write agent.generate.token as a literal value — set it to an environment reference such as ${SPEC_LLM_TOKEN} and export the variable, so the credential stays out of ~/.spec/config.yaml")
		}
	}
	return out, nil
}

// mappingValue returns the value node for key in a YAML mapping, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// SetTokenSource records the literal pre-interpolation spelling of the token so
// a later marshal re-emits the reference instead of the resolved secret. The
// loader calls this after parsing, because ${VAR} expansion happens on the raw
// bytes before YAML sees them.
func (p *ProviderConfig) SetTokenSource(literal string) {
	p.Generate.tokenSource = literal
}

// envRefPattern matches a whole-value environment reference such as
// ${SPEC_LLM_TOKEN}. Only a whole-value match is safe to re-emit: a token
// assembled from a prefix plus a variable cannot be reconstructed losslessly.
var envRefPattern = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

// agentTokenSource extracts the literal spelling of agent.generate.token from
// raw config bytes, before ${VAR} interpolation. Returns empty when the token is
// absent or not a whole-value env reference.
func agentTokenSource(data []byte) string {
	var probe struct {
		Agent struct {
			Generate struct {
				Token string `yaml:"token"`
			} `yaml:"generate"`
		} `yaml:"agent"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ""
	}
	token := strings.TrimSpace(probe.Agent.Generate.Token)
	if envRefPattern.MatchString(token) {
		return token
	}
	return ""
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
	// AutoPush governs whether local edits and comments are published to the
	// specs repo automatically. One of AutoPushAuto, AutoPushPrompt, AutoPushOff.
	AutoPush string `yaml:"auto_push"`
}

// Auto-push policy values for SyncConfig.AutoPush. They govern whether a local
// mutation (an editor edit, a thread comment) is published to the specs repo
// without the user remembering to run 'spec push'.
const (
	// AutoPushAuto publishes local edits automatically (the default).
	AutoPushAuto = "auto"
	// AutoPushPrompt asks for confirmation before publishing on interactive
	// surfaces; async surfaces (TUI, MCP) treat it as AutoPushAuto.
	AutoPushPrompt = "prompt"
	// AutoPushOff preserves the manual model: edits stay local until the user
	// runs 'spec push'.
	AutoPushOff = "off"
)

// DashboardConfig defines dashboard behaviour.
type DashboardConfig struct {
	StaleThreshold string        `yaml:"stale_threshold"`
	RefreshTTL     int           `yaml:"refresh_ttl"`
	Blocked        BlockedConfig `yaml:"blocked,omitempty"`
	Urgency        UrgencyConfig `yaml:"urgency,omitempty"`
	Review         ReviewConfig  `yaml:"review,omitempty"`
}

// UrgencyConfig tunes the time-urgency gradient shared by the dashboard DO
// section and the pipeline screen.
type UrgencyConfig struct {
	// Easing selects how the raw dwell fraction is shaped into colour intensity:
	// "linear", "ease-in" (default), or "ease-in-strong".
	Easing string `yaml:"easing,omitempty"`
}

// ReviewConfig tunes the REVIEW section of the dashboard.
type ReviewConfig struct {
	// StaleAfter is the review-age window for the time-urgency gradient on
	// REVIEW rows, measured from when the PR was opened. Accepts m/h/d/w units
	// (e.g. "4h", "2d"). Empty, "none", or "0" (the default) means review rows
	// are never coloured — the gradient is opt-in, with no global fallback.
	StaleAfter string `yaml:"stale_after,omitempty"`
}

// ReviewWindow parses the REVIEW staleness window. ok is false when no window
// is configured (empty, "none", "0", or unparseable), meaning REVIEW rows are
// never stale.
func (d DashboardConfig) ReviewWindow() (window time.Duration, ok bool) {
	return parseStaleWindow(d.Review.StaleAfter)
}

// EasingCurve resolves the configured easing name to an urgency.Curve,
// defaulting to ease-in when unset or unrecognised.
func (d DashboardConfig) EasingCurve() urgency.Curve {
	curve, _ := urgency.ParseCurve(d.Urgency.Easing)
	return curve
}

// Blocked scope constants govern which blocked specs appear in a viewer's
// BLOCKED section.
const (
	// BlockedScopeAll shows every blocked spec (default; back-compat).
	BlockedScopeAll = "all"
	// BlockedScopeInvolved shows blocked specs the viewer authors or is assigned.
	BlockedScopeInvolved = "involved"
	// BlockedScopeOwningRole shows blocked specs whose pre-block stage the
	// viewer's role owned.
	BlockedScopeOwningRole = "owning_role"
)

// BlockedConfig configures the dashboard BLOCKED section.
type BlockedConfig struct {
	// VisibleTo lists roles that may see the BLOCKED section. Empty = all roles.
	VisibleTo []string `yaml:"visible_to,omitempty"`

	// Scope filters which blocked specs appear: "all" (default), "involved",
	// or "owning_role".
	Scope string `yaml:"scope,omitempty"`
}

// EffectiveScope returns the configured blocked scope, defaulting to "all".
func (b BlockedConfig) EffectiveScope() string {
	switch b.Scope {
	case BlockedScopeInvolved, BlockedScopeOwningRole:
		return b.Scope
	default:
		return BlockedScopeAll
	}
}

// RoleCanSee reports whether a role may see the BLOCKED section at all. An
// empty VisibleTo list means every role can (back-compat).
func (b BlockedConfig) RoleCanSee(role string) bool {
	if len(b.VisibleTo) == 0 {
		return true
	}
	for _, r := range b.VisibleTo {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
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
	if cfg.Dashboard.Urgency.Easing == "" {
		cfg.Dashboard.Urgency.Easing = urgency.EasingEaseIn
	}
	if cfg.Sync.ConflictStrategy == "" {
		cfg.Sync.ConflictStrategy = "warn"
	}
	if cfg.Sync.AutoPush == "" {
		cfg.Sync.AutoPush = AutoPushAuto
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
//
// It expands presets, applies Skip, and merges stage overrides via
// ResolveStages, so callers that read Stages directly (the TUI pipeline view,
// dashboard, list/status/assign, etc.) see the same final stage list as
// internal/pipeline.Resolve. Without this, a team config that sets only
// preset: minimal would silently fall back to the full default pipeline.
func EffectivePipeline(tc *TeamConfig) PipelineConfig {
	if tc != nil {
		stages, _, _, err := ResolveStages(tc.Pipeline)
		if err == nil && len(stages) > 0 {
			return PipelineConfig{Stages: stages}
		}
		// On an invalid preset, degrade to the default rather than panic; the
		// config linter reports the underlying error separately.
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

// AutoPushPolicy returns the effective auto-push policy, defaulting to
// AutoPushAuto when unset or no team config is present.
func (r *ResolvedConfig) AutoPushPolicy() string {
	if r == nil || r.Team == nil || r.Team.Sync.AutoPush == "" {
		return AutoPushAuto
	}
	return r.Team.Sync.AutoPush
}

// AutoPushEnabled reports whether automatic publishing is active for async
// surfaces (TUI, MCP). Only AutoPushOff disables it; AutoPushPrompt is treated
// as enabled because those surfaces cannot prompt.
func (r *ResolvedConfig) AutoPushEnabled() bool {
	return r.AutoPushPolicy() != AutoPushOff
}

// EffectiveAgentConfig returns the coding-agent provider config, which comes
// solely from personal config (~/.spec/config.yaml `agent:`).
//
// There is no team fallback: a harness and its auth are individual, so the
// team-default-plus-override precedence was a resolve path nobody needed.
// Shared agent-adjacent policy (build.router, build.strategy) still lives in
// team config.
func (r *ResolvedConfig) EffectiveAgentConfig() ProviderConfig {
	if r.User != nil && r.User.Agent != nil {
		return *r.User.Agent
	}
	return ProviderConfig{}
}

// HasAgent reports whether a usable agent provider is configured. Agent config
// is personal, so this deliberately does not consult team config — callers that
// used HasIntegration("agent") must use this instead or they silently report
// "no agent" for every user.
func (r *ResolvedConfig) HasAgent() bool {
	p := r.EffectiveAgentConfig().Provider
	return p != "" && p != "none"
}

// AgentTransitionsAllowed reports whether agent sessions may drive stage
// transitions through the MCP authoring port. Off by default: section writes are
// recoverable from the specs-repo diff, but a transition fires team-visible
// pipeline effects, so it needs an explicit opt-in.
func (r *ResolvedConfig) AgentTransitionsAllowed() bool {
	return r.User != nil && r.User.Preferences.AgentAuthoring.Transitions
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

// UserHandle returns the configured spec-canonical handle. Retained as an
// alias of CanonicalHandle for callers that predate per-integration identity
// resolution; spec-internal identity (frontmatter, threads) uses this.
func (r *ResolvedConfig) UserHandle() string {
	return r.CanonicalHandle()
}

// CanonicalHandle returns the user's spec-internal identity token, falling
// back to the display name when no handle is configured.
func (r *ResolvedConfig) CanonicalHandle() string {
	if r.User == nil {
		return ""
	}
	if h := strings.TrimSpace(r.User.User.Handle); h != "" {
		return h
	}
	return strings.TrimSpace(r.User.User.Name)
}

// ProviderHandle returns the user's handle for a named integration provider
// (e.g. "github", "slack"), falling back to the canonical handle when the
// provider is unmapped. An empty or "none" provider yields the canonical
// handle.
func (r *ResolvedConfig) ProviderHandle(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if r.User != nil && provider != "" && provider != "none" {
		if h, ok := r.User.User.Identities[provider]; ok {
			if h = strings.TrimSpace(h); h != "" {
				return h
			}
		}
	}
	return r.CanonicalHandle()
}

// IdentityForCategory resolves the handle to use for an integration category
// ("repo", "comms", "pm", "docs", "design", "deploy"): it maps the category to
// the team's configured provider, then resolves that provider's handle. Falls
// back to the canonical handle when the category has no provider or no mapping
// exists. "agent" is not a team category — see EffectiveAgentConfig.
func (r *ResolvedConfig) IdentityForCategory(category string) string {
	return r.ProviderHandle(r.providerForCategory(category))
}

// IntegrationProvider returns the team's configured provider name for an
// integration category (e.g. "repo" -> "github"), or "" when no team config or
// category is set.
func (r *ResolvedConfig) IntegrationProvider(category string) string {
	return r.providerForCategory(category)
}

// providerForCategory returns the team's configured provider name for an
// integration category, or "" when no team config or category is set.
func (r *ResolvedConfig) providerForCategory(category string) string {
	if r.Team == nil {
		return ""
	}
	in := r.Team.Integrations
	switch strings.ToLower(category) {
	case "comms":
		return in.Comms.Provider
	case "pm":
		return in.PM.Provider
	case "docs":
		return in.Docs.Provider
	case "repo":
		return in.Repo.Provider
	case "design":
		return in.Design.Provider
	case "deploy":
		return in.Deploy.Provider
	default:
		return ""
	}
}

// UserIdentities returns every identity the user is known by — the canonical
// handle, the display name, and every per-provider handle — de-duplicated.
// Identity-matching callers (dashboard scope, awareness) use this so a spec
// authored or assigned under any of the user's handles is recognised as theirs.
func (r *ResolvedConfig) UserIdentities() []string {
	if r.User == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		key := strings.TrimPrefix(strings.ToLower(v), "@")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	add(r.User.User.Handle)
	add(r.User.User.Name)
	for _, h := range r.User.User.Identities {
		add(h)
	}
	return out
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
	case "design":
		return r.Team.Integrations.Design.Provider != "" && r.Team.Integrations.Design.Provider != "none"
	case "deploy":
		return r.Team.Integrations.Deploy.Provider != "" && r.Team.Integrations.Deploy.Provider != "none"
	default:
		return false
	}
}

// AgentDraftsEnabled returns whether agent-assisted drafting is enabled for the
// user: an agent must be configured (personal config) and the preference must
// not be switched off.
func (r *ResolvedConfig) AgentDraftsEnabled() bool {
	if r.User != nil && !r.User.Preferences.AgentDraftsEnabled() {
		return false
	}
	return r.HasAgent()
}

// SpecsRepoRoot returns the local path to the specs repo clone root (the
// parent of the specs/ sub-directory), or "" when no specs repo is configured.
// Template files (templates/spec.md, templates/triage.md) are resolved
// relative to this root.
func (r *ResolvedConfig) SpecsRepoRoot() string {
	if r == nil || r.SpecsRepoDir == "" {
		return ""
	}
	return filepath.Dir(r.SpecsRepoDir)
}
