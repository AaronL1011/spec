package git

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var specBranchPattern = regexp.MustCompile(`^spec-(\d+)/step-(\d+)-(.+)$`)

// BranchInfo represents parsed branch name information.
type BranchInfo struct {
	SpecNumber string // e.g., "042"
	StepNumber string // e.g., "1"
	Slug       string // e.g., "token-bucket"
}

// ParseSpecBranch extracts spec and step info from a branch name.
// Returns nil if the branch doesn't match the spec branch pattern.
func ParseSpecBranch(branch string) *BranchInfo {
	matches := specBranchPattern.FindStringSubmatch(branch)
	if matches == nil {
		return nil
	}
	return &BranchInfo{
		SpecNumber: matches[1],
		StepNumber: matches[2],
		Slug:       matches[3],
	}
}

// SpecBranchName generates a branch name for a build step.
func SpecBranchName(specID string, stepNumber int, slug string) string {
	// Extract number from SPEC-042 → 042
	num := strings.TrimPrefix(strings.ToUpper(specID), "SPEC-")
	// Slugify: lowercase, replace spaces with hyphens
	slug = strings.ToLower(slug)
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove non-alphanumeric chars except hyphens
	var clean []byte
	for _, c := range []byte(slug) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			clean = append(clean, c)
		}
	}
	slug = string(clean)
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return fmt.Sprintf("spec-%s/step-%d-%s", strings.ToLower(num), stepNumber, slug)
}

// CreateBranch creates and checks out a new branch.
func CreateBranch(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "checkout", "-b", branch)
	return err
}

// CheckoutBranch checks out an existing branch.
func CheckoutBranch(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "checkout", branch)
	return err
}

// BranchExists checks if a branch exists locally.
func BranchExists(ctx context.Context, dir, branch string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--verify", branch)
	return err == nil
}

// DetectSpecFromBranch attempts to detect the spec ID from the current branch.
func DetectSpecFromBranch(ctx context.Context, dir string) string {
	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		return ""
	}
	info := ParseSpecBranch(branch)
	if info == nil {
		return ""
	}
	return fmt.Sprintf("SPEC-%s", info.SpecNumber)
}

// Diff returns the diff for the current branch compared to a base ref.
func Diff(ctx context.Context, dir, baseRef string) (string, error) {
	return Run(ctx, dir, "diff", baseRef)
}
