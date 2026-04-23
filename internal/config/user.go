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
		Handle    string `yaml:"handle"`
	} `yaml:"user"`

	Preferences PreferencesConfig `yaml:"preferences"`

	// Workspaces maps repo names to local filesystem paths.
	// Used for cross-repo navigation in multi-repo build plans.
	// Example: workspaces: { auth-service: ~/code/auth-service }
	Workspaces map[string]string `yaml:"workspaces,omitempty"`
}

// PreferencesConfig holds personal preferences.
type PreferencesConfig struct {
	Editor            string   `yaml:"editor"`
	DashboardSections []string `yaml:"dashboard_sections"`
	StandupAutoPost   bool     `yaml:"standup_auto_post"`
	AIDrafts          *bool    `yaml:"ai_drafts,omitempty"`

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

// ValidMultiplexers returns the valid multiplexer values.
func ValidMultiplexers() []string {
	return []string{MultiplexerTmux, MultiplexerZellij, MultiplexerWezterm, MultiplexerIterm2, MultiplexerNone}
}

// IsValidMultiplexer checks if a multiplexer string is valid.
func IsValidMultiplexer(m string) bool {
	if m == "" {
		return true // empty is valid (defaults to none)
	}
	for _, v := range ValidMultiplexers() {
		if v == m {
			return true
		}
	}
	return false
}

// AIDraftsEnabled returns whether AI drafts are enabled.
// Defaults to true if not explicitly set.
func (p PreferencesConfig) AIDraftsEnabled() bool {
	if p.AIDrafts == nil {
		return true
	}
	return *p.AIDrafts
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
	data = interpolateEnvVars(data)

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing user config %s: %w", path, err)
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
