# TUI guide

The interactive terminal UI is the primary way to use `spec`. Run it with no
arguments:

```bash
spec
```

The TUI is persistent and auto-refreshing. It shares state with commands but is
optimized for human work: finding the next task, reading context, reviewing,
triage, and safe lifecycle actions.

For first-run setup, begin with the [Quickstart](QUICKSTART.md).

---

## Global navigation

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move selection or scroll the focused surface |
| `pgup` / `pgdn` | Page |
| `home` / `end` | Top / bottom |
| `enter` | Open or select |
| `esc` | Close the current layer; twice at top level exits |
| `ctrl+c` | Exit immediately |
| `?` | Contextual keyboard help |
| `/` | Search active and archived specs |
| `r` | Refresh |
| `E` | Expand the current error |
| `1`–`6` | Jump to a view |
| `tab` / `shift+tab` | Cycle views |

Mouse support is optional. Enable it in **Settings**. Tabs, list rows, reader
sections, discussion markers, and picker blocks are clickable; wheel routing
follows the surface under the pointer.

---

## Views

### 1 — Dashboard

Your personal queue, grouped into sections such as:

- **DO** — work you own, authored work, or claimable work according to stage
  configuration;
- **REVIEW** — plans and pull requests waiting for you;
- **INCOMING** — mentions, role handoffs, and triage;
- **BLOCKED** — visible according to the team's blocked-scope policy.

Rows can develop a time-urgency color as they approach the current stage's
`stale_after` window. This is a prioritization cue, not an error.

### 2 — Pipeline

Every active spec grouped by its current stage. Use this when you need the team
view rather than your personal queue. The same urgency gradient appears here.

### 3 — Specs

The active spec browser. Press backtick (`` ` ``) to toggle the archive list.
Opening a row shows a metadata overview before entering the reader.

### 4 — Triage

Lightweight incoming work. Press `enter` or `space` to open an item's detail
pane.

| Key | Action | Roles |
| --- | --- | --- |
| `n` | Add note | Everyone |
| `e` | Edit title, priority, source, and body | PM, engineer |
| `c` | Close | PM, engineer |
| `x` | Escalate / de-escalate | PM, engineer |
| `p` | Promote to a full spec | PM |
| `esc` | Close detail | Everyone |

### 5 — Reviews

Pull requests awaiting review from the configured repository adapter. Press
`enter` to open the selected review URL.

### 6 — Settings

Personal configuration. Editable fields:

- name;
- role;
- spec handle;
- theme;
- refresh interval;
- mouse;
- editor.

Select a row and press `enter`. Text fields support normal cursor navigation.
Enum fields use `space` / `l` for next and `h` for previous. Enter confirms;
Esc cancels. Theme changes preview before save.

Integration status and paths are read-only. They explain the effective team
configuration; edit `spec.config.yaml` for shared changes.

---

## Creating work

### `n` — new spec

Enter a title in the modal. `spec` claims the next ID, scaffolds the standard
spec, commits it to the specs repository, and refreshes the dashboard.

### `i` — triage intake

The intake form has title, priority, and source fields. Use Tab to move. Enter
advances from title, cycles priority, and submits from the final source field.
Esc cancels.

Use triage when the work still needs classification. Create a full spec when it
is ready to enter the pipeline.

---

## Actions on a selected spec

These keys work in top-level spec lists and the detail overview where relevant.

| Key | Action |
| --- | --- |
| `a` | Advance; validates gates and asks for confirmation |
| `v` | Revert to an earlier stage with a reason |
| `e` | Edit in the configured editor, or show docs URL for external roles |
| `b` | Validate and start/resume the configured coding agent |
| `x` | Block with a reason |
| `u` | Resume a blocked spec |
| `g c` | Claim, assign handles, or type `-` to unassign |
| `f` | Toggle focused-spec context |
| `c` | Record a decision |
| `p` | Push local spec edits |
| `s` | Publish/sync through the docs adapter |
| `y` | Copy the spec ID |
| `g a` | Archive |
| `g r` | Restore |
| `g s` | Generate standup |

Actions that move or remove work use confirmation modals. In confirmation
modals press `y` or `n`; Esc cancels.

---

## Spec overview and reader

Press Enter on a spec to open the overview. It presents the current state and
section list. Press `o` to enter the reader.

### Reader movement

| Key | Action |
| --- | --- |
| `[` / `]` | Previous / next section |
| `1`–`9` | Jump to numbered section |
| `0` | Jump to Decision Log when present |
| `g` / `G` | Top / bottom of section |
| `n` / `p` | Next / previous thread in document order |
| `t` | Show / hide thread pane |
| `T` | Cycle peek, review, and conversation pane sizes |
| `tab` | Focus prose / threads |
| `o` / `esc` | Return to overview |

`n` / `p` follows the active filter across sections and wraps through a review
pass. Completing a pass shows visited, open, and unread counts.

### Discussion gutter

A marker beside rendered text means one or more discussions anchor there.
Markers reflect selected, unread, open, and resolved state. Multiple threads on
one block collapse into a count. If source text changes, the anchor degrades to
the section rather than pointing at the wrong text.

### Section-level question

Press `a`. Compose a question with normal text editing:

- Enter inserts a newline;
- Ctrl+S sends;
- Esc cancels.

### Exact-block question

Press `A` to enter pick mode. The picker moves by source block — paragraph,
list item, table row, or code block — even when the renderer wraps or clips it.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Next / previous block |
| `pgup` / `pgdn` | Move several blocks |
| `enter` | Use highlighted block as the quote anchor |
| `s` | Ask on section instead |
| `esc` | Cancel |

The ask prompt displays a quote preview before you type.

### Thread focus

With the pane focused and a thread selected:

| Key | Action |
| --- | --- |
| `r` | Reply |
| `x` | Resolve |
| `u` | Toggle read/unread |
| `n` / `p` | Next / previous matching thread |
| `f` | Cycle filter |
| arrows | Scroll a long conversation |
| `tab` | Return focus to prose |

Filters are `open` (default), `all`, `mine`, and `unread`. Unread traversal uses
a snapshot so reading an item does not make it disappear under the cursor.
Read-state is per identity, per machine.

Threads whose original section no longer exists appear in a final
**Unanchored threads** section. If a quote matches exactly one live section,
Enter offers a safe re-anchor. Resolve and re-anchor briefly offer `u` to undo.

---

## Search

Press `/` from any non-modal screen. Search covers active and archived spec
sections. Selecting a result deep-links into the reader at that section. Esc
returns to the search overlay with the query intact.

---

## Errors, refresh, and publishing

The TUI watches the open spec and its thread sidecar. Markdown changes preserve
the current section and scroll position; discussion-only updates appear without
requiring a document edit.

Status-bar messages report success, queued-offline publishing, or failures.
Press `E` when an error is too long for the bar.

Local edits and comments publish according to `sync.auto_push`:

- `auto` — publish automatically;
- `prompt` — interactive commands ask, asynchronous TUI changes auto-publish;
- `off` — keep changes local until `p` or `spec push`.

---

## Static and non-interactive use

If stdout is not an interactive terminal, plain `spec` renders a static
snapshot instead of starting the TUI. Force this behavior with:

```bash
spec --static
```

Use `--json` on supported commands for machine-readable automation.
