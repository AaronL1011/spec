package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/identity"
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
// thread event, routed per recipient via the comms adapter's direct-message
// path (Comms().NotifyUser). Recipients that don't resolve to a DM-able user
// — including everyone when the adapter has no per-user delivery at all —
// fall back to a single channel broadcast naming them. Notifications only
// fire on local user actions and degrade silently: a broken comms adapter
// never blocks the git operation.
func notifyThreadParticipants(p *printer, rc *config.ResolvedConfig, specID string, recipients []string, message string) {
	if !rc.HasIntegration("comms") {
		return
	}
	comms := buildRegistry(rc).Comms()
	acting := identity.Viewer{Name: rc.UserName(), Handle: rc.CanonicalHandle(), Identities: rc.UserIdentities()}

	var unresolved []string
	for _, recipient := range recipients {
		// The acting user is never notified of their own action, and agent
		// contributions ("agent" or "agent:<adapter>") are never notification
		// targets — routing to them would always hit ErrRecipientUnknown and
		// only add broadcast noise.
		if isAgentHandle(recipient) || identity.MatchesIdentity(recipient, acting) {
			continue
		}

		err := comms.NotifyUser(ctx(), recipient, adapter.Notification{
			SpecID:  specID,
			Title:   specID + " discussion",
			Message: message,
		})
		switch {
		case err == nil:
			// delivered directly
		case errors.Is(err, adapter.ErrRecipientUnknown):
			unresolved = append(unresolved, recipient)
		default:
			p.Warn("could not notify %s: %v", recipient, err)
		}
	}

	if len(unresolved) == 0 {
		return
	}
	broadcast := fmt.Sprintf("%s\n(could not reach: %s)", message, strings.Join(unresolved, ", "))
	if err := comms.Notify(ctx(), adapter.Notification{
		SpecID:  specID,
		Title:   specID + " discussion",
		Message: broadcast,
	}); err != nil {
		p.Warn("could not send notification: %v", err)
	}
}

// isAgentHandle reports whether handle names an agent contribution rather
// than a person: the literal "agent" author, or an "agent:<adapter>" identity
// (discussion-03-reader-cockpit.md §6). Agents are never comms recipients.
func isAgentHandle(handle string) bool {
	h := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(handle, "@")))
	return h == "agent" || strings.HasPrefix(h, "agent:")
}

// excludeIdentity returns handles with every handle matching self removed
// (case-insensitive, tolerant of a leading '@') — used to drop the acting
// user from a Participants() list before routing a notification, since
// Participants() may record them under a different handle/name than
// threadAuthor(rc) returns.
func excludeIdentity(handles []string, self string) []string {
	v := identity.Viewer{Identities: []string{self}}
	var out []string
	for _, h := range handles {
		if identity.MatchesIdentity(h, v) {
			continue
		}
		out = append(out, h)
	}
	return out
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
