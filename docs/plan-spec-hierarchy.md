# Spec Hierarchy — Design Document

Status: draft · Owner: tl · Scope: a two-level parent/child relationship between
specs, so one initiative spec can carry vision and direction for the deliverable
slices executed beneath it

## 1. Problem

The pipeline has exactly one unit of work: a spec. A spec is sized for
*execution* — it carries a PR stack plan (`SPEC.template.md` §7.3), gates on
acceptance criteria, and terminates in `done`. That is the right shape for a
deliverable.

It is the wrong shape for an initiative. When a body of work is large enough to
need slicing, the vision that justifies it has nowhere to live, so it gets
handled in one of three bad ways:

1. **Copy-paste.** Each slice restates the rationale in §1/§4. Five copies, five
   drift trajectories, no authority about which is current.
2. **One giant spec.** The vision is coherent but the spec is unexecutable —
   a §7.3 DAG spanning months, gates that can't be satisfied incrementally, and
   a single `status:` for work that is simultaneously `done` and `draft`.
3. **Out of band.** The vision lives in a Confluence page or a Slack thread that
   no gate, no dashboard and no coding agent can see.

Nothing in the codebase relates one spec to another. `SpecMeta`
(`internal/markdown/frontmatter.go`) has `EpicKey`, but that points *outward* to
a PM tool; there is no spec-to-spec edge. `TriageMeta.LinkedSpec` is the only
adjacent concept and it is a promotion pointer, not a hierarchy.

The consequence for AI-assisted execution is the sharpest one. `spec build`
hands a slice's §7.3 to a coding agent, and the agent sees only that slice. The
"why" — the part that stops it building a locally-sensible, globally-wrong
thing — is precisely what has no home.

## 2. Goals

- A spec may declare a **parent spec** in frontmatter. Two levels: initiative
  above, deliverable slices below.
- An initiative's completion is **earned**, not asserted — it can be gated on
  its children reaching terminal stages.
- **Both directions are legible**: from the initiative, see slice progress; from
  a slice, see the initiative.
- A slice's coding agent **inherits the initiative's rationale** automatically,
  so vision is written once and never copy-pasted.
- The relationship **projects into the PM tool**: initiative → epic, slice →
  task on that epic.
- **Zero migration for existing specs.** A spec with no parent behaves exactly
  as it does today, including every gate.

## 3. Non-Goals

- **No new document type.** An initiative is a `SPEC-NNN` like any other. A
  second type would duplicate ID claiming, listing, search indexing, archiving
  and sync for one field's worth of meaning (Decision 1).
- **No `kind:` discriminator.** "Is this an initiative?" is *derived* from having
  children, never declared. A declared kind is a second source of truth that can
  disagree with the graph.
- **No nesting beyond two levels, and no multi-parent specs.** Structurally
  refused, not merely discouraged (Decision 3).
- **No initiative-specific template.** Initiatives use the standard spec
  template (Decision 10).
- **No rewrite of any team's `pipeline.stages` config on upgrade.** The gate that
  makes initiatives closable is opt-in (Decision 4).
- **No hierarchy nesting in the Dashboard or Pipeline TUI views.** Those views
  sort by urgency and stage respectively; a tree fights both (Decision 6).
- **No automatic stage mutation of one spec by another.** Ever (Decision 2).

## 4. Decisions

| # | Question | Decision | Rationale |
|---|---|---|---|
| 1 | New document type, `kind:` discriminator, or plain frontmatter link? | **Plain link.** A `parent:` field on the child. Any spec may be a parent. | Simplest thing that expresses the relationship. A new type duplicates the entire spec surface; a `kind:` field is a second source of truth beside the graph itself. |
| 2 | Where is the parent/child invariant enforced? | **At link time.** Attaching a child to a terminal-stage parent is refused, naming `spec revert` as the escape. The `children_complete` gate is a point-in-time check, matching every existing gate. | `children_complete` is non-monotonic — it can flip back to false after a parent has closed. Enforcing at the mutation point makes "parent done with an incomplete child" unreachable by construction, rather than detectable after the fact. |
| 3 | How deep, and how many parents? | **Two levels, one parent.** A spec with a parent may not become one; a spec with children may not gain a parent; no self-parenting. All refused at link time. | This is the shape the workflow actually needs, and enforcing it is *cheaper* than the alternative: three one-line checks replace a cycle detector, recursive traversal, and depth-ambiguous rollup rendering. Relaxing later doesn't change the field. |
| 4 | How does a parent get past the delivery-only gates? | **New `children_complete` gate predicate**, composed by teams via the existing `any:` operator. Passes iff the spec has **≥1 child and every child is terminal**. Childless → **false**. Opt-in; no default config rewrite. | An initiative has no PR stack, so `pr_stack_exists` and `prs_approved` wedge it permanently. The `≥1 child` condition is the safety catch: the predicate can never waive a delivery gate for an ordinary spec, so shipping it is risk-free before anyone adopts it. Vacuous-true would silently disable `pr_stack_exists` repo-wide. |
| 5 | Broken parent link — error or warning? | **Split.** Dangling parent and self-parent are **errors**. Depth >2, terminal parent and archived parent are **warnings**. | A dangling or self-referential parent makes every downstream query undefined. But blocking an engineer's advance because the initiative closed last week punishes the wrong person at the wrong moment — and would let the feature retroactively wedge specs that predate its rules. |
| 6 | Which surfaces show hierarchy? | `spec status` (both directions), `spec list --parent`, and the **Specs** TUI view (nested). Dashboard and Pipeline rows get a **marker glyph** only. | Hierarchy is structure, so it belongs in the structural browser. Pipeline groups by stage: an initiative in `engineering` with children in `build` and `done` cannot nest without lying about stage. Dashboard sorts by personal urgency, which re-parenting would scramble. |
| 7 | Does the link project into the PM tool? | **Yes.** Initiative → epic, slice → **task** on that epic. | Author decision, overriding the recommendation to defer PM projection. It is the shape the team's board needs, and Epic → Task → Sub-task is a standard Jira hierarchy. |
| 8 | When is a child's PM object type decided? | **Once, at first PM sync. Never converted.** A spec with `parent:` set at first sync becomes a task under the parent's epic. A spec that is *already* an epic and later gains a parent **keeps its epic** and is linked to the parent, with a warning. | A Jira epic cannot be cleanly converted to a task — issue-type "Move" is lossy and provider-specific, and `pm_queue` replays operations on retry. A replayed lossy Move is how issue history gets destroyed. Degrading to a visible link is strictly safer. |
| 9 | Does spec store the PM object's type? | **No.** Rename `epic_key` → **`pm_key`** and store nothing else. The adapter resolves type from the key. | Author decision, overriding a `pm_kind` field. Correct per AGENTS.md adapter isolation: Jira's issue-type taxonomy is provider knowledge belonging in `internal/adapter/jira/`, not in spec frontmatter. It also auto-resolves story-vs-sub-task for a parented child's build steps — the adapter's call, not the spec's. |
| 10 | Does an initiative get its own template? | **No.** Standard template. | An initiative legitimately *should* state success criteria (§6) and design inputs (§5), so those gates are real work, not friction. The only meaningless section is §7.3, already handled by Decision 4. Keeping one template also means the child list stays derived from the graph rather than duplicated in a hand-maintained section that can drift. |
| 11 | Does a slice's coding agent see the initiative? | **Yes — TL;DR + §1 Problem + §4 Proposed Solution only**, delimited and read-only. | The payoff of the whole feature: vision written once, inherited by every slice. Bounding it excludes the parent's §6/§7, which describe nothing the child should implement — a child agent reading the parent's §7 may build the wrong slice. |

## 5. Data model

### 5.1 Frontmatter

One new field on `markdown.SpecMeta` (`internal/markdown/frontmatter.go`):

```go
// Parent is the ID of the initiative spec this spec is a deliverable slice of
// (e.g. "SPEC-004"). Empty means the spec stands alone, which is the state of
// every spec written before hierarchy existed. A spec may have at most one
// parent, and the tree is exactly two levels deep: a spec with a parent may
// not itself be a parent (enforced at link time, warned in `spec validate`).
Parent string `yaml:"parent,omitempty"`
```

`omitempty` plus the empty default means **every existing spec parses and
validates unchanged**, and specs never touched by this feature are
byte-identical after a round-trip through `WriteMeta`.

### 5.2 `epic_key` → `pm_key`

A child spec's PM object is a task, not an epic, so `epic_key` becomes a lie
(Decision 7). The field is renamed to `pm_key`, meaning "this spec's PM object
key", whatever type the PM tool gave it.

```go
// PMKey is this spec's PM object key (e.g. "PLAT-123"). It is an epic for a
// standalone or initiative spec and a task for a deliverable slice; the type is
// the PM adapter's concern, not the spec's, and is resolved from the key.
PMKey string `yaml:"pm_key,omitempty"`

// EpicKey is the pre-hierarchy name for PMKey, retained read-only so specs in
// the wild keep resolving. Never written. Callers use PMKey via ReadMeta, which
// coalesces the two.
EpicKey string `yaml:"epic_key,omitempty"`
```

`ParseMeta` coalesces: if `PMKey` is empty and `EpicKey` is set, `PMKey` takes
its value and `EpicKey` is cleared, so the next `WriteMeta` migrates the field
in place with no separate rewrite pass. **Read fallback is mandatory** — specs
already in the specs repo say `epic_key:` and will not all be rewritten at once.

Touch points to migrate: `cmd/status.go:37,95-96,164`, `cmd/promote.go:153`,
`cmd/pm.go`, `cmd/helpers.go:399-415`, `internal/workflow/pm.go:104`,
`internal/workflow/advance.go:106,143,147`, `internal/workflow/revert.go:58`,
`internal/workflow/workflow.go:80`, `internal/pipeline/effects/adapters.go:122`,
`internal/search/search.go:171`, `internal/store/search.go:41,62,94-100`,
`internal/build/pr_tools.go:154,174`.

`cmd/status.go:96` prints `Epic: PLAT-45`; it becomes `PM: PLAT-45`, since the
CLI no longer knows the type.

### 5.3 Store migration v7

Two changes, one transaction, following the `migrateV6` precedent
(`internal/store/db.go`):

1. **`spec_search`** — FTS5 tables cannot `ALTER` their column set, so DROP and
   recreate with `pm_key` replacing `epic_key`, then `DELETE FROM
   spec_search_state` to force a full reindex on next reconcile. Precedented,
   tested, and cheap: specs repos hold hundreds of documents, not millions.
2. **`pm_queue`** — an ordinary table, so `ALTER TABLE pm_queue RENAME COLUMN
   epic_key TO pm_key` (`internal/store/db.go:304,402`,
   `internal/store/pm_queue.go:23,51-65,77,133`).

`parent` is **not** added to the search index in this iteration; hierarchy is a
navigation concern, not a text-search one, and adding it would cost a second
FTS rebuild for no query anyone has asked for.

## 6. Invariants and enforcement

Three checks, one meaning: *the tree is two levels deep and its parents are
live*.

| Invariant | Refused at link time | `spec validate` |
|---|---|---|
| Parent must exist | yes | **error** |
| Parent must not be self | yes | **error** |
| Parent must not itself have a parent | yes | warning |
| Child must not already have children | yes | warning |
| Parent must not be in a terminal stage | yes | warning |
| Parent must not be archived | yes | warning |

Link-time refusal is the primary defence (Decision 2), but **frontmatter is
hand-editable** — anyone can type `parent: SPEC-999` via `spec edit`. So
`spec validate` re-checks all six as a backstop, with the severity split from
Decision 5. Errors fail validation and block `spec advance`; warnings print and
let the author proceed.

Terminal-stage detection uses the existing derived helper
`pipeline.IsTerminalStage` (`internal/pipeline/stages.go:180`) — never a
hardcoded stage list, because terminal stages are derived from `auto_archive`
and the last non-optional stage (`stages.go:127-149`).

### 6.1 Second-order divergence is warned, never repaired

A child can ship, its parent close, and *then* that child be `revert`ed or
`eject`ed. Link-time enforcement cannot catch this.

The parent is **never** auto-reverted. `spec validate` and the dashboard surface
it as `SPEC-004 closed · SPEC-009 reopened to build`, and a human decides. One
document silently mutating another's stage is far worse to debug than a visible
inconsistency, and would fan out spurious PM-sync and comms notifications
through `internal/pipeline/effects/`.

### 6.2 One resolver, no exceptions

All parent/child path resolution goes through the existing
`resolveSpecPathIn` (`cmd/helpers.go:244-264`), which already searches the
repo root, `triage/`, then `archive/` in that order. **No second resolver.**

This is not stylistic. The `sidecarDirFor` doc comment
(`cmd/helpers.go:369-381`) records this exact bug class already biting this
codebase: two code paths resolved a spec's location independently, disagreed
once the spec was archived, and a spec's own discussion history became
invisible to itself. A hierarchy resolver that doesn't look in `archive/` would
report every closed initiative as a dangling parent.

## 7. The `children_complete` gate

### 7.1 Semantics

```
children_complete passes  ⟺  len(children) ≥ 1  ∧  ∀ c ∈ children: IsTerminalStage(c.Status)
```

A childless spec evaluates **false**, never vacuously true. This is the whole
safety argument for shipping the predicate before any team adopts it: composed
under `any:`, a false branch is inert; a vacuously-true branch would disable
`pr_stack_exists` for every spec in the repo.

### 7.2 Expression context

`expr.Context` (`internal/pipeline/expr/expr.go:13-37`) gains a tagged
sub-context, which makes the data available to gate expressions and `skip_when`
for free:

```go
// Children contains deliverable-slice rollup for an initiative spec.
Children ChildrenContext `expr:"children"`

type ChildrenContext struct {
    Total    int `expr:"total"`
    Complete int `expr:"complete"`
    Open     int `expr:"open"`
    Blocked  int `expr:"blocked"`
}
```

`BuildExprContext` (`internal/pipeline/gates.go:27`) takes an additional
`hierarchy.Rollup` parameter and populates it via `WithChildren`. Callers to
update: `EvaluateGates` (`gates.go:64`) and the `skip_when` evaluation path.

### 7.3 Team configuration (opt-in)

Documented in `docs/CONFIGURATION.md`; not applied automatically.

```yaml
pipeline:
  stages:
    - name: pr-review
      owner_role: engineer
      gates:
        - any:
            - pr_stack_exists: true
            - children_complete: true
    - name: qa-validation
      owner_role: qa
      gates:
        - any:
            - prs_approved: true
            - children_complete: true
```

**Both** stages must be relaxed — `pr-review` gates on `pr_stack_exists` and
`qa-validation` on `prs_approved` (`spec.config.yaml`,
`internal/pipeline/gates.go:129-160`). Relaxing one leaves the initiative wedged
one stage later.

A team that never edits config gets links, rollup views, agent inheritance and
validation — their initiatives simply stop at `pr-review`. That is an accepted
consequence of not rewriting anyone's pipeline on upgrade.

## 8. Architecture

Per AGENTS.md: engines take plain types, `cmd/` stays thin, only `internal/git`
shells out, only `internal/store` touches SQLite.

```
internal/hierarchy/
  graph.go        SpecRef{ID, Title, Status, Parent, Path}
                  Graph — parent→children and child→parent indexes
                  Build([]SpecRef) *Graph
                  (*Graph) Parent(id) (SpecRef, bool)
                  (*Graph) Children(id) []SpecRef
  rollup.go       Rollup{Total, Complete, Open, Blocked}
                  (*Graph) Rollup(id, pipelineCfg) Rollup
  invariant.go    Check(g *Graph, id string, pipelineCfg) []Finding
                  Finding{Rule, Severity, Message}   // severity: error | warning
  link.go         Link(g *Graph, child, parent string, pipelineCfg) error
                  // pure precondition check; returns actionable errors
```

The package is **pure**: it takes a slice of `SpecRef` and a pipeline config,
touches no filesystem and no database. Callers supply the refs from the existing
spec load path, which keeps the whole package table-testable without fixtures.

`internal/hierarchy` imports `internal/pipeline` for `IsTerminalStage` only.
`internal/pipeline` must **not** import `internal/hierarchy` — the rollup
arrives as a plain struct on `BuildExprContext`, avoiding an import cycle.

## 9. Command surface

```
spec new --title "…" --parent SPEC-004    # scaffold a slice under an initiative
spec link SPEC-009 --parent SPEC-004      # attach an existing spec
spec link SPEC-009 --parent ""            # detach
spec list --parent SPEC-004               # the initiative's slices
spec status SPEC-004                       # initiative: children + rollup
spec status SPEC-009                       # slice: parent line
```

`spec link` already branches on `--epic` to `runLinkEpic` (`cmd/link.go:39`), so
`--parent` follows the established shape. Both mutate through
`gitpkg.WithSpecsRepoOpts` like every other spec write.

`spec status` output gains, on an initiative:

```
Slices: 3/5 complete
  SPEC-009  done      Token bucket limiter
  SPEC-010  done      Redis backend
  SPEC-011  done      Gateway middleware
  SPEC-012  build     Frontend error states
  SPEC-013  draft     Admin overrides
```

and on a slice:

```
Parent: SPEC-004 — API rate limiting
```

`StatusJSON` (`cmd/status.go:37`) gains `parent` and `children` fields, both
`omitempty` so existing consumers see an unchanged shape for standalone specs.

## 10. TUI

Design intent: hierarchy is visible where it is *structural* and merely *hinted*
where the surface is sorted by something else (Decision 6).

### Specs view (3) — nested

```
  ◆ SPEC-004  engineering   API rate limiting                 3/5
    ├─ SPEC-009  done        Token bucket limiter
    ├─ SPEC-010  done        Redis backend
    ├─ SPEC-011  done        Gateway middleware
    ├─ SPEC-012  build       Frontend error states
    └─ SPEC-013  draft       Admin overrides
      SPEC-014  tl-review    Unrelated standalone spec
```

- Children sort under their initiative; standalone specs keep their normal
  position in the list.
- The initiative row carries a compact `3/5` rollup, right-aligned.
- Connectors are `├─`/`└─` with an ASCII fallback (`|-`, `` `- ``) under
  `NO_COLOR`/non-TTY, matching `cmd/output.go` conventions.
- Collapse/expand on the initiative row; children remain individually
  selectable and actionable.
- The archive toggle (`` ` ``) is unaffected: an archived initiative appears in
  the archive list, its live children in the active list, both resolving
  correctly via §6.2.

### Dashboard (1) and Pipeline (2) — marker only

Rows keep their existing sort. A slice row gains a subtle parent marker glyph
following the `internal/tui/bountymark.go` precedent — meaning carried inside an
existing row, no new chrome, no extra timers — and the detail pane shows
`Part of SPEC-004 — <title>`.

### Urgency suppression

**An initiative row does not warm with the time-urgency gradient while it has
open children.** A vision doc sitting still is correct behaviour, not staleness.
Without this, every initiative is permanently red and the gradient's meaning
degrades across the whole dashboard. Implemented at the `StageUrgency` call site
(`internal/dashboard/dashboard.go:191`, computed at `:343`) as a rollup check
returning 0, not by special-casing `stale_after` in the pipeline config. Note
`StageEnteredAt` deliberately survives ordinary edits
(`internal/markdown/frontmatter.go`), so an initiative accrues dwell from the
moment it enters a stage regardless of authoring activity — which is exactly why
the suppression is needed rather than optional.

## 11. Agent context inheritance

For any spec with a `parent:`, `spec context` and the `spec build` prompt gain a
clearly delimited, read-only block carrying **only** the parent's TL;DR, §1
Problem Statement, and §4 Proposed Solution:

```markdown
<!-- inherited from SPEC-004 — read-only context, do not implement -->
## Initiative context — SPEC-004: API rate limiting
### TL;DR
…
### 1. Problem Statement
…
### 4. Proposed Solution
…
<!-- end inherited context -->
```

Sections are pulled with the existing `markdown.FindSection` /
`ExtractSectionsFromFile` machinery. §5–§8 of the parent are **excluded**: the
parent's acceptance criteria and technical implementation describe nothing this
slice should build, and injecting them invites an agent to implement the wrong
scope. The delimiters state that explicitly, since prompt text is the only
enforcement available.

Applies to `cmd/context.go`, the MCP spec-read path (`cmd/mcp_server.go`), and
the build prompt (`internal/build/`).

## 12. PM projection

Mapping (Decisions 7–9):

| Spec | PM object |
|---|---|
| Initiative (no parent, has children) | Epic |
| Standalone (no parent, no children) | Epic |
| Slice (has parent) | **Task** on the parent's epic |
| Build step | Story / sub-task — **adapter's choice** |

### 12.1 Interface change

`PMAdapter` (`internal/adapter/pm.go:10-29`) gains one method and renames a
parameter:

```go
// CreateTask creates a task under an existing epic, for a spec that is a
// deliverable slice of an initiative. parentKey is the parent spec's PM key.
CreateTask(ctx context.Context, spec SpecMeta, parentKey string) (key string, err error)

// SyncStories reconciles per-step children of a PM object. The adapter resolves
// the object's type from pmKey and chooses the appropriate child issue type.
SyncStories(ctx context.Context, pmKey string, stories []StorySpec) ([]StoryLink, error)
```

Issue-type knowledge stays entirely inside `internal/adapter/jira/`. Resolving
a key's type costs one extra API call on the sync path; that is the adapter's
problem to cache, and PM failures are already non-fatal via `pm_queue`.

The noop adapter returns `("", nil)` from `CreateTask`, per the never-panic
rule.

### 12.2 Decided once, never converted

```
first PM sync of SPEC-009:
  FindEpic(SPEC-009) → ""           # no PM object yet
  parent set?  yes → parentKey = pm_key of SPEC-004
    parentKey != ""  → CreateTask(SPEC-009, parentKey)
    parentKey == ""  → parent not synced yet; queue and retry (pm_queue)
  parent set?  no  → CreateEpic(SPEC-009)

later link of an already-synced SPEC-009 to SPEC-004:
  FindEpic(SPEC-009) → "PLAT-12"    # already has a PM object
    → LinkEpic only. Never a type conversion.
    → warn: SPEC-009 already exists as PLAT-12 — linked to PLAT-04 rather than
             converted
```

`FindEpic` remains the idempotency guard; its meaning widens from "does this
spec have an epic" to "does this spec have a PM object". A parent that hasn't
synced yet is an ordinary queued retry, not an error — the initiative is always
created before its slices in the normal flow, and `pm_queue` handles the
out-of-order case.

## 13. Degradation

- **Specs repo not configured** → hierarchy features print the standard
  `specs repo not configured` guidance and exit cleanly. No panic.
- **Specs repo present but stale** → resolve against the local clone. Hierarchy
  reads are pure frontmatter, so a stale clone gives stale-but-coherent rollup;
  the existing freshness banner covers it.
- **Parent not resolvable while the specs repo is unavailable** → report
  `cannot verify parent SPEC-004 (specs repo unavailable)` as a **warning**,
  never the dangling-parent **error**. `EnsureSpecsRepo` clones the whole repo,
  so a genuine dangling parent is only provable with a healthy clone. Getting
  this backwards would turn every offline `spec validate` into a false failure.
- **PM tool unreachable** → `CreateTask` failures queue in `pm_queue` exactly as
  `CreateEpic` failures do today. The spec link is authoritative regardless of
  whether the board caught up.
- **Circular or deep data already on disk** (hand-edited) → `spec validate`
  warns; graph construction must not recurse. `Build` indexes one level and
  treats a grandchild edge as a finding, never as a traversal.

## 14. Testing

Per AGENTS.md: table-driven, interfaces not implementations, `t.TempDir()`,
each test owning its state.

- **`internal/hierarchy`** — table-driven over `[]SpecRef` fixtures: happy
  two-level tree, standalone specs, dangling parent, self-parent, depth-3
  attempt, child-with-children attempt, terminal parent, archived parent,
  duplicate IDs. Rollup: all-complete, none-complete, mixed, blocked child,
  zero children. No filesystem, no git.
- **`children_complete` gate** — table-driven in `internal/pipeline`: childless
  spec (must be **false**), one open child, all children terminal, composed
  under `any:` with `pr_stack_exists` both ways, and under `not:`. Assert
  explicitly that a childless spec cannot pass `any: [pr_stack_exists,
  children_complete]` — that assertion is the guard on the whole safety
  argument.
- **`internal/markdown`** — round-trip: a spec with no `parent:` is
  byte-identical after `ReadMeta`/`WriteMeta`; `epic_key`-only frontmatter
  coalesces into `PMKey`; both fields present prefers `pm_key`.
- **`internal/store`** — migration v7 on a `:memory:` DB: `pm_queue` column
  renamed with rows preserved, `spec_search` recreated, `spec_search_state`
  cleared, v6→v7 upgrade path from a populated v6 schema.
- **`cmd`** — smoke tests following `cmd/smoke_reads_test.go`: `spec link
  --parent` success and each refusal with its actionable message; `spec status`
  both directions; `spec list --parent`; `StatusJSON` shape stability for a
  standalone spec (must be unchanged).
- **`internal/tui`** — golden snapshot tests at fixed width, colour disabled
  (`snapshot_test.go` pattern): nested Specs view, ASCII connector fallback,
  collapsed initiative, marker glyph on a Dashboard row, and an initiative with
  open children showing **no** urgency warming.
- **`internal/adapter/jira`** — `CreateTask` against a stubbed transport;
  already-an-epic child takes the link path and never issues a Move.

## 15. Implementation plan

Each step passes `make lint-strict` before merge.

1. `feat: parent frontmatter field and hierarchy graph` — `SpecMeta.Parent`,
   `internal/hierarchy` (graph, rollup, invariant, link) with full table tests.
   No behaviour change, nothing wired up.
2. `feat: link a spec to a parent initiative` — `spec link --parent`,
   `spec new --parent`, link-time refusals with actionable errors.
3. `feat: validate parent link invariants` — the error/warning split from
   Decision 5 in `cmd/validate.go`, plus the offline-safe degradation rule.
4. `feat: children_complete gate predicate` — `expr.ChildrenContext`,
   `BuildExprContext` signature change, gate wiring, `docs/CONFIGURATION.md`
   snippet.
5. `feat: surface hierarchy in status and list` — `spec status` both
   directions, `spec list --parent`, `StatusJSON` fields.
6. `feat: nest hierarchy in the specs browser` — TUI Specs view nesting,
   marker glyph on Dashboard/Pipeline rows, urgency suppression for
   initiatives.
7. `feat: inherit initiative context for coding agents` — `spec context`, MCP
   read path, build prompt injection.
8. `refactor: rename epic_key to pm_key` — frontmatter with read fallback,
   store migration v7, all touch points from §5.2. **Independent of steps 1–7**
   and can land in parallel; must land before step 9.
9. `feat: project spec hierarchy into the PM tool` — `PMAdapter.CreateTask`,
   noop implementation, Jira implementation, decided-once logic, link-not-convert
   fallback with its warning.

Steps 1–7 deliver the complete spec-side feature with no PM dependency; a team
with `pm.provider: none` (this repo included) needs nothing beyond step 7.

## 16. Open questions

- **Dashboard rollup rendering.** Decision 6 deliberately keeps hierarchy out of
  the Dashboard and Pipeline views beyond a marker glyph. Whether an
  INITIATIVES rollup section earns its place there should be revisited once **at
  least two initiatives have run end-to-end** — the right rendering isn't
  knowable from the design chair, and the urgency model needs rethinking for a
  row that is supposed to sit still.
- **Does `spec metrics` need initiative-level lead time?** An initiative's lead
  time is a different and arguably more interesting number than a slice's
  (`docs/plan-flow-analytics.md` §5 defines only per-spec metrics). Out of scope
  here; worth a follow-up once real data exists.
- **Detach semantics on a synced slice.** `spec link --parent ""` removes the
  spec-side link, but the PM task remains a task under the old epic and
  Decision 8 forbids conversion. Current intent: detach the spec, warn that the
  PM object is unchanged, and leave board hygiene to a human. Confirm with a
  real case before hardening.
