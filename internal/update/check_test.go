package update

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func tempCachePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "update-check.json")
}

// TestCheckLatest_DevSkips asserts a dev build never produces a notice, even
// when a newer release exists — the passive notice must not nag local builds.
func TestCheckLatest_DevSkips(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v2.0.0"}})
	if latest, ok := checkLatest(context.Background(), "dev", u, tempCachePath(t)); ok {
		t.Errorf("dev build produced notice %q, want suppressed", latest)
	}
}

// TestCheckLatest_NewerAvailable asserts a real newer release is reported and
// written to the cache for the next launch.
func TestCheckLatest_NewerAvailable(t *testing.T) {
	path := tempCachePath(t)
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v2.0.0"}})

	latest, ok := checkLatest(context.Background(), "v1.0.0", u, path)
	if !ok || latest != "v2.0.0" {
		t.Fatalf("checkLatest = (%q, %v), want (v2.0.0, true)", latest, ok)
	}
	if cached := readCheckCache(path); cached.LatestVersion != "v2.0.0" {
		t.Errorf("cache LatestVersion = %q, want v2.0.0", cached.LatestVersion)
	}
}

// TestCheckLatest_AlreadyLatest asserts no notice when current is up to date.
func TestCheckLatest_AlreadyLatest(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v1.0.0"}})
	if latest, ok := checkLatest(context.Background(), "v1.0.0", u, tempCachePath(t)); ok {
		t.Errorf("up-to-date produced notice %q, want suppressed", latest)
	}
}

// TestCheckLatest_FreshCacheNoFetch asserts a recent cache is served without
// touching the release source. The source errors, so a notice can only come
// from the cache.
func TestCheckLatest_FreshCacheNoFetch(t *testing.T) {
	path := tempCachePath(t)
	writeCheckCache(path, checkCache{CheckedAt: time.Now(), LatestVersion: "v2.0.0"})
	u := newTestUpdater(fakeSource{err: errors.New("network must not be hit")})

	latest, ok := checkLatest(context.Background(), "v1.0.0", u, path)
	if !ok || latest != "v2.0.0" {
		t.Errorf("fresh-cache checkLatest = (%q, %v), want (v2.0.0, true)", latest, ok)
	}
}

// TestCheckLatest_StaleCacheRefetches asserts an expired cache triggers a live
// query and reflects the newer result.
func TestCheckLatest_StaleCacheRefetches(t *testing.T) {
	path := tempCachePath(t)
	writeCheckCache(path, checkCache{
		CheckedAt:     time.Now().Add(-2 * checkCacheTTL),
		LatestVersion: "v2.0.0",
	})
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v3.0.0"}})

	latest, ok := checkLatest(context.Background(), "v1.0.0", u, path)
	if !ok || latest != "v3.0.0" {
		t.Errorf("stale-cache checkLatest = (%q, %v), want (v3.0.0, true)", latest, ok)
	}
}

// TestCheckLatest_FetchErrorFallsBackToCache asserts a failed live query
// degrades to the last cached version rather than going dark.
func TestCheckLatest_FetchErrorFallsBackToCache(t *testing.T) {
	path := tempCachePath(t)
	writeCheckCache(path, checkCache{
		CheckedAt:     time.Now().Add(-2 * checkCacheTTL),
		LatestVersion: "v2.0.0",
	})
	u := newTestUpdater(fakeSource{err: errors.New("offline")})

	latest, ok := checkLatest(context.Background(), "v1.0.0", u, path)
	if !ok || latest != "v2.0.0" {
		t.Errorf("error fallback checkLatest = (%q, %v), want (v2.0.0, true)", latest, ok)
	}
}

// TestCheckLatest_FreshCacheBehindCurrentRefetches asserts that when the
// running binary is newer than the cache's recorded "latest" (i.e. the user
// upgraded since the last check), a still-fresh cache is treated as stale and
// a live query runs anyway — so a brand-new release surfaces immediately
// rather than being suppressed for the rest of the TTL window. This is the
// regression for "on v0.24.0, v0.25.0 released, no update notice shown".
func TestCheckLatest_FreshCacheBehindCurrentRefetches(t *testing.T) {
	path := tempCachePath(t)
	// Fresh timestamp, but the cached "latest" is older than the running binary.
	writeCheckCache(path, checkCache{CheckedAt: time.Now(), LatestVersion: "v0.23.0"})
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v0.25.0"}})

	latest, ok := checkLatest(context.Background(), "v0.24.0", u, path)
	if !ok || latest != "v0.25.0" {
		t.Fatalf("checkLatest = (%q, %v), want (v0.25.0, true)", latest, ok)
	}
	if cached := readCheckCache(path); cached.LatestVersion != "v0.25.0" {
		t.Errorf("cache LatestVersion = %q, want v0.25.0 (should refresh)", cached.LatestVersion)
	}
}

// TestCheckLatest_FreshCacheUpToDateNoFetch asserts the polite path is intact:
// when current equals the cached latest, the cache is served without a network
// call (the source errors to prove it is not hit).
func TestCheckLatest_FreshCacheUpToDateNoFetch(t *testing.T) {
	path := tempCachePath(t)
	writeCheckCache(path, checkCache{CheckedAt: time.Now(), LatestVersion: "v1.0.0"})
	u := newTestUpdater(fakeSource{err: errors.New("network must not be hit")})

	if latest, ok := checkLatest(context.Background(), "v1.0.0", u, path); ok {
		t.Errorf("up-to-date fresh cache produced notice %q, want suppressed and no fetch", latest)
	}
}

func TestGitHubToken(t *testing.T) {
	t.Setenv("SPEC_GITHUB_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if got := GitHubToken(); got != "" {
		t.Errorf("GitHubToken with no env = %q, want empty", got)
	}

	t.Setenv("GITHUB_TOKEN", "legacy")
	if got := GitHubToken(); got != "legacy" {
		t.Errorf("GitHubToken legacy = %q, want legacy", got)
	}

	t.Setenv("SPEC_GITHUB_TOKEN", "preferred")
	if got := GitHubToken(); got != "preferred" {
		t.Errorf("GitHubToken precedence = %q, want preferred", got)
	}
}
