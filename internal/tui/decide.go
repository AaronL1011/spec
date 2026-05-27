package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// recordDecision appends a question to the spec's decision log.
func recordDecision(rc *config.ResolvedConfig, specID, question string) tea.Cmd {
	return func() tea.Msg {
		user := rc.UserName()

		err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
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

		if err != nil {
			return actionResultMsg{Action: "decide", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "decide", SpecID: specID, Detail: "question recorded"}
	}
}
