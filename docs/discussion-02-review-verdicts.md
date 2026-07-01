# Discussion: Review Verdicts and Structure

> **Status:** Proposed · **Owner:** Discussion · **Scope:** `internal/thread/`, `internal/pipeline/`, `internal/pipeline/expr/`, `internal/config/pipeline.go`, `internal/markdown/decisionlog.go`, `cmd/ask.go`, `cmd/answer.go`, `cmd/resolve.go`, `cmd/review.go`, `internal/mcp/handler.go`
> **Effort estimate:** Large · **Risk:** Medium (touches the pipeline gate path; gated behind opt-in config)
> **Delivery:** a stack of five PRs (see §8). The gate wiring (PR 4) is opt-in and ships dark until a team enables it.

A thread today has one terminal state — `resolved`, meaning "stop drawing
attention." There is no blocking concept, no comment type, no reviewer verdict,
and no link between discussion state and the pipeline gate. A spec can advance
past `tl-review` or `pr-review` with open blocking questions because nothing
connects the two. When a thread that changed the design is resolved, the reason
evaporates instead of flowing into the per-spec Decision Log (SPEC.md §4.8) —
the exact asynchronous-reference failure that table exists to prevent.

This adds structure on top of the existing sidecar: comment kinds, a
per-reviewer verdict, gate wiring, and resolution → Decision Log capture.

It assumes the model changes in `discussion-01-awareness-loop.md` (the
`Mentions`/`Participants` work). It is buildable independently, but the PR stack
below sequences after it.

---

## 1. Outcome

- A thread carries a **kind**: `question | blocking | nit | suggestion | praise`.
  `blocking` threads gate stage advancement when the team opts in.
- A spec carries a **review verdict** per reviewer: `approve | request-changes |
  comment`, recorded as a first-class entry in the sidecar and aggregated into a
  per-spec review state.
- The pipeline gate can require "no open blocking threads" and "review approved
  by role" via the existing `expr` gate mechanism — a new `discussion`
  namespace in the expression context.
- Resolving a thread can capture an **outcome** that writes a Decision Log row
  through the existing `markdown.AppendDecision` / `ResolveDecision` engine.
- `spec resolve` requires an outcome for `blocking` threads; `question`/`nit`
  resolve with no outcome, as today.

---

## 2. Data model

`internal/thread/thread.go`.

### 2.1 Thread kind

```go
const (
    KindQuestion   = "question" // default
    KindBlocking   = "blocking"
    KindNit        = "nit"
    KindSuggestion = "suggestion"
    KindPraise     = "praise"
)

type Thread struct {
    // ... existing fields ...

    // Kind classifies the thread. Empty means question (back-compat default).
    Kind string `yaml:"kind,omitempty"`

    // Outcome records why a thread was resolved, when an outcome was given.
    // Set on resolve; required for blocking threads.
    Outcome string `yaml:"outcome,omitempty"`

    // DecisionRef links a resolved thread to a Decision Log row number when the
    // resolution was promoted to a decision. Zero means no link.
    DecisionRef int `yaml:"decision_ref,omitempty"`
}

// EffectiveKind returns Kind, defaulting to KindQuestion when empty.
func (t Thread) EffectiveKind() string

// IsBlocking reports whether an open thread blocks advancement.
func (t Thread) IsBlocking() bool { return t.IsOpen() && t.EffectiveKind() == KindBlocking }
```

Validation: `validateKind(kind string) (string, error)` rejects unknown kinds
with a message listing the valid set. `Store.Create` gains a `kind` parameter
(after `section`), validated and defaulted to `question`.

Kind is immutable after creation — no `Store` method changes it — which also
means `mergeThread` needs no merge branch for it: both sides of any offline
divergence trace back to the same `Create` call and already agree. (Escalating
a `question` to `blocking` after the fact is a known gap, not an oversight;
see §10.)

### 2.2 Review verdicts

A verdict is a spec-level statement by one reviewer, not anchored to a section.
Store verdicts in the same sidecar document so they ride the same git flow.

```go
const (
    VerdictApprove        = "approve"
    VerdictRequestChanges = "request-changes"
    VerdictComment        = "comment"
)

type Verdict struct {
    Reviewer string    `yaml:"reviewer"`
    State    string    `yaml:"state"`
    At       time.Time `yaml:"at"`
    Note     string    `yaml:"note,omitempty"`
}
```

Extend the on-disk `document` in `internal/thread/store.go`:

```go
type document struct {
    Threads  []Thread  `yaml:"threads"`
    Verdicts []Verdict `yaml:"verdicts,omitempty"`
}
```

Store methods:

```go
// SetVerdict records or replaces the calling reviewer's verdict. A reviewer has
// at most one current verdict; a new verdict supersedes the prior one (the
// prior is not retained — verdicts are current state, not a log; the activity
// log already records history).
func (s *SidecarStore) SetVerdict(specID, reviewer, state, note string) (Verdict, error)

// Verdicts returns all current verdicts for a spec.
func (s *SidecarStore) Verdicts(specID string) ([]Verdict, error)
```

Add `SetVerdict` / `Verdicts` to the `Store` interface.

### 2.3 Aggregate state

New file `internal/thread/review.go`:

```go
// ReviewState summarises a spec's discussion for gates and dashboards.
type ReviewState struct {
    OpenBlocking int
    OpenTotal    int
    Approvals    int
    ChangesReq   int
    Approvers    []string
}

// Summarise computes ReviewState from a thread set and verdict set.
func Summarise(threads []Thread, verdicts []Verdict) ReviewState

// ApprovedBy reports whether any current verdict from the given reviewer set
// (roles resolved by the caller) is VerdictApprove with no outstanding
// request-changes from the same set.
func (r ReviewState) ApprovedBy(reviewers []string) bool
```

`Merge` (`internal/thread/merge.go`): union verdicts by `reviewer`, keeping the
one with the later `At`. Add to `mergeThread`'s sibling — a top-level
`mergeVerdicts(a, b []Verdict) []Verdict`. Keep ordering deterministic.

`mergeThread` itself needs a matching fix for the fields this plan adds to
`Thread`. Today it does:

```go
if !x.IsOpen() {
    out.Status, out.ResolvedBy, out.ResolvedAt = x.Status, x.ResolvedBy, x.ResolvedAt
} else if !y.IsOpen() {
    out.Status, out.ResolvedBy, out.ResolvedAt = y.Status, y.ResolvedBy, y.ResolvedAt
}
```

`out := x` at the top means the `y`-wins branch (`x` was open, `y` resolved)
copies `Status`/`ResolvedBy`/`ResolvedAt` from `y` but leaves `out.Outcome` and
`out.DecisionRef` at `x`'s values — empty and zero, because `x` was never
resolved. The merged thread comes out `Status: resolved`, `Outcome: ""` — a
resolved-with-no-reason thread, silently, on every offline-then-synced resolve
where the resolver's copy isn't `x`. That is exactly the failure this plan
exists to prevent, reintroduced by the merge path it depends on. Fix: copy
`Kind`, `Outcome`, and `DecisionRef` together with
`Status`/`ResolvedBy`/`ResolvedAt` as one atomic group in both branches — never
update the resolution triple without its outcome.

The harder case is two people resolving the *same* blocking thread offline
with *different* outcomes before either has synced. The current
resolution-wins rule ("if either side resolved, that side wins," `x` checked
first) picks whichever side happens to land first in `Merge`'s `a`/`b`
concatenation — arbitrary, not deterministic — and silently drops the other
side's `Outcome` and `DecisionRef` even though both are meaningful. Replace it
with the same policy already adopted for verdicts above: when both sides are
resolved, keep the one with the later `ResolvedAt`, matching `mergeVerdicts`'
"later `At` wins." This does not need to be conflict-free in the git sense —
`sectionOverlap` (`internal/git/conflict.go`) already forces a genuine,
un-auto-mergeable conflict on this exact scenario when both resolves also
promoted to the Decision Log, because both sides then touch the same
`decision_log` markdown section and `collidingSection` flags that as a real
collision. But when `--no-decision` was used on both sides, only the sidecar
YAML changed, which `sectionOverlap` treats as safely auto-mergeable — the
timestamp rule is what makes that auto-merge correct instead of merely quiet.

---

## 3. Pipeline gate wiring

The gate engine already supports expression gates (`GateConfig.Expr`) and a
typed `expr.Context` (`internal/pipeline/expr/expr.go`). `PRsContext` even has a
`ThreadsResolved` bool today. Add a `discussion` namespace rather than
overloading PR state.

### 3.1 Expression context

`internal/pipeline/expr/expr.go`:

```go
type Context struct {
    // ... existing namespaces ...
    Discussion DiscussionContext `expr:"discussion"`
}

type DiscussionContext struct {
    OpenBlocking int  `expr:"open_blocking"`
    OpenTotal    int  `expr:"open_total"`
    Approvals    int  `expr:"approvals"`
    ChangesReq   int  `expr:"changes_requested"`
    Approved     bool `expr:"approved"` // ChangesReq == 0 && Approvals > 0
}
```

`NewContext` and `Compile`'s type-check context need no map init for this struct.

### 3.2 Builder and call site

`internal/pipeline/expr/context.go`: add
`WithDiscussion(open, openBlocking, approvals, changesReq int)`.

`internal/pipeline/gates.go`: `BuildExprContext` currently takes
`(sections, hasPRStack, prsApproved, meta)`. It has no access to the sidecar.
Two options:

1. Thread a `*thread.ReviewState` parameter through `BuildExprContext` and
   `EvaluateGates`. Clean, but `internal/pipeline` would import
   `internal/thread`.
2. Pass the four integers directly (no new package import).

Choose **(2)**: pass primitives. The caller (`cmd/advance.go`) owns the
sidecar read and computes `thread.Summarise`, then passes counts into
`EvaluateGates`. This keeps `internal/pipeline` free of a discussion dependency
and matches how PR/section data is already injected as primitives.

`cmd/advance.go`: before evaluating gates, load the sidecar
(`thread.NewSidecarStore(...).List` + `.Verdicts`), compute `Summarise`, resolve
the stage's required reviewer roles to handles, and pass the counts plus an
`approvedByReviewers` bool.

That handle resolution, and matching it against `Verdict.Reviewer`, must go
through `internal/identity` (`discussion-01-awareness-loop.md` §2.4), not
string equality. `Verdict.Reviewer` is written from `threadAuthor(rc)` — a
handle if configured, else a display name — the same ambiguity
`matchesIdentity` exists to resolve for dashboard scoping. A naive comparison
against a role's configured handle list makes a real approval invisible to the
gate: the spec sits blocked at `tl-review` forever with an approval already on
record, and nothing explains why, because the gate believes no one approved.
Do the identity resolution in `cmd/advance.go`, not inside `ReviewState` —
`internal/thread` must not depend on `internal/identity` or `internal/config`,
the same boundary that keeps `internal/pipeline` free of `internal/thread`
(§3.2 above).

### 3.3 Config

No new gate type is required — teams express the gate with the existing `expr`:

```yaml
- name: tl-review
  gates:
    - expr: "discussion.open_blocking == 0"
      message: "Resolve all blocking discussion threads before advancing."
    - expr: "discussion.approved"
      message: "tl-review requires an approving verdict with no requested changes."
```

For ergonomics, add two sugar gate types to `GateConfig`
(`internal/config/pipeline.go`), each compiling to the equivalent expression so
`Type()`, validation, and display stay uniform:

```go
NoBlockingThreads *bool             `yaml:"no_blocking_threads,omitempty"`
ReviewApprovedBy  *ReviewApprovedBy `yaml:"review_approved_by,omitempty"` // roles/handles
```

`ReviewApproved` already exists for plan review (`StageReviewConfig`); keep them
distinct — `ReviewApproved` is the technical-plan review at `engineering`;
`review_approved_by` is the discussion verdict at review stages. Document the
difference in `docs/CONFIGURATION.md`.

### 3.4 Default pipeline

Update the bundled default pipeline so `tl-review` and `pr-review` carry
`no_blocking_threads: true`. Ship it **off by default for existing configs**
(teams have their own `spec.config.yaml`); only `spec config init` scaffolds the
new gates. Existing teams opt in by editing config.

---

## 4. Resolution → Decision Log

`internal/markdown/decisionlog.go` already exposes `AppendDecision(path,
question, user)` and `ResolveDecision(path, number, decision, rationale, user)`.

On `spec resolve` of a `blocking` thread with an outcome:

1. `AppendDecision(specPath, thread.Question, resolver)` → returns row number.
2. `ResolveDecision(specPath, number, thread.Outcome, "resolved via "+thread.ID, resolver)`.
3. Set `thread.DecisionRef = number` on the resolved thread.

Both the spec markdown edit and the sidecar write happen inside the same
`withThreadStore` → `WithSpecsRepo` transaction so they commit together. If the
decision write fails, the resolve fails and nothing is committed — no partial
state (AGENTS.md robustness rule).

Make the promotion opt-out per resolve with `--no-decision`, and opt-in for
non-blocking kinds with `--decision`. Blocking threads promote by default.

---

## 5. CLI

- `spec ask`: `--kind <question|blocking|nit|suggestion|praise>` (default
  `question`). `spec ask --blocking` as a shorthand for `--kind blocking`.
- `spec resolve`: `--outcome "<text>"` (required when the thread is `blocking`;
  the command errors with the next action when omitted). `--decision` /
  `--no-decision` control Decision Log promotion.
- `spec review <id>`: `cmd/review.go` already registers `--approve` and
  `--request-changes` on `reviewCmd`, but `runReview` only acts on them when
  `--plan` is set (technical-plan approval, writing `meta.Review` via
  `planning`); without `--plan` they are parsed and currently silently ignored.
  Wire that dead path: when `--plan` is absent and a verdict flag is given,
  branch into the new verdict path instead of the PR-listing path.
  - `spec review SPEC-x --approve [--note "..."]`
  - `spec review SPEC-x --request-changes --note "..."` (note required)
  - `spec review SPEC-x --comment --note "..."` (new flag; note required)
  - With no verdict flag, current behaviour (list PRs, post the PR review
    request) is retained.
  - `--note` is deliberately a separate flag from `--plan`'s existing
    `--feedback`: they write to different stores (sidecar verdict vs.
    frontmatter plan review) and must stay independently addressable. Reusing
    `--feedback` would couple two unrelated review mechanisms by accident.
  - The verdict flags write to the sidecar via `withThreadStore`, attributed to
    `threadAuthor(rc)` exactly like `spec ask`/`spec answer`/`spec resolve`.
- `spec ask --list`: annotate each thread with its kind glyph; print the verdict
  summary (`✓ 2 approve · ✗ 1 request-changes`) as a header.

---

## 6. MCP

`internal/mcp/handler.go`:

- `spec_list_threads`: include `kind` and `outcome` in the rendered output, and
  append a verdict summary line.
- Add `spec_set_verdict` (id, state, note) so a build agent can record
  `request-changes` when it finds the spec contradicts the code. Attribute to
  `"agent"` consistent with `spec_decide` / `toolReplyThread`.
- Add `spec_resolve_thread` (id, thread_id, outcome) — the agent can close a
  thread it answered, with the decision-promotion rules of §4 applied.

---

## 7. Dashboard and reader

- Dashboard `DISCUSSION` rows (from `discussion-01`) prefix blocking threads with
  a distinct glyph and sort them first.
- Reader pane (`internal/tui/threadpane.go`): render the kind as a coloured tag
  before the question; show the verdict summary in the pane header
  (`Threads (2 open · 1 blocking · changes requested)`).
- These are display-only; the model and gate work above is the substance.

---

## 8. PR stack

| PR | Scope | Depends on |
|----|-------|------------|
| 1 | `thread`: `Kind`, `Outcome`, validation, `Verdict`, `document.Verdicts`, `SetVerdict`/`Verdicts`, `Summarise`, `mergeVerdicts`, `mergeThread` resolution-triple fix + later-`ResolvedAt`-wins (§2.3), tests | discussion-01 PR 1 |
| 2 | CLI kinds + verdicts: `spec ask --kind/--blocking`, `spec review --approve/--request-changes/--comment` | 1 |
| 3 | Resolution → Decision Log: `spec resolve --outcome/--decision`, transactional double-write | 1 |
| 4 | Gate wiring: `discussion` expr namespace, `WithDiscussion`, `advance.go` sidecar read, `no_blocking_threads` / `review_approved_by` sugar, default-pipeline scaffolding | 1, 2 |
| 5 | MCP `spec_set_verdict` / `spec_resolve_thread`; dashboard + reader display | 1, 2, 3 |

---

## 9. Tests

- `internal/thread`: `validateKind` table; `IsBlocking`; `SetVerdict` supersede;
  `Summarise` counts; `ApprovedBy` with a request-changes outstanding; merge
  unions of kinds, outcomes, and verdicts.
- `internal/pipeline`: `DiscussionContext` expression evaluation
  (`discussion.open_blocking == 0`, `discussion.approved`); gate pass/fail with
  injected counts; `Compile` accepts the new namespace.
- `cmd`: smoke — advance is blocked by an open blocking thread when the gate is
  enabled; resolving with `--outcome` unblocks and writes a Decision Log row;
  `spec review --approve` records a verdict; `--request-changes` without a note
  errors.
- `internal/markdown`: the resolve double-write produces a well-formed decision
  table row and links `DecisionRef`.

---

## 10. Risks and constraints

- **Gate path is load-bearing.** A bug here blocks advancement. Mitigation: the
  new gates are opt-in (§3.4); default configs are unaffected until a team edits
  `spec.config.yaml`. The expr namespace is additive and type-checked at config
  load via `Compile`.
- **`internal/pipeline` must not import `internal/thread`.** Enforced by passing
  primitives into `BuildExprContext` (§3.2). A direct import would violate the
  engine/adapter boundary in AGENTS.md.
- **Transactional double-write.** Decision Log promotion edits the spec markdown
  and the sidecar in one `WithSpecsRepo` call. Both succeed or neither commits.
  Test the failure path (decision write errors → sidecar unchanged).
- **Verdict is current-state, not history.** Superseding drops the prior
  verdict. The activity log (`logThreadActivity`) records the transition, so
  history is not lost; the sidecar stays a clean state document.
- **Back-compat.** `Kind`/`Outcome`/`Verdicts` are `omitempty`. An empty `Kind`
  reads as `question`. Old sidecars parse unchanged; no migration.
- **No path to escalate a thread's kind.** A `question` that turns out to be a
  blocker can only be captured by opening a *new* `blocking` thread — the
  original stays open as an unrelated `question` alongside it, fragmenting the
  conversation. Accepted for v1 because Kind's immutability is what keeps
  merge simple (§2.1); the workaround is to reply on the original pointing at
  the new thread's ID, then resolve the original with an outcome saying so.
- **`DecisionRef` can go stale.** A spec's Decision Log is hand-editable
  markdown; a row can be renumbered or deleted after a thread links to it.
  Treat the link as best-effort on display: when rendering `DecisionRef`
  (`spec ask --list`, the reader pane), look the row up via
  `ParseDecisionLogFromFile` and show "(decision removed)" on a miss instead
  of a number that resolves to nothing. Never treat a missing row as an error.
- **Resolving is one-way, deliberately.** There is no `spec resolve --reopen`,
  matching the base engine's existing `Resolve` idempotency (re-resolving is a
  no-op, not a state transition). Revisiting a resolved blocking thread means
  opening a new thread that references the old one's ID in its question — the
  Decision Log entry the old thread promoted stays as the historical record of
  what was decided *then*, which is correct audit behaviour, not a gap.
