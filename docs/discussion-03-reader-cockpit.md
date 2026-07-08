# Discussion: The Reader Cockpit

> **Status:** Implemented (PRs 1–4; PR 5 blocked on discussion-02) · **Owner:** Discussion / TUI · **Scope:** `internal/tui/specdetail.go`, `internal/tui/threadpane.go`, `internal/tui/anchormap.go` (new), `internal/tui/help.go`, `internal/thread/`, `internal/markdown/anchor.go` (new), `internal/store/`, `internal/mcp/handler.go`
> **Effort estimate:** Large · **Risk:** Medium (concentrated in `internal/tui`, covered by snapshot/golden tests)
> **Delivery:** a stack of five PRs (see §8). Anchoring (PR 1) and navigation (PR 2) are independently shippable.
> **Dependency status (verified against the tree):** discussion-01's model work has **shipped** — `Thread.Mentions`/`Participants()` exist and the mention grammar already accepts `:` (`internal/thread/mention.go`). PR 1 is unblocked today. discussion-02 (`Kind`/`IsBlocking`) has **not** shipped; everything here that touches kinds degrades until it does (called out inline).

The reader surfaces threads as a half-height bottom pane scoped to one section
at a time. Thread selection (`selectThread`) only moves within the focused
section; there is no way to step through all open threads in a document, no
read/unread state, no filter, and no finer anchor than a section slug. Reviewing
a spec means visiting every section by hand and toggling the pane to check for
comments.

It turns the reader into a review cockpit: span-level anchoring that degrades
gracefully, document-wide thread navigation, filters, read-state, and AI
affordances under the existing accept/edit/skip contract.

**Scope decision: the cockpit is a TUI-and-MCP surface.** CLI parity for quote
anchors (`spec ask --quote`) and AI affordances (`--summarise`) is deliberately
out of scope. Quote capture needs the section text in hand — pasted CLI text
diverges from source markup, multi-match disambiguation needs interactive
resolution, and none of that is worth building for a flag. `spec ask
--section` keeps creating section-level threads exactly as today; the two
anchor shapes coexist in the same sidecar with no migration. The one CLI
touch that stays is read-only: the `--list` unanchored marker in §2.4.

---

## 1. Outcome

- A thread can anchor to a **quoted text span** within a section, not just the
  section. The anchor resolves by searching the live section text and falls back
  to the section header when the text has changed — keeping the
  "never orphaned" guarantee the current design prizes.
- The reader shows a **gutter marker** beside the rendered line a thread
  anchors to, composed as a view-time overlay so the render cache stays pure.
- `n` / `p` step through **all** open threads in the document, scrolling the
  reader to each anchor and focusing the pane — the core review motion.
- A **filter** (`open | all | blocking | mine | unread`, default `open`)
  narrows what the pane and the stepper traverse.
- **Read-state** is tracked per user per machine; the pane highlights "new
  since last visit", `u` toggles it by hand, and a header shows review
  progress (`3 / 7 resolved · 2 unread`).
- AI affordances (summarise threads, draft reply, propose resolution) appear
  under the accept/edit/skip flow when an `ai` integration is configured —
  reader keys only, no CLI flags.

---

## 2. Span anchoring with graceful degradation

### 2.1 Model

`internal/thread/thread.go`: add an optional quote anchor. Section remains the
durable anchor; the quote is a refinement.

```go
type Thread struct {
    // ... existing fields ...

    // Quote is the verbatim text span the thread refers to within Section.
    // Optional. When the text no longer appears in the section, the thread
    // degrades to a section-level anchor — it is never orphaned.
    Quote string `yaml:"quote,omitempty"`

    // QuotePrefix is a short run of text immediately before Quote, used to
    // disambiguate when Quote appears more than once in the section.
    QuotePrefix string `yaml:"quote_prefix,omitempty"`
}
```

No line numbers, no offsets — those shift on every edit. The anchor is content,
resolved at render time. Quotes are captured at **block granularity** (a
paragraph, a list item, a table row — see §2.3): blocks survive reflow and
rewording far better than single lines, so the anchor keeps resolving months
later, and co-anchored threads collapse naturally onto one gutter badge.

### 2.2 Source-side resolution

New file `internal/markdown/anchor.go`:

```go
// AnchorMatch locates a quote within section content.
type AnchorMatch struct {
    Found bool
    Line  int // 0-based line within the section body where the quote starts
    Col   int
}

// ResolveAnchor finds quote (disambiguated by prefix) within sectionBody.
// Matching is whitespace-normalised so reflowed markdown still resolves. When
// the quote is absent, Found is false and the caller anchors to the section.
func ResolveAnchor(sectionBody, quote, prefix string) AnchorMatch
```

Resolution is best-effort and pure. Its job is **existence and
disambiguation only** — it answers "does this quote still live in this
section, and where in the source?". It never touches rendered output.

### 2.3 Rendered-side mapping: one component, three consumers

The obvious implementation — inject a zero-width marker into the source line
before rendering and scan for it afterwards — **does not survive the
renderer**. Glamour parses the body through a goldmark AST, and the reader
routes tables through a separate custom path (`splitTableSegments` →
`renderTableBlock`). A marker byte at line start destroys block syntax
(`\u200b- item` is no longer a list item; same for headings, blockquotes,
table rows, and fences — exactly the lines reviewers most want to anchor to),
may not survive inline AST round-trips, and worst of all **poisons the render
cache**: `readerCacheKey` is keyed on the spec's content hash, which thread
mutations don't change, so markers baked into cached renders go stale on every
`threadsChangedMsg` or force full cache invalidation — defeating the
single-flight/coalesce design `specdetail.go` works hard to preserve.

Instead, match **after** rendering and overlay at view time. New file
`internal/tui/anchormap.go` (one file per concern):

```go
// anchorMap maps threads to rendered-line positions within one section's
// rendered output, and rendered lines back to source blocks. It is pure and
// memoized per (cacheKey, threadsRevision).
type anchorMap struct { /* ... */ }

// buildAnchorMap resolves each thread's quote against the raw section body
// (markdown.ResolveAnchor), then locates the match in the rendered output by
// token matching: strip ANSI, whitespace-normalise, and search for the
// quote's token stream. Glamour reflows text but preserves content and order,
// so a token match is stable where byte offsets are not.
func buildAnchorMap(sectionBody, rendered string, threads []thread.Thread) anchorMap

// renderedLineFor returns the rendered line a thread anchors to, ok=false on
// a degrade-to-section miss.
func (am anchorMap) renderedLineFor(threadID string) (int, bool)

// sourceBlockAt maps a rendered line back to its source block — the reverse
// direction, used by the anchor picker (§2.3a).
func (am anchorMap) sourceBlockAt(renderedLine int) (block string, prefix string, ok bool)
```

The map is computed in `handleRenderedMsg` and `handleThreadsChanged` — never
in `view()` — and cached on the model alongside the render. Three features
consume the same map, which is why it is one component and not three ad-hoc
scans:

1. **Gutter markers.** `viewReader*` composes the gutter glyph (`▍`) onto the
   cached rendered content as a view-time overlay at each thread's
   `renderedLineFor`. The rendered prose in `readerCache` stays pure — thread
   churn never invalidates it. The glyph is coloured by the thread's kind once
   discussion-02 ships; until then it renders in the accent colour. Multiple
   threads on one block collapse to a count badge. On a miss, the thread falls
   back to the section-level pane treatment already in place.
2. **Scroll-to-anchor.** `stepThread` (§3) needs a viewport offset, and
   `readerViewport` scrolls **rendered** lines — a source line number is
   useless to it. `renderedLineFor` is the offset.
3. **Quote capture.** The anchor picker (below) maps the picked rendered line
   back to its source block via `sourceBlockAt`.

### 2.3a Capturing a quote: the anchor picker

The reader has no text-selection primitive, and an implicit cursor (e.g. "the
first visible line when `a` is pressed") anchors to whatever happens to sit at
the top of the viewport while the reviewer reads mid-screen — a silently wrong
anchor is worse than a section-level one. Capture is explicit instead:

- `a` is **unchanged**: it opens the ask input anchored to the section, exactly
  today's behaviour. Zero surprise for existing users.
- `A` enters **pick mode**: a highlighted line-cursor over the rendered body,
  `j`/`k` (or `↑`/`↓`) to move, `Enter` confirms, `Esc` falls back to a
  section-level ask. On confirm, `sourceBlockAt` yields the source block; the
  ask input opens with the quote visible in its prompt label
  (`ask ▍"The gate can require…" › `) so the reviewer sees exactly what the
  thread will pin to before typing. `Quote` is set to the block text,
  `QuotePrefix` to the tokens immediately before it in the section source.
- Pick mode is a self-contained key layer, following the existing
  overlay-absorbs-all-keys pattern in `handleKey` — no key inside it can leak
  to reader hotkeys.

The pane input flow (`threadInput`, `submitInput` in `threadpane.go`) carries
`quote`/`prefix` alongside the existing `section`.

### 2.4 When the section itself is gone

§§2.1–2.3 degrade a drifted *quote* back to its section. They don't cover the
section disappearing entirely: `Thread.Section` is a slug derived from live
heading text (`markdown.slugify`), not a stored ID, so rewording a heading —
"## 7. Technical Implementation" → "## 7. Technical Approach" — is enough to
silently sever every thread anchored to `technical_implementation`.
`threadsForSection(slug)` only matches an exact live slug, so these threads
stop appearing anywhere in the reader (not degraded — just gone from this
view), while `spec ask --list` and the dashboard
(`discussion-01-awareness-loop.md` §4.1) keep showing them unchanged, because
neither validates the slug against current sections.

Close that gap instead of leaving it implicit:

- `orderedThreads` (§3) partitions into two buckets: threads whose `Section`
  resolves against `m.readableSections()`, and those that don't. The second
  bucket renders as a synthetic `Unanchored` entry at the **end** of the
  section list (its own slug, `_unanchored`, that no real section ever
  produces — headings never slugify to a leading underscore) so `n`/`p`
  stepping and the filter still reach them. Trailing rather than leading keeps
  the `1-9` section jumps and `firstReadableSectionIndex` stable — the whole
  point of document-wide navigation is that nothing requires guessing which
  section a thread used to belong to, not that it interrupts the reading
  order.
- **One-key repair.** When an unanchored thread carries a `Quote` that
  resolves in **exactly one** live section, the pane offers
  `⚠ section renamed? · enter re-anchor to technical_approach`. Accepting
  rewrites `Thread.Section` — an ordinary sidecar mutation like reply or
  resolve; the ID-keyed merge layer handles sync. Ambiguous or quote-less
  threads stay in `_unanchored` rather than guessing.
- `spec ask --list` (`cmd/ask.go`) marks these with
  `⚠ section not found — now unanchored` next to the thread instead of
  printing a slug that no longer resolves to anything. This is the one CLI
  change that survives the parity cut: it is read-only display in an existing
  command, and it is the safety valve for the invariant that a thread is never
  simply absent from a surface claiming to show all threads.

Spec headings are template-driven (`markdown.ValidSectionSlugs`), so severing
should be rare in practice — which is exactly why the cheap repair path is
proportionate and a heavier re-anchoring engine is not.

---

## 3. Document-wide navigation

`internal/tui/specdetail.go` + `threadpane.go`.

### 3.1 Selection by ID, not index

Before adding document-wide stepping, fix the substrate: thread selection is
positional (`threadIdx` within the focused section), and `specdetail.go`
already carries three separate clamp/restore paths
(`restoreThreadSelection`, `handleThreadsChanged`, `selectThread`) to keep an
index honest against a live-refreshing sidecar. Stepping across sections
multiplies those bugs. Replace the index with `selectedThreadID string` as the
single source of truth; indexes are derived where rendering needs them. The
refresh path already restores by ID — this makes that the rule, not the
exception.

### 3.2 The stepper

```go
// orderedThreads returns the active filter's threads across the whole document
// in reading order: by section position, then by source anchor line
// (markdown.ResolveAnchor — pure and renderer-independent, since only the
// focused section has an anchorMap), then by created. Threads in the
// _unanchored bucket (§2.4) traverse last, matching their trailing entry.
func (m specDetailModel) orderedThreads() []thread.Thread

// stepThread moves to the next/prev thread in orderedThreads, switching the
// focused section, scrolling the reader to the thread's rendered anchor line,
// focusing the pane, and selecting the thread. It wraps at either end with a
// one-shot status flash ("wrapped · thread 1/7") — a review pass never
// dead-ends.
func (m specDetailModel) stepThread(delta int) (specDetailModel, tea.Cmd)
```

`n`/`p` in `updateReader` currently do double duty: next/prev section when the
pane is unfocused, next/prev thread *within the current section* when it is
focused. Document-wide stepping replaces both: `n`/`p` become
`stepThread(delta)` unconditionally, and section-stepping moves to `[`/`]`
(verified unbound anywhere in the TUI). This is a deliberate behaviour change
for anyone who learned `n`/`p` as section-next/prev — thread review is the
more frequent motion — call it out in the release notes.

Scrolling to an anchor sets the viewport offset from
`anchorMap.renderedLineFor`; a section-level thread scrolls to the section
top, as section navigation does today.

Header shows position: `Thread 3 / 7`.

### 3.3 Key plan (audited against the live dispatch chain)

In reader mode, `updateDetail` intercepts only `ctrl+c`, `?`, `E`, and
`tab`/`shift+tab`; everything else reaches `updateReader`, and the
overview-mode spec-action keys (`handleSpecAction`) never fire. Audited
bindings:

| Key | Cockpit meaning | Clash status |
|-----|-----------------|--------------|
| `n` / `p` | thread next/prev (document-wide) | rebind of existing reader keys; `n`=new-spec / `p`=push are top-level/overview-only — no cross-mode clash |
| `[` / `]` | section prev/next | free — unbound anywhere |
| `f` | cycle filter | free in reader; contextual overload with overview `f`=focus (same accepted pattern as `x` block/resolve) |
| `u` | toggle read/unread | free in reader; contextual overload with overview `u`=unblock |
| `A` | anchor-pick mode | free |
| `S` | AI summarise (§7) | free (`s`=sync is overview-only) |
| `ctrl+g` | AI draft while composing (§7) | free — see textarea note below |
| `g` / `G` | top / bottom of section | unchanged, and **reserved**: no reader-mode `g`-chords, ever. The g-prefix state machine does not exist in reader mode (`g` is GotoTop), and introducing chords later would collide with it. |

Two dispatch rules the implementation must pin down:

- **`tab` ownership follows pane visibility, full stop.** Today `updateDetail`
  steals `tab` for view-switching unless `paneActiveForCurrentSection()` — a
  predicate the filter work changes (an empty-filter state renders the pane,
  §4.1). If the predicate flips with filter state, `tab` silently alternates
  between "focus pane" and "switch view". Rule: pane visible ⇒ reader owns
  `tab`.
- **Compose-mode keys must be ctrl-chords, and the textarea reserves most of
  them.** While composing, `handleThreadInputKey` routes everything except
  `esc`/`ctrl+s` to the textarea, whose default keymap binds
  `ctrl+a/b/d/e/f/h/k/m/n/p/t/u/v/w` for emacs-style editing. `ctrl+g` is
  free and mnemonic ("generate"); do not use `ctrl+d` or `ctrl+p`, which would
  silently break editing.

Three surfaces carry the old `n`/`p` hints and all must move together:
`threadHintLine` (`threadpane.go`), the section footer in
`renderSectionContent` (`specdetail.go` — easy to miss), and the reader
section of the help overlay (`help.go`). The help overlay renders per-context,
so `f`/`u` show their reader meaning beside their overview meaning.

---

## 4. Filters and read-state

### 4.1 Filter

Add `threadFilter string` to `specDetailModel` with values
`open | all | blocking | mine | unread`. **Default `open`** — this is a review
cockpit; resolved threads are noise and `all` is one keypress away. `f`
cycles. `orderedThreads` and `threadsForSection` apply the filter. The pane
header names the active filter and the matching count.

- `blocking` uses `Thread.IsBlocking()` (discussion-02). Until that ships, the
  filter position is hidden from the cycle — never a dead stop.
- `mine` uses `Thread.Participants()` against the viewer identity
  (`rc.UserHandle()`, the same identity `author()` already uses).
- `unread` uses read-state (§4.2) with **snapshot semantics**: the unread set
  is captured when the filter is activated and refreshed only when `f`
  re-cycles to it. Without this, selecting a thread marks it seen, it vanishes
  from the filtered traversal, and the cursor jumps — items must never
  disappear under the cursor.
- **Empty filter state renders, never hides.** `paneActiveForCurrentSection`
  currently hides the pane when there are no threads; under a filter that
  reads as breakage. Render
  `no threads match · filter: blocking · f to change` instead.

### 4.2 Read-state

Read-state is per-user and inherently personal, so it does **not** belong in the
git-synced sidecar (noise commits, merge churn). Store it in the local store
(`internal/store`, the SQLite boundary — AGENTS.md: only `internal/store`
touches SQLite). It is therefore **per machine**, not per user across
machines — accepted and documented, not accidental.

`internal/store/db.go` versions its schema through numbered migrations, each
a self-contained `CREATE TABLE IF NOT EXISTS` run once and recorded in the
`migrations` table. Add the next migration in that sequence (the tree is at
V6 as of SPEC-028's search index, so this lands as `migrateV7`):

```sql
CREATE TABLE IF NOT EXISTS thread_seen (
    spec_id   TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    last_seen INTEGER NOT NULL, -- latest activity (unix seconds) the user has viewed
    PRIMARY KEY (spec_id, thread_id)
);
```

Store API (`internal/store`), alongside the existing per-file methods
(`activity.go`, `sessions.go`, `settings.go` — add `thread_seen.go`):

```go
func (db *DB) ThreadSeen(specID string) (map[string]time.Time, error)
func (db *DB) MarkThreadSeen(specID, threadID string, at time.Time) error
```

A thread is **unread** when its latest activity timestamp (last reply `At`, else
`Created`) is after the recorded `last_seen`, or no row exists.

- Selecting a thread marks it seen (records the latest activity timestamp).
  The write is a **fire-and-forget `tea.Cmd`**, best-effort like
  `logThreadActivity` — never a synchronous SQLite write on the `n` keypress
  hot path.
- `u` toggles the selected thread's read-state by hand. Auto-mark-on-select
  without an undo makes fast `n n n` scanning destructive; "keep this for
  later" is a core review motion.
- The pane renders unread threads with an accent dot and a `NEW` tag; the
  header shows `2 unread`.
- Read-state degrades cleanly: when the DB is unavailable, every thread reads
  as unread.

---

## 5. Pane layout

`internal/tui/threadpane.go` currently collapses every non-selected thread to
one line in a half-height windowed buffer. In scope:

- **Progress header.** Replace the bare `Threads (N open)` with
  `Threads · 3/7 resolved · 2 unread · filter: open`.
- **Full-conversation view.** The selected thread already word-wraps question +
  replies; add author/time per reply and (once discussion-02 ships) the kind
  tag. Keep the `↑/↓ more` windowing for long threads.
- **Geometry extraction.** `maxThreadScroll` hand-mirrors `renderThreadPane`'s
  geometry with hardcoded width/height constants; the progress header and
  filter/empty states add more chrome rows to keep in sync. Extract a shared
  pane-geometry struct *before* adding rows — the mirror is already one
  header-line change away from a scroll bug.

**Deferred: inline-beside-text mode.** An earlier draft rendered the selected
thread in a right-hand column aligned to its anchor line. The existing
≥100-col sidebar is the *section navigator* on the left; a thread column is a
third column needing ~140+ cols and new geometry — not "the existing
responsive split". It is speculative until gutter anchoring proves out in real
use. Revisit after PRs 1–4 land; if built, it gets its own
`readerThreadSidebarMinWidth ≥ 140` constant and an explicit three-column
budget.

These are rendering changes within the existing pane geometry contract; the
height-budget math (`renderThreadPane`) is preserved.

---

## 6. Agent identity in threads

`internal/mcp/handler.go` attributes every agent contribution to the literal
`"agent"`. Replace with a stable agent identity so the reader can distinguish
"the build agent said X" from a human:

- Thread `Author` / `Reply.Author` accept an agent handle of the form
  `agent:<adapter>` (e.g. `agent:claude`), resolved from the MCP handler's
  known adapter context. Where unknown, retain `"agent"`.
- The mention-grammar dependency is **already satisfied**: `ParseMentions`'s
  pattern (`internal/thread/mention.go`) accepts `:` in handles, with the
  `agent:<adapter>` rationale documented at the pattern. No precondition work
  remains — `@agent:claude` parses whole today.
- **`spec_create_thread` must never create an unreachable blocking thread.** An
  agent-raised thread with no supplied mention has `Participants() ==
  [author]` — itself. `discussion-01`'s dashboard "your turn" predicate (§4.1)
  requires the viewer to *be* a participant, so a thread no human is ever
  mentioned on never appears on anyone's dashboard, `no_blocking_threads`
  (`discussion-02` §3) still blocks the pipeline, and the spec sits stuck with
  no visible reason on any surface a human actually looks at. Default the
  mention set when the agent supplies none: the spec's `Author` plus the
  current stage's owning role's handles (already resolved for
  `review_approved_by`, `discussion-02` §3.2 — reuse that lookup). The MCP
  argument for an explicit mention still overrides the default. This is a
  gate-level correctness rule, tested as such (§9).
- The reader renders agent authors with a distinct glyph (the existing
  `internal/tui/glyph` set) so agent comments are visually separable.
- Add `spec_create_thread` to the MCP toolset (currently agents can only list
  and reply — `discussion-02` adds verdict/resolve; this adds create) so an
  agent can raise a `blocking` thread when the spec contradicts the
  implementation. The tool accepts an optional `quote` — agents are the main
  non-TUI quote producer, since they hold the exact section text when they
  raise a contradiction. This is independent of the CLI parity cut.

---

## 7. AI affordances

Gated entirely on a configured `ai` integration (Decision 013: progressive
enhancement, accept/edit/skip, never auto-write). All three are **reader keys
only** — no CLI flags — and all return a draft the user accepts, edits, or
skips; none mutate the sidecar without confirmation.

- **Summarise open threads.** `S` in the reader: feed open threads to the `ai`
  service, render a short digest. Read-only.
- **Draft a reply.** While composing, `ctrl+g` requests an AI draft seeded
  with the thread and the anchored section text (the quote, when present,
  gives the model the precise context); the draft populates the textarea for
  editing before send.
- **Propose a resolution.** On resolve, offer an AI-drafted `--outcome`
  (`discussion-02` §4) that the user accepts/edits/skips before it is written
  and promoted to the Decision Log.

Each path uses the existing AI service layer and returns `(string, error)` or
`(nil, nil)`; the caller handles the nil case (AGENTS.md: AI is never a hard
dependency).

---

## 8. PR stack

| PR | Scope | Depends on |
|----|-------|------------|
| 1 | Span anchoring: `Quote`/`QuotePrefix` model, `markdown.ResolveAnchor`, `tui.anchorMap` (post-render token matching), view-time gutter overlay, `A` anchor picker, `_unanchored` bucket + one-key re-anchor (§2.4), `--list` unanchored marker | — (discussion-01 model work verified shipped) |
| 2 | Document-wide navigation: `selectedThreadID` refactor, `orderedThreads`, `stepThread` with wrap-flash, `n`/`p` rebind, `[`/`]` sections, position header, all three hint surfaces + help contexts, `tab`-ownership rule | 1 |
| 3 | Filters + read-state: `migrateV7` `thread_seen` + store API, `threadFilter` (default `open`, snapshot `unread`), `f` key, `u` toggle, empty-filter pane state, async mark-seen | 1 |
| 4 | Pane layout: progress header, full-conversation rendering, pane-geometry extraction | 1, 2, 3 |
| 5 | Agent identity (`agent:<adapter>`, `spec_create_thread` with default-addressee + optional `quote`) + AI affordances (`S` summarise, `ctrl+g` draft, resolution outcome) | discussion-02 (role lookup + kinds); 1 |

PRs 1–3 are the substance and can land in sequence. PR 4 is polish; PR 5
waits on discussion-02's role lookup for the default-addressee rule. Within
PR 1, land `ResolveAnchor` + model fields first, then `anchorMap` + gutter +
picker — the second half is where the risk lives.

---

## 9. Tests

- `internal/markdown`: `ResolveAnchor` table — exact match, whitespace-reflowed
  match, prefix-disambiguated duplicate, absent quote (graceful miss).
- **Anchor-drift corpus** (`internal/tui` golden): realistic edits applied to
  fixture sections — reword within the block, reflow, paragraph move, block
  deletion, heading rename — each asserting the exact degrade path:
  quote hit → gutter line; quote miss → section-level; section miss →
  `_unanchored`; unanchored + unique quote match → repair offer. This table
  *is* the invariant the plan is built on.
- `internal/tui/anchormap`: token matching through Glamour output — plain
  paragraphs, list items, table rows (the custom table path), wrapped long
  lines; `sourceBlockAt` round-trip for the picker.
- `internal/tui`: snapshot tests (existing `snapshot_test.go` harness) for
  gutter markers and count badges, pick-mode cursor, the progress header,
  unread `NEW` tags, empty-filter pane state, at wide/narrow widths;
  `stepThread` crossing section boundaries and wrapping; filter cycling with
  the `blocking` position hidden pre-discussion-02; `tab` ownership with the
  pane visible-but-empty.
- `internal/store`: `ThreadSeen` / `MarkThreadSeen` round-trip; unread
  predicate with and without a row.
- `internal/mcp`: `spec_create_thread` writes an anchored thread when `quote`
  is supplied; agent author is `agent:<adapter>` when the adapter is known; a
  call with no mention defaults `Mentions` to the spec author and the current
  stage's owning role (the unreachable-blocking-thread guard).
- AI paths: with `provider: none`, `S` and `ctrl+g` are no-ops that leave the
  sidecar and the textarea unchanged.

---

## 10. Risks and constraints

- **Anchor resolution is best-effort by design.** A quote that no longer
  matches degrades to section-level — never an error, never an orphan. This
  preserves the v1 robustness guarantee while regaining precision. Do not
  store offsets.
- **Section-level orphaning is handled too, not just quote-level.** §2.4
  extends the quote-degrades-to-section guarantee to cover the section itself
  disappearing. Both paths must stay consistent: a thread is never simply
  absent from a surface that claims to show "all threads."
- **Never mutate source markdown for rendering concerns.** The rejected
  marker-injection approach is documented in §2.3 precisely so it isn't
  re-proposed: it corrupts block syntax and poisons the render cache. Gutter
  state is a view-time overlay; the render cache stays keyed on content alone.
- **Token matching is the load-bearing trick.** `anchorMap` assumes Glamour
  preserves text content and order through reflow. That holds for prose, lists
  and the custom table path today, and the anchormap test suite pins it; a
  renderer upgrade that breaks it degrades anchors to section-level (visible
  in the drift corpus), not to wrong lines.
- **Read-state must stay local.** It is personal and high-churn; putting it in
  the git sidecar would create noise commits and merge churn. SQLite via
  `internal/store` only, async writes only, per-machine by design.
- **Key changes ship together with their surfaces.** The `n`/`p` rebind
  touches three hint surfaces plus help; landing the handler without the hints
  (or vice versa) ships a lying UI. The `tab`-ownership and `g`-reservation
  rules in §3.3 are constraints on future work, not just this plan.
- **TUI surface area.** The changes concentrate in `specdetail.go` /
  `threadpane.go`, both already large. New logic lives in small files
  (`anchormap.go`, ordered-threads, pane geometry) per the
  one-file-per-concern rule; lean on the snapshot harness to catch
  regressions.
- **AI strictly optional.** Every affordance checks for a configured provider
  and no-ops otherwise; no path makes AI a dependency of reading or replying.
