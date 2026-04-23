package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/tui"
	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix <title>",
	Short: "Create a fast-track bug fix spec",
	Long: `Create a fast-track spec for small bug fixes.

Fast-track specs skip ceremony stages (design, QA expectations) and go
straight to build. Use for:
- Small bug fixes
- Hotfixes
- Typo corrections
- Minor config changes

The spec is auto-assigned to you and starts at the build stage.

Example:
  spec fix "Fix login button not responding on mobile"
  spec fix "Correct typo in error message" --label hotfix`,
	Args: cobra.MinimumNArgs(1),
	RunE: runFix,
}

func init() {
	fixCmd.Flags().StringSlice("label", nil, "labels to add (e.g., bug, hotfix)")
	fixCmd.Flags().String("repo", "", "target repository")
	rootCmd.AddCommand(fixCmd)
}

func runFix(cmd *cobra.Command, args []string) error {
	title := strings.Join(args, " ")
	labels, _ := cmd.Flags().GetStringSlice("label")
	repo, _ := cmd.Flags().GetString("repo")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Check if fast-track is enabled
	if rc.Team == nil {
		return fmt.Errorf("no team config found — run 'spec config init' first")
	}

	ftConfig := rc.Team.FastTrack
	if ftConfig == nil || !ftConfig.IsEnabled() {
		return fmt.Errorf("fast-track is not enabled for this team\n\nTo enable, add to spec.config.yaml:\n\nfast_track:\n  enabled: true")
	}

	// Check if user's role is allowed
	userRole := ""
	userName := "unknown"
	if rc.User != nil {
		userRole = rc.User.User.OwnerRole
		userName = rc.User.User.Name
		if userName == "" {
			userName = rc.User.User.Handle
		}
	}

	if !ftConfig.IsRoleAllowed(userRole) {
		return fmt.Errorf("role %q cannot create fast-track specs\n\nAllowed roles: %s",
			userRole, strings.Join(ftConfig.GetAllowedRoles(), ", "))
	}

	// Check required labels
	if len(ftConfig.RequireLabels) > 0 {
		hasRequired := false
		for _, req := range ftConfig.RequireLabels {
			for _, l := range labels {
				if strings.EqualFold(l, req) {
					hasRequired = true
					break
				}
			}
			if hasRequired {
				break
			}
		}
		if !hasRequired {
			return fmt.Errorf("fast-track specs require one of these labels: %s\n\nUse: spec fix %q --label %s",
				strings.Join(ftConfig.RequireLabels, ", "), title, ftConfig.RequireLabels[0])
		}
	}

	// Ensure specs repo is available
	if _, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo); err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Compute next spec ID
	specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
	allFiles := append(specFiles, archiveFiles...)
	specID := markdown.NextSpecID(allFiles)

	// Build the spec content
	now := time.Now()
	content := buildFastTrackSpec(specID, title, userName, rc.Team.Team.CycleLabel, labels, repo, now)

	// Write and commit using WithSpecsRepo for safe concurrent access
	err = gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		specPath := filepath.Join(repoPath, specID+".md")
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing spec: %w", err)
		}
		return fmt.Sprintf("[%s] Fast-track: %s", specID, title), nil
	})
	if err != nil {
		return fmt.Errorf("committing spec: %w", err)
	}

	tui.PrintSuccess(fmt.Sprintf("Created fast-track spec: %s", specID))
	fmt.Printf("\n  Title: %s\n", title)
	fmt.Printf("  Stage: build (skipped ceremony)\n")
	fmt.Printf("  Owner: %s\n", userName)
	if len(labels) > 0 {
		fmt.Printf("  Labels: %s\n", strings.Join(labels, ", "))
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Start work: spec do %s\n", specID)
	fmt.Printf("  2. Or add steps: spec plan add %s \"Fix the issue\"\n", specID)

	if ftConfig.MaxDuration != "" {
		fmt.Printf("\n⏱ Note: Fast-track specs should complete within %s\n", ftConfig.MaxDuration)
	}

	return nil
}

func buildFastTrackSpec(id, title, author, cycle string, labels []string, repo string, now time.Time) string {
	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "id: %s\n", id)
	fmt.Fprintf(&sb, "title: %q\n", title)
	sb.WriteString("status: build\n")
	fmt.Fprintf(&sb, "author: %s\n", author)
	fmt.Fprintf(&sb, "cycle: %s\n", cycle)
	sb.WriteString("fast_track: true\n")
	if repo != "" {
		fmt.Fprintf(&sb, "repos:\n  - %s\n", repo)
	}
	fmt.Fprintf(&sb, "created: %s\n", now.Format("2006-01-02"))
	sb.WriteString(fmt.Sprintf("updated: %s\n", now.Format("2006-01-02")))
	sb.WriteString("---\n\n")

	// Title
	sb.WriteString(fmt.Sprintf("# %s: %s\n\n", id, title))

	// Labels badge
	if len(labels) > 0 {
		for _, l := range labels {
			sb.WriteString(fmt.Sprintf("`%s` ", l))
		}
		sb.WriteString("\n\n")
	}

	// Fast-track notice
	sb.WriteString("> **Fast-track spec** — Skipped ceremony stages. Fix, test, ship.\n\n")

	// Minimal sections
	sb.WriteString("## Problem\n\n")
	sb.WriteString("<!-- Brief description of the bug or issue -->\n\n")

	sb.WriteString("## Solution\n\n")
	sb.WriteString("<!-- How you'll fix it -->\n\n")

	sb.WriteString("## Testing\n\n")
	sb.WriteString("<!-- How to verify the fix -->\n\n")

	// Decision log
	sb.WriteString("---\n\n")
	sb.WriteString("## Decision Log\n\n")
	sb.WriteString("| # | Date | Decision |\n")
	sb.WriteString("|---|------|----------|\n")
	fmt.Fprintf(&sb, "| 1 | %s | Created as fast-track spec |\n", now.Format("2006-01-02"))

	return sb.String()
}
