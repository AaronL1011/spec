// Package awareness provides passive awareness about pending items.
package awareness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// Summary holds counts of items needing attention.
type Summary struct {
	ReviewsNeeded   int // Plan reviews awaiting your approval
	SpecsBlocked    int // Specs you own that are blocked
	SpecsInProgress int // Specs you own in build/engineering
	SpecsTotal      int // Total specs you own
}

// HasItems returns true if there are any items needing attention.
func (s Summary) HasItems() bool {
	return s.ReviewsNeeded > 0 || s.SpecsBlocked > 0
}

// OneLiner returns a brief summary string, or empty if nothing to report.
func (s Summary) OneLiner() string {
	if !s.HasItems() {
		return ""
	}

	var parts []string
	if s.ReviewsNeeded > 0 {
		parts = append(parts, fmt.Sprintf("%d review%s pending", s.ReviewsNeeded, plural(s.ReviewsNeeded)))
	}
	if s.SpecsBlocked > 0 {
		parts = append(parts, fmt.Sprintf("%d spec%s blocked", s.SpecsBlocked, plural(s.SpecsBlocked)))
	}

	return fmt.Sprintf("📥 %s (spec list --mine)", strings.Join(parts, ", "))
}

// Gather collects awareness info for the current user.
func Gather(rc *config.ResolvedConfig) (*Summary, error) {
	if rc.Team == nil {
		return &Summary{}, nil
	}

	identities := rc.UserIdentities()
	userRole := ""
	if rc.User != nil {
		userRole = rc.User.User.OwnerRole
	}

	specFiles, err := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	if err != nil {
		return nil, err
	}

	summary := &Summary{}

	for _, f := range specFiles {
		path := filepath.Join(rc.SpecsRepoDir, f)
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}

		// Check if user owns this spec (Author field). Match against every
		// identity the user is known by so display-name vs handle drift across
		// teams does not hide their own work.
		isOwner := matchesAnyIdentity(meta.Author, identities)

		if isOwner {
			summary.SpecsTotal++

			// Check for blocked steps
			for _, step := range meta.Steps {
				if step.Status == "blocked" {
					summary.SpecsBlocked++
					break
				}
			}

			// Check if in progress
			if meta.Status == "build" || meta.Status == "engineering" {
				summary.SpecsInProgress++
			}
		}

		// Check for pending plan reviews (if user is a reviewer)
		if meta.Review != nil && meta.Review.Status == "pending" {
			if canReview(meta.Review.Reviewers, identities, userRole) {
				summary.ReviewsNeeded++
			}
		}
	}

	return summary, nil
}

// canReview checks if the user can review based on the reviewers list. A
// reviewer entry matches any of the user's identities (name, canonical handle,
// or a per-provider handle) or their role.
func canReview(reviewers, identities []string, userRole string) bool {
	for _, r := range reviewers {
		if matchesAnyIdentity(r, identities) {
			return true
		}
		// Role match (e.g., "tl")
		if strings.EqualFold(r, userRole) {
			return true
		}
	}
	return false
}

// matchesAnyIdentity reports whether candidate names any of the user's
// identities. Matching is case-insensitive and tolerates a leading '@'.
func matchesAnyIdentity(candidate string, identities []string) bool {
	c := normaliseIdentity(candidate)
	if c == "" {
		return false
	}
	for _, id := range identities {
		if c == normaliseIdentity(id) {
			return true
		}
	}
	return false
}

func normaliseIdentity(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "@")
}

// Print outputs the awareness line to stdout if there are items.
func Print(rc *config.ResolvedConfig) {
	// Note: Caller is responsible for checking build context if needed.
	// The ShowPassiveAwarenessDuringBuild preference is handled by callers.

	summary, err := Gather(rc)
	if err != nil {
		return // Silently fail - don't interrupt user's command
	}

	line := summary.OneLiner()
	if line != "" {
		fmt.Println(line)
		fmt.Println()
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
