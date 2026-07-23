# Flow Analytics — Design Document

Status: draft · Owner: tl · Scope: rework of `spec metrics` into a git-history-backed team flow analytics engine

## 1. Problem

`spec metrics` today computes pipeline metrics from the **local SQLite activity
log** (`internal/store/activity.go`). That log only contains events the current
user performed on their own machine, so the numbers are a per-user fragment of
team activity — near-useless for answering the questions a tech lead actually
has:

- How long does it take an idea to reach production? (lead time)
- Once engineering starts, how long until it ships? (cycle time)
- Where does work sit and wait? (bottlenecks, aging WIP)
- Is our process getting better or worse cycle over cycle?

Meanwhile, the specs repo git history **already is a team-wide, durable,
append-only event log**. Every stage transition lands as a commit with a
machine-parseable conventional message and an authoritative frontmatter diff:

| Event | Commit message source |
|---|---|
| Scaffold | `feat: scaffold SPEC-012 — title` (`cmd/new.go`, `internal/tui/newspec.go`) |
| Promote from triage | `feat: promote TRIAGE-004 to SPEC-012 — title` (`cmd/promote.go`) |
| Advance | `feat: advance SPEC-012 to build` (`internal/workflow/advance.go:145`) |
| Revert | `fix: revert SPEC-012 to draft — reason` (`internal/workflow/revert.go:73`) |
| Eject | `fix: eject SPEC-012 — reason` (`internal/workflow/block.go:62`) |
| Deploy | `feat: deploy SPEC-012 to prod` (`cmd/deploy.go`) |

No new infrastructure is required. Clone history in, insight out.

## 2. Goals

- Replace the internals of `spec metrics` with git-history-derived, team-wide
  flow analytics. Same command name; radically more useful output.
- Produce **system-centred** insight: where work waits, not who is slow.
- Report distributions (p50/p85/p95), never bare averages — lead-time data is
  long-tailed and averages lie.
- Beautiful, dense terminal rendering consistent with the dashboard aesthetic
  (SPEC.md §UX Requirements).
- `--json` output for external dashboards.

## 3. Non-Goals

- **No per-person metrics.** Cycle time by assignee invites blame games and
  measures the wrong thing. Flow problems are system problems; the unit of
  analysis is the stage and the spec, never the individual. Author identity
  from commits is used only to attribute events to the pipeline engine vs.
  manual edits, and is never surfaced in output.
- No caching layer. Parse the full log on every run (see Decision 4).
- No web UI, exports beyond JSON, or historical data backfill from external
  tools (Jira etc.).
- No changes to `spec retro` in this iteration (it can adopt the new engine
  later).

## 4. Decisions

| # | Question | Decision | Rationale |
|---|---|---|---|
| 1 | Where does the cycle-time clock start? | First entry into the first `owner_role: engineer` stage in the configured pipeline (e.g. `engineering` in this repo's `spec.config.yaml`). | Zero-config default that matches the standard definition of cycle time (work started → done). No new config key until a team needs one. |
| 2 | New command or rework `spec metrics`? | Rework `spec metrics` in place. | The current command serves little value (local-only fragment). One command, one meaning. |
| 3 | Per-person breakdowns? | Excluded entirely. | System-centred flow visibility finds weak spots in the delivery process; individual metrics create blame incentives and distort behaviour. |
| 4 | Cache extracted events? | No — parse the full `git log` each run. | Specs repos are small (thousands of commits at most); a single `git log` with a structured format is fast. Caching is premature complexity; revisit only if runs exceed ~1s on real repos. |

## 5. Metric definitions

All computed per spec from its reconstructed event timeline, then aggregated
into distributions over the report window.

| Metric | Definition |
|---|---|
| **Lead time** | First commit creating the spec (scaffold, or promote — which captures triage wait) → first entry into a terminal stage (`pipeline.TerminalStages`, `internal/pipeline/stages.go:98`). |
| **Cycle time** | First entry into the first engineer-owned stage → first entry into a terminal stage. Wall clock; reversions do not reset it. |
| **Time-in-stage** | Dwell per stage, accumulated across revisits (a revert re-opens that stage's clock). |
| **Blocked time** | Sum of eject → resume gaps. Reported as its own line, and subtracted nowhere — reality includes blockage. |
| **Flow efficiency** | Time in engineer-owned stages ÷ lead time. A crude but honest work-vs-wait signal. |
| **Throughput** | Specs entering a terminal stage per week (and per cycle when `--cycle` is given). |
| **Reversion rate** | Reverts ÷ advances, broken down per stage boundary (`review→draft ×4`) so it points at *where* quality escapes, not at whom. |
| **Aging WIP** | Open (non-terminal, non-blocked) specs whose current-stage dwell exceeds the historical p85 for that stage. |

Fast-track specs (`fast_track: true` frontmatter) are segmented into their own
distribution so they don't pollute the main percentiles.

## 6. Architecture

Follows the AGENTS.md layering rules: only `internal/git` shells out, engines
consume data through plain types, `cmd/` stays thin.

```
internal/git/log.go        Log(ctx, dir, opts) ([]LogEntry, error)
                           LogEntry{SHA, Author, When time.Time, Message, Files []string}
                           FileAtCommit(ctx, dir, sha, path) (string, error)   // for tier-2 frontmatter diffing

internal/analytics/
  event.go                 ExtractEvents([]git.LogEntry, FrontmatterReader) []Event
                           Event{SpecID, Kind, FromStage, ToStage, At, Source}
  timeline.go              BuildTimelines([]Event, pipelineCfg) map[string]*Timeline
  flow.go                  ComputeFlow(timelines, window, pipelineCfg) *FlowReport
  percentile.go            distribution helpers (p50/p85/p95, sparkline buckets)
  render.go                RenderFlowReport(*FlowReport, width) string
  render_json.go           JSON marshalling of FlowReport

cmd/metrics.go             flags → resolve specs repo → git.Log → analytics → print
```

`internal/metrics` (the SQLite-based package) is deleted once `spec retro`'s
usage is migrated or pinned; until then it remains but `cmd/metrics.go` no
longer imports it.

### 6.1 Event extraction — two tiers

**Tier 1 (fast path):** match commit messages against the exact formats the
workflow engine emits (see §1 table). Anchored regexes, one per event kind.
Covers every commit produced by `spec` itself.

**Tier 2 (truth path):** for any commit that touches a `specs/SPEC-*.md` file
but matches no tier-1 pattern (manual edits, squashed history, external
tooling), diff the frontmatter `status:` field between the commit and its
parent (`git show <sha>:<path>` on both sides, parse with
`internal/markdown/frontmatter.go`). A status change yields a synthesized
`Advanced`/`Reverted` event with `Source: frontmatter`.

Frontmatter is the source of truth; commit messages are an optimisation. The
report footer states coverage honestly: `analysed 212/230 transitions
(18 unattributable commits skipped)`.

### 6.2 Timeline reconstruction

Per spec: sort events ascending, walk them into `[]StageVisit{Stage, Entered,
Exited}` intervals plus `[]BlockedInterval`. Open specs get a synthetic
`Exited: now` on their current visit for aging-WIP purposes only (never counted
into completed-spec distributions).

Ordering edge: commits pushed from different machines can have non-monotonic
timestamps. Sort by commit time, break ties by topological order from `git log
--topo-order`.

### 6.3 Degradation

- No specs repo configured → `metrics requires a specs repo — run 'spec config init' to set one up` and exit 0 with no partial output. Never panic (noop-adapter philosophy).
- Specs repo unreachable → operate on the existing local clone with a `data may be stale (last fetch 2h ago)` banner, mirroring `internal/git/specsrepo.go` freshness handling.
- Empty history / no completed specs in window → render the WIP sections only, with an explicit `no completed specs in this window` note instead of zeros.

## 7. Command surface

```
spec metrics                       # team flow report, trailing 90 days
spec metrics --cycle "Cycle 7"     # scope to a cycle label (frontmatter `cycle:`)
spec metrics --since 2026-01-01    # explicit window
spec metrics --spec SPEC-012       # single spec journey (timeline render)
spec metrics --stage pr-review     # deep-dive one stage: dwell distribution, arrivals/departures, reversion boundaries
spec metrics --json                # machine-readable FlowReport
```

Existing `--cycle` flag semantics are preserved; the local-SQLite code path is
removed.

## 8. Rendering

Design intent: the report should read top-down as *outcomes → where time goes →
what needs attention*, in under one screen for a typical cycle.

```
  Flow — Cycle 7 (Mar 3 – Mar 28) · 14 specs completed

  Lead time    p50 6.2d   p85 11.4d   p95 19.1d      ▂▅█▆▃▂▁▁ ▁
  Cycle time   p50 3.1d   p85  5.8d   p95  9.2d      ▃█▅▂▁▁
  Flow efficiency 42%  ·  Throughput 3.5/wk ↑ 12% vs Cycle 6

  Time in stage (p50, this cycle vs last)
  draft          ████████░░░░░░░░░░  1.8d   ▼ 0.4d
  tl-review      ██████████████████  4.2d   ▲ 1.1d   ◀ bottleneck
  engineering    ██████████░░░░░░░░  2.3d   ▬
  qa-validation  ████░░░░░░░░░░░░░░  0.9d   ▼ 0.2d

  Reversions: 5 (tl-review→draft ×4, qa-validation→build ×1)
  ⚠ tl-review→draft reversion rate 29% — specs may be entering review under-baked

  Aging WIP
  SPEC-031  tl-review    6.1d in stage   p85 is 5.8d
  SPEC-029  build        9.0d in stage   p85 is 4.9d   ⚠ 1.8× p85

  Blocked: 2 specs, 4.5d total eject time this cycle

  analysed 212/230 transitions · window Mar 3 – Mar 28 · fetched 4m ago
```

Rendering rules:

- Unicode block/spark characters with plain-ASCII fallback when `NO_COLOR` /
  non-TTY (match existing `cmd/output.go` conventions).
- Deltas (`▲ ▼ ▬`) only when a comparison window exists (previous cycle or
  preceding equal-length window).
- Bottleneck callout: single stage with the largest p50 dwell among
  non-terminal stages, annotated with a *diagnostic hypothesis* keyed to the
  dominant reversion boundary when one exists.
- Aging WIP lists spec + stage + dwell vs p85. **No assignee column** (Decision
  3). The dashboard is where individuals see their own work.
- Empty sections are hidden, not rendered blank.
- `--spec` mode renders one horizontal timeline: stage bands with durations,
  reversion arrows, blocked hatching.

## 9. Testing

Per AGENTS.md: table-driven, interfaces not implementations, `t.TempDir()`.

- **event.go** — table-driven: every tier-1 message format, malformed messages,
  tier-2 frontmatter diffs (status changed, unchanged, file added, file
  deleted, unparsable frontmatter). No git required — fixture `[]git.LogEntry`.
- **timeline.go / flow.go** — golden scenarios: happy path, revert loop,
  eject/resume, skipped optional stages, fast-track, non-monotonic timestamps,
  spec still open. Assert exact percentiles on synthetic durations.
- **internal/git/log.go** — integration test against a scripted temp repo
  (pattern from `internal/git/git_test.go`).
- **render.go** — golden-file output tests at fixed width, colour disabled.
- **cmd/metrics.go** — smoke tests following `cmd/smoke_reads_test.go` pattern:
  no repo configured, empty repo, `--json` shape stability.

## 10. Implementation plan

1. `feat: git log reader` — `internal/git/log.go` + tests. No behaviour change.
2. `feat: analytics event extraction` — `internal/analytics/event.go` (tier 1 + tier 2) + tests.
3. `feat: analytics timelines and flow computation` — timeline.go, flow.go, percentile.go + tests.
4. `feat: rework spec metrics onto flow analytics` — render.go, render_json.go, rewire `cmd/metrics.go`, smoke tests. Remove the `internal/metrics` import from cmd.
5. `chore: retire internal/metrics pipeline path` — migrate or pin `spec retro`'s remaining usage (follow-up, may land later).

Each step passes `make lint-strict` before merge.

## 11. Open questions

- Should `spec retro` adopt the git-backed engine in the same effort, or stay
  on `internal/metrics` until a later pass? (Current plan: later pass, step 5.)
- Histogram bucket strategy for the sparklines (fixed-count buckets vs
  log-scale) — decide during render implementation with real data shapes.
