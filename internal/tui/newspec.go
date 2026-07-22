package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// createSpec scaffolds a new SPEC-NNN.md and commits it.
func createSpec(rc *config.ResolvedConfig, title string) tea.Cmd {
	return func() tea.Msg {
		// Claim an authoritative ID before writing (SPEC-018). Offline is a hard
		// fail surfaced as an action error — the TUI must not scaffold a guessed ID.
		specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
		archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
		bootstrapMax := markdown.MaxSpecNum(append(append([]string{}, specFiles...), archiveFiles...))
		specID, claimErr := gitpkg.ClaimNextID(context.Background(), &rc.Team.SpecsRepo, gitpkg.CounterSpec, bootstrapMax)
		if claimErr != nil {
			return actionResultMsg{Action: "new", Err: claimErr}
		}

		author := gitpkg.UserName(context.Background())
		cycle := rc.CycleLabel()

		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("new", specID), func(repoPath string) (string, error) {
			sd := filepath.Join(repoPath, gitpkg.SpecsSubDir)
			_ = os.MkdirAll(sd, 0o755)

			// Resolve and render the template inside the sync wrapper so the
			// spec scaffolds from the just-pulled (latest) team template state.
			content := markdown.ScaffoldSpecFromConfig(repoPath, tuiTemplateConfig(rc),
				markdown.SpecFields{ID: specID, Title: title, Author: author, Cycle: cycle, Source: "tui", Date: time.Now().Format("2006-01-02"), Assignees: creatorAssignees(rc)})

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
