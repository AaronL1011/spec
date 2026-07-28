package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UserConfig represents the ~/.spec/config.yaml personal config file.
type UserConfig struct {
	User struct {
		OwnerRole string `yaml:"owner_role"`
		Name      string `yaml:"name"`
		// Handle is the spec-canonical identity token — a stable, user-chosen
		// name that identifies the person inside spec (frontmatter author and
		// assignees, thread author, decision log). It never leaves spec, so it
		// can be anything the user likes. It is also the universal fallback for
		// any per-integration identity that is not explicitly mapped.
		Handle string `yaml:"handle"`
		// Identities maps an integration provider name (e.g. "github", "slack",
		// "teams", "jira") to the user's handle on that service. A person's
		// handle differs on every service, so adapter calls must receive the
		// service-specific value, not the canonical handle. Optional and
		// additive: an absent or partial map falls back to Handle, so existing
		// configs behave identically.
		Identities map[string]string `yaml:"identities,omitempty"`
	} `yaml:"user"`

	Preferences PreferencesConfig `yaml:"preferences"`

	// Agent configures the coding harness and its completion plane. A harness
	// and its auth are personal tools — teammates on one spec legitimately run
	// different agents — so this is the ONLY place an agent is configured;
	// integrations.agent was removed from team config.
	Agent *ProviderConfig `yaml:"agent,omitempty"`

	// Workspaces maps repo names to local filesystem paths.
	// Used for cross-repo navigation in multi-repo build plans.
	// Example: workspaces: { auth-service: ~/code/auth-service }
	Workspaces map[string]string `yaml:"workspaces,omitempty"`
}

// AgentAuthoringConfig tiers agent authority over spec content. The authoring
// tier (section reads and writes) is always available in an agent session and
// needs no preference: it is what makes the port useful, and every write is
// recoverable from the specs-repo diff. Stage transitions are separate because
// they are not symmetric with writes — they fire the pipeline's configured
// effects (Jira status sync, Slack posts) and move work through a human review
// pipeline.
type AgentAuthoringConfig struct {
	// Transitions allows spec_advance / spec_revert in agent sessions. When
	// false (default) those tools are absent from tools/list entirely, which is
	// self-documenting: a missing tool beats one that fails at call time.
	Transitions bool `yaml:"transitions,omitempty"`
}

// PreferencesConfig holds personal preferences.
type PreferencesConfig struct {
	Editor            string   `yaml:"editor"`
	DashboardSections []string `yaml:"dashboard_sections"`
	StandupAutoPost   bool     `yaml:"standup_auto_post"`
	// AgentDrafts gates agent-assisted drafting. Renamed from ai_drafts, which
	// gated on an integration this release deletes; lint reports the old
	// spelling with its replacement.
	AgentDrafts *bool `yaml:"agent_drafts,omitempty"`

	// AgentAuthoring tiers what an agent session may do through the MCP
	// authoring port. Reads and section writes are always available; stage
	// transitions are opt-in because they fire team-visible pipeline effects.
	AgentAuthoring AgentAuthoringConfig `yaml:"agent_authoring,omitempty"`

	// Theme sets the TUI colour theme.
	// Valid values: auto (default), catppuccin-mocha, catppuccin-latte,
	// catppuccin-macchiato, catppuccin-frappe, gruvbox-dark, dracula,
	// tokyo-night, nord, solarized-dark, solarized-light, rose-pine,
	// kanagawa, everforest-dark, everforest-light, github-dark, github-light,
	// ayu-mirage, ayu-light, modus-vivendi, modus-operandi, graphite.
	Theme string `yaml:"theme,omitempty"`

	// RefreshInterval sets the TUI auto-refresh period (e.g. "30s", "1m").
	// Defaults to 30s.
	RefreshInterval string `yaml:"refresh_interval,omitempty"`

	// Mouse enables mouse support in the TUI (click tabs, click items).
	// Defaults to false.
	Mouse bool `yaml:"mouse,omitempty"`

	// Multiplexer specifies the terminal multiplexer for cross-repo navigation.
	// Valid values: tmux, zellij, wezterm, iterm2, none
	// If empty or "none", falls back to manual navigation prompts.
	Multiplexer string `yaml:"multiplexer,omitempty"`

	// AutoPull automatically pulls stale specs when running `spec do`.
	// If false, prompts the user before pulling.
	AutoPull bool `yaml:"auto_pull,omitempty"`

	// AutoNavigate opens a new terminal pane when switching repos.
	// Defaults to true. Set to false for manual navigation.
	AutoNavigate *bool `yaml:"auto_navigate,omitempty"`

	// PassiveAwareness configures the passive awareness line shown on commands.
	PassiveAwareness *PassiveAwarenessConfig `yaml:"passive_awareness,omitempty"`
}

// PassiveAwarenessConfig controls what pending items are shown in the
// awareness line on every spec command.
type PassiveAwarenessConfig struct {
	// Show whitelists item types to display. If empty, shows all.
	// Valid types: review_requests, spec_owned, mentions, triage, fyi, blocked
	Show []string `yaml:"show,omitempty"`

	// Hide blacklists item types to suppress.
	Hide []string `yaml:"hide,omitempty"`

	// DuringBuild shows awareness during `spec do` and `spec build`.
	// Defaults to false to avoid interrupting flow state.
	DuringBuild bool `yaml:"during_build,omitempty"`

	// DismissDuration is how long dismissed items stay hidden.
	// Defaults to "2h". Valid formats: "30m", "2h", "1d".
	DismissDuration string `yaml:"dismiss_duration,omitempty"`
}

// Multiplexer constants.
const (
	MultiplexerTmux    = "tmux"
	MultiplexerZellij  = "zellij"
	MultiplexerWezterm = "wezterm"
	MultiplexerIterm2  = "iterm2"
	MultiplexerNone    = "none"
)

// AgentDraftsEnabled returns whether agent-assisted drafting is enabled.
// Defaults to true if not explicitly set: an agent that is configured should
// work without a second opt-in, and skipping agent setup leaves the provider
// unset so nothing is offered anyway.
func (p PreferencesConfig) AgentDraftsEnabled() bool {
	if p.AgentDrafts == nil {
		return true
	}
	return *p.AgentDrafts
}

// AutoNavigateEnabled returns whether auto-navigation to new repos is enabled.
// Defaults to true if not explicitly set.
func (p PreferencesConfig) AutoNavigateEnabled() bool {
	if p.AutoNavigate == nil {
		return true
	}
	return *p.AutoNavigate
}

// GetDismissDuration returns the dismiss duration or the default "2h".
func (p PreferencesConfig) GetDismissDuration() string {
	if p.PassiveAwareness == nil || p.PassiveAwareness.DismissDuration == "" {
		return "2h"
	}
	return p.PassiveAwareness.DismissDuration
}

// ShowPassiveAwarenessDuringBuild returns whether to show awareness during builds.
func (p PreferencesConfig) ShowPassiveAwarenessDuringBuild() bool {
	if p.PassiveAwareness == nil {
		return false
	}
	return p.PassiveAwareness.DuringBuild
}

// GetWorkspacePath returns the local path for a repo, or empty string if not configured.
func (c *UserConfig) GetWorkspacePath(repoName string) string {
	if c.Workspaces == nil {
		return ""
	}
	return c.Workspaces[repoName]
}

// UserConfigDir returns the path to the ~/.spec/ directory.
func UserConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".spec")
	}
	return filepath.Join(home, ".spec")
}

// UserConfigPath returns the path to ~/.spec/config.yaml.
func UserConfigPath() string {
	return filepath.Join(UserConfigDir(), "config.yaml")
}

// LoadUserConfig reads and parses the user config file.
func LoadUserConfig(path string) (*UserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading user config %s: %w", path, err)
	}
	// Capture the token's literal spelling before interpolation so a later
	// marshal (e.g. a TUI settings save) re-emits ${VAR} rather than the
	// resolved secret.
	tokenSource := agentTokenSource(data)

	data = interpolateEnvVars(data)

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing user config %s: %w", path, err)
	}
	if cfg.Agent != nil && tokenSource != "" {
		cfg.Agent.SetTokenSource(tokenSource)
	}

	// Defaults
	if cfg.Preferences.Editor == "" {
		if editor := os.Getenv("EDITOR"); editor != "" {
			cfg.Preferences.Editor = editor
		} else {
			cfg.Preferences.Editor = "vi"
		}
	}
	if len(cfg.Preferences.DashboardSections) == 0 {
		cfg.Preferences.DashboardSections = []string{"do", "review", "incoming", "blocked"}
	}
	return &cfg, nil
}

// LoadUserConfigWithDefaults loads user config or returns defaults if file doesn't exist.
func LoadUserConfigWithDefaults() (*UserConfig, string) {
	path := UserConfigPath()
	cfg, err := LoadUserConfig(path)
	if err != nil {
		// Return default config
		cfg = &UserConfig{}
		cfg.Preferences.Editor = os.Getenv("EDITOR")
		if cfg.Preferences.Editor == "" {
			cfg.Preferences.Editor = "vi"
		}
		cfg.Preferences.DashboardSections = []string{"do", "review", "incoming", "blocked"}
		return cfg, path
	}
	return cfg, path
}

// WriteUserConfig writes a user config to disk.
func WriteUserConfig(path string, cfg *UserConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling user config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing user config %s: %w", path, err)
	}
	return nil
}

// ValidRoles returns the valid owner roles.
func ValidRoles() []string {
	return []string{"pm", "tl", "designer", "qa", "engineer"}
}

// IsValidRole checks if a role string is valid.
func IsValidRole(role string) bool {
	for _, r := range ValidRoles() {
		if r == role {
			return true
		}
	}
	return false
}
