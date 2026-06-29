package update

import (
	"context"
	"errors"
	"testing"
)

// TestCheckLatest_DevSkips asserts a dev build never produces a notice, even
// when a newer release exists — the passive notice must not nag local builds.
func TestCheckLatest_DevSkips(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v2.0.0"}})
	if latest, ok := checkLatest(context.Background(), "dev", u); ok {
		t.Errorf("dev build produced notice %q, want suppressed", latest)
	}
}

// TestCheckLatest_NewerAvailable asserts a real newer release is reported.
func TestCheckLatest_NewerAvailable(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v2.0.0"}})

	latest, ok := checkLatest(context.Background(), "v1.0.0", u)
	if !ok || latest != "v2.0.0" {
		t.Fatalf("checkLatest = (%q, %v), want (v2.0.0, true)", latest, ok)
	}
}

// TestCheckLatest_AlreadyLatest asserts no notice when current is up to date.
func TestCheckLatest_AlreadyLatest(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v1.0.0"}})
	if latest, ok := checkLatest(context.Background(), "v1.0.0", u); ok {
		t.Errorf("up-to-date produced notice %q, want suppressed", latest)
	}
}

// TestCheckLatest_FetchErrorDegradesSilently asserts a failed live query yields
// no notice (and no surfaced error) rather than disrupting the session.
func TestCheckLatest_FetchErrorDegradesSilently(t *testing.T) {
	u := newTestUpdater(fakeSource{err: errors.New("offline")})

	if latest, ok := checkLatest(context.Background(), "v1.0.0", u); ok {
		t.Errorf("fetch error produced notice %q, want suppressed", latest)
	}
}

// TestCheckLatest_NewReleaseSurfacesImmediately is the regression guard for
// "literally nobody but me saw the update notice": when the running binary is
// the latest the user previously knew about and a newer release ships, the very
// next launch must surface it. With no on-disk cache to mask it, a live query
// reports the new release every time.
func TestCheckLatest_NewReleaseSurfacesImmediately(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v0.33.0"}})

	latest, ok := checkLatest(context.Background(), "v0.32.0", u)
	if !ok || latest != "v0.33.0" {
		t.Fatalf("checkLatest = (%q, %v), want (v0.33.0, true)", latest, ok)
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
