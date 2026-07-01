# Discussion: The Reader Cockpit

> **Status:** Proposed · **Owner:** Discussion / TUI · **Scope:** `internal/tui/specdetail.go`, `internal/tui/threadpane.go`, `internal/tui/review.go`, `internal/tui/keys.go`, `internal/tui/keymap.go`, `internal/thread/`, `internal/markdown/sections.go`, `internal/mcp/handler.go`
> **Effort estimate:** Large · **Risk:** Medium (concentrated in `internal/tui`, covered by snapshot/golden tests)
> **Delivery:** a stack of five PRs (see §8). Anchoring (PR 1) and navigation (PR 2) are independently shippable.

The reader surfaces threads as a half-height bottom pane scoped to one section
at a time. Thread selection (`selectThread`) only moves within the focused
section; there is no way to step through all open threads in a document, no
read/unread state, no filter, and no finer anchor than a section slug. Reviewing
a spec means visiting every section by hand and toggling the pane to check for
comments.

It turns the reader into a review cockpit: span-level anchoring that degrades
gracefully, document-wide thread navigation, filters, read-state, and AI
affordances under the existing accept/edit/skip contract.

It assumes the model work in `discussion-01-awareness-loop.md` (mentions) and
benefits from `discussion-02-review-verdicts.md` (kinds) for rendering, but the
anchoring and navigation work stands alone.

---

## 1. Outcome

- A thread can anchor to a **quoted text span** within a section, not just the
  section. The anchor resolves by searching the live section text and falls back
  to the section header when the text has changed — keeping the
  "never orphaned" guarantee the current design prizes.
- The reader shows a **gutter marker** beside the paragraph a thread anchors to,
  so comments sit next to their text.
- `n` / `p` step through **all** open threads in the document, scrolling the
  reader to each anchor and focusing the pane — the core review motion.
- A **filter** (`all | open | blocking | mine | unread`) narrows what the pane
  and the stepper traverse.
- **Read-state** is tracked per user; the pane highlights "new since last visit"
  and a header shows review progress (`3 / 7 resolved · 2 unread`).
- AI affordances (summarise threads, draft reply, propose resolution) appear
  under the accept/edit/skip flow when an `ai` integration is configured.

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
resolved at render time.

### 2.2 Resolution

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

Resolution is best-effort and pure. The reader calls it per visible thread
against the current section text from `markdown.ExtractSections`; a miss is not
an error — it renders the existing section-level treatment.

### 2.3 Reader integration

`renderSectionContent` (`internal/tui/specdetail.go`) hands the raw section body
straight to `renderer.Render`, which reflows and wraps markdown — the rendered
output's line boundaries do not correspond to the source's, so anchor matching
cannot run against rendered text. Resolve before rendering instead: for each
open thread anchored to the visible section, call `ResolveAnchor` against the
raw `sec.Content`, and on a match, inject a zero-width marker byte at the start
of that source line before the body reaches `renderer.Render`. After rendering,
`renderSectionContent` scans the output for the marker, strips it, and prefixes
that rendered line with the gutter glyph (e.g. `▍`), coloured by the thread's
kind (from `discussion-02`). Multiple threads on one source line collapse to a
count badge. On a miss, fall back to the section-level pane indicator already in
place.

Capturing a quote when asking: the reader has no text-selection primitive, but
`Up`/`Down` already move `m.readerViewport` by exactly one line
(`ScrollUp(1)`/`ScrollDown(1)`), so the first visible source line is a
well-defined implicit cursor with no new state to track. An `a`sk taken while
the viewport has scrolled within the section sets `Quote` to that line's text
and `QuotePrefix` to the tokens immediately before it in the section source; an
`a`sk at the section's top (no scroll yet) stays section-level, matching
current behaviour. The pane input flow (`threadInput`, `submitInput` in
`threadpane.go`) carries `quote`/`prefix` alongside the existing `section`.

CLI parity: `spec ask --quote "<text>"` sets the anchor from the command line.

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
  bucket renders as a synthetic `Unanchored` entry at the top of the section
  list (its own slug, `_unanchored`, that no real section ever produces) so
  `n`/`p` stepping and the filter still reach them — the whole point of
  document-wide navigation is that nothing requires guessing which section a
  thread used to belong to.
- `spec ask --list` (`cmd/ask.go`) marks these with
  `⚠ section not found — now unanchored` next to the thread instead of
  printing a slug that no longer resolves to anything, so a CLI-only reviewer
  isn't left guessing why the reader can't find it either.

This is a pre-existing gap in the base engine — heading rewording has always
been able to do this — not something this section introduces. But this plan is
the first to build enough reader tooling around section anchors that the gap
is worth closing rather than inheriting silently.

---

## 3. Document-wide navigation

`internal/tui/specdetail.go` + `threadpane.go`.

Current `selectThread(delta)` is section-scoped. Add a document-level stepper
that is independent of the focused section:

```go
// orderedThreads returns the active filter's threads across the whole document
// in reading order: by section position, then by anchor line, then by created.
func (m specDetailModel) orderedThreads() []thread.Thread

// stepThread moves to the next/prev thread in orderedThreads, switching the
// focused section, scrolling the reader to the thread's anchor, focusing the
// pane, and selecting the thread.
func (m specDetailModel) stepThread(delta int) specDetailModel
```

`n`/`p` in `updateReader` (`internal/tui/specdetail.go`) already do double duty:
next/prev section when the pane is unfocused, next/prev thread *within the
current section* (`selectThread`) when it is focused. Document-wide stepping
replaces both: `n`/`p` become `stepThread(delta)` unconditionally, and
section-stepping moves to `[`/`]` (unused in the reader today). This is a
deliberate behaviour change for anyone who learned `n`/`p` as section-next/prev
— thread review is the more frequent motion — call it out in the release
notes. Update the help overlay (`internal/tui/help.go`) and the pane hint
(`threadHintLine`) for both rebindings.

Scrolling to an anchor reuses the section-jump logic already used by section
navigation; when the thread has a resolved quote, offset the scroll to the
matched line.

Header shows position: `Thread 3 / 7`.

---

## 4. Filters and read-state

### 4.1 Filter

Add `threadFilter string` to `specDetailModel` with values
`all | open | blocking | mine | unread`. A key (`f`) cycles it. `orderedThreads`
and `threadsForSection` apply the filter. The pane header names the active
filter and the matching count.

- `blocking` uses `Thread.IsBlocking()` (`discussion-02`).
- `mine` uses `Thread.Participants()` against the viewer identity.
- `unread` uses read-state (§4.2).

### 4.2 Read-state

Read-state is per-user and inherently personal, so it does **not** belong in the
git-synced sidecar. Store it in the local store (`internal/store`, the SQLite
boundary — AGENTS.md: only `internal/store` touches SQLite).

`internal/store/db.go` versions its schema through `migrateV1`..`migrateV4`,
each a self-contained `CREATE TABLE IF NOT EXISTS` run once and recorded in the
`migrations` table. Add `migrateV5` following that exact pattern:

```sql
CREATE TABLE IF NOT EXISTS thread_seen (
    spec_id   TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    last_seen TIMESTAMP NOT NULL, -- latest reply timestamp the user has viewed
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
`Created`) is after the recorded `last_seen`, or no row exists. Selecting a
thread in the reader marks it seen (records the latest activity timestamp).
The pane renders unread threads with an accent dot and a `NEW` tag; the header
shows `2 unread`.

Read-state degrades cleanly: when the DB is unavailable, every thread reads as
unread (the existing best-effort pattern in `logThreadActivity`).

---

## 5. Pane layout

`internal/tui/threadpane.go` currently collapses every non-selected thread to
one line in a half-height windowed buffer. Improvements:

- **Progress header.** Replace the bare `Threads (N open)` with
  `Threads · 3/7 resolved · 2 unread · filter: open`.
- **Inline-beside-text mode.** When the terminal is wide enough
  (`m.width >= readerSidebarMinWidth`, already a constant), render the selected
  thread in the right sidebar aligned to its anchor line, rather than the bottom
  pane. Narrow terminals keep the bottom pane. Gate this on width using the
  existing responsive split in `sizeInputArea`.
- **Full-conversation view.** The selected thread already word-wraps question +
  replies; add author/time per reply and the kind tag. Keep the `↑/↓ more`
  windowing for long threads.

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
- This only works as an `@mention` target because
  `discussion-01-awareness-loop.md` §2.1 defines the mention grammar as
  `[A-Za-z0-9_.:-]+` — widened specifically so an `agent:<adapter>` identity
  is mentionable like any human handle. Treat that widening as load-bearing
  for this section, not an independent nice-to-have: drop it and
  `@agent:claude` truncates to `agent`, silently losing `:claude`.
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
  argument for an explicit mention still overrides the default.
- The reader renders agent authors with a distinct glyph (the existing
  `internal/tui/glyph` set) so agent comments are visually separable.
- Add `spec_create_thread` to the MCP toolset (currently agents can only list
  and reply — `discussion-02` adds verdict/resolve; this adds create) so an
  agent can raise a `blocking` thread when the spec contradicts the
  implementation. Anchoring uses §2's quote when the agent supplies one.

---

## 7. AI affordances

Gated entirely on a configured `ai` integration (Decision 013: progressive
enhancement, accept/edit/skip, never auto-write). All three return a draft the
user accepts, edits, or skips — none mutate the sidecar without confirmation.

- **Summarise open threads.** `spec ask <id> --summarise` and a reader key:
  feed open threads to the `ai` service, render a short digest. Read-only.
- **Draft a reply.** While composing a reply, a key requests an AI draft
  seeded with the thread and the anchored section text; the draft populates the
  textarea for editing before send.
- **Propose a resolution.** On resolve, offer an AI-drafted `--outcome`
  (`discussion-02` §4) that the user accepts/edits/skips before it is written and
  promoted to the Decision Log.

Each path uses the existing AI service layer and returns `(string, error)` or
`(nil, nil)`; the caller handles the nil case (AGENTS.md: AI is never a hard
dependency).

---

## 8. PR stack

| PR | Scope | Depends on |
|----|-------|------------|
| 1 | Span anchoring: `Quote`/`QuotePrefix` model, `markdown.ResolveAnchor`, reader gutter markers, `spec ask --quote`, capture-on-ask, `Unanchored` bucket for removed sections (§2.4) | discussion-01 PR 1 |
| 2 | Document-wide navigation: `orderedThreads`, `stepThread`, `n`/`p` keys, position header, help/hint updates | 1 |
| 3 | Filters + read-state: `thread_seen` table + store API, `threadFilter`, `f` key, unread rendering | 1 |
| 4 | Pane layout: progress header, inline-beside-text sidebar mode, full-conversation rendering | 1, 2, 3 |
| 5 | Agent identity (`agent:<adapter>`, `spec_create_thread` with default-addressee, mention-grammar dependency confirmed) + AI affordances (summarise / draft reply / propose resolution) | discussion-02; 1 |

PRs 1–3 are the substance and can land in sequence. PRs 4–5 are enhancements.

---

## 9. Tests

- `internal/markdown`: `ResolveAnchor` table — exact match, whitespace-reflowed
  match, prefix-disambiguated duplicate, absent quote (graceful miss).
- `internal/tui`: snapshot tests (existing `snapshot_test.go` harness) for
  gutter markers, the progress header, unread tags, and inline sidebar mode at
  wide/narrow widths; `stepThread` crossing section boundaries; filter cycling.
- `internal/store`: `ThreadSeen` / `MarkThreadSeen` round-trip; unread predicate
  with and without a row.
- `internal/mcp`: `spec_create_thread` writes an anchored thread; agent author
  is `agent:<adapter>` when the adapter is known; a call with no mention
  defaults `Mentions` to the spec author and the current stage's owning role.
- `internal/thread`: `ParseMentions("@agent:claude ...")` returns `["agent:claude"]`,
  not `["agent"]`.
- AI paths: with `provider: none`, every affordance is a no-op that leaves the
  sidecar unchanged.

---

## 10. Risks and constraints

- **Anchor resolution is best-effort by design.** A quote that no longer matches
  degrades to section-level — never an error, never an orphan. This preserves
  the v1 robustness guarantee while regaining precision. Do not store offsets.
- **Section-level orphaning is handled too, not just quote-level.** §2.4
  extends the quote-degrades-to-section guarantee to cover the section itself
  disappearing. Both paths must stay consistent: a thread is never simply
  absent from a surface that claims to show "all threads."
- **Agent participants and mention parsing must agree.** §6's `agent:<adapter>`
  format only works as an `@mention` target if `ParseMentions`'s grammar
  accepts `:`. That widening lives in `discussion-01` §2.1 — verify it shipped
  before building PR 5's agent-identity work on top of it.
- **Read-state must stay local.** It is personal and high-churn; putting it in
  the git sidecar would create noise commits and merge churn. SQLite via
  `internal/store` only.
- **TUI surface area.** The changes concentrate in `specdetail.go` /
  `threadpane.go`, both already large. Keep new logic in small files
  (`orderedThreads`, anchoring render) per the one-file-per-concern rule; lean on
  the snapshot harness to catch regressions.
- **Width-responsive layout.** Inline-beside-text must fall back cleanly on
  narrow terminals using the existing `readerSidebarMinWidth` split; never
  render the sidebar when it would collide with the body.
- **AI strictly optional.** Every affordance checks for a configured provider and
  no-ops otherwise; no path makes AI a dependency of reading or replying.
