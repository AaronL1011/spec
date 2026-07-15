package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold a new SPEC.md in the specs repo",
	Long: `Create a new spec document in the configured specs repository.

The command assigns the next available spec ID, applies the standard
template, commits the file to the specs repo, and triggers configured
notifications when integrations are enabled.`,
	Example: "  spec new --title \"Auth token expiration fix\"",
	RunE:    runNew,
}

func init() {
	newCmd.Flags().String("title", "", "spec title (required)")
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required — e.g., spec new --title \"Auth refactor\"")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	reg := buildRegistry(rc)

	// Ensure specs repo is cloned and up to date
	repoDir, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo)
	if err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Claim an authoritative ID before writing (SPEC-018 two-phase
	// claim→write). This hard-fails when offline rather than guessing a number.
	specID, err := claimSpecID(ctx(), rc)
	if err != nil {
		return err
	}

	author := gitpkg.UserName(ctx())
	cycle := rc.CycleLabel()

	// Write to specs repo via WithSpecsRepo
	err = gitpkg.WithSpecsRepoOpts(ctx(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		sd := specsDir(repoPath)
		_ = os.MkdirAll(sd, 0o755)

		// Resolve and render the template inside the sync wrapper so the spec
		// scaffolds from the just-pulled (latest) team template state.
		content := markdown.ScaffoldSpecFromConfig(repoPath, teamTemplateConfig(rc),
			markdown.SpecFields{ID: specID, Title: title, Author: author, Cycle: cycle, Source: "direct", Date: time.Now().Format("2006-01-02")})

		specPath := filepath.Join(sd, specID+".md")
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing spec: %w", err)
		}

		// Ensure templates directory exists
		templatesDir := filepath.Join(repoPath, "templates")
		_ = os.MkdirAll(templatesDir, 0o755) // Best-effort directory creation

		// Ensure triage and archive dirs exist
		_ = os.MkdirAll(filepath.Join(sd, "triage"), 0o755)
		_ = os.MkdirAll(filepath.Join(sd, config.ArchiveDir(rc.Team)), 0o755)

		return fmt.Sprintf("feat: scaffold %s — %s", specID, title), nil
	})
	if err != nil {
		return err
	}

	// Find-or-create the PM epic if configured (idempotent, crash-safe).
	if rc.HasIntegration("pm") {
		sm := pmSpecMeta(rc, specID, title, &markdownMeta{Status: "draft"})
		if epicKey := ensureEpic(rc, reg, specID, sm); epicKey != "" {
			fmt.Printf("Linked PM epic: %s\n", epicKey)
		}
	}

	// Notify — non-fatal, warn on failure
	if rc.HasIntegration("comms") {
		if err := reg.Comms().Notify(ctx(), adapter.Notification{
			SpecID:  specID,
			Title:   title,
			Message: fmt.Sprintf("New spec created: %s — %s (status: draft)", specID, title),
		}); err != nil {
			warnf("could not send notification: %v", err)
		}
	}

	fmt.Printf("✓ Created %s — %s\n", specID, title)
	fmt.Printf("  Location: %s/%s.md\n", filepath.Join(repoDir, gitpkg.SpecsSubDir), specID)
	fmt.Printf("  Status: draft\n")
	fmt.Printf("  Edit with: spec edit %s\n", specID)

	return nil
}
