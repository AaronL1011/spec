---
id: SPEC-XXX
title: Spec Bounty — Scarce, Reasoned Pull Signal on Claimable Work
status: draft
version: 0.1.0
author: —
cycle: TBD
epic_key:
repos: [spec-cli]
revert_count: 0
created: 2026-07-28
updated: 2026-07-28
---

# SPEC-XXX — Spec Bounty

> *Let leadership mark a small number of specs as **worth taking** — rendered as a gold, glimmering spark and spec ID everywhere the spec appears — and keep a durable record of who claimed and finished them, so prioritisation becomes a pull signal with a payout instead of a ranking argument.*

---

## TL;DR                             <!-- owner: anyone -->

Every prioritisation signal `spec` has today is a **push**: stage ownership decides whose queue you're in, `spec assign` decides who owns an item, and the time-urgency gradient makes stalled work look hot. None of them let a TL or PM say *"this one is worth taking"* to an engineer choosing their next piece of work. We add a **bounty**: a role-gated, reason-carrying, **capped-scarcity** marker on a spec. A bountied spec renders its **spark glyph (`✦`, the boot-splash mark) and its SPEC-ID in gold with a periodic shimmer sweep**, everywhere a spec row appears in the TUI; the title and metadata keep whatever colour the time-urgency gradient gives them, so the two signals compose instead of fighting. Bounties are **scarce by configuration** (`bounty.max_active`), **explained** (a mandatory reason, visible in the detail view), and **paid out**: claiming and completing a bountied spec is recorded durably in git, and `spec bounty ledger` aggregates earned bounties per person over a period so a TL can run a quarterly leaderboard and fund a real reward. The reward itself is deliberately out of scope.

---

## 1. Problem Statement

Prioritisation in a TL/PM partnership is a communication problem, not a ranking problem. The failure mode is familiar: everything important gets labelled important, `priority: high` inflates until it means nothing, and the engineer picking up their next task has no signal beyond "whatever is oldest in my queue".

Concretely, in `spec` today:

- **There is no pull signal.** Stage ownership (`owner_role`), assignment (`spec assign`), and the time-urgency gradient are all push or ambient-decay signals. The claimable queue exists — `dashboard.do_scope: assignee` with `claimable: true` surfaces unassigned specs to the whole owning role — but every item in it looks identical. A TL who wants a specific unclaimed spec taken next has to go ask someone.
- **`priority` is the wrong instrument.** Triage items carry `priority: low|medium|high|critical`, but priority is a property of the *work*, permanently attached, uncapped, and set by whoever filed it. A bounty is a property of *this moment's plan*, deliberately rationed, and set by someone accountable for sequencing.
- **Unglamorous work starves.** The specs that most need a nudge are the ones with no intrinsic appeal — the migration nobody wants, the flaky-test cleanup, the third quarter of a long refactor. Nothing in the product lets leadership put a thumb on that scale.
- **Finishing high-leverage work leaves no trace.** `spec` records activity and stage durations, but there is no durable, person-level record of *"took the thing the team most needed taken"*. Without a payout, any priority marker decays into "the TL wants this", and engineers correctly learn to ignore it.

The opportunity is that a scarce, visually distinctive, reasoned marker changes the *register* of prioritisation from instruction to invitation — and it fits machinery that already exists (claimable queues, frontmatter, the theme system, an archive that already serves as a completion record).

---

## 2. Goals & Non-Goals

### Goals

- Let roles named in team config (`bounty.grantable_by`) place a **bounty** on a spec, with a **mandatory reason** that is stored and displayed.
- Enforce **scarcity by configuration** (`bounty.max_active`): granting past the cap fails with an actionable error naming the currently bountied specs.
- Render a bountied spec's **spark glyph and SPEC-ID in gold with a periodic shimmer**, in every TUI surface where that spec appears (dashboard, pipeline, spec list, spec detail, search results), while **leaving the title and metadata to the existing time-urgency ramp**.
- Reuse the **boot-splash spark (`glyph.Spark`, `✦`)** so the marker reads as the product's own "rare item" mark rather than a new invented icon.
- Record **claim** and **completion** of a bountied spec durably, in git, so the record survives clones, machine loss, and cache wipes.
- Provide `spec bounty ledger` to aggregate **earned bounties per person over a period**, enough to run a quarterly review and hand out an externally-funded reward.
- Represent bounties in **non-TUI output** (plain text and JSON) so scripts, CI, and pipes can see them.
- Degrade cleanly: `NO_COLOR`, non-TTY, monochrome themes, and 16-colour terminals all still communicate "bountied" — by glyph and shape, never by animation alone.

### Non-Goals

- **No reward definition.** What an earned bounty buys (gift card, recognition, nothing) is a people decision made outside the tool. `spec` records the facts; it does not model prizes, points, currency, or budgets.
- **No reordering or gating.** A bounty does not sort a spec to the top, does not change gates, does not change advancement, and does not notify anyone. The shine *is* the mechanism.
- **Not a replacement for `priority`.** Triage priority stays exactly as it is. A bounty is orthogonal and additive.
- **No auto-granting.** Nothing infers a bounty from staleness, age, or dependency depth. A human with a named role decides, and says why.
- **No team-visible live leaderboard in v1.** The ledger is a command a TL runs; it is not a dashboard section, not a status-bar element, and not published anywhere.
- **No per-spec bounty "size"/tiers in v1.** One bounty is one bounty. Tiers reintroduce the ranking argument the feature exists to avoid.
- **No enforcement of fairness.** The tool will not stop a granter from bountying only fun work; that is a review-and-culture problem, monitored during pilot.

---

## 3. User Stories

| # | As a... | I want to... | So that... | Acceptance Criteria |
|---|---|---|---|---|
| US-1 | tech lead | mark a small number of specs as bountied, with a written reason | engineers choosing their next task can see what the team most needs taken, and why | `spec bounty set <id> --reason "..."` stores granter, timestamp, and reason; reason is mandatory; the spec renders as bountied everywhere |
| US-2 | tech lead | be blocked from having too many bounties active at once | the marker stays rare enough to mean something | granting past `bounty.max_active` fails with an error listing the active bountied specs and how to clear one |
| US-3 | engineer | spot a bountied spec instantly while scanning any list | the invitation lands without me reading a column or a flag | the spark glyph and SPEC-ID render gold with a periodic shimmer sweep on dashboard, pipeline, spec-list, detail, and search rows |
| US-4 | engineer | read *why* a spec is bountied before I claim it | I can judge whether I'm the right person, instead of chasing shine | the spec detail view shows the reason, granter, and grant date |
| US-5 | engineer | have my claim and completion of a bountied spec recorded | the effort is visible later without me keeping my own tally | claiming a bountied spec stamps `claimed_by`/`claimed_at`; reaching a terminal stage stamps `earned_by`/`earned_at`, and both persist into the archive |
| US-6 | tech lead | see earned bounties per person for a period | I can run a quarterly review and fund a real reward off real data | `spec bounty ledger --since <date>` (and `--cycle`) prints a per-person count derived from git, with the contributing spec IDs |
| US-7 | PM | see bounties in plain and JSON output | I can read them from a script or a non-TUI context | plain output marks bountied rows with `✦`; JSON output carries a `bounty` object |
| US-8 | engineer on `NO_COLOR` / mono theme / piped output | still be able to tell a bountied spec apart | the signal never depends on colour or animation alone | with `NO_COLOR`, on `graphite`, and when piped, the spark glyph (and bold where available) still marks the row; no escape-code artefacts, no animation |
| US-9 | tech lead | remove a bounty I no longer stand behind | a stale plan doesn't leave misleading gold on the board | `spec bounty clear <id>` removes the active bounty, is permitted only for `grantable_by` roles, and is logged |
| US-10 | tech lead | bounty the spec under my cursor in the TUI with `g b` | prioritising happens where I already triage — without dropping to a shell and retyping a spec ID | `g b` on any spec row opens a reason prompt pre-filled with the current reason; submitting grants or updates the bounty, `-` clears it, and a role or cap refusal surfaces in the status bar |

---

## 4. Proposed Solution

### 4.1 Concept Overview

A bounty has a **three-state lifecycle**, each state a fact recorded in the spec's frontmatter and therefore in git:

1. **Granted** — a role in `bounty.grantable_by` runs `spec bounty set <id> --reason "..."`. Stores `granted_by`, `granted_at`, `reason`. Counts against `bounty.max_active`. Revocable via `spec bounty clear <id>`.
2. **Claimed** — an engineer claims the spec through the *existing* claim path (`spec assign`, or the auto-claim on `spec do` / `spec build`). If a bounty is active and unclaimed, the claim also stamps `claimed_by` / `claimed_at`. First claimant wins; a later reassignment before completion overwrites `claimed_by` and is logged.
3. **Earned** — the spec enters a stage in `pipeline.TerminalStages`. If a bounty is active with a `claimed_by`, that value is frozen into `earned_by` and `earned_at` is stamped. Immutable thereafter, and carried into the archive on auto-archive, which is what makes the ledger durable.

The **visual treatment** deliberately occupies a channel the time-urgency gradient does not use. Time-urgency already owns the **whole-row foreground** (`Text → yellow → amber → orange → red` by stage dwell). A bounty therefore owns exactly two spans:

- the **row's leading glyph**, replaced by `glyph.Spark` (`✦`) — the same mark as the boot splash, so gold-spark reads as "spec's rare thing";
- the **SPEC-ID span**, painted in the theme's new `Bounty` (gold) token;

with a **periodic shimmer sweep** across those ~13 cells: one eased pass, then a pause, repeating — the boot-splash sheen, slowed down and looped. The title, stage, assignee, and detail columns keep their urgency-ramp colour untouched. A bountied *and* badly stale spec therefore reads correctly: gold spark and ID, red title.

### 4.2 Architecture / Approach

- **`internal/config`** — new `BountyConfig` on `TeamConfig`: `enabled`, `grantable_by`, `max_active`, `require_reason`, `shimmer`. Lint validates roles against the pipeline's known roles, `max_active >= 1`, and rejects unknown keys.
- **`internal/markdown`** — new `Bounty *BountyState` on `SpecMeta` (`yaml:"bounty,omitempty"`), holding grant, claim, and earn facts. Absent field = no bounty, which is the state of every existing spec.
- **`internal/bounty`** (new, pure) — the rules: who may grant (`CanGrant(role, cfg)`), cap enforcement (`CheckCap(active []string, cfg)`), state transitions (`Grant`, `Clear`, `Claim`, `Earn`), and ledger aggregation over a slice of `SpecMeta` (`Tally(metas, window) []Award`). No I/O, no config file reads, no git — takes already-resolved config and already-parsed metas, mirroring how `internal/urgency` stays pure.
- **`cmd/bounty.go`** — thin: `spec bounty set|clear|list|ledger`, resolving config, loading metas, calling `internal/bounty`, writing frontmatter through `markdown.WriteMeta`, committing via the existing specs-repo write path, and logging to the activity log.
- **Claim/earn hooks** — `cmd/assign.go`'s claim path and `internal/pipeline/transitions.go`'s advance path call into `internal/bounty` to stamp claim/earn. Both are additive: no bounty means no-op.
- **`internal/dashboard` + list/pipeline loaders** — carry a `Bounty` flag (and enough detail for the detail view) onto `DashboardItem`, `pipelineSpec`, and `specListItem`, alongside the existing `StaleFraction`.
- **`internal/tui/theme.go`** — new `Theme.Bounty color.Color` (gold) per theme, plus a shimmer helper reusing the splash's `shimmerIntensity`/`smoothstep` maths. Monochrome `graphite` sets `Bounty` to its brightest luminance step and relies on bold + glyph.
- **Render call-sites** — one shared helper (`renderBountyPrefix(icon, id string, frame int, styles Styles) string`) used by `dashboard.renderRow`, `pipeline.renderPipelineRow`, `speclist.formatRow`, the detail header, and search results, so the treatment cannot drift between surfaces.
- **Animation** rides the **existing** `spinnerTickMsg` (100 ms, `app.go`), which already re-renders the view; no new ticker, no new render loop. Frame count is already in hand at those call-sites via the app model.

**Data flow (ledger):** `spec bounty ledger` reads `specs/` and the archive directory (`config.ArchiveDir`), parses frontmatter, filters `bounty.earned_at` into the requested window, groups by `earned_by`, and prints counts plus contributing spec IDs. **Git is the source of truth**; SQLite is used only as an optional read cache, never as the record.

---

## 5. Design Inputs

**Current state discovered in the codebase (grounding the design):**

- **The claimable queue already exists.** `SPEC.md §L878-883` defines `dashboard.do_scope: assignee` with `claimable: true`, and `cmd/assign.go:106 shouldAutoClaim` / `cmd/assign.go:119 autoClaim` already claim a spec when work starts (`cmd/do.go:72`, `cmd/build.go:83`). A bounty needs no new claim mechanic — it decorates the queue that exists and hooks the claim that already happens.
- **Time-urgency has shipped and owns the whole-row foreground.** `internal/tui/dashboard.go:475-482`, `internal/tui/pipeline.go:321-327`, and `internal/tui/review.go:207` all apply `Theme.RampColor(staleFraction)` to the entire assembled row. Any bounty colour applied to the row would be overwritten by, or overwrite, the urgency signal. This is the direct motivation for **D-1** (bounty owns glyph + ID only).
- **The urgency ramp already claims yellow/amber.** `Theme.urgencyStops()` (`internal/tui/theme.go:44`) derives `[Text, Warning, Warning⇢Error, Error]`, and the splash paints its spark in `t.Warning` (`internal/tui/splash.go:131`). Gold-as-`Warning` would be indistinguishable from a mid-urgency row → a **dedicated `Theme.Bounty` token** is required, not a reuse of `Warning`.
- **The spark glyph and a shimmer implementation already exist.** `internal/tui/glyph/glyph.go:16` defines `Spark = "✦"`, exposed as `IconSpark` (`internal/tui/icons.go:12`); `internal/tui/splash.go` implements a single eased sheen (`shimmerCol`, `shimmerIntensity`, `smoothstep`, `blendColor`) with tuning constants. The bounty shimmer is that code, looped with a pause, applied to a 13-cell span instead of the wordmark.
- **A 100 ms repaint tick already runs.** `spinnerInterval = 100 * time.Millisecond` (`internal/tui/app.go:26`), `App.spinnerTick` (`internal/tui/refresh.go:194`), handled at `app.go:370`. Animation therefore adds **no new ticker and no new wake-ups** — only string building over a few cells per bountied row, bounded by `max_active`.
- **The row glyph is already conditionally overridden.** `dashboard.renderRow` swaps in `IconFocus` for the focused spec (`internal/tui/dashboard.go:434-438`), so a glyph-precedence rule is an established pattern — but it also means focus and bounty now compete for one cell (see Open Questions).
- **Role-gated config has a precedent.** `FastTrackConfig` (`internal/config/config.go:179`) uses `AllowedRoles` + `GetAllowedRoles()` + `IsRoleAllowed()` with a documented default list, and `cmd/advance.go:49` gates on `requireRole`. `BountyConfig` should follow that shape exactly rather than invent a new permission idiom.
- **Frontmatter already carries structured sub-objects.** `SpecMeta` holds `Steps []BuildStep` and `Review *ReviewState` (`internal/markdown/frontmatter.go`), so a `Bounty *BountyState` is idiomatic and round-trips through the existing YAML path.
- **The local store is a per-machine cache, not a team ledger.** `internal/store` opens a local SQLite DB with an append-only `activity` table (`internal/store/db.go:171`) keyed to this machine's history. A quarterly leaderboard must not depend on it → **D-6** (git is the ledger; SQLite may cache).
- **Completion records already live in git.** `pipeline.TerminalStages` (`internal/pipeline/stages.go:98`) plus auto-archive into `config.ArchiveDir` (`internal/config/config.go:789`), which `cmd/history.go` already scans with `markdown.ReadMeta`, gives a durable, cloneable completion corpus. The ledger is a second reader of that same corpus.

**UX intent (from the request):**

- A bountied spec should read like a **rare item**: instantly recognisable, visually "shiny", and *scarce* — the desirability is the point.
- The marker is an **invitation to claim**, not an instruction. It exists so leadership can prioritise by making work attractive rather than by assigning it.
- The reward loop is real but external: `spec` must persist enough truth for a human to run a quarterly leaderboard and fund an actual incentive.
- Known cultural risk, accepted for pilot: bounties belong on **unglamorous but critical** work. If they end up on the fun greenfield specs, the mechanism inverts and starves the boring queue further. Monitored, not enforced.

---

## 6. Acceptance Criteria

- **AC-1 (grant, with reason):** `spec bounty set <id> --reason "..."` records `granted_by`, `granted_at` (RFC3339), and `reason` in the spec's frontmatter, commits through the normal specs-repo write path, and logs an activity event. With `require_reason: true` (default), omitting `--reason` fails with an actionable message.
- **AC-2 (role gate):** Only roles listed in `bounty.grantable_by` (default `[tl, pm]`) may grant or clear. Any other role receives a permission error naming the permitted roles, in the same shape as the existing fast-track/advance permission errors. No spec is modified on rejection.
- **AC-3 (scarcity cap):** With `bounty.max_active: N` and N bounties already active, a further grant fails, lists the currently bountied spec IDs, and tells the user to clear one. The cap counts specs with an active, unearned bounty; earned and cleared bounties do not count.
- **AC-4 (glyph + ID only):** A bountied row renders its leading glyph as `✦` and its SPEC-ID in the theme's `Bounty` colour. The title, stage, assignee, and detail spans are **unchanged** from the non-bountied case, including their time-urgency ramp colour. A bountied, fully-stale row shows a gold spark and ID with a red title.
- **AC-5 (shimmer):** With `bounty.shimmer: true` (default) in an interactive TTY, a sheen sweeps across the glyph+ID span periodically: one eased pass, then a pause, repeating. Consecutive animation frames produce different rendered strings; a non-bountied row's rendering is byte-identical across frames. With `shimmer: false`, the span is static gold.
- **AC-6 (all TUI surfaces):** The same treatment appears on the dashboard rows, the pipeline screen, the spec list, the spec detail header, and search results, produced by a single shared render helper. A given spec reads identically on every surface.
- **AC-7 (reason is discoverable):** The spec detail view displays the bounty's reason, granter, and grant date, and — once claimed/earned — the claimant and earn date.
- **AC-8 (claim stamping):** Claiming a bountied spec through any existing path (`spec assign`, auto-claim on `spec do` / `spec build`) stamps `claimed_by` / `claimed_at` when the bounty is active and unclaimed. Claiming a non-bountied spec behaves exactly as today. A reassignment before earn overwrites `claimed_by` and logs the change.
- **AC-9 (earn stamping, immutable):** Advancing a bountied, claimed spec into a `pipeline.TerminalStages` stage stamps `earned_by` (frozen from `claimed_by`) and `earned_at`. Re-advancing, reverting, or re-archiving never changes an existing `earned_at`. An unclaimed bountied spec reaching terminal records no award.
- **AC-10 (ledger from git):** `spec bounty ledger [--since <date>] [--cycle <label>]` scans the specs directory and the archive, and prints per-person earned-bounty counts with contributing spec IDs, sorted by count descending. Deleting the local SQLite database does not change the output.
- **AC-11 (non-TUI representation):** Plain `spec list` / `spec` output marks bountied rows with `✦`; `--json` output includes a `bounty` object (granter, granted_at, reason, claimed_by, earned_by, earned_at). Non-TTY output is never animated.
- **AC-12 (graceful degradation):** With `NO_COLOR`, on the monochrome `graphite` theme, and on 16/256-colour terminals, a bountied row is still distinguishable (glyph, and bold where supported), with no escape-code artefacts and no broken width/alignment. Column alignment is identical to a non-bountied row (the glyph is mono-width; no extra cells are consumed).
- **AC-13 (disabled + legacy = invisible):** With `bounty.enabled: false` or no `bounty` block, no bounty UI appears anywhere and `spec bounty set` fails with an actionable "enable it in team config" message. Specs with no `bounty` frontmatter render exactly as they do today.
- **AC-16 (`g b` in the TUI):** With a spec row selected on any spec-bearing view, `g b` opens an input modal titled for that spec, pre-filled with the existing bounty reason (empty when unbountied). Submitting text grants or updates the bounty; submitting `-` clears it; submitting an empty value on an unbountied spec is a no-op. A role refusal is reported before the prompt opens (no pointless typing), and a cap refusal arrives through the normal action-result path into the status bar. The binding appears in the help overlay alongside `g a` / `g r` / `g c`.
- **AC-17 (TUI claim parity):** Claiming from the TUI (`g c`) stamps the bounty claimant exactly as the CLI claim path does, so the award record does not depend on which surface the engineer used.
- **AC-14 (no behavioural side effects):** A bounty never changes gates, advancement, sort order, notifications, or any persisted content beyond the `bounty` frontmatter object. Presentation plus one structured field.
- **AC-15 (tests):** Table-driven tests cover `CanGrant` across roles, `CheckCap` at/over/under the cap, the grant→claim→earn transitions (including double-earn, unclaimed-earn, reassignment, and revoke), ledger tallying across window boundaries and across specs + archive, frontmatter round-tripping of `BountyState`, the shimmer helper (frame-varying output, static when disabled, monotone span width), and render snapshots for bountied × {fresh, stale} × {selected, unselected} × {colour, `NO_COLOR`, graphite}.

---

## 7. Technical Implementation

### 7.1 Architecture Notes

- **New pure package `internal/bounty`** holds every rule and no I/O:
  - `func CanGrant(role string, cfg config.BountyConfig) bool` — role-gate check.
  - `func CheckCap(activeIDs []string, cfg config.BountyConfig) error` — returns a rich error carrying the active IDs so `cmd/` can render an actionable message without re-deriving them.
  - `func Grant(meta *markdown.SpecMeta, granter, reason string, now time.Time) error`, `Clear`, `Claim(meta *markdown.SpecMeta, who string, now time.Time) bool`, `Earn(meta *markdown.SpecMeta, now time.Time) bool` — state transitions returning whether anything changed, so callers can skip a pointless commit.
  - `func Tally(metas []markdown.SpecMeta, from, to time.Time) []Award` — ledger aggregation, sorted deterministically (count desc, then handle asc).
  - Imports `internal/config` types and `internal/markdown` types only. If that pairing risks an import cycle (as it did for `internal/urgency` → see that spec's escape hatch), the transition helpers move to methods on `markdown.SpecMeta` and `internal/bounty` keeps only the pure predicates.
- **`BountyState` on `SpecMeta`** (`internal/markdown/frontmatter.go`), as `Bounty *BountyState` with `yaml:"bounty,omitempty"`:

  ```yaml
  bounty:
    granted_by: aaron
    granted_at: 2026-07-28T09:12:00Z
    reason: Unblocks the Q3 billing migration; untouched for three weeks
    claimed_by: priya
    claimed_at: 2026-07-29T22:40:00Z
    earned_by: priya
    earned_at: 2026-08-04T03:15:00Z
  ```

  A nil pointer means "never bountied" — the state of every existing spec, so there is no migration and no backfill.
- **`BountyConfig` on `TeamConfig`**, following `FastTrackConfig`'s shape (`internal/config/config.go:179`) including a `GetGrantableRoles()` default accessor:

  ```yaml
  bounty:
    enabled: true
    grantable_by: [tl, pm]     # default when unset
    max_active: 3              # default when unset
    require_reason: true       # default when unset
    shimmer: true              # default when unset
  ```

  `internal/config/lint.go` gains checks mirroring the existing `stale_after` diagnostics: unknown role names, `max_active < 1`, and non-boolean flags each produce a `SeverityError` diagnostic with a suggestion.
- **`Theme.Bounty color.Color`** in `internal/tui/theme.go` — a dedicated gold token per theme, explicitly **not** `Warning` (which the urgency ramp owns). Light themes take a darker amber-gold for contrast; `graphite` takes its brightest luminance step and leans on bold + glyph, consistent with how that theme conveys every other status.
- **Shimmer helper** in the tui package, extracted from the splash rather than duplicated: `shimmerIntensity`, `smoothstep`, and `blendColor` already exist and are reusable as-is. The bounty variant differs only in its schedule — a looped sweep with a dwell between passes (target: ~1.5 s sweep, ~5 s pause, tuned during implementation) — and is driven by the frame counter already advanced by `spinnerTickMsg`. New tuning constants live beside the existing splash constants.
- **One shared render helper** produces the glyph+ID prefix for every surface. `dashboard.renderRow`, `pipeline.renderPipelineRow`, `speclist.formatRow`, the detail header, and search results call it instead of formatting the icon and ID themselves. This is the only defensible way to satisfy AC-6 without five drifting copies; it also keeps the `IconFocus` override rule in one place.
- **Claim and earn hooks are additive one-liners.** `cmd/assign.go`'s claim path calls `bounty.Claim`; `internal/pipeline/transitions.Advance` calls `bounty.Earn` when the target stage is terminal. Both return "changed?" booleans, so the existing commit-only-if-changed behaviour is preserved and a non-bountied spec takes exactly the path it takes today.
- **Activity logging** uses the existing `store.ActivityLog` with new event types (`bounty_granted`, `bounty_cleared`, `bounty_claimed`, `bounty_earned`) so `spec history` and metrics see bounty churn — deliberately *including* churn, so a granter who reshuffles bounties weekly is visible.

### 7.2 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Bounties end up on already-attractive work, starving the boring queue further | Medium | High | Documented usage principle (bounties are for unglamorous critical work); `max_active` keeps the sample small enough to review; grant/clear churn is in the activity log; explicitly monitored during pilot (owner accepted) |
| Gold reads as "urgent" and collides semantically with the urgency ramp | Medium | Medium | Separate channels by construction (glyph+ID vs row body), dedicated `Theme.Bounty` token distinct from `Warning`, and snapshot tests for bountied × stale combinations |
| Shimmer is distracting or reads as a rendering bug | Medium | Medium | Looped sweep with a long pause rather than a continuous pulse; `bounty.shimmer: false` opt-out; never animated in non-TTY; capped row count bounds total motion on screen |
| Animation cost / battery on the 100 ms repaint | Low | Low | No new ticker (the repaint already happens); work is bounded by `max_active` rows × ~13 cells; measure a frame render before and after |
| Gold indistinguishable on 16-colour or monochrome terminals | Medium | Medium | Meaning carried by the `✦` glyph and bold as well as colour (AC-12); `graphite` ramp defined explicitly; QA on light, `modus-*`, and `graphite` |
| Focus indicator and bounty compete for the single glyph cell | High | Low | Explicit precedence rule needed (see Open Questions); recommendation: focus wins the glyph, bounty keeps the gold ID, so neither signal disappears |
| Earned-bounty record diverges between machines or is lost | Low | High | Git is the sole source of truth (frontmatter + archive); SQLite is cache-only; ledger recomputes from files on every run (AC-10) |
| Ledger becomes a performance/perception problem at scale | Low | Low | Reuses `cmd/history.go`'s archive-scan pattern; ledger is an explicit command, not a dashboard section, so cost is opt-in |
| Reassignment mid-flight makes "who earned it" ambiguous | Medium | Medium | Last claimant before terminal earns it; `earned_by` frozen at earn time and immutable; every reassignment logged (see Open Questions for a stricter rule) |
| Granter bounties their own spec and claims it | Low | Medium | Recommend rejecting self-claim of a self-granted bounty (see Open Questions); at minimum the ledger exposes granter and claimant together |
| The mechanism decays into "the TL wants this" with no payout | Medium | High | The ledger is in scope, not a follow-up; the loop is closed on day one even though the reward itself is external |

### 7.3 Change Package

<!--
One line = one node:  N. [repo:layer] Description (after: …)
repo `spec-cli` must be mapped in ~/.spec/config.yaml workspaces.
Layers are Go (single module); no layer tag needed beyond the repo.
-->

Unlike the time-urgency gradient, this feature **does** split cleanly: each node is independently observable and independently reviewable, and the ledger is where most of the correctness risk lives. Three nodes, stacked:

1. [spec-cli] Bounty core: `BountyConfig` + lint, `BountyState` frontmatter, pure `internal/bounty` package, `spec bounty set|clear|list`, claim/earn hooks, activity events, and `✦` in plain + JSON output
2. [spec-cli] Bounty TUI: `Theme.Bounty` token per theme, looped shimmer helper, shared glyph+ID render helper wired into dashboard, pipeline, spec list, detail header, and search rows, plus the `g b` grant/clear binding and TUI claim parity (after: 1)
3. [spec-cli] Bounty ledger: `spec bounty ledger` aggregation over specs + archive with `--since` / `--cycle` windows (after: 1)

Node 1 is fully usable and testable on its own (bounties exist, are gated, capped, stamped, and visible in plain output). Node 2 owns every TUI surface — both reading the marker and the `g b` verb that sets it — over data and rules node 1 already provides. Node 3 is a pure reader over the same data. Nodes 2 and 3 are independent of each other and can land in either order or in parallel.

**Ordered implementation checklist within node 1:**

- **Config (`internal/config`)** — `BountyConfig` + defaults accessor + lint diagnostics (AC-2, AC-3, AC-13).
- **Frontmatter (`internal/markdown`)** — `Bounty *BountyState`, round-trip tests, nil-safe everywhere (AC-1).
- **Pure logic (`internal/bounty`)** — `CanGrant`, `CheckCap`, `Grant`/`Clear`/`Claim`/`Earn`, `Tally` (AC-1..AC-3, AC-8, AC-9).
- **Commands (`cmd/bounty.go`)** — `set`, `clear`, `list`; role gate, cap error, commit, activity log (AC-1, AC-2, AC-3, AC-9).
- **Hooks (`cmd/assign.go`, `internal/pipeline/transitions.go`)** — claim and earn stamping, additive and no-op when unbountied (AC-8, AC-9, AC-14).
- **Plain/JSON output (`internal/dashboard`, `cmd/list.go`, `cmd/output.go`)** — `✦` marker and `bounty` JSON object (AC-11).
- **Docs** — `CONFIGURATION.md` for the `bounty` block; a short "what bounties are for" note carrying the unglamorous-work principle from §7.2.

---

## 8. Escape Hatch Log

<!-- Populated during implementation when reality diverges from this design. -->

- —

---

## Decision Log

> *Record all significant decisions, questions and changes here for asynchronous reference.*

| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| D-1 | Which spans does the bounty colour own, given time-urgency already owns the whole row? | (1) Whole row gold (overwrites urgency), (2) Gold glyph + SPEC-ID only, urgency keeps title/metadata, (3) A separate right-hand badge column | **DECIDED: (2) glyph + SPEC-ID gold; title and metadata keep the urgency ramp** | Owner decision. The two signals answer different questions ("is this stalled?" vs "is this worth taking?") and must be readable simultaneously; the row body is already claimed by `RampColor`, and a badge column costs horizontal space the compact layout doesn't have | Owner | 2026-07-28 |
| D-2 | Which glyph marks a bounty? | (1) A new star glyph, (2) Reuse the boot-splash spark `glyph.Spark` (`✦`), (3) Emoji | **DECIDED: (2) `glyph.Spark`** | Owner decision. Brand cohesion — the spark is already the product's mark on the boot splash, so gold-spark reads as "spec's own rare item" rather than a new vocabulary. Emoji is barred by the existing no-emoji snapshot invariant | Owner | 2026-07-28 |
| D-3 | Static gold or animated glimmer? | (1) Static gold, (2) Continuous pulse, (3) Periodic eased sheen sweep (looped splash shimmer), (4) Opt-in animation only | **DECIDED: (3) periodic sheen, with `bounty.shimmer: false` opt-out** | Owner decision. The "rare item" affordance depends on motion; a looped single sweep with a pause reads as light catching a surface rather than an alarm. It rides the existing 100 ms repaint tick, so it costs no new wake-ups, and the narrow glyph+ID span bounds the motion | Owner | 2026-07-28 |
| D-4 | How is bounty scarcity enforced? | (1) Convention only, (2) Config cap `max_active` enforced at grant time, (3) Time-based expiry, (4) Both cap and expiry | **DECIDED: (2) config-driven cap** | Owner agreed a cap is required: an uncapped marker debases to `priority: high`. Enforced at grant time with an error listing active bounties, so the granter must make an explicit trade-off. Expiry deferred (see Open Questions) | Owner | 2026-07-28 |
| D-5 | Who may grant a bounty? | (1) TL only, (2) Roles listed in team config, (3) Anyone | **DECIDED: (2) `bounty.grantable_by`, default `[tl, pm]`** | Owner decision. Prioritisation is a TL+PM collaboration per §1, so both are permitted by default; teams can narrow to `[tl]`. Follows the established `FastTrackConfig.AllowedRoles` idiom rather than inventing a new permission model | Owner | 2026-07-28 |
| D-6 | Where does the earned-bounty record live? | (1) Local SQLite `activity`, (2) Spec frontmatter + archive (git), derived on read, (3) A dedicated committed ledger file, (4) External service | **DECIDED: (2) git-derived from frontmatter + archive** | Owner requires a durable per-person record for quarterly review. `internal/store` is a per-machine cache and cannot back a team leaderboard; a separate ledger file would diverge from the specs that are the actual facts. Deriving from frontmatter makes the record self-consistent, reviewable in PRs, and survivable across clones. SQLite may cache, never own | Owner | 2026-07-28 |
| D-7 | Does the payout mechanism belong in this spec? | (1) Model rewards/points in `spec`, (2) Record facts only; reward defined outside the tool, (3) No payout at all | **DECIDED: (2) record facts, `spec bounty ledger` reports them** | Owner intends a real, company-funded quarterly incentive but does not want the reward encoded in the tool. Facts (who claimed, who finished, when) are stable; reward policy is not. This closes the loop without the tool taking a position on prizes | Owner | 2026-07-28 |
| D-8 | Which surfaces show the bounty treatment in v1? | (1) Dashboard only, (2) Dashboard + pipeline, (3) Everywhere a spec row appears in the TUI, (4) Everywhere including plain CLI colour | **DECIDED: (3) every TUI surface, plus a plain `✦` marker in non-TUI output** | Owner intent: the marker must be unmissable wherever the spec surfaces, otherwise an engineer can meet the spec without ever seeing the invitation. Achieved cheaply by one shared render helper; plain output gets the glyph but never animation | Owner | 2026-07-28 |
| D-9 | Does a bounty affect sort order or gates? | (1) Sort bountied specs to the top, (2) No behavioural effect — presentation and record only | **DECIDED: (2) no behavioural effect** | Owner intent is a pull signal, not a queue override. Sorting would make the bounty a push mechanism and duplicate what assignment already does. The shine is the mechanism | Owner | 2026-07-28 |
| D-10 | Does the bounty use the existing claim path or a new `spec claim` verb? | (1) New dedicated verb, (2) Hook the existing claim/auto-claim path | **Proposed: (2) hook the existing path** | `shouldAutoClaim`/`autoClaim` already claim on `spec do` and `spec build`; a second verb would create two ways to take the same spec and let the two disagree. Bounty stamping is additive to the claim that already happens | — | 2026-07-28 |
| D-12 | How is a bounty granted from the TUI? | (1) CLI only, (2) A bare hotkey, (3) `g b` on the existing g-prefix, (4) A settings/detail-view form | **DECIDED: (3) `g b` with a reason prompt** | Owner decision. The `g`-prefix state machine already hosts the deliberate, consequential verbs (`g a` archive, `g r` restore, `g c` assign/claim), which is exactly the register a bounty belongs in — and it keeps every bare letter free. The reason prompt reuses the same input modal as assign, pre-filled with the current reason, with `-` clearing, so no new modal vocabulary is introduced | Owner | 2026-07-28 |
| D-11 | Delivery shape: single change package or stacked? | (1) Single package, (2) Three stacked nodes (core → rendering, ledger) | **Proposed: (2) three nodes** | Node 1 is independently observable via plain output; nodes 2 and 3 are a pure renderer and a pure reader over node 1's data, are independent of each other, and isolate the two riskiest areas (theme/animation, and ledger correctness) for focused review | — | 2026-07-28 |

---

## 9. Open Questions

1. ~~**Colour channel (D-1):**~~ **Resolved:** gold glyph + SPEC-ID; title and metadata keep the urgency ramp.
2. ~~**Glyph (D-2):**~~ **Resolved:** reuse the boot-splash spark `✦`.
3. ~~**Animation (D-3):**~~ **Resolved:** periodic eased sheen on the glyph+ID span, opt-out via config, never in non-TTY.
4. ~~**Scarcity (D-4):**~~ **Resolved:** config-driven `max_active`, enforced at grant time.
5. ~~**Payout (D-6, D-7):**~~ **Resolved:** durable git-derived record plus `spec bounty ledger`; reward policy lives outside the tool.
6. **`g b` scope:** the binding is live wherever a spec row is selected (dashboard, pipeline, spec list, search results, spec detail). Should it also be available on the triage view for an item that has not been promoted yet? Recommendation: no — triage items carry no bounty (they are not specs), so the key should stay inert there rather than silently doing nothing surprising.
7. **Bounty expiry:** should an active bounty expire (end of cycle, or a configurable `expires_after`) so an abandoned plan doesn't leave permanent gold and permanently consume a cap slot? Recommendation: not in v1 — `max_active` already forces the granter back to the list, and expiry adds a clock to reason about. Revisit if pilot shows stale bounties.
8. **Focus vs bounty glyph precedence:** the focused spec already replaces the row glyph with `IconFocus` (`internal/tui/dashboard.go:434`). Recommendation: focus wins the glyph cell (it is the user's own transient pointer) while the SPEC-ID stays gold, so neither signal is lost. Confirm.
9. **Self-dealing:** may a granter claim a bounty they granted? Recommendation: reject with an actionable error, since a self-granted, self-claimed bounty is an unreviewed self-award on the ledger. Alternative: allow it and rely on the ledger exposing granter and claimant side by side.
10. **Reassignment before earn:** if a bountied spec passes between engineers, does the last claimant earn it (recommended: yes, simplest and matches "who finished it"), the first claimant, or nobody? Should a reassignment after substantial progress split or void the award?
11. **Revert after earn:** if an earned spec is reverted out of a terminal stage and later re-completed by someone else, `earned_by` is immutable by AC-9 — is that the right call, or should a revert clear the award and let it be re-earned?
12. **Ledger window semantics:** should `spec bounty ledger` default to the current cycle, the current quarter, or all time? Cycles are team-labelled (`cycle_label`), so a quarter may not map cleanly onto them.
13. **Ledger visibility:** the ledger is TL-run in v1 (D-8 keeps it off the dashboard). Should any role be able to run it, or is it gated to `grantable_by` roles? A visible-to-all leaderboard is a culture decision, not a technical one.
14. **Cleared-after-claim:** if a granter clears a bounty on a spec someone already claimed for it, does the claimant keep a pending award? Recommendation: clearing after a claim is refused with a message pointing at the claimant, so leadership cannot retract an accepted invitation.
15. **Gold token per theme:** does every shipped theme need a hand-picked `Bounty` value, or is a single derived gold (e.g. blended toward the theme's `Warning` with fixed luminance targeting) acceptable for the long tail of themes?
