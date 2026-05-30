package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/store"
	syncengine "github.com/aaronl1011/spec/internal/sync"
	"github.com/aaronl1011/spec/internal/syncaudit"
)

// tuiRecorder backs TUI auto-push audit, queue, and freshness. Set once at app
// construction. A nil DB leaves git's no-op recorder in place.
var tuiRecorder gitpkg.Recorder = nil

func setTUIRecorder(db *store.DB) {
	rec := syncaudit.New(db)
	if rec == nil {
		gitpkg.SetRecorder(nil)
		tuiRecorder = nil
		return
	}
	gitpkg.SetRecorder(rec)
	gitpkg.SetReadSurface(store.SurfaceTUI)
	tuiRecorder = rec
}

// tuiSyncOpts attributes a committing TUI action to the tui surface so the
// audit log records which surface triggered each sync (SPEC-013 §Decision 007).
func tuiSyncOpts(trigger, specID string) gitpkg.SyncOptions {
	return gitpkg.SyncOptions{
		Surface:  store.SurfaceTUI,
		Trigger:  trigger,
		SpecID:   specID,
		Recorder: tuiRecorder,
	}
}

// pushOutcome maps a committing-action error to an inline status string and a
// flag for whether the action otherwise succeeded. A queued/offline push is
// NOT an error in the new model — the commit is durable and the work survives.
func pushOutcome(err error) (status string, fatal bool) {
	switch {
	case err == nil:
		return "pushed", false
	case gitpkg.IsSectionConflict(err):
		return "conflict", true
	default:
		return "error", true
	}
}

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
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("advance", specID), func(repoPath string) (string, error) {
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

		status, fatal := pushOutcome(err)
		detail := ""
		if !fatal {
			detail = "advanced to next stage (" + status + ")"
			err = nil
		}
		return actionResultMsg{Action: "advance", SpecID: specID, Detail: detail, Err: err}
	}
}

// blockSpec transitions a spec to blocked status with a reason.
func blockSpec(rc *config.ResolvedConfig, specID, reason, user string) tea.Cmd {
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("block", specID), func(repoPath string) (string, error) {
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

		status, fatal := pushOutcome(err)
		detail := ""
		if !fatal {
			detail = "blocked (" + status + ")"
			err = nil
		}
		return actionResultMsg{Action: "block", SpecID: specID, Detail: detail, Err: err}
	}
}

// unblockSpec resumes a blocked spec to its previous stage.
func unblockSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		pl := rc.Pipeline()
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("unblock", specID), func(repoPath string) (string, error) {
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

		status, fatal := pushOutcome(err)
		detail := ""
		if !fatal {
			detail = "unblocked (" + status + ")"
			err = nil
		}
		return actionResultMsg{Action: "unblock", SpecID: specID, Detail: detail, Err: err}
	}
}

// revertSpec sends a spec back to a previous stage.
func revertSpec(rc *config.ResolvedConfig, specID, targetStage, reason, user string) tea.Cmd {
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("revert", specID), func(repoPath string) (string, error) {
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

		status, fatal := pushOutcome(err)
		detail := ""
		if !fatal {
			detail = "reverted (" + status + ")"
			err = nil
		}
		return actionResultMsg{Action: "revert", SpecID: specID, Detail: detail, Err: err}
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

// pushSpec commits and pushes local spec edits to the specs repo.
func pushSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		pushed, err := gitpkg.PushLocalEditsOpts(
			context.Background(),
			&rc.Team.SpecsRepo,
			fmt.Sprintf("feat: update %s", specID),
			tuiSyncOpts("push", specID),
		)
		if err != nil {
			// A genuine same-section conflict is fatal; transient/offline is
			// queued (non-fatal) and handled inside PushLocalEditsOpts.
			return actionResultMsg{Action: "push", SpecID: specID, Err: fmt.Errorf("pushing %s: %w", specID, err)}
		}
		if !pushed {
			return actionResultMsg{Action: "push", SpecID: specID, Detail: "no local changes"}
		}
		return actionResultMsg{Action: "push", SpecID: specID, Detail: "pushed"}
	}
}

// syncSpec runs a bidirectional sync between the spec and external docs.
func syncSpec(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID, role string) tea.Cmd {
	return func() tea.Msg {
		if !rc.HasIntegration("docs") {
			return actionResultMsg{Action: "sync", SpecID: specID, Err: fmt.Errorf("docs integration not configured — add 'integrations.docs' to spec.config.yaml")}
		}

		engine := syncengine.NewEngine(reg.Docs(), db)
		var prepared *syncengine.PreparedRun

		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("sync", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}

			strategy := rc.Team.Sync.ConflictStrategy
			var sErr error
			prepared, sErr = engine.Prepare(context.Background(), syncengine.Options{
				SpecID:           specID,
				SpecPath:         path,
				Direction:        syncengine.DirectionBoth,
				ConflictStrategy: strategy,
				OwnerRole:        role,
				UserName:         rc.UserName(),
			})
			if sErr != nil {
				return "", sErr
			}
			if prepared != nil && prepared.Report != nil && len(prepared.Report.InboundApplied) > 0 {
				return fmt.Sprintf("chore: sync %s from docs", specID), nil
			}
			return "", nil
		})
		if err != nil {
			return actionResultMsg{Action: "sync", SpecID: specID, Err: err}
		}

		if prepared != nil {
			if fErr := engine.Finalize(context.Background(), prepared); fErr != nil {
				return actionResultMsg{Action: "sync", SpecID: specID, Err: fErr}
			}
		}

		detail := formatSyncDetail(prepared)
		return actionResultMsg{Action: "sync", SpecID: specID, Detail: detail}
	}
}

// formatSyncDetail builds a short summary from the sync report.
func formatSyncDetail(prepared *syncengine.PreparedRun) string {
	if prepared == nil || prepared.Report == nil {
		return "no changes"
	}
	r := prepared.Report
	var parts []string
	if r.OutboundPushed {
		parts = append(parts, fmt.Sprintf("%d out", len(r.OutboundSections)))
	}
	if len(r.InboundApplied) > 0 {
		parts = append(parts, fmt.Sprintf("%d in", len(r.InboundApplied)))
	}
	if len(r.Conflicts) > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", len(r.Conflicts)))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

func archiveSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		archDir := config.ArchiveDir(rc.Team)
		err := gitpkg.ArchiveSpec(context.Background(), &rc.Team.SpecsRepo, specID, archDir)
		if err != nil {
			return actionResultMsg{Action: "archive", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "archive", SpecID: specID, Detail: "archived"}
	}
}

func restoreSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		archDir := config.ArchiveDir(rc.Team)
		err := gitpkg.RestoreSpec(context.Background(), &rc.Team.SpecsRepo, specID, archDir)
		if err != nil {
			return actionResultMsg{Action: "restore", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "restore", SpecID: specID, Detail: "restored"}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
