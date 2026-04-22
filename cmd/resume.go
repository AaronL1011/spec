package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	gitpkg "github.com/nexl/spec-cli/internal/git"
	"github.com/nexl/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume <id>",
	Short: "Return a blocked spec to its pre-block stage",
	Args:  cobra.ExactArgs(1),
	RunE:  runResume,
}

func init() {
	resumeCmd.Flags().String("stage", "", "stage to resume to (defaults to pre-block stage from escape hatch log)")
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])
	resumeStage, _ := cmd.Flags().GetString("stage")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		meta, err := readSpecMeta(path)
		if err != nil {
			return "", err
		}

		if meta.Status != pipeline.StatusBlocked {
			return "", fmt.Errorf("%s is not blocked (status: %s) — 'spec resume' only works on blocked specs", specID, meta.Status)
		}

		// Determine which stage to resume to
		if resumeStage == "" {
			// Try to detect from escape hatch log (last entry)
			resumeStage = detectPreBlockStage(path)
			if resumeStage == "" {
				return "", fmt.Errorf("could not detect pre-block stage — use --stage to specify")
			}
		}

		if err := pipeline.Resume(path, meta, resumeStage); err != nil {
			return "", err
		}

		fmt.Printf("✓ %s resumed to %s\n", specID, resumeStage)

		return fmt.Sprintf("fix: resume %s to %s", specID, resumeStage), nil
	})
}

// detectPreBlockStage tries to find the pre-block stage from the escape hatch log.
func detectPreBlockStage(path string) string {
	data, err := readFileContent(path)
	if err != nil {
		return ""
	}

	// Look for "Blocked from `<stage>`" pattern in escape hatch log
	lines := strings.Split(data, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if idx := strings.Index(line, "Blocked from `"); idx >= 0 {
			start := idx + len("Blocked from `")
			end := strings.Index(line[start:], "`")
			if end > 0 {
				return line[start : start+end]
			}
		}
	}
	return ""
}

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
