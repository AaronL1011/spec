package cmd

import (
	"context"
	"os"
	"strings"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/workflow"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [id]",
	Short: "Return a blocked spec to its pre-block stage",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runResume,
}

func init() {
	resumeCmd.Flags().String("stage", "", "stage to resume to (defaults to pre-block stage from escape hatch log)")
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	specID, err := resolveSpecIDArg(args, "spec resume <id>")
	if err != nil {
		return err
	}
	resumeStage, _ := cmd.Flags().GetString("stage")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	db, _ := openDB()
	if db != nil {
		defer func() { _ = db.Close() }()
	}
	deps := workflow.Deps{Config: rc, Registry: buildRegistry(rc), DB: db, Role: rc.OwnerRole("")}

	var res *workflow.ResumeResult
	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		// Detect the pre-block stage from the escape-hatch log when --stage is
		// not supplied. Detection needs the file path inside the repo clone.
		stage := resumeStage
		if stage == "" {
			stage = detectPreBlockStage(path)
		}

		var rErr error
		res, rErr = workflow.Resume(context.Background(), deps, workflow.ResumeInput{
			SpecID:      specID,
			SpecPath:    path,
			ResumeStage: stage,
		})
		if rErr != nil {
			return "", rErr
		}
		return res.CommitMsg, nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(res)
	}
	p.Line("✓ %s resumed to %s", res.SpecID, res.ResumeStage)
	return nil
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
