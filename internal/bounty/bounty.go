// Package bounty holds the rules for spec bounties: who may grant one, how
// many may be active at once, and how a bounty moves from granted to claimed
// to earned.
//
// The package is pure — it mutates already-parsed frontmatter and reads
// already-resolved config, and performs no I/O. Callers own reading, writing,
// committing, and logging.
package bounty

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// Sentinel errors for the bounty lifecycle. Callers compare with errors.Is and
// present them directly: each message names the next action.
var (
	// ErrDisabled means the team has not turned bounties on.
	ErrDisabled = errors.New("bounties are not enabled — set 'bounty.enabled: true' in spec.config.yaml")

	// ErrReasonRequired means a grant was attempted without a reason.
	ErrReasonRequired = errors.New("a bounty needs a reason — pass --reason \"why this is worth claiming now\"")

	// ErrNoBounty means an operation needs a bounty that is not there.
	ErrNoBounty = errors.New("spec has no bounty")

	// ErrAlreadyEarned means the bounty is a settled historical fact.
	ErrAlreadyEarned = errors.New("bounty was already earned — earned bounties are a permanent record and cannot be changed")
)

// RoleError reports that the acting role may not grant or clear bounties.
type RoleError struct {
	Role    string
	Allowed []string
}

func (e *RoleError) Error() string {
	return fmt.Sprintf("role %q cannot grant or clear bounties — allowed roles: %s (set bounty.grantable_by in spec.config.yaml)",
		e.Role, strings.Join(e.Allowed, ", "))
}

// CapError reports that granting would exceed the configured scarcity cap. It
// carries the currently bountied spec IDs so the caller can tell the granter
// exactly what to trade off.
type CapError struct {
	Active []string
	Max    int
}

func (e *CapError) Error() string {
	return fmt.Sprintf("bounty cap reached (%d of %d active: %s) — clear one with 'spec bounty clear <id>' before granting another",
		len(e.Active), e.Max, strings.Join(e.Active, ", "))
}

// ClaimedError reports that a clear was refused because someone already took
// the spec on the strength of the bounty.
type ClaimedError struct {
	SpecID    string
	ClaimedBy string
}

func (e *ClaimedError) Error() string {
	return fmt.Sprintf("%s was already claimed by %s on the strength of this bounty — retracting an accepted invitation needs --force",
		e.SpecID, e.ClaimedBy)
}

// Authorize checks that bounties are enabled and that role may grant or clear
// them, returning the specific reason it may not.
func Authorize(role string, cfg *config.BountyConfig) error {
	if !cfg.IsEnabled() {
		return ErrDisabled
	}
	if !cfg.IsRoleAllowed(role) {
		return &RoleError{Role: role, Allowed: cfg.GrantableRoles()}
	}
	return nil
}

// CheckCap reports whether another bounty may be granted given the spec IDs
// that already carry an active one. activeIDs must exclude the spec being
// granted, so re-stating an existing bounty is never blocked by the cap.
func CheckCap(activeIDs []string, cfg *config.BountyConfig) error {
	limit := cfg.Cap()
	if len(activeIDs) < limit {
		return nil
	}
	return &CapError{Active: activeIDs, Max: limit}
}

// Grant places a bounty on a spec, or updates the reason of one already there.
// Re-granting keeps the original granter and grant time: the bounty was placed
// once, and only its stated reason is being sharpened.
func Grant(meta *markdown.SpecMeta, granter, reason string, requireReason bool, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if requireReason && reason == "" {
		return ErrReasonRequired
	}
	if meta.BountyEarned() {
		return ErrAlreadyEarned
	}
	if meta.BountyActive() {
		meta.Bounty.Reason = reason
		return nil
	}
	meta.Bounty = &markdown.BountyState{
		GrantedBy: granter,
		GrantedAt: Stamp(now),
		Reason:    reason,
	}
	return nil
}

// Clear removes an active bounty. A bounty that someone has already claimed is
// only removable with force, so leadership cannot quietly retract an
// invitation an engineer has accepted.
func Clear(specID string, meta *markdown.SpecMeta, force bool) error {
	switch {
	case !meta.HasBounty():
		return ErrNoBounty
	case meta.BountyEarned():
		return ErrAlreadyEarned
	case meta.BountyClaimed() && !force:
		return &ClaimedError{SpecID: specID, ClaimedBy: meta.Bounty.ClaimedBy}
	}
	meta.Bounty = nil
	return nil
}

// Claim records who took a bountied spec. It returns false when there is
// nothing to record: no active bounty, an empty handle, or the same claimant
// as before. Reassignment before completion overwrites the claimant, so the
// person who actually finishes the work is the one who earns it.
//
// A granter claiming their own bounty is refused (SelfClaim reports this
// separately so callers can explain it): a self-granted, self-claimed award is
// an unreviewed self-award on the ledger.
func Claim(meta *markdown.SpecMeta, who string, now time.Time) bool {
	if !meta.BountyActive() || strings.TrimSpace(who) == "" || SelfClaim(meta, who) {
		return false
	}
	if markdown.NormalizeHandle(meta.Bounty.ClaimedBy) == markdown.NormalizeHandle(who) {
		return false
	}
	meta.Bounty.ClaimedBy = who
	meta.Bounty.ClaimedAt = Stamp(now)
	return true
}

// SelfClaim reports whether who granted the active bounty they are claiming.
func SelfClaim(meta *markdown.SpecMeta, who string) bool {
	if !meta.BountyActive() {
		return false
	}
	granter := markdown.NormalizeHandle(meta.Bounty.GrantedBy)
	return granter != "" && granter == markdown.NormalizeHandle(who)
}

// Earn freezes the current claimant into the award record. It returns false
// when there is nothing to award: no bounty, no claimant, or an award already
// recorded. Callers invoke it only when the spec has reached a terminal stage.
func Earn(meta *markdown.SpecMeta, now time.Time) bool {
	if !meta.BountyActive() || meta.Bounty.ClaimedBy == "" {
		return false
	}
	meta.Bounty.EarnedBy = meta.Bounty.ClaimedBy
	meta.Bounty.EarnedAt = Stamp(now)
	return true
}

// Stamp formats a bounty timestamp. RFC3339 in UTC so grant, claim, and earn
// times are comparable across contributors' machines.
func Stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// ParseStamp parses a bounty timestamp, reporting whether it was usable. A
// malformed stamp is treated as absent rather than fatal: a hand-edited spec
// must never break a read path.
func ParseStamp(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Award is one person's earned-bounty tally over a window, with the specs that
// produced it so a leaderboard can always be traced back to real work.
type Award struct {
	// Handle is the earner as first spelled in the specs (display form).
	Handle string `json:"handle"`

	// Count is how many bounties they earned in the window.
	Count int `json:"count"`

	// SpecIDs are the contributing specs, in ascending ID order.
	SpecIDs []string `json:"spec_ids"`
}

// Tally aggregates earned bounties per person from already-parsed spec
// frontmatter, restricted to awards earned within [from, to]. A zero from or to
// means unbounded on that side.
//
// Grouping is case- and '@'-insensitive so "@Ana" and "ana" are one person; the
// first spelling encountered is kept for display. Results are ordered by count
// descending then handle ascending, so a leaderboard is stable across runs.
//
// Specs with an unparseable earned_at are skipped rather than counted at the
// zero time: a hand-edited stamp should not silently distort the ledger.
func Tally(metas []markdown.SpecMeta, from, to time.Time) []Award {
	byHandle := make(map[string]*Award)
	for _, meta := range metas {
		if !meta.BountyEarned() || meta.Bounty.EarnedBy == "" {
			continue
		}
		earned, ok := ParseStamp(meta.Bounty.EarnedAt)
		if !ok || !withinWindow(earned, from, to) {
			continue
		}
		key := markdown.NormalizeHandle(meta.Bounty.EarnedBy)
		award, seen := byHandle[key]
		if !seen {
			award = &Award{Handle: meta.Bounty.EarnedBy}
			byHandle[key] = award
		}
		award.Count++
		award.SpecIDs = append(award.SpecIDs, meta.ID)
	}

	awards := make([]Award, 0, len(byHandle))
	for _, a := range byHandle {
		sort.Strings(a.SpecIDs)
		awards = append(awards, *a)
	}
	sort.Slice(awards, func(i, j int) bool {
		if awards[i].Count != awards[j].Count {
			return awards[i].Count > awards[j].Count
		}
		return markdown.NormalizeHandle(awards[i].Handle) < markdown.NormalizeHandle(awards[j].Handle)
	})
	return awards
}

// withinWindow reports whether t falls inside [from, to], treating a zero bound
// as unbounded.
func withinWindow(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	return to.IsZero() || !t.After(to)
}
