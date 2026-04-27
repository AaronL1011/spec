package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [id]",
	Short: "Fetch spec from specs repo to .spec/<id>.md in the current service repo",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPull,
}

func init() {
	pullCmd.Flags().Bool("force", false, "overwrite local copy without prompting")
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec pull <id>")
	if err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	// Ensure specs repo is up to date
	_, err = gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo)
	if err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	// Find the spec in specs repo
	srcPath, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	// Read the spec content
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	// Create .spec/ directory
	destDir := ".spec"
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating .spec directory: %w", err)
	}

	destPath := filepath.Join(destDir, specID+".md")

	// Check if local copy exists and has changes
	if !force {
		if _, err := os.Stat(destPath); err == nil {
			// File exists — check if it differs
			existing, _ := os.ReadFile(destPath)
			if string(existing) != string(content) {
				fmt.Printf("Local copy of %s exists and differs from specs repo.\n", specID)
				fmt.Print("Overwrite? [y/N] ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(answer)) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}
		}
	}

	if err := os.WriteFile(destPath, content, 0o644); err != nil {
		return fmt.Errorf("writing local copy: %w", err)
	}

	fmt.Printf("✓ Pulled %s → %s\n", specID, destPath)
	return nil
}
