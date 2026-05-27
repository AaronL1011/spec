package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/store"
)

// actionResultMsg carries the result of any TUI-initiated mutation.
type actionResultMsg struct {
	Action string // "advance", "block", "unblock", "revert", "focus", "yank"
	SpecID string
	Detail string
	Err    error
}

// advanceSpec advances a spec to the next pipeline stage.
func advanceSpec(rc *config.ResolvedConfig, specID, role string) tea.Cmd {
	return func() tea.Msg {
		pl := rc.Pipeline()
		err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			meta, pErr := markdown.ReadMeta(path)
			if pErr != nil {
				return "", pErr
			}

			if err := pipeline.ValidateAdvance(pl, meta.Status, "", role); err != nil {
				return "", err
			}

			next, err := pipeline.NextStage(pl, meta.Status, true)
			if err != nil {
				return "", fmt.Errorf("cannot advance from %q: %w", meta.Status, err)
			}

			result, err := pipeline.Advance(path, meta, next)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("chore: advance %s %s → %s", specID, result.PreviousStage, result.NewStage), nil
		})

		detail := ""
		if err == nil {
			detail = "advanced to next stage"
		}
		return actionResultMsg{Action: "advance", SpecID: specID, Detail: detail, Err: err}
	}
}

// blockSpec transitions a spec to blocked status with a reason.
func blockSpec(rc *config.ResolvedConfig, specID, reason, user string) tea.Cmd {
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			meta, pErr := markdown.ReadMeta(path)
			if pErr != nil {
				return "", pErr
			}

			if meta.Status == pipeline.StatusBlocked {
				return "", fmt.Errorf("%s is already blocked", specID)
			}

			_, err := pipeline.Eject(path, meta, reason, user)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("chore: block %s — %s", specID, reason), nil
		})

		return actionResultMsg{Action: "block", SpecID: specID, Err: err}
	}
}

// unblockSpec resumes a blocked spec to its previous stage.
func unblockSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		pl := rc.Pipeline()
		err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			meta, pErr := markdown.ReadMeta(path)
			if pErr != nil {
				return "", pErr
			}

			if meta.Status != pipeline.StatusBlocked {
				return "", fmt.Errorf("%s is not blocked (status: %s)", specID, meta.Status)
			}

			// Find the first non-blocked stage to resume to.
			prev := ""
			for _, s := range pl.Stages {
				if s.Name != pipeline.StatusBlocked {
					prev = s.Name
				}
			}
			if prev == "" {
				return "", fmt.Errorf("no stage to resume to")
			}

			if err := pipeline.Resume(path, meta, prev); err != nil {
				return "", err
			}
			return fmt.Sprintf("chore: unblock %s → %s", specID, prev), nil
		})

		return actionResultMsg{Action: "unblock", SpecID: specID, Err: err}
	}
}

// revertSpec sends a spec back to a previous stage.
func revertSpec(rc *config.ResolvedConfig, specID, targetStage, reason, user string) tea.Cmd {
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			meta, pErr := markdown.ReadMeta(path)
			if pErr != nil {
				return "", pErr
			}

			if err := pipeline.Revert(path, meta, targetStage, reason, user); err != nil {
				return "", err
			}
			return fmt.Sprintf("chore: revert %s → %s", specID, targetStage), nil
		})

		return actionResultMsg{Action: "revert", SpecID: specID, Err: err}
	}
}

// focusSpec sets the focused spec in the local store.
func focusSpec(db *store.DB, specID string) tea.Cmd {
	return func() tea.Msg {
		if db == nil {
			return actionResultMsg{Action: "focus", SpecID: specID, Err: fmt.Errorf("local store unavailable")}
		}

		if err := db.FocusedSpecSet(specID); err != nil {
			return actionResultMsg{Action: "focus", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "focus", SpecID: specID, Detail: "focused"}
	}
}

// unfocusSpec clears the focused spec.
func unfocusSpec(db *store.DB) tea.Cmd {
	return func() tea.Msg {
		if db == nil {
			return actionResultMsg{Action: "unfocus", Err: fmt.Errorf("local store unavailable")}
		}

		if err := db.FocusedSpecClear(); err != nil {
			return actionResultMsg{Action: "unfocus", Err: err}
		}
		return actionResultMsg{Action: "unfocus", Detail: "focus cleared"}
	}
}

// yankSpecID copies a spec ID to the clipboard.
func yankSpecID(specID string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(specID); err != nil {
			return actionResultMsg{Action: "yank", SpecID: specID, Err: fmt.Errorf("clipboard: %w", err)}
		}
		return actionResultMsg{Action: "yank", SpecID: specID, Detail: "copied to clipboard"}
	}
}

// yankText copies arbitrary text to the clipboard.
func yankText(text string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(text); err != nil {
			return actionResultMsg{Action: "copy", Err: fmt.Errorf("clipboard: %w", err)}
		}
		return actionResultMsg{Action: "copy", Detail: "copied to clipboard"}
	}
}

// openInBrowser opens a URL in the default browser.
func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if url == "" {
			return actionResultMsg{Action: "open", Err: fmt.Errorf("no URL available")}
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		err := cmd.Start()
		return actionResultMsg{Action: "open", Detail: url, Err: err}
	}
}

// editSpec suspends the TUI and opens the spec in $EDITOR.
func editSpec(rc *config.ResolvedConfig, specID, editor string) tea.Cmd {
	if editor == "" {
		editor = "vi"
	}
	return tea.ExecProcess(
		exec.Command(editor, resolveLocalSpecPath(rc, specID)),
		func(err error) tea.Msg {
			return actionResultMsg{Action: "edit", SpecID: specID, Err: err}
		},
	)
}

// resolveSpecIn finds a spec file in the specs repo clone.
func resolveSpecIn(repoPath string, rc *config.ResolvedConfig, specID string) (string, error) {
	specsDir := repoPath + "/" + gitpkg.SpecsSubDir
	archiveDir := config.ArchiveDir(rc.Team)

	candidates := []string{
		specsDir + "/" + specID + ".md",
		specsDir + "/triage/" + specID + ".md",
		specsDir + "/" + archiveDir + "/" + specID + ".md",
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("spec %s not found", specID)
}

// resolveLocalSpecPath returns a path for editing — prefers local .spec/ copy.
func resolveLocalSpecPath(rc *config.ResolvedConfig, specID string) string {
	local := ".spec/" + specID + ".md"
	if fileExists(local) {
		return local
	}
	if rc.SpecsRepoDir != "" {
		return rc.SpecsRepoDir + "/" + specID + ".md"
	}
	return local
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
