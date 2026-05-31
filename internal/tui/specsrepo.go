package tui

import (
	"context"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
)

// syncSpecsRepo brings the local specs repo up to date before a read-backed
// TUI loader reads it from disk.
//
// The TUI's spec-backed views (pipeline, specs, triage, dashboard) read spec
// markdown straight off rc.SpecsRepoDir. Without this call a refresh only
// re-reads stale local files, so a teammate's pushed advance never appears
// until some other command fetches. Routing reads through EnsureSpecsRepo —
// the non-destructive, TTL-gated, shared-lock fetch path — makes the TUI
// reflect remote changes on refresh.
//
// It degrades, never crashes (AGENTS.md robustness): when no team/specs repo
// is configured there is nothing to fetch, and a fetch failure is returned so
// the caller can surface a stale-data cue while still rendering the cached
// local files. The freshness TTL inside EnsureSpecsRepo coalesces the periodic
// tick + manual refreshes so this does not hammer the remote.
func syncSpecsRepo(ctx context.Context, rc *config.ResolvedConfig) error {
	if rc == nil || rc.Team == nil {
		return nil
	}
	if rc.Team.SpecsRepo.Owner == "" || rc.Team.SpecsRepo.Repo == "" {
		return nil
	}
	_, err := gitpkg.EnsureSpecsRepo(ctx, &rc.Team.SpecsRepo)
	return err
}
