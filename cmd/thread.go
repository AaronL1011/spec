package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/thread"
)

// withThreadStore runs fn against a thread store rooted in the specs repo
// clone, then commits and pushes the resulting sidecar change. The commit
// message is returned by fn so each verb describes its own change.
//
// Mutations go through WithSpecsRepo so threads ride the same git flow as
// specs — no separate transport, retries on push conflict are handled there.
func withThreadStore(rc *config.ResolvedConfig, specID string, fn func(store *thread.SidecarStore) (commitMsg string, err error)) error {
	if err := requireTeamConfig(rc); err != nil {
		return err
	}
	return gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(nil, specID), func(repoPath string) (string, error) {
		// sidecarDirFor both confirms the spec exists in the freshly-synced
		// repo and resolves wherever it currently lives (specs/, triage/, or
		// archive/) — the sidecar always sits next to it, never hardcoded to
		// specs/ root.
		dir, err := sidecarDirFor(specsDir(repoPath), rc, specID)
		if err != nil {
			return "", err
		}
		store := thread.NewSidecarStore(dir)
		return fn(store)
	})
}

// notifyThreadParticipants sends a best-effort comms notification about a
// thread event. Notifications only fire on local user actions and degrade
// silently — a broken comms adapter never blocks the operation.
func notifyThreadParticipants(p *printer, rc *config.ResolvedConfig, specID, message string) {
	if !rc.HasIntegration("comms") {
		return
	}
	reg := buildRegistry(rc)
	if err := reg.Comms().Notify(ctx(), adapter.Notification{
		SpecID:  specID,
		Title:   specID + " discussion",
		Message: message,
	}); err != nil {
		p.Warn("could not send notification: %v", err)
	}
}

// threadAuthor returns the identity to attribute a thread action to,
// preferring the configured handle (e.g. "@alice") over the display name.
func threadAuthor(rc *config.ResolvedConfig) string {
	if h := rc.UserHandle(); h != "" {
		return h
	}
	return rc.UserName()
}

// logThreadActivity records a thread event in the local activity log
// (best-effort; failures are ignored like other activity writes).
func logThreadActivity(rc *config.ResolvedConfig, specID, summary, threadID string) {
	db, err := openDB()
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()
	meta := fmt.Sprintf(`{"thread_id":%q}`, threadID)
	_ = db.ActivityLog(specID, "discuss", summary, meta, rc.UserName())
}
