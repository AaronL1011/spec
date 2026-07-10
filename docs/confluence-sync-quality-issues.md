# Confluence Sync — Replication Quality Issues

**Status:** analysis, 2026-07-10
**Scope:** `internal/adapter/confluence/docs.go`, `internal/sync/engine.go`, `cmd/sync.go`, `internal/tui/actions.go`, `internal/pipeline/effects/adapters.go`
**Trigger:** a fully filled-out spec in `plan_review` was synced and every content section was deleted from the local spec — only the frontmatter and the `# SPEC-NNN — Title` line survived.

All findings below were verified against the current code; the headline data-loss
mechanism was reproduced with a round-trip harness (markdown → storage →
sections) using the real `markdownToStorage`, `parseStorageToSections`,
`markdown.ExtractSections`, and `markdown.ReplaceSectionContent` functions.

---

## 1. Root cause of the reported wipe (CRITICAL — data loss)

The wipe is not a Confluence API problem. It is a **section-granularity
mismatch between the two sides of the sync engine**, detonated by inbound sync
running when the user believes the integration is outbound-only.

### 1.1 The two sides disagree about what a "section" is

- **Local** (`markdown.ExtractSections`, `internal/markdown/sections.go:60`):
  a section runs from its heading to the next heading of the **same or higher
  level**. The `# SPEC-042 — Title` H1 therefore owns *the entire document
  body* — every `##` section is inside it. Its slug is the slugified title
  (e.g. `spec_042_rate_limiting`).
- **Remote** (`parseStorageToSections`, `internal/adapter/confluence/docs.go`):
  outbound push emits a `<!-- spec-section: slug -->` marker before **every**
  heading, h1 through h6 (`markdownToStorage`). Inbound splits the page on
  those markers, so each slug's remote content is only what sits between its
  heading and the **next heading of any level**.

For the H1 title section, the remote content is whatever sits between
`<h1>…</h1>` and the first `## TL;DR` marker — i.e. **the empty string**.

### 1.2 Reproduced round trip

| slug | owner | local len | remote len after round trip |
|---|---|---|---|
| `spec_042_rate_limiting` (H1) | — | 529 (whole body) | **0** |
| `proposed_solution` (has `### 4.1`) | pm | 53 | **0** |
| `technical_implementation` (has `### 7.1`) | engineer | 55 | **0** |
| `architecture_notes` (code block) | engineer | 27 | 2 (`"go"` — code destroyed) |
| `problem_statement` (`Q&A`, `List<T>`) | pm | 78 | 86 (entities not unescaped) |

### 1.3 The state machine then approves the deletion

`prepareInbound` (`internal/sync/engine.go`) applies a remote section over
local when: remote hash ≠ last-seen inbound hash (**remoteChanged**), local
hash == last outbound hash (**not localChanged**), and no owner mismatch.

After any outbound push, `prepareOutbound` records **both** state hashes as
the hash of the *local* markdown (`pendingStateUpdate{localHash: hash,
remoteHash: hash}`) — it never reads back what Confluence actually stored. So
on the next run:

1. `remoteChanged` is **always true** for any section whose storage→markdown
   round trip is lossy (which is most of them, and *always* the H1).
2. `localChanged` is **false** precisely when the user has *not touched the
   spec since the last push* — the safest possible local state is the one that
   gets overwritten.
3. The H1 section has **no owner marker**, so the owner guard never protects it.

Result: `ReplaceSectionContent(content, <title-slug>, "")` replaces everything
from the line after `# SPEC-042 — Title` to EOF. **Frontmatter + title is all
that remains** — exactly the reported symptom.

### 1.4 Why `plan_review` is the perfect arming condition

- `spec.config.yaml` in this repo sets `sync.outbound_on_advance: true`, and
  `workflow.Advance` runs an outbound sync on every stage advance. By
  `plan_review` the sync-state table is fully populated with the lying
  `remoteHash = localHash` entries for **every slug, including the H1**.
- A spec sitting in review is by definition not being edited, so
  `localChanged` is false across the board.
- The reviewer (or the TUI `s` action, or a pipeline `sync:` effect) runs
  `spec sync SPEC-NNN` — whose **default direction is `both`**
  (`cmd/sync.go: syncCmd.Flags().String("direction", "both", …)`); the TUI
  hardcodes `DirectionBoth` (`internal/tui/actions.go:529`). Inbound runs
  first, the H1 apply guts the file.
- Remaining sections mostly escape the loop without error (which is what lets
  the wipe reach the file write): template sections are owned by
  `pm`/`designer`/`qa`/`engineer`/`anyone`/`auto`, so any section not matching
  the runner's single role is skipped by `ownerMismatch`; simple-prose
  sections round-trip byte-identically and land in `Unchanged`.

### 1.5 The same run then propagates the damage outward

`Prepare` runs inbound **and then** outbound in one pass, and
`prepareOutbound` **re-reads the spec file from disk** — the file inbound just
gutted. `Finalize` then `PushFull`s the wiped spec to Confluence and persists
sync-state hashes for the wiped content. Both replicas are now empty and the
state table says everything is in sync. The only recovery path is the specs
repo git history (the wipe is committed as `chore: sync SPEC-NNN from docs`).

### 1.6 Contract violation

The stated model is "spec is the source of truth; the Confluence integration
is outward-only." The code contradicts this in three places:

| Surface | Direction |
|---|---|
| `spec sync` CLI | default `both` |
| TUI sync action | hardcoded `DirectionBoth` |
| pipeline `sync:` effect | YAML-selectable, `inbound` accepted (`internal/config/lint.go: validSyncDirections`) |

There is no config switch to disable inbound globally. The adapter doc comment
itself calls inbound "best-effort … lossy … retained for compatibility but not
the focus" — yet it runs by default and can rewrite the source of truth.

### Recommended fixes (1.x)

1. **Make outbound the default everywhere**; require an explicit
   `--direction in` plus confirmation (or remove inbound entirely until it can
   round-trip safely). This alone honours the "outward-only" contract.
2. **Never emit a marker for, or apply inbound to, the H1 title section.**
   Exclude level-1 sections from the inbound loop (`prepareInbound`) and from
   marker emission (`markdownToStorage`).
3. **Align granularity**: either mark only `##`-level sections outbound and
   fold `###` content into the parent when parsing, or key both sides by the
   same until-next-same-or-higher-level rule.
4. **Stop recording a remote hash outbound never observed.** After `PushFull`,
   either fetch the page back and hash the parsed sections, or store a
   sentinel that means "remote unverified" and treat it as *not* remoteChanged.
5. **Empty-overwrite guard**: refuse to replace non-empty local content with
   empty remote content unless `--force` is given — a one-line invariant that
   would have converted this data-loss bug into a skipped section.
6. **Do not run outbound over a file inbound just modified** in the same run —
   capture the outbound snapshot before inbound applies, or require separate
   runs for each direction.

---

## 2. Sync-engine logic issues (beyond the wipe)

### 2.1 `Prepare` mutates the local file before `Finalize` (HIGH)

`prepareInbound` writes the spec file during *prepare*, while docs pushes and
state persistence are deferred to `Finalize`. The name promises plan-only; the
behaviour is destructive. If `Finalize` is never called (callers bail on
conflicts), the file is already rewritten but sync state isn't — the next run
sees `localChanged=true` and reports phantom conflicts.

### 2.2 First-sync conflict storm, then silent auto-apply (MEDIUM)

With no prior state, every non-empty section is `localChanged=true` →
conflict → skipped (warn). The *outbound* half of that same run then poisons
the state (§1.3), so the **second** run silently auto-applies lossy remote
content with no conflict raised. The system degrades from noisy to silently
destructive across two invocations.

### 2.3 `ownerMismatch` semantics (LOW)

- `owner: anyone` is treated as a literal role name — it matches nobody
  (unless someone's role is literally "anyone"). It should match everyone.
- Sibling inheritance: in `ExtractSections`, a `##` heading with no owner
  inherits the *previous* `##` section's owner (`currentOwner` is only reset
  by owned `##`s). `## Decision Log` inherits `auto` from
  `## 11. Retrospective`. Coincidentally protective here, but wrong.

### 2.4 `--force` amplifies the wipe (MEDIUM)

`ConflictForce` bypasses **both** the owner guard and the localChanged guard.
The CLI's own conflict error message recommends `--force to accept remote
changes` — following that advice on a lossy/empty remote wipes even the
sections the owner guard was protecting.

### 2.5 Pipeline `sync: inbound` effects rewrite specs invisibly (MEDIUM)

`SyncerAdapter.Sync` runs `engine.Run` with the configured strategy and
discards the report; effect outcomes reduce to a one-line message. An
`inbound` effect on a stage transition can rewrite spec content with no
conflict surfacing to the user at all.

---

## 3. Outbound fidelity issues (markdown → Confluence storage)

These degrade the *published page* — the primary artifact of an
outward-only integration.

| # | Issue | Effect on page |
|---|---|---|
| 3.1 | Multi-line HTML comments are not handled. `markdownToStorage` is line-based, so the template's `<!-- Parsed into a DAG … -->` block under §7.3 PR Stack Plan is emitted as escaped paragraphs (`<p>&lt;!--</p>` …). | Internal authoring guidance leaks as visible junk text on the published page. |
| 3.2 | Nested lists flatten. `- ` / `1. ` detection is done on `TrimSpace(line)`, discarding indentation; every item becomes a top-level `<li>`. | Hierarchical lists (common in Goals, Acceptance Criteria) lose structure. |
| 3.3 | Multi-line list items break. A continuation line under a bullet closes the list and becomes a `<p>`. | List items split into stray paragraphs. |
| 3.4 | Blockquotes (`> `) unsupported → escaped paragraph beginning with `&gt;`. | Literal "> " text on the page. |
| 3.5 | Images `![alt](url)` unsupported; the `[alt](url)` tail matches the link regex. | Renders as a hyperlink with a stray `!` — no image. |
| 3.6 | Indented (4-space) code blocks unsupported → paragraphs. | Code flattened to prose. |
| 3.7 | `_emphasis_`, `~~strikethrough~~`, task-list checkboxes unsupported. | Markers render literally. |
| 3.8 | Bare `---` inside a section becomes `<hr/>` — fine — but the same rule fires on YAML-ish content in prose. | Minor. |
| 3.9 | Table header detection assumes row 0 is the header; escaped pipes `\|` in cells split the cell. | Broken cells for content containing `|`. |

Positives worth keeping (already hardened): XML escaping of prose
(`List<T>`, `Q&A`), CDATA terminator splitting, fence matching per
CommonMark, code-span/link placeholder protection before emphasis, durable
label binding, metadata info panel.

## 4. Inbound fidelity issues (storage → markdown)

Every one of these becomes **local spec corruption** the moment inbound
applies (§1), because applied remote content replaces the source of truth.

| # | Issue | Where | Effect |
|---|---|---|---|
| 4.1 | `codePattern` lacks `(?s)`; multi-line CDATA never matches. The macro tags are later stripped by `stripTags`, leaving fragments — a fenced Go block round-trips to the string `"go"`. | `storageToMarkdown` | **Code blocks destroyed** on inbound apply. |
| 4.2 | XML entities are never unescaped. `Q&A` → `Q&amp;A`, `List<T>` → `List&lt;T&gt;` in the *local markdown* after apply; hashes then differ forever, re-flagging the section every run. | `storageToMarkdown` | Progressive text corruption + permanent conflict churn. |
| 4.3 | `<a href="url">text</a>` is not converted back to `[text](url)`; `stripTags` keeps only the text. | `storageToMarkdown` | **URLs silently dropped** from local specs. |
| 4.4 | `<li>` always becomes `- `; `<ol>` numbering lost. Nested lists flattened. | `storageToMarkdown` | Ordered lists degrade to bullets. |
| 4.5 | All content regexes (`<p>`, `<strong>`, `<em>`, `<h\d>`, `<tr>`, `<td>`) are non-DOTALL; any element spanning a newline falls through to `stripTags`, losing formatting. | `storageToMarkdown`, `parseStorageByHeadings` | Formatting silently stripped. |
| 4.6 | Table cells run through `stripTags`, so bold/links/code inside cells are erased (this includes every Decision Log row). | `storageTableToMarkdown` | Decision Log fidelity loss. |
| 4.7 | Heading regex `<h(\d)>(.*?)</h\d>` requires attribute-free tags. Confluence's editor rewrites storage (and strips HTML comments, killing the section markers — acknowledged in the package doc). After any human edit the fallback parser typically finds nothing. | `parseStorageToSections` / `parseStorageByHeadings` | Inbound silently becomes a no-op ("remote section missing" skips) — inconsistent, though accidentally safe. |
| 4.8 | Closing tag level isn't matched to the opening (`</h\d>` not a backreference). | `parseStorageByHeadings` | Mis-nesting tolerated silently. |

## 5. Confluence client issues

| # | Issue | Where | Severity |
|---|---|---|---|
| 5.1 | `getPageVersion` never checks `resp.StatusCode`. A 404/401/429 body unmarshals to version 0, then `updatePage` sends version 1 and fails with a confusing 409. | `docs.go: getPageVersion` | MEDIUM |
| 5.2 | `PageURL` returns `{base}/pages/{id}` — not a valid Confluence Cloud URL shape (`{base}/spaces/{key}/pages/{id}`). Links surfaced to users likely 404. | `docs.go: PageURL` | LOW |
| 5.3 | No read-after-write verification: `PushFull` never confirms the stored page still contains the section markers/structure it sent, so outbound "success" can still mean a mangled mirror (Confluence normalises storage). | `docs.go: PushFull` | MEDIUM (feeds §1.3) |
| 5.4 | Update race: version is fetched, then PUT with `version+1`; a concurrent human edit between the two calls is overwritten without warning. Acceptable for a declared mirror, but worth stating on the page ("do not edit — mirrored from spec"). The metadata panel does not currently say this. | `docs.go: updatePage` | LOW |
| 5.5 | If `attachLabel` failed on an earlier create (network blip after page creation), `findPage` will never find that page again and the next push **creates a duplicate**, which then trips the "multiple pages labelled" error only if the second label attach succeeds. | `docs.go: createPage/findPage` | LOW |

## 6. Priority summary

| Priority | Fix |
|---|---|
| **P0** ✅ done | Make sync outbound-only by default across CLI, TUI, and pipeline effects; gate inbound behind an explicit flag + confirmation (§1.6). Implemented: `spec sync` defaults to `--direction out`; the engine's own empty-direction default is now `out`; the TUI sync action is hardcoded outbound; explicit `--direction in\|both` prompts `[y/N]` on interactive surfaces. Pipeline `sync:` effects keep their explicit YAML direction but now pass through the engine guards below. |
| **P0** ✅ done | Exclude the H1/title section from inbound apply and from marker emission (§1.1). Implemented: `prepareInbound`/`prepareOutbound` skip level-1 sections (state is no longer recorded for the title, disarming §1.3 for legacy pages too); `markdownToStorage` emits no marker for h1; the heading-fallback parser skips `<h1>`. |
| **P0** ✅ done | Empty-overwrite guard: never replace non-empty local content with empty remote content (§1.5, rec 5). Implemented unconditionally — stricter than rec 5: **not even `--force` bypasses it**, since the spec is the source of truth and deletions belong in the spec, not the mirror. Regression tests: `internal/sync/engine_test.go` (`TestRun_Inbound_TitleSection_NeverApplied` reproduces the field wipe end-to-end), `internal/adapter/confluence/docs_test.go` (`TestRoundTrip_TitleSlugNeverKeyed`). |
| **P1** | Stop fabricating `remoteHash` in `prepareOutbound`; verify what Confluence stored or mark it unverified (§1.3). |
| **P1** | Don't let outbound re-read a file inbound modified in the same run (§1.5). |
| **P1** | Align section granularity between `ExtractSections` and `parseStorageToSections` (§1.1). |
| **P2** | Inbound converter correctness: `(?s)` on block regexes, entity unescaping, link reconstruction (§4.1–4.3) — prerequisites for ever re-enabling inbound. |
| **P2** | Outbound fidelity: multi-line HTML comments, nested lists, images, blockquotes (§3). |
| **P2** | `getPageVersion` status check; `PageURL` shape; "mirrored page — do not edit" notice in the metadata panel (§5). |
| **P3** | `owner: anyone` semantics; sibling owner inheritance; `--force` scope (§2.3, §2.4). |

## 7. Reproduction harness

The §1.2 table came from a throwaway test dropped into
`internal/adapter/confluence/`, feeding a realistic filled spec through
`markdownToStorage` → `parseStorageToSections` and comparing against
`markdown.ExtractSections`, then applying the H1 slug's remote content via
`markdown.ReplaceSectionContent`. Output after the single H1 apply:

```
---
id: SPEC-042
title: Rate Limiting
status: plan_review
---

# SPEC-042 - Rate Limiting
```

Everything else was gone — matching the field report exactly. This should be
promoted into a permanent regression test (golden round-trip: for every
template section, `apply(parse(push(spec)))` must be a no-op) once the P0
fixes land.
