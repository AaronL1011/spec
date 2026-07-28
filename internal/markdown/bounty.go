package markdown

import "strings"

// BountyState records a spec bounty in frontmatter: the fact that leadership
// marked this spec as worth claiming, who claimed it, and who finished it.
//
// It is deliberately stored in the spec itself rather than a local database:
// the earned record has to survive clones and machine loss so a per-person
// tally can be derived from the specs repo (and its archive) alone.
//
// JSON tags mirror the YAML keys so the same shape is emitted by every
// machine-readable surface (`spec bounty list --json`, `spec list --json`, the
// dashboard payload) as is stored in the file.
type BountyState struct {
	// GrantedBy is the handle of the person who placed the bounty.
	GrantedBy string `yaml:"granted_by" json:"granted_by"`

	// GrantedAt is when the bounty was placed (RFC3339).
	GrantedAt string `yaml:"granted_at" json:"granted_at"`

	// Reason explains why this spec is worth claiming now. It is the
	// prioritisation message and is shown wherever the bounty is inspected.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`

	// ClaimedBy is the handle of the current claimant, stamped when the spec is
	// assigned while the bounty is unearned. Reassignment overwrites it: the
	// last claimant before completion is the one who earns it.
	ClaimedBy string `yaml:"claimed_by,omitempty" json:"claimed_by,omitempty"`

	// ClaimedAt is when the current claimant took the spec (RFC3339).
	ClaimedAt string `yaml:"claimed_at,omitempty" json:"claimed_at,omitempty"`

	// EarnedBy is frozen from ClaimedBy when the spec reaches a terminal
	// stage. Once set it never changes — the award is a historical fact.
	EarnedBy string `yaml:"earned_by,omitempty" json:"earned_by,omitempty"`

	// EarnedAt is when the bounty was earned (RFC3339).
	EarnedAt string `yaml:"earned_at,omitempty" json:"earned_at,omitempty"`
}

// HasBounty reports whether the spec carries a bounty at all (active or
// already earned). It drives the visual marker.
func (m *SpecMeta) HasBounty() bool {
	return m != nil && m.Bounty != nil && m.Bounty.GrantedAt != ""
}

// BountyActive reports whether the spec carries an unearned bounty. Active
// bounties are the ones that count against the scarcity cap.
func (m *SpecMeta) BountyActive() bool {
	return m.HasBounty() && m.Bounty.EarnedAt == ""
}

// BountyEarned reports whether the spec's bounty has been earned.
func (m *SpecMeta) BountyEarned() bool {
	return m.HasBounty() && m.Bounty.EarnedAt != ""
}

// BountyClaimed reports whether an active bounty already has a claimant.
func (m *SpecMeta) BountyClaimed() bool {
	return m.HasBounty() && m.Bounty.ClaimedBy != ""
}

// NormalizeHandle lowercases a user handle and strips a leading '@' so the same
// person written "@Ana" and "ana" groups as one identity. Exported because the
// bounty ledger groups awards by handle across many specs.
func NormalizeHandle(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "@")
}
