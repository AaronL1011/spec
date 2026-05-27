# Spec Reader — In-TUI Spec Content Viewer

> Enable the user to read full spec content within the TUI detail view, navigating by section with a clean, distraction-free reading experience.

---

## Problem

The spec detail view currently shows metadata, build steps, decisions, and a section *outline* — but not the actual prose content. Users see `◼ problem_statement [pm]` but have to press `e` to open `$EDITOR` to read what it says. This breaks the "one pane of glass" promise — the most fundamental action (reading a spec) requires leaving the TUI.

A real SPEC.md can be 1000+ lines with 25+ sections. Dumping the entire document into a scrollable viewport would be unreadable. The reader needs structure-aware navigation.

---

## Design

### Two Modes in the Detail View

The detail view gains two modes, toggled with `Tab`:

| Mode | What it shows | Navigation |
|------|--------------|------------|
| **Overview** (current default) | Metadata grid, build steps, decisions, section outline | `j`/`k` scroll viewport |
| **Reader** | Full section content, one section at a time | `j`/`k` scroll within section, `n`/`p` next/prev section, number keys jump to section |

The status bar breadcrumb updates to show the mode: `Specs › SPEC-042 › Overview` or `Specs › SPEC-042 › §1 Problem Statement`.

### Reader Mode Layout

```
  SPEC-042 — Auth Service Rebuild
  ─────────────────────────────────────────────────

  § 1. Problem Statement                    [pm]
  ─────────────────────────────────────────────────

  Engineers are drowning in tools. The average product
  engineering team operates across 5-10 distinct platforms
  daily — Jira for tickets, Confluence for docs, Slack or
  Teams for communication, GitHub or GitLab for code...

  The result is death by a thousand context switches. An
  engineer's day looks like this:

  1. Open Slack — scan 4 channels for anything urgent
  2. Open Jira — check the board, figure out what's assigned
  ...

                                        § 1/11  3/42
```

### Content Rendering

Markdown content is rendered as **plain text with lightweight formatting**:

- **Headings** (`###`, `####`) rendered with the section title style (bold + accent)
- **Lists** (`-`, `*`, `1.`) preserved as-is — markdown list syntax is already readable
- **Bold** (`**text**`) rendered with the bold style (strip markers, apply lipgloss bold)
- **Tables** (`| col | col |`) preserved as-is — monospace tables render correctly in terminals
- **Code blocks** (`` ``` ``) rendered with the surface background colour
- **Links** (`[text](url)`) rendered as `text (url)` — clickable in terminals that support it
- **HTML comments** (`<!-- owner: pm -->`) stripped — these are metadata, not content

No external markdown rendering library (glamour, goldmark). The content is already close to terminal-readable — light touch-ups are sufficient and keep the dependency footprint at zero.

### Navigation

| Key | Action | Context |
|-----|--------|---------|
| `Tab` | Toggle between Overview and Reader mode | Detail view |
| `j`/`k` / `↑`/`↓` | Scroll content line by line | Both modes |
| `n` | Next section | Reader mode |
| `p` | Previous section | Reader mode |
| `1`-`9` | Jump to section by number | Reader mode |
| `0` | Jump to Decision Log | Reader mode |
| `g` | Jump to top of current section | Reader mode |
| `G` | Jump to bottom of current section | Reader mode |
| `Esc` | Back to overview (if in reader) or back to list | Both |

### Section Index Sidebar (Wide Terminals Only)

On terminals ≥100 columns, the reader renders a narrow left sidebar (20 cols) showing the section index with the current section highlighted. On narrower terminals, the sidebar is hidden and only the section header + content renders.

```
  ┌──────────────────┬─────────────────────────────────────┐
  │ § Sections       │  § 1. Problem Statement       [pm]  │
  │                  │  ─────────────────────────────────── │
  │  ◼ 1 Problem     │                                     │
  │  ◼ 2 Goals       │  Engineers are drowning in tools...  │
  │  ◻ 3 User Stori  │                                     │
  │  ◼ 4 Solution  ◂ │  The result is death by a thousand   │
  │  ◻ 5 Design      │  context switches...                 │
  │  ...             │                                     │
  └──────────────────┴─────────────────────────────────────┘
```

---

## Implementation

### Files

| File | Purpose |
|------|---------|
| `internal/tui/specdetail.go` | Modify — add `readerMode bool`, `sectionIdx int`, mode toggle, section navigation |
| `internal/tui/mdrender.go` (new) | Lightweight markdown-to-styled-text renderer. Takes raw section content + `Styles`, returns `[]string` (pre-rendered lines). Handles headings, bold, lists, code blocks, link rewriting, comment stripping. |
| `internal/tui/specdetail_test.go` | Extend — test mode toggle, section nav, reader rendering |
| `internal/tui/mdrender_test.go` (new) | Test markdown rendering: bold stripping, code block styling, link rewriting, comment stripping, heading detection |

### Changes to `specdetail.go`

```go
type specDetailModel struct {
    // ... existing fields ...

    // Reader mode
    readerMode  bool
    sectionIdx  int      // which section is being read
    readerLines []string // pre-rendered lines for current section
}
```

**Mode toggle:** `Tab` flips `readerMode`. When entering reader mode, render the current `sectionIdx` content via `mdrender`. When leaving, return to overview.

**Section navigation:** `n`/`p` advance/retreat `sectionIdx`, re-render content, reset scroll to 0. Number keys `1`-`9` jump directly. `0` jumps to Decision Log.

**Esc behaviour:** In reader mode, Esc returns to overview. In overview, Esc returns to the list (current behaviour).

### `mdrender.go` — Lightweight Markdown Renderer

```go
// RenderMarkdown takes raw markdown content and styles, returns
// pre-rendered terminal lines ready for display.
func RenderMarkdown(content string, width int, styles Styles) []string
```

The renderer operates line-by-line:

1. Strip HTML comments (`<!-- ... -->`)
2. Detect code block fences (`` ``` ``), toggle code mode
3. In code mode: indent + apply Surface background
4. Detect headings (`###`, `####`): apply SectionTitle style
5. Detect list items (`- `, `* `, `1. `): preserve, apply RowNormal
6. Process inline formatting: `**bold**` → lipgloss Bold, `[text](url)` → `text (url)`
7. Wrap long lines to `width` (word-wrap, not character-wrap)

### Status Bar Updates

In reader mode, the status bar shows:
- Breadcrumb: `Specs › SPEC-042 › §1 Problem Statement`
- Scroll: `3/42` (line within section)
- Section position: `§ 1/11`

---

## What This Enables

- Read any spec section without leaving the TUI
- Section-by-section navigation mirrors how people actually read specs (jump to the section they care about)
- Markdown formatting preserved enough to be readable without being rendered to HTML
- Works on very small terminals (no sidebar, just content)
- `e` still opens `$EDITOR` for editing — the reader is read-only
