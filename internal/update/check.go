package update

import (
	"context"
	"os"
	"strings"
	"time"
)

// checkCacheTTL bounds how long a passive update check is served from the
// on-disk cache before a fresh GitHub query is made. The TUI runs the check on
// every launch (potentially many times a day), so the cache keeps the common
// path network-free and polite to the GitHub API. `spec update` deliberately
// does NOT use this cache — an explicit update is always a live query.
const checkCacheTTL = 24 * time.Hour

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
// update`, differing only in two deliberate policies: it never nags
// development builds, and it is throttled by a 24h on-disk cache so startup
// stays fast and the GitHub API stays unhit on most launches.
//
// It degrades silently: any error (network down, rate-limited, corrupt cache)
// yields ("", false) — or a still-valid cached value — never a surfaced error,
// because an ambient notice must never disrupt the session.
func CheckLatest(ctx context.Context, current string) (latest string, available bool) {
	return checkLatest(ctx, current, NewUpdater(GitHubToken()), defaultCachePath())
}

// checkLatest is the testable core of CheckLatest with the release source and
// cache path injected.
func checkLatest(ctx context.Context, current string, u *Updater, cachePath string) (string, bool) {
	// Dev builds compare as always-behind for `spec update` (so a developer
	// can jump to a real release on demand); the passive notice applies the
	// opposite policy and stays quiet rather than nagging every local build.
	if IsDev(current) {
		return "", false
	}

	cache := readCheckCache(cachePath)

	// Serve a fresh cache without any network call.
	if time.Since(cache.CheckedAt) < checkCacheTTL && cache.LatestVersion != "" {
		return notice(current, cache.LatestVersion)
	}

	plan, err := u.Plan(ctx, Options{CurrentVersion: current})
	if err != nil {
		// Fall back to a possibly-stale cached version rather than going dark.
		return notice(current, cache.LatestVersion)
	}

	writeCheckCache(cachePath, checkCache{CheckedAt: time.Now(), LatestVersion: plan.LatestVersion})
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
