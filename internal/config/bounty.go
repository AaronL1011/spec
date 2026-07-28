package config

// Default bounty settings. A bounty is deliberately scarce: an uncapped
// marker debases to a second priority field, so the cap is a first-class
// default rather than an opt-in.
const (
	// DefaultBountyMaxActive is the number of concurrently bountied specs
	// allowed when max_active is unset.
	DefaultBountyMaxActive = 3
)

// defaultBountyRoles are the roles allowed to grant bounties when
// grantable_by is unset. Prioritisation is a TL+PM collaboration, so both are
// permitted by default; a team can narrow the list.
var defaultBountyRoles = []string{"tl", "pm"}

// BountyConfig configures spec bounties: a scarce, role-gated marker that
// invites an engineer to claim a spec, and a durable record of who finished
// one. Absent or disabled means no bounty UI and no bounty commands.
type BountyConfig struct {
	// Enabled turns bounties on. Defaults to false so an unconfigured team
	// sees no bounty surface at all.
	Enabled bool `yaml:"enabled,omitempty"`

	// GrantableBy lists the roles allowed to grant and clear bounties.
	// Defaults to ["tl", "pm"].
	GrantableBy []string `yaml:"grantable_by,omitempty"`

	// MaxActive caps how many specs may carry an unearned bounty at once.
	// Defaults to DefaultBountyMaxActive. Scarcity is the whole point: past
	// the cap, granting fails until the granter clears one.
	MaxActive int `yaml:"max_active,omitempty"`

	// RequireReason forces a written reason on every grant. Defaults to true —
	// the reason is the prioritisation message, not decoration. A pointer so
	// an explicit `false` is distinguishable from an unset field.
	RequireReason *bool `yaml:"require_reason,omitempty"`

	// Shimmer animates the bounty marker in the TUI. Defaults to true; set
	// false for a static gold marker.
	Shimmer *bool `yaml:"shimmer,omitempty"`
}

// IsEnabled reports whether bounties are configured and turned on.
func (b *BountyConfig) IsEnabled() bool {
	return b != nil && b.Enabled
}

// GrantableRoles returns the roles allowed to grant, or the default list.
func (b *BountyConfig) GrantableRoles() []string {
	if b == nil || len(b.GrantableBy) == 0 {
		return defaultBountyRoles
	}
	return b.GrantableBy
}

// IsRoleAllowed reports whether role may grant or clear a bounty.
func (b *BountyConfig) IsRoleAllowed(role string) bool {
	return contains(b.GrantableRoles(), role)
}

// Cap returns the maximum number of concurrently active bounties.
func (b *BountyConfig) Cap() int {
	if b == nil || b.MaxActive <= 0 {
		return DefaultBountyMaxActive
	}
	return b.MaxActive
}

// ReasonRequired reports whether a grant must carry a reason (default true).
func (b *BountyConfig) ReasonRequired() bool {
	if b == nil || b.RequireReason == nil {
		return true
	}
	return *b.RequireReason
}

// ShimmerEnabled reports whether the TUI marker animates (default true).
func (b *BountyConfig) ShimmerEnabled() bool {
	if b == nil || b.Shimmer == nil {
		return true
	}
	return *b.Shimmer
}

// BountyEnabled reports whether the resolved team config has bounties on. It
// is nil-safe so read paths (dashboard, TUI, list) can call it unguarded.
func (r *ResolvedConfig) BountyEnabled() bool {
	if r == nil || r.Team == nil {
		return false
	}
	return r.Team.Bounty.IsEnabled()
}

// Bounties returns the effective bounty config, never nil, so callers can read
// defaults without a presence check.
func (r *ResolvedConfig) Bounties() *BountyConfig {
	if r == nil || r.Team == nil || r.Team.Bounty == nil {
		return &BountyConfig{}
	}
	return r.Team.Bounty
}
