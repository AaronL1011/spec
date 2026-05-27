# TUI Architecture Uplift Summary

## Purpose

This change set reduces event-loop blocking in the Bubble Tea TUI so keyboard input remains responsive during reader rendering, refreshes, SQLite-backed actions, and settings updates.

The guiding rule is:

> `Update()` and `View()` should only mutate or read lightweight UI state. Slow work should run in `tea.Cmd` workers and return result messages.

## Problems Addressed

- The spec reader opened with `o` rendered markdown synchronously during key handling.
- `viewReader()` could render missing reader lines from `View()`.
- Reader enter and exit returned `tea.ClearScreen`, adding unnecessary event-loop churn in alt-screen mode.
- Focus and unfocus opened SQLite per action, exposing the TUI to the store's 5 second busy timeout.
- Periodic refreshes could re-fetch detail data while the reader was active.
- Theme persistence wrote user config synchronously from `Update()`.

## Architecture Changes

### App-Scoped TUI Store

`App` now owns a long-lived `*store.DB` session handle. Focus, unfocus, focused-spec reads, and standup generation use this shared handle instead of opening SQLite during user interaction.

This removes repeated `store.Open()` calls from TUI hot paths and avoids lock waits from surfacing as whole-UI freezes.

### Async Spec Reader Rendering

Reader rendering now runs through an async `tea.Cmd`:

1. Key handling updates reader state and schedules a render.
2. The render command builds markdown lines off the event loop.
3. A `sectionRenderedMsg` returns the rendered lines.
4. The detail model accepts the result only if it matches the current spec, section, and generation.

The detail model also keeps a section render cache keyed by content hash, section, width, and content size. Returning to a previously rendered section can use cached lines immediately.

### Stale Result Guards

Reader renders use a generation counter. If the user quickly changes sections, resizes the terminal, or navigates away, older render results are ignored.

This prevents out-of-order async work from replacing the currently visible section.

### Reader-Safe Refresh

Periodic refreshes are now coalesced and skipped while detail reader mode is active. Detail fetches also carry a content hash, allowing unchanged data to be ignored cheaply.

### Refresh Coalescing

The app tracks in-flight refreshes per view/resource:

- Dashboard
- Pipeline
- Specs
- Triage
- Reviews
- Detail spec

If a refresh is already running for a key, another refresh for the same key is not scheduled until the result arrives.

### Async Theme Persistence

Theme cycling updates UI state immediately but writes the user config through an async command. A failed write returns a `themePersistedMsg` and displays an error toast.

## Files Changed

- `internal/tui/app.go`
  - Added app-scoped DB handle.
  - Added refresh coalescing.
  - Routed section render results.
  - Skipped periodic detail refresh in reader mode.
  - Moved theme persistence into async command flow.

- `internal/tui/specdetail.go`
  - Added reader render states.
  - Added async `sectionRenderedMsg` flow.
  - Added render cache and generation guards.
  - Removed `tea.ClearScreen`.
  - Removed rendering from `View()`.
  - Starts reader on the first meaningful level-2 or level-3 section.

- `internal/tui/actions.go`
  - Focus and unfocus now use the app's shared DB handle.

- `internal/tui/standup.go`
  - Standup generation can reuse the app's shared DB handle.

- `internal/tui/*_test.go`
  - Added regression coverage for async render scheduling, stale render results, render cache hits, refresh coalescing, reader-safe refresh, and non-mutating reader views.

## Validation

The following checks pass:

```sh
go test ./internal/tui
go test ./...
```

Cursor lints for `internal/tui` also report no errors.

## Expected UX Impact

- Pressing `o` no longer performs markdown rendering on the event loop.
- `j`, `k`, tab switching, and numeric tab shortcuts should remain responsive while reader rendering is in progress.
- Repeated refresh ticks should not stack duplicate view refreshes.
- Focus/unfocus should no longer expose interactive navigation to repeated SQLite open waits.
- The reader displays a lightweight pending state while a section render is running.
