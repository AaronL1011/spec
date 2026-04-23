// Package awareness provides passive awareness about pending items.
package awareness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
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

	userName := rc.UserName()
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

		// Check if user owns this spec (Author field)
		isOwner := strings.EqualFold(meta.Author, userName)

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
			if canReview(meta.Review.Reviewers, userName, userRole) {
				summary.ReviewsNeeded++
			}
		}
	}

	return summary, nil
}

// canReview checks if the user can review based on reviewers list.
func canReview(reviewers []string, userName, userRole string) bool {
	for _, r := range reviewers {
		// Direct name match
		if strings.EqualFold(r, userName) {
			return true
		}
		// Handle match (e.g., @mike)
		if strings.HasPrefix(r, "@") && strings.EqualFold(r[1:], userName) {
			return true
		}
		// Role match (e.g., "tl")
		if strings.EqualFold(r, userRole) {
			return true
		}
	}
	return false
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
