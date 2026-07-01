// Package identity resolves whether a stored handle or display name refers
// to a given person. Teams configure identity per integration (a Slack
// handle, a GitHub login, a display name in the spec's frontmatter), and
// those rarely agree verbatim — so every caller that needs to ask "is this
// me?" needs the same tolerant, case-insensitive, @-agnostic comparison.
//
// This package has no dependency on any other internal package: it operates
// purely on the strings callers already have. Adapters, the dashboard, and
// the discussion engine all import it directly rather than each growing its
// own ad hoc comparison, which is how a valid approval or a valid mention
// ends up silently unrecognised.
package identity

import "strings"

// Viewer identifies a person by every name they might be recorded under:
// display name, canonical handle, and any per-provider login. Matching checks
// all of them so a spec recorded by GitHub login still resolves to "you".
type Viewer struct {
	Role   string
	Name   string
	Handle string
	// Identities is the full set of handles the viewer is known by (canonical
	// handle, name, and per-provider handles). Matching checks all of them.
	Identities []string
}

// MatchesIdentity reports whether candidate names the viewer, by display
// name, canonical handle, or any per-provider handle. Matching is
// case-insensitive and tolerates a leading '@'.
func MatchesIdentity(candidate string, v Viewer) bool {
	c := normalise(candidate)
	if c == "" {
		return false
	}
	if c == normalise(v.Name) || c == normalise(v.Handle) {
		return true
	}
	for _, id := range v.Identities {
		if c == normalise(id) {
			return true
		}
	}
	return false
}

// AnyIdentity reports whether the viewer matches any of the candidate identities.
func AnyIdentity(candidates []string, v Viewer) bool {
	for _, c := range candidates {
		if MatchesIdentity(c, v) {
			return true
		}
	}
	return false
}

func normalise(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "@")
}
