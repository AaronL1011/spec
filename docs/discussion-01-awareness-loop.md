# Discussion: The Awareness Loop

> **Status:** Proposed · **Owner:** Discussion · **Scope:** `internal/thread/`, `internal/dashboard/`, `internal/identity/` (new), `internal/git/specsrepo.go`, `cmd/thread.go`, `cmd/ask.go`, `cmd/answer.go`, `cmd/resolve.go`, `internal/adapter/`, `internal/mcp/handler.go`
> **Effort estimate:** Medium · **Risk:** Low (additive; no breaking changes to the sidecar format)
> **Delivery:** a stack of four PRs (see §7). Each merges independently and ships value on its own.

A discussion thread today is a message committed to git that nobody is told
about. `notifyThreadParticipants` fires one broadcast to a comms channel with a
generic title, tracks no recipients, and degrades silently. The dashboard —
the surface the product is built around — has zero thread awareness, despite
SPEC.md §4 promising `2 unresolved threads` and `@carlos: "..."` rows. A
question asked on your spec is invisible until you happen to open the reader and
scroll to the right section.

A comment becomes a routed, addressable unit of attention: it finds its
recipient and surfaces on the dashboard and the passive awareness line.

---

## 1. Outcome

After this change:

- A thread or reply can address people with `@handle` mentions. Mentions are
  parsed from the body and persisted as a derived participant set.
- `spec` (the dashboard) shows a `DISCUSSION` section listing threads awaiting
  the viewer's reply, newest line quoted, scoped to the viewer's identity.
- The passive awareness line (`PrintAwarenessLine`) counts open threads
  addressed to the viewer alongside the existing pending-stage count.
- Comms notifications route to the mentioned user via the comms adapter's
  direct-message path, falling back to the channel broadcast when no mention is
  present or no DM path exists.
- `spec ask` / `spec answer` accept a `--to @handle` flag and inline `@handle`
  mentions, both feeding the same participant set.

No change to the gate, the pipeline, or the resolve semantics — those belong to
the review-verdicts plan (`discussion-02-review-verdicts.md`).

---

## 2. Data model

Extend `thread.Thread` and `thread.Reply` in `internal/thread/thread.go`. All
fields are `omitempty`, so existing sidecars parse unchanged and a thread with
no mentions serialises identically to today.

```go
type Thread struct {
    // ... existing fields ...

    // Mentions are the handles addressed by the question, in first-seen order.
    // Derived from the body at write time; never hand-edited.
    Mentions []string `yaml:"mentions,omitempty"`
}

type Reply struct {
    Author   string    `yaml:"author"`
    At       time.Time `yaml:"at"`
    Body     string    `yaml:"body"`
    Mentions []string  `yaml:"mentions,omitempty"`
}
```

Add a derived, non-persisted accessor for "everyone involved in this thread"
used by routing and dashboard scoping:

```go
// Participants returns the deduplicated set of handles involved in a thread:
// the author, every replier, and every mentioned handle. Order is stable
// (author first, then first-seen).
func (t Thread) Participants() []string
```

### 2.1 Mention parsing

New file `internal/thread/mention.go`:

```go
// ParseMentions extracts @handle tokens from a body in first-seen order,
// deduplicated, with the leading @ stripped. A handle is [A-Za-z0-9_.:-]+
// following an @ at a word boundary. Email addresses and mid-word @ are
// ignored.
func ParseMentions(body string) []string
```

The grammar includes `:` deliberately, not just word characters: agent
identities are `agent:<adapter>` (`discussion-03-reader-cockpit.md` §6), and
they must be mentionable exactly like a human handle. A narrower grammar would
silently truncate `@agent:claude` to `agent` and drop `:claude`.

Parsing happens in the engine, not the caller, so the CLI, the TUI, and the MCP
handler all populate `Mentions` identically. `Create` and `Reply` in
`internal/thread/store.go` call `ParseMentions` and merge in any explicit
handles passed by the caller (the `--to` flag), deduplicating against the parsed
set.

`Store.Create` and `Store.Reply` signatures gain a `mentions []string`
parameter for explicit `--to` handles. The parsed-from-body set is unioned
inside the store. Update the `Store` interface, `SidecarStore`, the MCP handler,
and all call sites together — these are compile-time breaks caught by the build.

### 2.2 Merge

`internal/thread/merge.go`: `mergeThread` must union `Mentions` (thread-level)
and preserve per-reply `Mentions` through the existing reply union. Add the
union to keep `Merge` associative. Extend `store_test.go` merge cases with a
mention-bearing thread.

### 2.3 Prerequisite fix: sidecar/spec colocation is currently broken

Two bugs already exist in the base engine, independent of this proposal, and
must be fixed before mentions/routing/dashboard work is layered on top —
otherwise everything below silently stops working the moment a spec is
archived.

- **`withThreadStore` (`cmd/thread.go`) resolves the sidecar directory wrong.**
  It calls `specPathIn(repoPath, rc, specID)` to confirm the spec exists —
  which correctly checks `specs/`, `specs/triage/`, and `specs/archive/` via
  `resolveSpecPathIn` — then discards that resolved path and opens
  `thread.NewSidecarStore(specsDir(repoPath))`, which is hardcoded to
  `specs/` regardless of where the spec actually is. Meanwhile `listThreads`
  (`cmd/ask.go`) does the opposite: it resolves the sidecar directory from
  `filepath.Dir(resolveSpecPath(rc, specID))`, which *is* archive-aware. The
  result: `spec ask`/`spec answer`/`spec resolve` against an archived spec
  write to `specs/<ID>.threads.yaml`, while `spec ask --list` on the same
  spec reads from `specs/archive/<ID>.threads.yaml` — two different files.
  Thread activity on an archived (or triaged) spec is invisible to `--list`
  immediately, and a sidecar written before archiving becomes invisible to
  `ask`/`answer`/`resolve` the moment the spec is archived.
- **`ArchiveSpec` / `RestoreSpec` (`internal/git/specsrepo.go`) move the `.md`
  file only.** The sidecar's own doc comment says it "sits beside the spec" —
  archiving breaks that invariant outright: `git mv` runs on the spec file
  and never touches `<ID>.threads.yaml`, so the sidecar is left behind at its
  old path. Combined with the bug above, a spec's discussion history —
  mentions, and after `discussion-02-review-verdicts.md`, verdicts and
  Decision Log links — effectively vanishes the moment the spec reaches
  `done`/`closed`, which is the single most common event in a spec's life and
  exactly when the audit trail matters most for "asynchronous reference"
  (SPEC.md §4.8).

Fix both as one prerequisite change, in PR 1:

- Add one resolver, `sidecarDirFor(repoPath string, rc *config.ResolvedConfig,
  specID string) (string, error)`, in `cmd/helpers.go`, built on the same
  `resolveSpecPathIn` call `specPathIn` already makes. `withThreadStore` and
  `listThreads` both call it — one source of truth, so the two paths cannot
  diverge again.
- `ArchiveSpec` / `RestoreSpec`: after moving the `.md`, `git mv` the sidecar
  too when `<ID>.threads.yaml` exists (`os.Stat` first — most specs have
  none). Both moves land in the same commit as the archive/restore, not a
  second one.

### 2.4 Shared identity resolution

Turn-detection (§4.1) and notification filtering (§3.2) both need to compare a
stored handle/name against "is this the viewer" — the same matching problem
`internal/dashboard/scope.go` already solved for DO-section scoping
(`matchesIdentity`, `anyIdentity`, `Viewer.Identities`). Those helpers are
unexported and dashboard-only. `discussion-02-review-verdicts.md`'s
reviewer-verdict matching (`ApprovedBy`) needs the identical resolution and
runs in `cmd/advance.go`, a different package, so it cannot reach them either.

Extract `matchesIdentity` / `anyIdentity` / `Viewer` into a new package,
`internal/identity`, with no dependency on `dashboard`, `pipeline`, or
`thread`. `internal/dashboard/scope.go` becomes a thin caller; `cmd/thread.go`
(notification filtering) and `cmd/advance.go` (`discussion-02` §3.2) import it
directly. This is a name-preserving move, not a rewrite — same functions, same
tests, new home. Do it in PR 1 so both this plan's routing and
`discussion-02`'s gate wiring build on one identity resolver instead of each
growing its own ad hoc string comparison, which is exactly how a valid
approval or a valid mention ends up silently unrecognised.

---

## 3. Notification routing

The current `notifyThreadParticipants` (`cmd/thread.go`) builds a registry,
constructs one `adapter.Notification`, and sends it to `Comms().Notify`. Replace
the broadcast-only path with mention-aware routing.

### 3.1 Comms adapter port

`internal/adapter/comms.go` (the interface): add a direct-message method.
Noop and every concrete adapter implement it.

```go
// NotifyUser sends a notification to a specific user by handle. Adapters that
// cannot resolve a handle to a user return ErrRecipientUnknown so the caller
// can fall back to a channel broadcast. Like all comms calls it is best-effort
// and never fatal.
NotifyUser(ctx context.Context, handle string, n Notification) error
```

- `internal/adapter/noop`: returns `nil` (silent, like all noop methods).
- `internal/adapter/slack`, `internal/adapter/teams`: resolve `@handle` to a
  user ID via the existing client and post a DM; return `ErrRecipientUnknown`
  when the handle does not resolve.
- Add `var ErrRecipientUnknown = errors.New("comms: recipient handle not resolvable")`
  to `internal/adapter/comms.go`.

### 3.2 Routing logic

Rewrite `notifyThreadParticipants` to take the mention set and route per
recipient:

```go
func notifyThreadParticipants(p *printer, rc *config.ResolvedConfig, specID string, recipients []string, n adapter.Notification)
```

- For each recipient handle, call `Comms().NotifyUser`.
- On `ErrRecipientUnknown` (via `errors.Is`), accumulate the recipient and fall
  back to a single channel `Notify` naming the unresolved recipients in the
  message.
- The acting user is never notified of their own action — filter `threadAuthor(rc)`
  out of the recipient set before routing, matched via `internal/identity`
  (§2.4), not raw string equality — a handle and a display name for the same
  person must both be recognised.
- Agent participants are never notified. Filter any handle equal to `"agent"`
  or matching `agent:<adapter>` (`discussion-03-reader-cockpit.md` §6) out of
  the recipient set in the same pass that drops the acting user. Without this,
  every agent-authored reply or agent-raised thread routes a `NotifyUser` call
  that always returns `ErrRecipientUnknown`, and the channel fallback
  broadcasts a confusing "could not reach: agent:claude" to the whole team.
- Any other error is surfaced through `p.Warn`, as today, and never blocks the
  git operation.

Callers (`runAsk`, `runAnswer`, `runResolve`) pass:

- **ask**: the thread's `Mentions`.
- **answer**: the thread's `Participants()` minus the replier — a reply notifies
  the asker and prior repliers, not just whoever was `@`-mentioned in the reply.
- **resolve**: the thread's `Participants()` minus the resolver.

---

## 4. Dashboard surface

### 4.1 Aggregation

`internal/dashboard/dashboard.go`:

- Add `Discussion []DashboardItem` to `DashboardData`.
- In `Aggregate`, after loading specs, load each spec's sidecar via
  `thread.NewSidecarStore(rc.SpecsRepoDir).List(meta.ID)` and select threads
  where:
  - the thread is open, and
  - the viewer is a participant (`identity.AnyIdentity` against
    `Thread.Participants()` — the package `dashboard/scope.go` itself now
    imports per §2.4), and
  - the viewer is not the last contributor — i.e. it is the viewer's turn.
    "Last contributor" is the author when there are no replies, else the last
    reply's author.
- Map each selected thread to a `DashboardItem`:
  - `SpecID` = thread's spec, `Title` = spec title, `Stage` = `§<section>`,
  - `Detail` = `@<lastAuthor>: "<truncated latest line>"`,
  - `SortTime` = the latest activity timestamp (last reply `At`, else `Created`),
  - `Urgency` via the existing stale-fraction curve over thread age.
- Sort oldest-first with `sortItemsByOldest`, consistent with other sections.

Both checks — "is a participant" and "is not the last contributor" — compare
through `internal/identity.MatchesIdentity` (§2.4), not raw string equality: a
viewer recorded as a reply's `Author` by display name must still be recognised
when their config handle is `@name`, or "whose turn is it" flips incorrectly
and a thread either nags forever or never surfaces.

A thread whose `Section` no longer matches any of the spec's current sections
(heading reworded or removed — slugs are derived from heading text, not a
stable ID) is not dropped from this list. Render it with its stored
`Stage = "§<section>"` as-is; the dashboard is a pointer to the sidecar, not a
live join against parsed sections, so a stale slug still surfaces the thread
even though the reader (`discussion-03-reader-cockpit.md` §2.4) can no longer
resolve it as an anchor. That asymmetry is intentional — the dashboard is the
fallback discovery path when a heading rename orphans an anchor, so it must
never apply the same liveness check the reader does.

Reading sidecars is local-file I/O (no network), so it fits the dashboard's
offline-capable contract. `Aggregate` is deliberately uncached today (see its
doc comment: caching cost more in TTL/invalidation complexity than it saved) —
the sidecar reads follow that same live-read contract, not a new cache path.

### 4.2 Render

`Render` in `internal/dashboard/dashboard.go`: add a `DISCUSSION` block between
`REVIEW` and `INCOMING`, matching the existing section header format:

```
─── DISCUSSION ──────────────────────────────────────────────────
💬 SPEC-039  Rate limiting   §technical_implementation
   @carlos: "can we use token bucket instead?"
```

Render only when `len(data.Discussion) > 0`, following the existing per-section
guard. Add the section to the TUI dashboard (`internal/tui/dashboard.go`) using
the same `DashboardData` field so the static and interactive renders agree.

### 4.3 Passive awareness

`internal/dashboard/awareness.go`: `PendingCount` currently counts DO-scoped
specs. Add `DiscussionCount(rc, role)` that counts open threads where it is the
viewer's turn (the §4.1 predicate), reading sidecars only. `PrintAwarenessLine`
combines both:

```
⚠ 2 pending · 1 reply awaited · run 'spec' for details
```

Keep the line to one row; omit a clause when its count is zero.

---

## 5. MCP symmetry

`internal/mcp/handler.go`: `toolReplyThread` already exists and attributes the
reply to `"agent"`. Apply mention parsing there too so an agent reply that
writes `@alice` routes correctly. Agent identity is out of scope here (covered
in `discussion-03-reader-cockpit.md` §6); the literal `"agent"` author is
retained.

---

## 6. CLI

- `spec ask`: add `--to <handle>` (repeatable). Inline `@handle` in the question
  is parsed automatically; `--to` adds handles that are awkward to inline.
- `spec answer`: same `--to`, plus automatic inline parsing.
- `spec ask --list` / `listThreads` (`cmd/ask.go`): render a `→ @handle, @handle`
  line under a thread when it has mentions.
- No new top-level command. Routing and surfacing are the whole change.

---

## 7. PR stack

| PR | Scope | Depends on |
|----|-------|------------|
| 1 | `thread` model: `Mentions`, `ParseMentions`, `Participants`, store/interface signature change, merge union, tests; sidecar path-resolution + archive/restore colocation fix (§2.3); `internal/identity` extraction (§2.4) | — |
| 2 | Comms port: `NotifyUser`, `ErrRecipientUnknown`, noop + slack + teams impls; rewrite `notifyThreadParticipants`; wire `runAsk`/`runAnswer`/`runResolve`; `--to` flags | 1 |
| 3 | Dashboard: `Discussion` aggregation + `Render` + TUI section | 1 |
| 4 | Awareness line `DiscussionCount`; MCP mention parsing in `toolReplyThread` | 1, 3 |

PR 1 is the foundation. PRs 2–4 fan out from it and can land in any order.

---

## 8. Tests

- `internal/thread`: table-driven `ParseMentions` (plain, multiple, dedup,
  email-ignored, mid-word-ignored, punctuation-bounded); `Participants` ordering;
  merge union of mentions.
- `cmd`: smoke test that `spec ask SPEC-x --section s "ping @bob"` records
  `bob` in the sidecar; that `spec answer` routes to the asker.
- `internal/adapter/noop`: `NotifyUser` returns nil.
- `internal/dashboard`: golden test that an open thread where it is the viewer's
  turn produces a `DISCUSSION` row, and that a thread where the viewer replied
  last does not.
- `internal/dashboard/awareness_test.go`: `DiscussionCount` turn semantics.

---

## 9. Risks and constraints

- **Sidecar compatibility.** All new fields are `omitempty` and additive. A
  sidecar written by an older binary parses cleanly; a sidecar written by a
  newer binary parses cleanly under an older binary (unknown-field tolerance of
  `yaml.v3` with the current `document` shape). No migration.
- **Handle resolution is adapter-specific.** Slack/Teams handle→user mapping can
  fail; `ErrRecipientUnknown` + channel fallback guarantees a notification still
  goes out. Never block the git write on a comms failure (existing contract).
- **Dashboard cost.** Reading one sidecar per spec is bounded by spec count and
  is local I/O, on the same uncached, read-live-every-call contract `Aggregate`
  already uses for specs and triage items. If spec count grows large, the
  sidecar reads parallelise trivially, but do not optimise pre-emptively.
- **Turn detection is heuristic.** "It is your turn" = you are a participant and
  not the last contributor. This is intentionally simple and matches how
  reviewers reason; richer per-user read state is in the reader-cockpit plan.
- **Sidecar/spec colocation.** Fixed as a prerequisite in PR 1 (§2.3) — without
  it, archiving a spec silently strands its discussion history at the old
  path, invisible to every read path that resolves the spec's current
  location.
- **Agent-authored participants are not notification targets.** Filtered out
  before routing (§3.2); cover with a test that an agent-authored mention
  never reaches `NotifyUser`.
