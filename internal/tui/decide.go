package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// recordDecision appends a question to the spec's decision log.
func recordDecision(rc *config.ResolvedConfig, specID, question string) tea.Cmd {
	return func() tea.Msg {
		user := rc.UserName()

		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("decide", specID), func(repoPath string) (string, error) {
			path, err := resolveSpecIn(repoPath, rc, specID)
			if err != nil {
				return "", err
			}
			num, err := markdown.AppendDecision(path, question, user)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("chore: decision #%d on %s", num, specID), nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "decide", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "decide", SpecID: specID, Detail: "question recorded (" + status + ")"}
	}
}
