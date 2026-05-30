package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// createSpec scaffolds a new SPEC-NNN.md and commits it.
func createSpec(rc *config.ResolvedConfig, title string) tea.Cmd {
	return func() tea.Msg {
		specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
		archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
		allFiles := slices.Concat(specFiles, archiveFiles)
		specID := markdown.NextSpecID(allFiles)

		author := gitpkg.UserName(context.Background())
		cycle := rc.CycleLabel()
		content := markdown.ScaffoldSpec(specID, title, author, cycle, "tui")

		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("new", specID), func(repoPath string) (string, error) {
			sd := filepath.Join(repoPath, gitpkg.SpecsSubDir)
			_ = os.MkdirAll(sd, 0o755)

			specPath := filepath.Join(sd, specID+".md")
			if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("writing spec: %w", err)
			}
			return fmt.Sprintf("feat: scaffold %s — %s", specID, title), nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "new", Err: err}
		}
		return actionResultMsg{Action: "new", SpecID: specID, Detail: title + " (" + status + ")"}
	}
}
