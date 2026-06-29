package update

import (
	"context"
	"os"
	"strings"
)

// GitHubToken returns an optional GitHub token to lift the anonymous API rate
// limit. Public release lookups work without it; it is read best-effort from
// the standard environment variables. This is the single source of truth for
// token resolution shared by `spec update` and the passive TUI check.
func GitHubToken() string {
	for _, key := range []string{"SPEC_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// CheckLatest reports the newest released version when it is newer than
// current, for the passive "update available" notice in the TUI. It reuses the
// same release source, semver comparison, and token resolution as `spec
// update`, differing only in one deliberate policy: it never nags development
// builds.
//
// It runs a live query on every call (the TUI invokes it once per startup, off
// the UI thread, bounded by the release source's 10s HTTP timeout). There is no
// on-disk cache: a release surfaces on the very next launch for every user,
// rather than being masked for up to a day by a stale cached "latest". The
// query is best-effort and degrades silently — any error (network down,
// rate-limited) yields ("", false), never a surfaced error, because an ambient
// notice must never disrupt the session.
func CheckLatest(ctx context.Context, current string) (latest string, available bool) {
	return checkLatest(ctx, current, NewUpdater(GitHubToken()))
}

// checkLatest is the testable core of CheckLatest with the release source
// injected.
func checkLatest(ctx context.Context, current string, u *Updater) (string, bool) {
	// Dev builds compare as always-behind for `spec update` (so a developer
	// can jump to a real release on demand); the passive notice applies the
	// opposite policy and stays quiet rather than nagging every local build.
	if IsDev(current) {
		return "", false
	}

	plan, err := u.Plan(ctx, Options{CurrentVersion: current})
	if err != nil {
		// Degrade silently rather than surfacing an error into an ambient
		// notice; the next launch simply tries again.
		return "", false
	}

	return notice(current, plan.LatestVersion)
}

// notice resolves whether a latest version warrants a notice for the running
// current version, reusing the same comparison `spec update` uses.
func notice(current, latest string) (string, bool) {
	if latest == "" || !updateAvailable(current, latest) {
		return "", false
	}
	return latest, true
}
