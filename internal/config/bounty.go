package config

import "strings"

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
	// false for a static (but still shaded) marker.
	Shimmer *bool `yaml:"shimmer,omitempty"`

	// Finish selects the marker's metal: "gold" (default), "platinum", or
	// "prismatic". It changes only the three tones the marker is shaded with.
	Finish string `yaml:"finish,omitempty"`
}

// BountyFinish names the metal the bounty marker is shaded with. The value is
// resolved and validated in config so the TUI only ever maps a known finish to
// a palette.
type BountyFinish string

// Supported bounty finishes.
const (
	// BountyFinishGold is warm and legible on every palette — the default.
	BountyFinishGold BountyFinish = "gold"

	// BountyFinishPlatinum is a cool, understated white metal. Deliberately
	// quiet; on most themes it reads close to bright primary text.
	BountyFinishPlatinum BountyFinish = "platinum"

	// BountyFinishPrismatic is a near-white body whose travelling highlight
	// rotates through hue, mimicking the dispersion ("fire") of a cut stone.
	BountyFinishPrismatic BountyFinish = "prismatic"
)

// BountyFinishNames lists the accepted finishes, for lint suggestions and docs.
func BountyFinishNames() []string {
	return []string{string(BountyFinishGold), string(BountyFinishPlatinum), string(BountyFinishPrismatic)}
}

// ParseBountyFinish resolves a configured finish name, reporting whether it is
// recognised. An empty value resolves to the default rather than an error, so an
// omitted key is not a failure.
func ParseBountyFinish(name string) (BountyFinish, bool) {
	switch BountyFinish(strings.ToLower(strings.TrimSpace(name))) {
	case "":
		return BountyFinishGold, true
	case BountyFinishGold:
		return BountyFinishGold, true
	case BountyFinishPlatinum:
		return BountyFinishPlatinum, true
	case BountyFinishPrismatic:
		return BountyFinishPrismatic, true
	default:
		return BountyFinishGold, false
	}
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

// MetalFinish returns the configured finish, falling back to gold for an unset
// or unrecognised value (lint reports the latter; rendering must never fail).
func (b *BountyConfig) MetalFinish() BountyFinish {
	if b == nil {
		return BountyFinishGold
	}
	finish, _ := ParseBountyFinish(b.Finish)
	return finish
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
