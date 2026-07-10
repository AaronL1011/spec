# Discussion: Elevating the Reader Cockpit

> **Status:** Proposed · **Owner:** Discussion / TUI · **Scope:** `internal/tui/specdetail.go`, `internal/tui/threadpane.go`, `internal/tui/threadnav.go`, `internal/tui/anchormap.go`, `internal/tui/readerpick.go`, `internal/tui/mouse.go`, `internal/tui/help.go`, `internal/markdown/anchor.go`, `internal/store/thread_seen.go`, `internal/thread/`
> **Effort estimate:** Large · **Risk:** Medium (concentrated in `internal/tui`; every phase extends the existing snapshot/golden harness)
> **Delivery:** a stack of five PRs (see §8). Phase 1 (trust) is independently shippable and should land before any polish.
> **Dependency status:** builds directly on discussion-03 (shipped, PRs 1–4). Nothing here requires discussion-02; the two items that touch kinds/verdicts are flagged inline and degrade cleanly until it ships.

Discussion-03 delivered the cockpit's skeleton: span anchors that degrade
gracefully, document-wide `n`/`p` stepping, filters, read-state, and the
unanchored repair path. A close read of the shipped implementation surfaced a
set of correctness gaps that undermine the cockpit's core promise — that what
the reviewer sees is live, complete, and precisely anchored — plus a second
tier of interaction gaps that keep the experience from being genuinely
lovable.

This plan is ordered by a single principle: **trust before delight.** A
reviewer who once sees a stale thread list, a wrong anchor, or a phantom
unread badge stops believing the cockpit, and no amount of polish recovers
that. Phase 1 fixes the lies; later phases make the truth pleasant.

---

## 1. Outcome

- A thread reply, resolve, or newly synced thread from **another person or
  process appears in the open reader within the watch debounce window** — no
  spec-markdown edit required.
- The prose viewport, thread pane, anchor scrolling, and picker all agree on
  **one geometry**: no content is ever hidden behind the pane, and `G`,
  scroll-to-anchor, and the pick cursor land where the reviewer can see them.
- Read-state **survives app restarts exactly** — no phantom unread on reopen,
  no cross-user leakage on a shared machine, no watermark regression from
  out-of-order async writes.
- A quote anchor is **never silently wrong**: ambiguity degrades to
  section-level (visibly), and the picker captures true markdown blocks — one
  list item, one table row — not contiguous-nonblank runs.
- The selected thread is **visually connected to its text**: a distinct
  selected-anchor marker, unread/resolved gutter states, and a brief highlight
  of the anchored block after each `n`/`p` step.
- Gutter badges, thread rows, and pick-mode blocks are **clickable**; the
  wheel scrolls the pane under the pointer.
- Help renders **only the active mode's keys** — reader, pane-focused,
  compose, picker, and repair each get their own truthful key set.
- A completed `n`-pass ends in a **review-pass summary** ("7 visited · 2 open
  · 1 unread") with jump-off actions, not a bare wrap flash.

---

## 2. Phase 1 — Trust: the cockpit never lies

### 2.1 Sidecar-only refreshes are currently discarded

The highest-consequence defect. `fetchData` reloads both the spec markdown
and the thread sidecar, and the watcher observes both files
(`watchPaths`, `specdetail.go:1197`) — but the refresh gate keys on the
**markdown hash alone**:

```go
// specdetail.go:226 — handleDataMsg
if msg.Hash != "" && msg.Hash == m.contentHash && m.meta != nil {
    m.err = nil
    m.buildLine = msg.BuildLine
    return m, nil // ← freshly-read Threads and Seen are dropped here
}
```

A teammate's reply, an agent's resolve, or a `spec pull` that lands new
threads triggers the watcher, gets read from disk successfully, and is then
thrown away because the spec body didn't change. Local mutations mask this in
solo use (they route through `threadsChangedMsg`); the collaborative path —
the whole point of threads — is the broken one.

Fix: hash the two artifacts independently.

- `specDetailDataMsg` carries `ThreadsHash` (hash of the sidecar bytes; empty
  when no sidecar) alongside the existing content `Hash`.
- The early return at `specdetail.go:226` applies **only to the render-cache
  path**. Threads, `Seen`, and `buildLine` are applied unconditionally before
  the gate; when `ThreadsHash` moved, run the existing
  `handleThreadsChanged` logic (rebuild anchors, keep ID-based selection).
- The hash-gate comment gains the invariant: *content hash gates rendering;
  it never gates thread or read-state application.*

### 2.2 One geometry: viewport height must subtract the pane

The reader viewport is sized to the full content height
(`m.readerViewport.SetHeight(max(h, 3))`, `specdetail.go:1105`) while the
pane is composed **over** its bottom rows at view time
(`composeContentColumn`, `specdetail.go:840`). Everything downstream of
viewport geometry is subtly wrong whenever the pane is visible:

- `maxScroll` (`specdetail.go:1123`) believes the last ~half-screen of a
  section is visible when it is actually behind the pane — the true end of a
  section can be unreachable.
- `scrollToAnchor` (`threadnav.go`) can "scroll to" a line that sits under
  the pane — the core `n` motion lands on invisible text.
- `movePick` (`readerpick.go:60`) keeps the pick cursor "visible" using the
  same wrong height — the picker can select a line the reviewer cannot see.
- `PageDown`, `G`, and wheel scrolling all overshoot.

Fix: make pane height a model-level fact, not a view-time trick.

- Extract `paneHeight() int` (0 when `!paneActiveForCurrentSection()`,
  otherwise the same budget `viewReader*` computes — `max(height/2, 6)`
  capped by actual pane row count). One definition; `renderThreadPane`,
  `maxThreadScroll`, and the viewport all derive from it, per the
  pane-geometry extraction discussion-03 §5 already started.
- `setSize`, pane toggle (`t`), focus changes, and input open/close call a
  `syncViewportHeight()` that sets the viewport to
  `height - paneHeight()`. `composeContentColumn` stops clipping prose — it
  only stacks two correctly-sized blocks.
- Regression tests: bottom-of-section anchor reachable with pane visible;
  `G` lands on the true last line; pick cursor never enters the pane band.

### 2.3 Read-state that survives restart, users, and races

Three defects in one table:

1. **Precision.** `MarkThreadSeen` stores Unix **seconds**
   (`thread_seen.go:40`) while `LastActivity()` carries nanoseconds from the
   YAML sidecar. In-session the in-memory `seen` map hides it; after restart,
   a thread seen at `12:00:00.500` reloads as seen at `12:00:00.000` and
   `LastActivity().After(seen)` re-marks it unread. Phantom unread on every
   reopen is the fastest way to teach reviewers to ignore the unread signal.
   Fix: store `last_seen` in **milliseconds** (new column semantics, next
   migration) *and* compare with second-truncation tolerance during the
   transition.
2. **Identity.** The table is keyed `(spec_id, thread_id)` but the feature is
   specified per-user (discussion-03 §4.2). Two configured identities on one
   machine share read-state. Fix: add `user_handle` to the primary key,
   populated from the same `rc.UserHandle()` the `mine` filter uses; empty
   handle keys a `_local` bucket so unconfigured users keep working.
3. **Monotonicity.** The upsert overwrites unconditionally
   (`DO UPDATE SET last_seen = excluded.last_seen`). Mark-seen writes are
   fire-and-forget `tea.Cmd`s; two in flight can complete out of order and
   move the watermark backwards. Fix:
   `DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen)`.

All three land as one migration (`migrateV8`: rebuild `thread_seen` with the
user column and millisecond values; existing rows migrate under `_local`).
Round-trip test: subsecond activity timestamp → mark seen → close DB →
reopen → still read.

### 2.4 Ambiguity degrades, never guesses

Discussion-03's own principle — *a silently wrong anchor is worse than a
section-level one* — is violated in two places:

- `ResolveAnchorTokens` (`anchor.go:44-53`) returns `matches[0]` when the
  quote is ambiguous and the prefix doesn't discriminate;
  `disambiguateByPrefix` also falls back to first-match on a prefix miss.
  For agent-created quotes with no prefix and boilerplate phrasing ("The
  system must…"), the gutter marker and `n`-scroll can land on the wrong
  occurrence with no signal.
- `sourceBlockAt` (`anchormap.go:85`) maps a rendered line back to the
  **first** matching source token run — a repeated short line captures the
  wrong block at pick time, baking the wrong anchor into the sidecar.

Fix: make ambiguity a first-class result.

```go
// ResolveAnchorTokens gains an explicit ambiguity signal.
// ok=false when absent; ambiguous=true when >1 match survives the prefix.
func ResolveAnchorTokens(body []AnchorToken, quote, prefix string) (idx int, ok, ambiguous bool)
```

- `buildAnchorMap` treats `ambiguous` as a miss → section-level degrade, and
  records the reason so the pane can render `⚓ ambiguous — showing section`
  instead of a bare fallback.
- The picker resolves `sourceBlockAt` ambiguity **at capture time**, when the
  human is present: extend the captured prefix until unique, and if the
  block genuinely repeats verbatim, say so in the prompt label rather than
  silently pinning the first.
- `reanchorTarget` (`threadnav.go`) already refuses ambiguity across
  sections; it inherits the same tri-state within a section for free.
- Drift-corpus rows added: duplicate quote + discriminating prefix (hit),
  duplicate quote + non-discriminating prefix (visible degrade, never first).

### 2.5 Real markdown blocks, not nonblank runs

`blockBounds` (`anchormap.go:125`) expands to the contiguous nonblank run —
so picking one list item quotes the whole list, one table row quotes the
whole table, and adjacent blockquotes fuse. That contradicts discussion-03
§2.1's stated block granularity ("a paragraph, a list item, a table row")
and produces big, drift-fragile anchors.

Fix: derive block ranges from markdown structure, not blank lines. A small
`markdown.BlockRanges(sectionBody) []BlockRange` (goldmark AST walk — the
dependency already renders every section) returns `[start,end)` line ranges
per leaf block: paragraph, list item, table row, fence, blockquote
paragraph. `blockBounds` becomes a lookup. The pick cursor (§3.3) then moves
**by block**, which is both more correct and fewer keystrokes.

### 2.6 Sidecar parse errors must not read as "no threads"

`fetchData` swallows the sidecar error (`threads, _ := …List(specID)`,
`specdetail.go:1239`). A corrupt or mid-merge sidecar renders as a spec with
zero threads — indistinguishable from resolved-and-clean, on the surface
whose one invariant is "a thread is never simply absent."

Fix: carry `ThreadsErr` on the data msg; on error keep the **last-good**
thread set and show a calm notice (`threads file unreadable — showing last
known state · see spec ask --list`), mirroring the existing spec-file-moved
notice pattern. Never blank the pane on a transient parse failure.

---

## 3. Phase 2 — Precision made visible

### 3.1 The gutter tells the whole truth

`rebuildAnchors` feeds `allThreadsForSection` (unfiltered) into the map
(`threadnav.go:320`), and the overlay renders one accent glyph for any count
(`readerpick.go`). The reviewer cannot tell selected from unselected, unread
from read, open from resolved, or filtered-in from filtered-out — and a
`filter: open` pane showing "no threads match" can sit under a gutter full of
markers for resolved threads.

Extend `anchorMap` to carry per-line state, composed at view time (the
render cache stays pure, §2.3 of discussion-03 still holds):

| State | Treatment |
|---|---|
| selected thread's anchor | bold accent glyph + the anchored block briefly highlighted after a step (one render pass with an inverted/tinted style, cleared on next key) |
| unread thread on line | accent glyph + unread dot (reuses the pane's `NEW` semantics) |
| open, read | plain accent glyph (today's treatment) |
| resolved (visible only under `all`) | muted glyph |
| filtered out | no marker — the gutter agrees with the pane, always |
| degraded to section / ambiguous | no line marker; pane shows the degrade reason (§2.4) |

The pane's selected-thread block also gains its quote as a one-line chip
(`▍ "The gate can require…"`) so the conversation is anchored in both
directions — text→pane via gutter, pane→text via chip.

### 3.2 Selection reconciles when the filter moves

`selectedThread()` (`threadpane.go:257`) *renders* a fallback when the
selected ID is filtered out, but never writes it back to
`selectedThreadID` — so after `f`, the pane highlights thread A while the
model still holds vanished thread B, and the next `n` "steps" to A: a
perceived no-op, position header wrong the whole time.

Rule: **selection is reconciled at mutation time, not render time.**
`cycleFilter` (and `handleThreadsChanged`) keep the ID when it still passes;
otherwise select the nearest thread in `orderedThreads` order and update the
ID. When nothing matches document-wide, clear the selection and render the
global empty state; `n` on an empty traversal flashes
`no threads match filter: mine · f to change` instead of doing nothing.

### 3.3 The picker: forgiving, block-wise, never silent

Three adjustments to pick mode (`readerpick.go`), keeping the overlay
key-absorption pattern intact:

- **Move by block.** With §2.5's block map, `j`/`k` step between pickable
  blocks (chrome and blanks skipped), the whole block highlights, and a
  preview chip shows exactly the text that will be quoted. Fewer keystrokes,
  no "cursor on a rule line" dead spots.
- **Start where the eye is.** Enter at the block nearest the viewport
  *centre* (the reading line), not `YOffset()` top (`readerpick.go:21`).
- **Esc cancels.** Esc closing pick mode and *opening a section-level ask*
  contradicts every other Esc in the app (and the picker's own
  muscle-memory as an overlay). Esc → cancel entirely; `s` inside pick mode →
  "ask on section instead" (hinted in the pick-mode footer); Enter on an
  unmappable block is impossible by construction after §2.5. This
  supersedes discussion-03 §2.3a's esc-falls-back rule — a deliberate,
  narrow behaviour change, called out in release notes.

### 3.4 Resolved activity counts as activity

`LastActivity()` (`thread.go:85`) considers replies and creation only. A
resolve by a teammate doesn't trip unread — under the `all` filter a thread
can flip to resolved with no signal to a reviewer who was waiting on it.
Include `ResolvedAt` in the max. (Once discussion-02 lands verdict/outcome
mutations, those inherit the same rule.)

---

## 4. Phase 3 — Direct manipulation

Keyboard flow stays primary; the mouse becomes a peer, not a garnish.
`handleContentClick` (`mouse.go:101`) currently stops at the section
navigator ("Prose and thread clicks remain keyboard/wheel-driven").

- **Click a gutter badge** → select that thread (the `n`-step path minus the
  scroll: focus pane, mark seen). A multi-thread badge (`▍2`) cycles through
  its co-anchored threads on repeat clicks.
- **Click a collapsed thread row** in the pane → select it; click the
  selected row again → focus the pane (Enter-parity, same rule the list
  views already follow via `clickActivated`).
- **Click a block in pick mode** → move the pick cursor there; click the
  highlighted block → confirm (Enter-parity).
- **Wheel scrolls the surface under the pointer.** `wheelScroll`
  (`specdetail.go:395`) routes by `paneFocused`, so wheeling over prose
  scrolls the pane whenever the pane has key focus. Hit-test the Y
  coordinate against `paneHeight()` (§2.2's single geometry makes this a
  two-line predicate) and route accordingly.

All of it lives in the existing single mouse entry point; no new event
plumbing.

---

## 5. Phase 4 — The interface explains itself

### 5.1 Mode-truthful help

`help.go` renders `ReaderBindings` + `ActionBindings` for every detail
context — in reader mode that shows `a`=advance beside `a`=ask, `x`=block
beside `x`=resolve. The cockpit now has real modes; help must follow:

| Mode | Sections shown |
|---|---|
| detail overview | Spec Actions, Navigation, Views, Global |
| reader (prose) | Reader keys, Navigation, Global — **no** Spec Actions |
| reader (pane focused) | Pane keys (reply/resolve/unread/repair), Reader nav, Global |
| compose | esc/ctrl+s + textarea hint only |
| pick mode | pick keys only |

`helpModel.setContext` gains the sub-mode label from the detail model
(`Detail: SPEC-12 · reader` / `· threads` / `· compose` / `· pick`). Same
overlay, truthful contents.

### 5.2 Hints that fit

The pane hint line (`threadHintLine`, `threadpane.go:678`) is a fixed
~70-col string; below ~78 cols it wraps and breaks the pane's row budget
(the exact class of bug the geometry extraction exists to prevent).
Priority-ordered hints — `[r]eply [x]resolve · n/p · ? more` first, filter
and hide dropped as width shrinks — rendered through a width-aware
`HintStrip` variant. The `?` in the hint opens mode-truthful help (§5.1),
so nothing is lost, only deferred.

The progress header (`paneHeaderLine`, truncated blindly at
`threadpane.go:375`) gets the same treatment with the opposite priority:
position (`3/7`) and filter survive to the narrowest width; resolved/unread
totals drop first.

### 5.3 Safety on one-key mutations

`x` (resolve) and Enter (re-anchor) are irreversible single keys operating
on the review's source of truth. Two cheap guards:

- **Undo toast.** After resolve/re-anchor, the status toast carries
  `· u undo` for its 2s lifetime; undo re-opens (`Status=open`, clears
  `ResolvedBy/At`) or re-anchors back. Plain sidecar mutations, ID-keyed
  merge handles sync — no new machinery. (While the toast is live, `u`'s
  read-toggle meaning is suppressed; after it expires, `u` is unread-toggle
  again.)
- **Draft preservation.** `t` (hide pane) and section jumps while
  `input.active()` currently drop the draft silently. Keep the draft in the
  model when the pane hides; restore it on re-show; a section jump with a
  non-empty draft asks once (`draft in progress — esc again to discard`).

### 5.4 After a mutation, the cockpit knows where to stand

`threadsChangedMsg` re-lists threads but carries no intent: after **ask**,
the new thread isn't selected; after **resolve** under `filter: open`, the
thread vanishes and selection falls to "first in section" wherever that is;
after **re-anchor**, the reviewer is left standing in `_unanchored` while
the thread moved elsewhere. Add `SelectID string` and `FollowAnchor bool`
to the msg: ask selects the created thread; resolve steps to the next
thread in `orderedThreads` (the review keeps moving forward); re-anchor
follows the thread to its new section via the existing
`pendingAnchorThreadID` scroll path.

---

## 6. Phase 5 — The review pass as a first-class motion

### 6.1 Completing a pass means something

Wrapping (`stepThread`, `threadnav.go:190`) flashes `wrapped · thread 1/7`
and keeps circling. Completing a pass is the natural summary moment:

- First wrap of a traversal renders a pane-level summary card instead of
  stepping: `review pass complete · 7 visited · 2 open · 1 unread`, with
  one-key follow-ups — `n` continue from the top, `u` jump to first unread,
  `f` change filter, `esc` dismiss. (Once discussion-02 ships, add the
  verdict affordance here: "0 open blocking — approve?".)
- The traversal-visited set already exists implicitly (mark-seen on step);
  the card derives from `orderedThreads` + `seen`, no new state store.

### 6.2 Enter the cockpit where the work is

`o` opens the reader at `firstReadableSectionIndex` (problem statement)
regardless of why the reviewer came. Two cheap entries:

- **Resume:** reopening a spec read this session returns to the last
  section/offset (already held in the model; persist per spec in the
  existing settings table for cross-session resume).
- **Review intent:** entering from the Reviews tab (or any surface that
  knows threads are waiting) lands on the first thread in `orderedThreads`
  with the pane focused — the reader *is* the review queue from keystroke
  one. Plumbed as an optional field on the existing
  `navigateToSpecMsg`, exactly like `navigateToSpecSectionMsg` deep-links.

### 6.3 Pane modes for different moments

One pane geometry serves three needs badly. Keep the budget math, make the
budget a mode: **peek** (header + selected thread chip, ~4 rows), **review**
(today's half-height), **conversation** (~⅔ height for long debates and
drafting). `T` cycles; the mode is remembered per session. §2.2's
`paneHeight()` is the only place the numbers live, so this is a constant
table, not new geometry. This supersedes the deferred inline-column idea
from discussion-03 §5 — same itch, no third column, no 140-col floor.

---

## 7. Explicitly out of scope

- **AI affordances** (summarise/draft/propose-resolution) — still gated on
  discussion-02 + a configured `ai` integration, per discussion-03 §7. The
  review-pass summary card (§6.1) is their natural future home.
- **Thread kinds / blocking filter** — discussion-02. The filter cycle,
  gutter colours, and summary card all have marked extension points.
- **CLI parity for quotes** — the discussion-03 scope cut stands.
- **Cross-machine read-state sync** — per-machine remains a documented
  property; §2.3 makes it per-user per-machine, which is the fixable half.

---

## 8. PR stack

| PR | Scope | Depends on |
|----|-------|------------|
| 1 | **Trust:** sidecar hash + unconditional thread application (§2.1); `paneHeight()` extraction + viewport height sync (§2.2); `thread_seen` migration — ms precision, user key, MAX watermark (§2.3); sidecar error surfacing (§2.6) | — |
| 2 | **Precision:** ambiguity tri-state in `ResolveAnchorTokens` + degrade-with-reason (§2.4); `markdown.BlockRanges` + block-wise `blockBounds` (§2.5); resolved-counts-as-activity (§3.4) | 1 |
| 3 | **Visibility:** gutter states + selected-block flash + quote chip (§3.1); filter-time selection reconciliation (§3.2); block-wise picker + Esc-cancels (§3.3) | 2 |
| 4 | **Interaction:** mouse targets + pointer-scoped wheel (§4); mode-truthful help (§5.1); width-aware hints/header (§5.2); undo toast + draft preservation (§5.3); post-mutation selection intent (§5.4) | 1, 3 |
| 5 | **Flow:** review-pass summary card (§6.1); resume + review-intent entry (§6.2); pane modes (§6.3) | 3, 4 |

PR 1 is the substance and ships alone if needed. PRs 2–3 restore the
precision the anchor model promised. PRs 4–5 are where "lovable" lives, and
they deliberately come last.

---

## 9. Tests

- **Refresh:** sidecar-only change with identical markdown hash updates the
  pane (watcher-path integration test, extends `app_watch_test.go`); corrupt
  sidecar preserves last-good threads and shows the notice.
- **Geometry:** with the pane visible — bottom-of-section anchor reachable;
  `G` lands on the true last visible line; pick cursor never enters the pane
  band; `PageDown` never skips prose. Snapshot rows at 80/120 cols for each
  pane mode.
- **Read-state:** subsecond activity → mark seen → DB close/reopen → still
  read; two identities on one DB don't share rows; out-of-order MarkThreadSeen
  never regresses the watermark.
- **Ambiguity corpus** (extends the drift corpus): duplicate quote +
  discriminating prefix → hit; non-discriminating prefix → visible
  section-level degrade with reason; picker capture on repeated blocks
  extends prefix until unique.
- **Blocks:** `BlockRanges` table — paragraph, list item (not whole list),
  table row (not whole table), fence, blockquote; picker round-trips each.
- **Selection:** filter cycle reconciles `selectedThreadID`; resolve under
  `open` steps forward; ask selects the created thread; re-anchor follows to
  the target section.
- **Help/hints:** per-mode help contexts contain no cross-mode bindings;
  hint line fits at 60/80/120 cols without wrapping (row-count assertion, the
  pane budget's own invariant).
- **Flow:** first wrap renders the summary card with correct counts; `n`
  from the card resumes at thread 1; review-intent entry lands focused on
  the first ordered thread.

---

## 10. Risks and constraints

- **Migration touches every install.** The `thread_seen` rebuild (§2.3) must
  be idempotent and tolerate the `_local` → handle transition; test both
  fresh and migrated paths. Losing read-state in migration is acceptable
  (it's per-machine convenience data); corrupting the migrations table is not.
- **Viewport resize churn.** `syncViewportHeight()` runs on pane toggle,
  focus change, and input open/close — each can shift the visible window.
  Anchor the top visible line across pane-height changes so prose never
  jumps under the reviewer's eyes.
- **Esc-cancels is a behaviour change.** Small, but it breaks the shipped
  §2.3a contract; release notes + the pick-mode footer hint carry it. No
  other key semantics change in this plan.
- **Goldmark AST for BlockRanges** must tokenise identically to
  `TokenizeAnchor` at the edges (emphasis, table pipes) or picker capture
  and anchor resolution disagree — the round-trip tests in §9 are the pin.
- **The gutter state table adds view-time work per visible line.** It stays
  O(visible rows) with map lookups; the anchor map itself is still built
  only on render/thread change, never in `view()`.
- **Undo suppressing `u` for 2s** is a deliberate, tiny mode. If it proves
  confusing, the fallback is a dedicated undo key — but don't spend a global
  key before the toast pattern has been tried.
- **One-file-per-concern still applies.** New logic lands in
  `anchormap.go`/`threadnav.go`/`readerpick.go` or new small files
  (`panegeometry.go`, `blockranges.go`); `specdetail.go` and
  `threadpane.go` must not grow another concern.
