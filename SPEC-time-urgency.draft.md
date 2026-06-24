---
id: SPEC-XXX
title: Time-Urgency Gradient — Ambient Staleness Pressure on Dashboard Titles
status: draft
version: 0.1.0
author: —
cycle: TBD
epic_key:
repos: [spec-cli]
revert_count: 0
created: 2026-06-24
updated: 2026-06-24
---

# SPEC-XXX — Time-Urgency Gradient

> *Make stalled work feel stalled — colour a task's whole row a little hotter the longer it sits in one stage, so there's gentle pressure to ship or trim scope, without ever asking anyone to estimate.*

---

## TL;DR                             <!-- owner: anyone -->

Tasks with no deadline expand to fill all available time (Parkinson's law) and grow a long, silent tail. We add an **ambient time-pressure signal**: on the **dashboard DO section** and the **pipeline screen**, a task's **whole row** is rendered in its normal primary text colour when fresh, then shifts progressively through yellow → amber → orange → red as it dwells in its current pipeline stage. The intensity follows an **ease-in curve** — it stays cool for most of the window, then ramps harder as the deadline nears, mimicking how real deadline pressure builds. Intensity is driven purely by *how long the task has been in its current stage* as a fraction of a per-stage **stale window** (`stale_after`) configured in the team's pipeline. No effort/complexity estimation is involved — only elapsed dwell time, which is cheap and unambiguous. The goal is a calm, glanceable nudge that gives engineers a felt "call it / trim scope" moment, not a hard gate or a notification.

---

## 1. Problem Statement

Feedback from TUI users converges on one gap: **a task has no felt end-point.** Pipeline stages tell you *where* a task is, but nothing communicates *how long it has been there* or applies any pressure to move it on. The consequences:

- **Work expands to fill the time allotted.** With no deadline and no decay signal, a task can sit in `engineering` or `build` indefinitely while scope quietly grows.
- **No natural "call it" moment.** A deadline gives an engineer a concrete point to declare the task good-enough and trim remaining scope to keep delivery moving. Without one, the cut-scope decision never gets triggered.
- **Long, invisible tails.** A task that should take two days drifts to two weeks, and nothing on the dashboard ever escalates to reflect that drift.

Crucially, the usual fix — estimating effort, story points, or due dates — is **repeatedly fraught with inaccuracy** and adds ceremony the product is built to avoid. We want the *pressure* a deadline provides **without** the *estimation* a deadline requires.

There is also existing, **dormant** machinery here: `dashboard.stale_threshold` (default `48h`) and a `DashboardItem.Urgency` field with `stale`/`critical` styling already exist, but `dashboard.Aggregate` never sets `Urgency` on DO items, so today an active task never actually surfaces as stale. This spec turns that latent binary concept into a continuous, per-stage, configurable gradient.

---

## 2. Goals & Non-Goals

### Goals

- Render a task's **whole row** on the dashboard DO section and the pipeline screen with a colour whose intensity reflects the **fraction of its stage's stale window already elapsed**, ramping primary-text → yellow → amber → orange → red along an **ease-in curve**.
- Drive the signal entirely from **elapsed dwell time in the current stage** — no effort/size/complexity input.
- Let teams configure the stale window **per stage** (`stale_after`), because `triage` dwell and `build` dwell mean very different things.
- Make the signal **per-stage opt-in**: a stage with no `stale_after` applies **no colouring at all** (the absence of a window means "this stage is never stale"). There is no global fallback window.
- Make the **easing curve team-configurable** (`dashboard.urgency.easing`) so a team can dial how aggressively the colour ramps (`linear` / `ease-in` / `ease-in-strong`), defaulting to `ease-in`.
- Degrade gracefully: respect `NO_COLOR`, monochrome/low-colour terminals, and themes without a hot palette (e.g. `graphite`).

### Non-Goals

- **No effort or due-date estimation.** We never ask for or store an estimate.
- **No notifications, gates, or auto-actions.** This is a passive visual signal only; it never blocks advancement or pings anyone. (Time-based *warnings*/*gates* already exist separately and are untouched.)
- **No reordering of work by urgency in v1** beyond what already exists. (Existing urgency sort may be re-derived from the new signal, but inventing new sort behaviour is out of scope — see Open Questions.)
- **Not applied to REVIEW/INCOMING in v1.** PR review age and triage priority follow different time models; this spec scopes to stage-dwell on the **dashboard DO section** and the **pipeline screen** (see §4).
- **Not applied to the spec-list or spec-detail views in v1.** Could follow once the core surfaces land.
- **No persistent history/analytics of dwell time.** We compute from a single timestamp at render time; we do not build a time-in-stage report (could be a follow-up).

---

## 3. User Stories

| # | As a... | I want to... | So that... | Acceptance Criteria |
|---|---|---|---|---|
| US-1 | engineer | see a task's row get visibly "hotter" the longer it sits in my current stage | I get a felt nudge to ship or trim scope without anyone setting a deadline | DO-section and pipeline-screen row colour interpolates from primary text toward red along an ease-in curve as stage-dwell approaches `stale_after`; verified at 0%, ~50%, ~100%+ |
| US-2 | tech lead | configure a different stale window for each pipeline stage | `triage` can go hot in a day while `build` is given a week, matching how my team actually works | `stale_after` is settable per stage and used to compute that stage's gradient; lint validates the value |
| US-3 | tech lead | apply the pressure signal only to the stages where it makes sense | `done`, `monitoring`, and `closed` (which simply have no `stale_after`) never nag people about work that is intentionally parked | a stage with no `stale_after` renders rows in plain primary text regardless of dwell |
| US-4 | engineer on a low-colour / NO_COLOR terminal | still read the dashboard cleanly | the urgency gradient never produces unreadable or broken output | with `NO_COLOR` set or a 16-colour terminal, rows render legibly (mono fallback) with no escape-code corruption |
| US-5 | engineer | have the urgency reflect real dwell, not my last keystroke | tweaking a spec's wording doesn't reset its staleness and hide a genuinely stuck task | dwell is measured from stage entry, not last edit (see §5 / Decision D-1) |

---

## 4. Proposed Solution

### 4.1 Concept Overview

For each task shown in the dashboard **DO** section **and the pipeline screen**:

1. Determine **dwell** = `now − stageEnteredAt` (time the task has spent in its *current* stage).
2. Determine the stage's **stale window** `W` = `stage.stale_after`. **If the stage has no `stale_after`, it is never stale — skip colouring entirely** (render the row in plain `Text`). There is no global fallback.
3. Compute the raw **fraction** `r = clamp(dwell / W, 0, 1)`, then apply an **ease-in** curve `f = ease(r)` (e.g. `r²` or `r³`) so the colour stays cool for most of the window and intensifies sharply near the end.
4. Map `f` onto an ordered **urgency ramp** (primary text → yellow → amber → orange → red) and render the **whole row** (id, title, detail) in the interpolated foreground colour.

The ramp is *progressive*: at `r≈0` the row is indistinguishable from a normal row; with ease-in it stays subtle through the early/middle of the window, then builds and reaches full red at/after `r=1`. Past `100%` the colour clamps at red (optionally bold) — it does not keep escalating.

```
dwell/stale_after:  0% ────────── 50% ────────── 100%+         (raw r; f = ease(r))
row colour:       [Text]   [Text~]   [amber]   [orange]   [red]
                          └ ease-in keeps it cool early, hot late ┘
```

### 4.2 Architecture / Approach

The feature is **purely local and read-only** at render time — no adapters, git, network, or SQLite. It touches six existing packages plus one small new pure-logic package, all consistent with `AGENTS.md` (thin `cmd/`, engines depend on interfaces, no new I/O concerns):

- **`internal/config`** — add `StaleAfter` to `StageConfig`; add an `Urgency UrgencyConfig` block to `DashboardConfig` carrying `Easing string` (`linear`/`ease-in`/`ease-in-strong`, default `ease-in`); add a duration parser that understands `d`/`w` (Go's `time.ParseDuration` does not); lint-validate both the new duration field and the easing enum. **No global window is read** — `dashboard.stale_threshold` is not a fallback for this feature.
- **`internal/markdown`** — add `StageEnteredAt` (RFC3339) to `SpecMeta` (see Decision D-1).
- **`internal/pipeline/transitions.go`** — stamp `StageEnteredAt` on every `Advance`/`Revert` so dwell is measured from stage entry, not last edit.
- **`internal/urgency`** *(new, pure)* — `Fraction(dwell, window) float64` (raw clamp), `Ease(r) float64` (ease-in curve), and the dwell/window resolution helpers. No I/O; fully table-testable. (Named for what it does, per the "no `util`/`helper`" rule.)
- **`internal/tui/theme.go`** — add an ordered **urgency ramp** to `Theme` and a `RampColor(f float64) color.Color` that interpolates in RGB across the ramp stops; provide per-theme ramps (hot palettes for colour themes, luminance ramp for `graphite`, identity/no-op for `NO_COLOR`).
- **`internal/tui/dashboard.go` (`renderRow`)** — carry the computed `f` on the dashboard row and apply `RampColor(f)` as the whole-row foreground; reconcile with the existing `urgency`/selection styling.
- **`internal/tui/pipeline.go` (`renderPipelineRow`)** — the pipeline screen already groups specs by stage and styles the whole row (`RowNormal`/`RowSelected`); apply the same `RampColor(f)` foreground. `pipelineSpec` gains `StageEnteredAt`, and the stage's `stale_after` is known from its column.
- **`internal/dashboard`** — `Aggregate` carries the computed `f` (and `StageEnteredAt`) onto `DashboardItem`.

**Data flow:** `dashboard.Aggregate` (and the pipeline data load) read each spec's `StageEnteredAt` and the resolved pipeline stage's `stale_after`. **If the stage has no `stale_after`, no fraction is computed and the row stays neutral.** Otherwise it computes `f = Ease(Fraction(dwell, W))` via `internal/urgency` and stores it on the item (e.g. a `StaleFraction float64`, with a sentinel/absent value meaning "no window"). The TUI render functions map `f` → colour via the theme ramp and style the whole row. The CLI/non-TUI `dashboard.Render` may apply a coarse banded variant or skip the gradient (decision in Open Questions).

---

## 5. Design Inputs

**Current state discovered in the codebase (grounding the design):**

- **Dwell time today is derived from `meta.Updated`** (`internal/pipeline/gates.go` duration gate: `time.Since(parse(meta.Updated))`). `Updated` is **day-granularity** (`"2006-01-02"`) and is **reset on every frontmatter write** (`markdown.WriteMeta` and `transitions.Advance/Revert` all stamp `meta.Updated = now`). It therefore tracks *last modification*, not *stage entry* — editing a spec resets it. This is too coarse and semantically wrong for a dwell signal → motivates **Decision D-1** (`stage_entered_at`).
- **`dashboard.stale_threshold` (default `48h`)** exists in `DashboardConfig` but is **not wired to DO items**; `dashboard.Aggregate` never sets `Urgency`. This feature does **not** adopt it as a fallback window — `stale_threshold` is a separate, global, binary concept and is left untouched. The gradient is driven solely by per-stage `stale_after`. (Whether to later retire or reconcile the dormant `stale_threshold` path is out of scope here — see Open Questions.)
- **`DashboardItem.Urgency`** (`"normal"|"stale"|"critical"`) and `renderRow`'s switch (`RowSelected` > `Error` (critical) > `Warning` (stale) > `RowNormal`) are the dashboard integration point; **`renderPipelineRow`** in `internal/tui/pipeline.go` is the pipeline-screen integration point (it already styles the whole row via `RowNormal`/`RowSelected` and groups specs by stage). Both gradients must coexist with the **selected-row** highlight (`RowSelected`: surface background + bold).
- **`Theme`** exposes 10 semantic `color.Color` tokens including `Warning` (yellow/amber-ish) and `Error` (red) but **no intermediate amber/orange**. The named four-stop ramp can't be expressed by `Warning`/`Error` alone → motivates an explicit ordered ramp token.
- **`time.ParseDuration` rejects `d`/`w`.** `WarningConfig.After` examples use `"5d"`, and `formatDuration` *prints* days, but nothing parses them. A small duration parser supporting `d`/`w` is required for ergonomic `stale_after: 5d`.
- **Themes vary widely:** light themes, accessibility themes (`modus-*`), and a bespoke **monochrome `graphite`** that conveys status by luminance only. The ramp must be defined per-theme, not as fixed global hex.

**UX intent (from the request):**

- The colouring must be **gentle and progressive**, not a sudden flip — it should read as ambient pressure, not an alarm. An **ease-in** curve serves this: cool for most of the window, intensifying near the deadline.
- It applies to the **whole task row** on the DO section and the pipeline screen.
- The mechanism deliberately avoids estimation; **elapsed dwell only**.

---

## 6. Acceptance Criteria

- **AC-1 (gradient by fraction, eased):** Given a DO/pipeline task in a stage with `stale_after: W`, its **whole row** renders in primary `Text` at dwell `0`, follows the configured easing curve through the window, and reaches the ramp's terminal red at dwell `≥ W`. The mapping is monotonic non-decreasing in dwell; under the default `ease-in` curve, early dwell changes colour less than late dwell.
- **AC-2 (per-stage window):** `stale_after` is honoured per stage; two tasks at the same dwell but in stages with different `stale_after` show different intensities.
- **AC-3 (no window = never stale):** A stage with **no** `stale_after` (and equivalently `none`/`0`) renders all its rows in plain `Text` regardless of dwell. There is no global fallback window; the absence of a configured window means the stage is never stale.
- **AC-3b (both surfaces):** The gradient appears on **both** the dashboard DO section and the pipeline screen, driven by the same `stage_entered_at` + `stale_after` inputs, so a given task reads at the same intensity on either screen.
- **AC-4 (dwell source):** Dwell is computed from `stage_entered_at` (Decision D-1). Advancing or reverting a spec resets the gradient to cold; editing the spec body/frontmatter does **not** reset it. (If D-1 is rejected in favour of reusing `updated`, this AC inverts — see Decision log.)
- **AC-5 (clamp):** Dwell beyond `W` does not escalate past the terminal red; `f` is clamped to `[0,1]`.
- **AC-6 (graceful degradation):** With `NO_COLOR` set, titles render with no colour and no escape-code artefacts. On the `graphite` theme, the ramp is a luminance ramp (no hue). On 256/16-colour terminals, lipgloss down-samples without corruption.
- **AC-7 (selection precedence):** A selected row keeps its selection affordance (background + bold) while still conveying urgency (the row retains its ramp foreground over the selection background); it is never rendered illegibly (no hot-on-hot). Holds on both surfaces.
- **AC-8 (validation):** `spec` config lint rejects an unparseable `stale_after` with an actionable message naming the stage and accepted formats (e.g. `30m`, `48h`, `5d`, `2w`, `none`).
- **AC-9 (duration parsing):** `stale_after` accepts `m`, `h`, `d`, `w` units (and `none`/`0`); `5d` is parsed as 120h.
- **AC-9b (configurable easing):** `dashboard.urgency.easing` selects the curve applied to the raw fraction. `linear` leaves it unchanged; `ease-in` (default, when unset) and `ease-in-strong` keep rows cooler early and intensify late. With the same dwell and window, `ease-in-strong` is never hotter than `ease-in`, which is never hotter than `linear`. Lint rejects an unrecognised value with an actionable message listing the accepted curves.
- **AC-10 (no behavioural side effects):** The gradient never changes advancement, gates, sorting semantics that callers depend on, or any persisted spec content beyond `stage_entered_at` on transition. Pure presentation + one timestamp.
- **AC-11 (tests):** Table-driven tests cover `urgency.Fraction` (cold/mid/hot/over/zero-window/opt-out), `urgency.Ease` across all curves (monotonic, `ease(0)=0`, `ease(1)=1`, eased curves convex, ordering `strong ≤ ease-in ≤ linear`), the duration parser, the easing-enum resolution + lint validation, ramp interpolation at representative `f`, and dashboard + pipeline render snapshots/assertions at low/mid/high urgency.

---

## 7. Technical Implementation

### 7.1 Architecture Notes

- **New pure package `internal/urgency`** keeps the math out of the TUI and the config packages and makes it trivially testable:
  - `func Fraction(dwell, window time.Duration) float64` → `clamp(dwell/window, 0, 1)`; returns `0` when `window <= 0`.
  - `func Ease(r float64, curve Curve) float64` → applies the **team-configured** easing curve. `Curve` is a small enum (`Linear` → `r`, `EaseIn` → `r²`, `EaseInStrong` → `r³`) resolved from `dashboard.urgency.easing`; `EaseIn` is the default when unset. All curves satisfy `Ease(0)=0`, `Ease(1)=1` and are monotonic; the eased ones are convex (gentle early, sharp late). The string→`Curve` resolution and its default live in `internal/config` (validated by lint), so `internal/urgency` stays a pure function of an already-resolved enum.
  - `func Window(stage config.StageConfig) (time.Duration, bool)` → returns the stage's `stale_after`; `bool=false` means the stage has no window (never stale). No global fallback is consulted.
  - This package imports `internal/config` types only (no I/O), satisfying "accept interfaces / pure logic" guidance.
- **`StageEnteredAt` (Decision D-1)** is added to `SpecMeta` as `stage_entered_at` (RFC3339, optional). `transitions.Advance` and `transitions.Revert` set it to `time.Now().UTC()`. When absent (legacy specs), fall back to `meta.Updated` parsed as date so the feature degrades rather than breaking; surface this as the documented migration path.
- **Theme ramp** lives entirely in `internal/tui/theme.go` (the file's stated invariant: "No hardcoded colour values outside this file"). Add `Theme.UrgencyRamp []color.Color` (ordered cold→hot, ≥2 stops) and `func (t Theme) RampColor(f float64) color.Color` doing piecewise-linear RGB interpolation across stops. Default/most themes: `[Text, yellow, amber, orange, Error]`. `graphite`: luminance steps. The first stop is always the theme's `Text` so `f≈0` is invisible.
- **Render change** touches two render functions, each applying the ramp as the **whole-row foreground**:
  - `renderRow` in `internal/tui/dashboard.go` — wrap the assembled DO row in `RowNormal.Foreground(theme.RampColor(f))` (or equivalent) instead of plain `RowNormal`.
  - `renderPipelineRow` in `internal/tui/pipeline.go` — same treatment; the stage column already gives the stage and thus its `stale_after`.
  - Selection still applies `RowSelected` (surface background + bold); the ramp foreground is composed over it so the urgency stays visible while selected (precedence rule per AC-7).
- **Reconciliation with `Urgency`:** derive the legacy `Urgency` string from `f` for any code that still needs the discrete value (e.g. icon `⏰`, existing sort): `f>=1 → "stale"`. This removes the dormant/never-set gap without inventing new sort behaviour.

### 7.2 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `stage_entered_at` absent on existing specs → wrong/zero dwell | High (all legacy specs) | Medium | Fall back to `updated` date when the field is missing; document a one-time backfill; treat missing as "cold" rather than erroring |
| Fixed hot hues clash with light/low-contrast themes | Medium | Medium | Define the ramp per-theme in `theme.go`; never hardcode hex in render code; QA on light + `modus-*` + `graphite` |
| Gradient reads as noisy/alarming rather than "gentle" | Medium | Medium | Ease-in curve keeps rows cool for most of the window; first stop = `Text`; only reach full red at `r≥1`; tune the ease exponent and stop positions |
| Whole-row colouring fights the selection background / muted detail | Medium | Medium | Compose ramp foreground over `RowSelected`; QA selected+hot per theme (AC-7); keep detail readable (it shares the row foreground) |
| Selection highlight + hot row becomes unreadable | Medium | Low | Precedence rule (AC-7); QA the selected+hot combination per theme |
| Continuous truecolor interpolation degraded on 16-colour terms | Low | Low | Rely on lipgloss colour down-sampling; provide banded fallback if needed; verify `NO_COLOR` path |
| Editing-resets-clock semantics surprise users (if D-1 rejected) | Medium | Medium | Prefer D-1 (`stage_entered_at`); if reusing `updated`, document the behaviour explicitly |

### 7.3 Change Package

<!--
One line = one node:  N. [repo:layer] Description (after: …)
repo `spec-cli` must be mapped in ~/.spec/config.yaml workspaces.
Layers are Go (single module); no layer tag needed beyond the repo.
-->

This feature ships as a **single, self-contained change package** — one node, one PR. It is intentionally not stacked: every part (config field, frontmatter field, pure `urgency` package, theme ramp, and the two render call-sites) is small, lives in `spec-cli` alone, has no external dependency, and is only meaningful once the whole chain is wired end-to-end. Splitting it would create PRs that compile but render nothing observable until the last one lands, so the review value of stacking is negative here. The DAG is a single root node:

1. [spec-cli] Time-urgency gradient: per-stage `stale_after` + configurable easing config, `stage_entered_at` frontmatter, pure `internal/urgency` package, theme ramp, and whole-row colouring on the dashboard DO section and pipeline screen

**Ordered implementation checklist within the package** (not separate PRs — a build/review sequence for the one node):

- **Config (`internal/config`)** — add `StaleAfter` to `StageConfig`; add `DashboardConfig.Urgency.Easing` (`linear`/`ease-in`/`ease-in-strong`, default `ease-in`) and its string→`Curve` resolution; add the `m/h/d/w/none` duration parser; extend lint to validate both (AC-8, AC-9, AC-9b).
- **Frontmatter (`internal/markdown`, `internal/pipeline/transitions.go`)** — add `stage_entered_at` to `SpecMeta`; stamp it on `Advance`/`Revert`; `updated`-date fallback for legacy specs (AC-4).
- **Pure logic (`internal/urgency`)** — `Fraction`, `Ease(r, curve)`, and per-stage `Window` resolution (no global fallback; missing window = never stale) (AC-2, AC-3, AC-5).
- **Theme (`internal/tui/theme.go`)** — `Theme.UrgencyRamp` + `RampColor(f)` interpolation; per-theme ramps incl. `graphite` luminance ramp and `NO_COLOR`/low-colour handling (AC-6).
- **Data (`internal/dashboard`)** — carry `StaleFraction` + `StageEnteredAt` onto `DashboardItem` in `Aggregate`; derive the legacy `Urgency` string from `f`.
- **Render (`internal/tui/dashboard.go`, `internal/tui/pipeline.go`)** — apply the whole-row ramp foreground in `renderRow` and `renderPipelineRow` with the selection-precedence rule; carry `StageEnteredAt` into `pipelineSpec` (AC-1, AC-3b, AC-7).
- **Docs** — `CONFIGURATION.md`: `stale_after`, `dashboard.urgency.easing`, theme-ramp note, and migration/backfill guidance.

---

## 8. Escape Hatch Log

- **2026-06-24 (implementation):** §7.1 sketched `urgency.Window(stage config.StageConfig)` living in the `urgency` package. Implementing it that way would force `internal/urgency` to import `internal/config`, while `internal/config` must import `internal/urgency` to resolve `dashboard.urgency.easing` → `Curve` (and lint it) — an import cycle. **Resolution:** kept `internal/urgency` fully pure (stdlib only). Window resolution moved to `config.StageConfig.StaleWindow()`, and the combined dwell→fraction helper lives in `internal/dashboard.StageUrgency` (which already depends on both config and urgency and is shared by the dashboard and pipeline render paths). Behaviour is identical to the spec; only the home of the resolution helper changed.

---

## Decision Log

> *Record all significant decisions, questions and changes here for asynchronous reference.*

| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| D-1 | What measures "time in current stage" for the dwell signal? | (1) Reuse `meta.Updated` (cheap; day-granularity; **resets on every edit/write**), (2) Add `stage_entered_at` RFC3339 stamped on advance/revert (accurate; needs frontmatter field + migration) | **DECIDED: (2) add `stage_entered_at`** | The signal must reflect *stage dwell*, not *last keystroke*. `updated` is reset by `WriteMeta` on any change, so a frequently-tweaked-but-stuck task would never go hot. `stage_entered_at` is the correct, sub-day-accurate source and also improves the existing duration gate. Legacy specs fall back to `updated` until they next transition. | Owner | 2026-06-24 |
| D-2 | Fixed hot hues vs theme-derived ramp? | (1) Hardcode yellow/amber/orange/red hex, (2) Per-theme ordered `UrgencyRamp` in `theme.go`, anchored on `Text`…`Error` | **Proposed: (2)** | `theme.go` already forbids hardcoded colour elsewhere; light/mono/accessibility themes need bespoke ramps; honours the four-stop intent while staying theme-correct | — | 2026-06-24 |
| D-3 | Continuous interpolation vs discrete bands? | (1) Continuous RGB lerp across ramp stops, (2) N discrete bands | **Proposed: (1)** in TUI; coarse bands acceptable for non-TUI `Render` | "Gentle and progressive" reads best as continuous; lipgloss down-samples for low-colour terms | — | 2026-06-24 |
| D-4 | Default-on (global fallback) vs per-stage opt-in? | (1) All stages get the gradient via `stale_threshold` fallback unless opted out, (2) Only stages with explicit `stale_after` get it | **DECIDED: (2) per-stage opt-in** | Owner decision: a stage with no `stale_after` is **never stale** and shows no colour. There is no global fallback window; `stale_threshold` is not consulted. Keeps the signal intentional and avoids nagging on stages a team hasn't opted in. | Owner | 2026-06-24 |
| D-5 | Scope of surfaces in v1 | (1) DO section only, (2) DO + REVIEW + INCOMING, (3) DO + pipeline screen, (4) DO + spec list + pipeline | **DECIDED: (3) DO section + pipeline screen** | Both are stage-dwell views and share the exact same inputs (`stage_entered_at` + `stale_after`), so the signal is consistent across them. REVIEW (PR age) and INCOMING (triage priority) use different time models and stay out of v1. | Owner | 2026-06-24 |
| D-6 | Colour the title only or the whole row? | (1) Title span only, (2) Whole row (id + title + detail) | **DECIDED: (2) whole row** | Owner decision: a hot row is more glanceable than a hot title alone; the ramp foreground is composed over the selection background so selected+hot stays legible (AC-7). | Owner | 2026-06-24 |
| D-7 | Ramp shape: linear or eased? | (1) Linear `f = r`, (2) Ease-in `f = ease(r)` (cool early, sharp late), (3) Eased **and team-configurable** | **DECIDED: (3) configurable, default ease-in** | Owner decision: ease-in better mimics real deadline pressure and is the default, but the exact curve is team-configurable via `dashboard.urgency.easing` (`linear`/`ease-in`/`ease-in-strong`). Teams that want a steadier or more aggressive ramp tune it without code changes; `internal/urgency.Ease` takes the resolved curve. | Owner | 2026-06-24 |
| D-8 | Delivery shape: stacked PRs or a single change package? | (1) Stacked PRs (one per layer, dependency-ordered), (2) A single self-contained change package (one node/PR) | **DECIDED: (2) single change package** | Owner decision: the change is small, confined to `spec-cli`, with no external dependency; intermediate slices compile but render nothing observable until the last lands, so stacking adds review overhead with no isolation benefit. §7.3 is one DAG node with an internal build/review checklist. | Owner | 2026-06-24 |

---

## 9. Open Questions

1. ~~**Dwell source (D-1):**~~ **Resolved:** add `stage_entered_at`; legacy specs fall back to `updated` until they next transition.
2. ~~**Default-on vs opt-in (D-4):**~~ **Resolved:** per-stage opt-in — no `stale_after` means never stale, no global fallback.
3. ~~**Title-only vs whole-row (D-6):**~~ **Resolved:** colour the whole row.
4. ~~**Surfaces (D-5):**~~ **Resolved:** dashboard DO section + pipeline screen for v1.
5. ~~**Ramp shape (D-7):**~~ **Resolved:** eased and **team-configurable** via `dashboard.urgency.easing` (`linear`/`ease-in`/`ease-in-strong`), default `ease-in`. The named curves map to fixed exponents (`r`/`r²`/`r³`); whether to expose a raw exponent instead of named curves is a minor follow-up, not a blocker.
6. ~~**Delivery shape (D-8):**~~ **Resolved:** single change package (one PR), not stacked.
7. **Past-deadline treatment:** Clamp at red only (recommended), or add a subtle bold/blink/marker once `f ≥ 1` to distinguish "at deadline" from "well over"?
8. **Non-TUI `dashboard.Render` (plain CLI):** Apply a coarse banded colour, an `⏰`/`🔥` glyph, or leave it text-only? (Pipes/CI should stay plain.)
9. **Per-stage default windows:** Should built-in pipeline presets ship with sensible `stale_after` values per stage (e.g. `triage: 1d`, `build: 5d`), or leave all windows to teams?
