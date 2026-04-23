package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aaronl1011/spec-cli/internal/adapter"
	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
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
	RunE:  runNew,
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
	specsDir, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo)
	if err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Compute next ID
	specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
	allFiles := append(specFiles, archiveFiles...)
	specID := markdown.NextSpecID(allFiles)

	author := gitpkg.UserName(ctx())
	cycle := rc.CycleLabel()

	content := markdown.ScaffoldSpec(specID, title, author, cycle, "direct")

	// Write to specs repo via WithSpecsRepo
	err = gitpkg.WithSpecsRepo(ctx(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		specPath := filepath.Join(repoPath, specID+".md")
		if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing spec: %w", err)
		}

		// Ensure templates directory exists
		templatesDir := filepath.Join(repoPath, "templates")
		_ = os.MkdirAll(templatesDir, 0o755) // Best-effort directory creation

		// Ensure triage and archive dirs exist
		_ = os.MkdirAll(filepath.Join(repoPath, "triage"), 0o755)
		_ = os.MkdirAll(filepath.Join(repoPath, config.ArchiveDir(rc.Team)), 0o755)

		return fmt.Sprintf("feat: scaffold %s — %s", specID, title), nil
	})
	if err != nil {
		return err
	}

	// Create PM epic if configured
	if rc.HasIntegration("pm") {
		epicKey, pmErr := reg.PM().CreateEpic(ctx(), adapter.SpecMeta{
			ID:    specID,
			Title: title,
		})
		if pmErr != nil {
			warnf("could not create PM epic: %v", pmErr)
		} else if epicKey != "" {
			fmt.Printf("Created PM epic: %s\n", epicKey)
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
	fmt.Printf("  Location: %s/%s.md\n", specsDir, specID)
	fmt.Printf("  Status: draft\n")
	fmt.Printf("  Edit with: spec edit %s\n", specID)

	return nil
}
