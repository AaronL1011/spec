package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
	syncengine "github.com/aaronl1011/spec/internal/sync"
	"github.com/aaronl1011/spec/internal/syncaudit"
	"github.com/aaronl1011/spec/internal/workflow"
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

// newTUIPublisher builds the background auto-push publisher for the TUI, or nil
// when auto-push is disabled (AutoPushOff) or no specs repo is configured. A nil
// publisher is valid — its methods are no-ops — so callers never nil-check.
func newTUIPublisher(rc *config.ResolvedConfig) *gitpkg.Publisher {
	if rc == nil || rc.Team == nil || !rc.AutoPushEnabled() {
		return nil
	}
	return gitpkg.NewPublisher(&rc.Team.SpecsRepo, tuiSyncOpts("comment", ""), 0)
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

// tuiTemplateConfig builds the markdown-local template config from the
// resolved team config (SPEC-025). Mirrors cmd.teamTemplateConfig; both are
// trivial boundary converters kept separate because cmd and tui cannot import
// each other.
func tuiTemplateConfig(rc *config.ResolvedConfig) markdown.TemplateConfig {
	var tc markdown.TemplateConfig
	if rc != nil && rc.Team != nil {
		tc.SpecPath = rc.Team.Templates.EffectiveSpecPath()
		tc.TriagePath = rc.Team.Templates.EffectiveTriagePath()
		for _, kv := range rc.Team.Templates.FrontmatterDefaults {
			tc.FrontmatterDefaults = append(tc.FrontmatterDefaults, markdown.KV{Key: kv.Key, Value: kv.Value})
		}
	}
	return tc
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

// advanceSpec advances a spec to the next pipeline stage. It routes through the
// shared workflow engine — the same path as the CLI `spec advance` — so gate
// evaluation, transition effects, PM/Jira status reflection, comms
// notifications, and the docs outbound mirror all fire from the TUI too,
// instead of the bare file mutation the TUI used previously.
func advanceSpec(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID, role string) tea.Cmd {
	return func() tea.Msg {
		deps := workflow.Deps{Config: rc, Registry: reg, DB: db, Role: role}
		var res *workflow.AdvanceResult
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("advance", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			var aErr error
			res, aErr = workflow.Advance(context.Background(), deps, workflow.AdvanceInput{
				SpecID:   specID,
				SpecPath: path,
				SpecDir:  repoPath + "/" + gitpkg.SpecsSubDir,
			})
			if aErr != nil {
				return "", aErr
			}
			return res.CommitMsg, nil
		})

		// Gate failures carry structured detail rather than git plumbing — surface
		// a clean, actionable message instead of a wrapped error.
		if errors.Is(err, workflow.ErrGatesNotMet) {
			return actionResultMsg{Action: "advance", SpecID: specID, Err: gateFailureError(res)}
		}

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "advance", SpecID: specID, Err: err}
		}
		return actionResultMsg{Action: "advance", SpecID: specID, Detail: advanceDetail(res, status)}
	}
}

// gateFailureError renders unmet gates into a single concise error for the
// status bar.
func gateFailureError(res *workflow.AdvanceResult) error {
	if res == nil || len(res.GateFailures) == 0 {
		return fmt.Errorf("gate conditions not met")
	}
	gates := make([]string, 0, len(res.GateFailures))
	for _, g := range res.GateFailures {
		gates = append(gates, g.Gate)
	}
	return fmt.Errorf("gate conditions not met: %s", strings.Join(gates, ", "))
}

// advanceDetail builds the status-bar summary for a successful advance,
// folding in the docs-mirror result and any failed transition effects.
func advanceDetail(res *workflow.AdvanceResult, pushStatus string) string {
	if res == nil {
		return "advanced (" + pushStatus + ")"
	}
	detail := fmt.Sprintf("%s → %s (%s)", res.PreviousStage, res.NewStage, pushStatus)
	if res.SyncedOut {
		detail += " · docs synced"
	}
	if n := failedEffectCount(res.Effects); n > 0 {
		detail += fmt.Sprintf(" · %d effect(s) failed", n)
	}
	return detail
}

func failedEffectCount(effs []workflow.EffectOutcome) int {
	n := 0
	for _, e := range effs {
		if e.Err != "" {
			n++
		}
	}
	return n
}

// blockSpec transitions a spec to blocked status with a reason. It routes
// through the shared workflow engine so the blocker is broadcast to comms
// (Slack/Teams) and logged, matching CLI `spec eject`.
func blockSpec(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID, reason, role string) tea.Cmd {
	return func() tea.Msg {
		deps := workflow.Deps{Config: rc, Registry: reg, DB: db, Role: role}
		var res *workflow.EjectResult
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("block", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			var eErr error
			res, eErr = workflow.Eject(context.Background(), deps, workflow.EjectInput{
				SpecID:   specID,
				SpecPath: path,
				Reason:   reason,
			})
			if eErr != nil {
				return "", eErr
			}
			return res.CommitMsg, nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "block", SpecID: specID, Err: err}
		}
		detail := "blocked (" + status + ")"
		if res != nil {
			detail = fmt.Sprintf("blocked from %s (%s)", res.PreviousStage, status)
		}
		return actionResultMsg{Action: "block", SpecID: specID, Detail: detail}
	}
}

// unblockSpec resumes a blocked spec to its pre-block stage. It routes through
// the workflow engine and resolves the resume target the same way the CLI does
// — the persisted `blocked_from` field, falling back to the escape-hatch log.
func unblockSpec(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID, role string) tea.Cmd {
	return func() tea.Msg {
		deps := workflow.Deps{Config: rc, Registry: reg, DB: db, Role: role}
		var res *workflow.ResumeResult
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("unblock", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			var rErr error
			res, rErr = workflow.Resume(context.Background(), deps, workflow.ResumeInput{
				SpecID:      specID,
				SpecPath:    path,
				ResumeStage: resolveResumeStage(path),
			})
			if rErr != nil {
				return "", rErr
			}
			return res.CommitMsg, nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "unblock", SpecID: specID, Err: err}
		}
		detail := "unblocked (" + status + ")"
		if res != nil {
			detail = fmt.Sprintf("resumed to %s (%s)", res.ResumeStage, status)
		}
		return actionResultMsg{Action: "unblock", SpecID: specID, Detail: detail}
	}
}

// resolveResumeStage finds the stage a blocked spec should return to: the
// persisted `blocked_from` frontmatter field, falling back to parsing the
// escape-hatch log for specs blocked before that field existed.
func resolveResumeStage(path string) string {
	if meta, err := markdown.ReadMeta(path); err == nil && meta.BlockedFrom != "" {
		return meta.BlockedFrom
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.Index(lines[i], "Blocked from `"); idx >= 0 {
			start := idx + len("Blocked from `")
			if end := strings.Index(lines[i][start:], "`"); end > 0 {
				return lines[i][start : start+end]
			}
		}
	}
	return ""
}

// revertSpec sends a spec back to a previous stage. It routes through the
// workflow engine so role validation, revert/on-enter transition effects, and
// activity logging fire exactly as on CLI `spec revert`.
func revertSpec(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID, targetStage, reason, role string) tea.Cmd {
	return func() tea.Msg {
		deps := workflow.Deps{Config: rc, Registry: reg, DB: db, Role: role}
		var res *workflow.RevertResult
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("revert", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			var rErr error
			res, rErr = workflow.Revert(context.Background(), deps, workflow.RevertInput{
				SpecID:      specID,
				SpecPath:    path,
				SpecDir:     repoPath + "/" + gitpkg.SpecsSubDir,
				TargetStage: targetStage,
				Reason:      reason,
			})
			if rErr != nil {
				return "", rErr
			}
			return res.CommitMsg, nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "revert", SpecID: specID, Err: err}
		}
		detail := "reverted (" + status + ")"
		if res != nil {
			detail = fmt.Sprintf("%s → %s (%s)", res.PreviousStage, res.TargetStage, status)
			if n := failedEffectCount(res.Effects); n > 0 {
				detail += fmt.Sprintf(" · %d effect(s) failed", n)
			}
		}
		return actionResultMsg{Action: "revert", SpecID: specID, Detail: detail}
	}
}

// assignSpec replaces a spec's assignees (an empty list clears them). It mirrors
// the CLI `spec assign`: claiming, reassigning, and unassigning are all the same
// frontmatter write committed through the specs repo.
func assignSpec(rc *config.ResolvedConfig, specID string, assignees []string) tea.Cmd {
	return func() tea.Msg {
		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("assign", specID), func(repoPath string) (string, error) {
			path, pErr := resolveSpecIn(repoPath, rc, specID)
			if pErr != nil {
				return "", pErr
			}
			meta, pErr := markdown.ReadMeta(path)
			if pErr != nil {
				return "", pErr
			}
			meta.Assignees = assignees
			if err := markdown.WriteMeta(path, meta); err != nil {
				return "", err
			}
			if len(assignees) == 0 {
				return fmt.Sprintf("chore: unassign %s", specID), nil
			}
			return fmt.Sprintf("chore: assign %s to %s", specID, strings.Join(assignees, ", ")), nil
		})

		status, fatal := pushOutcome(err)
		detail := ""
		if !fatal {
			if len(assignees) == 0 {
				detail = "unassigned (" + status + ")"
			} else {
				detail = "assigned to " + strings.Join(assignees, ", ") + " (" + status + ")"
			}
			err = nil
		}
		return actionResultMsg{Action: "assign", SpecID: specID, Detail: detail, Err: err}
	}
}

// selfAssignIdentity returns the current user's preferred assignment identity,
// favouring the spec-canonical handle over the display name. Assignment is
// spec-internal (written to frontmatter), so it uses the canonical handle.
func selfAssignIdentity(rc *config.ResolvedConfig) string {
	if h := strings.TrimSpace(rc.UserHandle()); h != "" {
		return h
	}
	if n := strings.TrimSpace(rc.UserName()); n != "" && n != "unknown" {
		return n
	}
	return ""
}

// creatorAssignees returns the default assignee list for a newly created
// spec: the creator claims it at creation (mirroring how author is stamped)
// so specs start claimed instead of relying on a remembered manual claim.
// Empty when no self identity is configured — the spec scaffolds unclaimed.
func creatorAssignees(rc *config.ResolvedConfig) []string {
	if self := selfAssignIdentity(rc); self != "" {
		return []string{self}
	}
	return nil
}

// parseAssignInput interprets the assign modal input into an assignee list:
// "-", "clear", or "none" clears all assignees; otherwise the input is split on
// spaces/commas and de-duplicated (case-insensitively, ignoring a leading '@').
func parseAssignInput(input string) []string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "-", "clear", "none":
		return []string{}
	}
	fields := strings.FieldsFunc(input, func(r rune) bool { return r == ' ' || r == ',' })
	seen := make(map[string]bool)
	var out []string
	for _, f := range fields {
		t := strings.TrimSpace(f)
		if t == "" {
			continue
		}
		key := strings.TrimPrefix(strings.ToLower(t), "@")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
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
		err := browserCmd(url).Start()
		return actionResultMsg{Action: "open", Detail: url, Err: err}
	}
}

// browserCmd builds the platform-specific command that opens a URL in the
// default browser.
func browserCmd(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("cmd", "/c", "start", url)
	default:
		return exec.Command("xdg-open", url)
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

// syncSpec publishes the spec outbound to the external docs mirror. The spec
// is the source of truth: the TUI never applies docs-provider content inbound
// (that requires an explicit 'spec sync --direction in' plus confirmation).
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
				Direction:        syncengine.DirectionOut,
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
