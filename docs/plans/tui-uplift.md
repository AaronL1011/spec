# TUI Uplift Plan — Interactive `spec` Dashboard

> Replace the static `spec` (no args) dashboard with a persistent, interactive Bubble Tea TUI that serves as the developer's single pane of glass.

---

## 1. Design Philosophy

**The terminal is the office. `spec` is the desk.**

The TUI is not a feature bolted onto a CLI — it *is* the primary interface. CLI subcommands remain for scripting, CI, and muscle-memory, but the default invocation (`spec`) becomes a persistent, living workspace. Think `htop` for your product engineering pipeline.

### Aesthetic Principles

- **Minimalist, not minimal.** Every pixel of terminal real estate earns its place. No decorative borders. No ASCII art logos. Whitespace is structure.
- **Information density without clutter.** Dense data, clear hierarchy, breathing room between sections.
- **Respectful theming.** Default palette derived from the terminal's background (dark/light detection via `termenv`). Optional user-declared theme (catppuccin, gruvbox, dracula, tokyo-night, nord, solarized, rosé-pine) in `~/.spec/config.yaml` under `preferences.theme`. The `catppuccin/go` dependency already exists.
- **Motion with purpose.** Spinner on async operations. Subtle transitions on view changes. No animation for animation's sake.

---

## 2. Architecture

### 2.1 Package Layout

```
internal/
  tui/                     # existing — keep prompts.go, add:
    tui.go                 # top-level Model, Init, Update, View — the app shell
    theme.go               # theme loading, palette resolution, style constructors
    keymap.go              # key bindings, help text generation
    nav.go                 # tab/view navigation state machine
    views/
      dashboard.go         # home view (replaces current dashboard.Render)
      pipeline.go          # pipeline kanban/list view
      spec_detail.go       # single spec deep-dive (sections, decisions, steps)
      spec_list.go         # filterable spec list with fuzzy search
      triage.go            # incoming triage queue
      review.go            # PR review queue
      settings.go          # config viewer/editor
      help.go              # keybinding reference overlay
    components/
      statusbar.go         # bottom bar: mode, focused spec, pending count, clock
      header.go            # top bar: greeting, role, cycle, breadcrumb
      table.go             # reusable table with selection, scrolling
      panel.go             # bordered content panel with title
      toast.go             # ephemeral notification messages
      spinner.go           # async operation indicator
      modal.go             # confirmation dialogs, input prompts (wraps huh)
      tabs.go              # top-level tab strip
```

### 2.2 Layering

```
cmd/root.go
  └─ tui.New(rc, reg, role)  → tea.Program     (entry point)
       └─ tui.Model                              (app shell)
            ├─ views.DashboardModel               (home)
            ├─ views.PipelineModel                 (kanban)
            ├─ views.SpecDetailModel               (detail)
            ├─ views.SpecListModel                 (search/filter)
            ├─ views.TriageModel                   (incoming)
            ├─ views.ReviewModel                   (PR queue)
            └─ views.SettingsModel                 (config)
```

- **`cmd/root.go`** detects interactive terminal → launches `tea.Program`. Non-interactive falls back to current static `Render`.
- **`tui.Model`** is the app shell: manages active view, global keymap, status bar, header. Delegates messages to the active view.
- **Each view** implements `tea.Model` and owns its own state, keybindings, and rendering. Views communicate upward via custom `tea.Msg` types (e.g., `NavigateToSpec{ID: "SPEC-042"}`).
- **`internal/dashboard`** remains as the data aggregation layer — the TUI views call `dashboard.Aggregate()` and other existing engine functions. No business logic moves into the TUI.

### 2.3 Data Flow

```
┌──────────────────────────────────────────────────────────┐
│  TUI (view + interaction)                                │
│  tui.Model → views.DashboardModel → components.Table     │
└──────────────┬───────────────────────────────────────────┘
               │ tea.Cmd (async)
               ▼
┌──────────────────────────────────────────────────────────┐
│  Engines (business logic)                                │
│  dashboard.Aggregate · pipeline.Advance · build.Start    │
│  markdown.ReadMeta · store.DB · git.WithSpecsRepo        │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│  Adapters (external systems)                             │
│  github · jira · slack · confluence · noop               │
└──────────────────────────────────────────────────────────┘
```

All external/IO calls happen inside `tea.Cmd` functions (async). The TUI never blocks on network. Stale data is shown with a refresh indicator.

---

## 3. Views & Navigation

### 3.1 View Map

| Key | View | Description |
|-----|------|-------------|
| `1` / `d` | **Dashboard** | Home. DO / REVIEW / INCOMING / BLOCKED sections. Default on launch. |
| `2` / `p` | **Pipeline** | All specs by stage. Kanban-style columns or compact list. |
| `3` / `l` | **Specs** | Full spec list. Fuzzy search, filter by status/role/cycle. |
| `4` / `t` | **Triage** | Incoming triage items. Quick promote/reject actions. |
| `5` / `r` | **Reviews** | PR review queue. Open in browser action. |
| `6` / `s` | **Settings** | View/edit config, integrations status, theme preview. |
| `?` | **Help** | Overlay showing all keybindings for current view. |

### 3.2 Navigation Model

- **Tab strip** across the top shows all views. Number keys or letter shortcuts switch instantly.
- **Within a view**, `j`/`k` or `↑`/`↓` navigate items. `Enter` drills into detail. `Esc`/`Backspace` returns.
- **Spec detail** is a drill-down from any list view, not a top-level tab. Shows: frontmatter, pipeline position, sections preview, decisions, steps, linked PRs, build sessions.
- **Breadcrumb** in the header shows navigation path: `Dashboard > SPEC-042`.

### 3.3 Global Keybindings

| Key | Action |
|-----|--------|
| `1`-`6` | Switch view |
| `Tab` / `Shift+Tab` | Next/prev view |
| `?` | Toggle help overlay |
| `/` | Search (scoped to current view) |
| `R` | Force refresh current view data |
| `q` / `Ctrl+C` | Quit |
| `n` | New spec (opens `spec new` flow) |
| `i` | New triage item (opens `spec intake` flow) |

### 3.4 Contextual Keybindings (when item selected)

| Key | Action | Context |
|-----|--------|---------|
| `Enter` | Open spec detail | Any list |
| `a` | Advance to next stage | Spec in actionable stage |
| `e` | Edit spec (opens `$EDITOR`) | Spec detail |
| `b` | Start/resume build | Spec in build stage |
| `B` | Block spec | Any active spec |
| `u` | Unblock spec | Blocked spec |
| `v` | Revert to previous stage | Spec detail |
| `o` | Open in browser (PM tool / repo) | Spec or PR |
| `c` | Record a decision | Spec detail |
| `f` | Focus on this spec | Any spec |
| `y` | Copy spec ID to clipboard | Any spec |

---

## 4. Theming System

### 4.1 Config Surface

```yaml
# ~/.spec/config.yaml
preferences:
  theme: catppuccin-mocha   # or: gruvbox-dark, dracula, tokyo-night, nord,
                            #     solarized-dark, solarized-light, rosé-pine,
                            #     auto (default — derives from terminal)
```

### 4.2 Theme Resolution

1. **`auto` (default):** Use `termenv.HasDarkBackground()` to detect light/dark. Apply a built-in neutral palette that respects the terminal's colour scheme — muted accent for highlights, terminal fg/bg for text. Looks good without config.
2. **Named theme:** Load palette from a `themes` map. Start with catppuccin (already a dependency), add gruvbox, dracula, tokyo-night, nord as simple colour maps.
3. **Theme struct:** A `Theme` holds ~10 semantic colours (Base, Surface, Overlay, Text, SubText, Accent, Success, Warning, Error, Muted). All `lipgloss.Style` constructors in `theme.go` reference the active `Theme` — no hardcoded colour values in views or components.

### 4.3 Palette (catppuccin-mocha example)

```
Base:    #1e1e2e    (background)
Surface: #313244    (panels, selected rows)
Overlay: #45475a    (borders, separators)
Text:    #cdd6f4    (primary text)
SubText: #a6adc8    (secondary, dimmed)
Accent:  #89b4fa    (highlights, active tab)
Success: #a6e3a1    (done, approved)
Warning: #f9e2af    (stale, blocked)
Error:   #f38ba8    (critical, failed)
Muted:   #585b70    (disabled, inactive)
```

---

## 5. Auto-Refresh & Real-Time Updates

### 5.1 Tick-Based Refresh

- A `tea.Tick` fires every **30 seconds** (configurable via `preferences.refresh_interval`).
- On tick: re-run `dashboard.Aggregate()` and adapter calls as `tea.Cmd`. View updates when the result `tea.Msg` arrives. No flicker — diff the data, only re-render changed rows.
- Network-dependent calls (PR reviews, PM status) have a **10s timeout** per existing convention. On timeout, show stale data with a `⏳ stale` indicator.

### 5.2 Manual Refresh

- `R` key forces an immediate refresh of the current view.
- After any mutating action (advance, block, build), auto-refresh the affected view.

---

## 6. Action Execution Model

When the user triggers an action (advance, block, build, etc.) from the TUI:

1. **Confirmation modal** for destructive/state-changing actions (advance, revert, block). Simple `y/n` via the modal component.
2. **Execute via existing engine functions.** The TUI calls the same `pipeline.Advance()`, `build.Start()`, etc. that the CLI subcommands use. No duplicated logic.
3. **Show result** as a toast notification (success/error). Auto-dismiss after 3s or on keypress.
4. **Refresh** the affected view data.

For actions that require extended input (new spec, intake, decide), the TUI either:
- Opens an inline `huh` form (for short inputs: decision text, block reason), or
- Suspends the TUI and opens `$EDITOR` (for spec editing), resuming on editor close.

---

## 7. Implementation Phases

### Phase 1 — Shell & Dashboard View (Foundation)
**Goal:** `spec` launches a Bubble Tea app, shows the dashboard, quits cleanly.

- [ ] `internal/tui/theme.go` — Theme struct, auto-detection, catppuccin palette
- [ ] `internal/tui/keymap.go` — Global keybindings with `bubbles/key`
- [ ] `internal/tui/nav.go` — View enum, tab state
- [ ] `internal/tui/tui.go` — Top-level Model (Init/Update/View), tea.Program setup
- [ ] `internal/tui/components/header.go` — Greeting, role, cycle, breadcrumb
- [ ] `internal/tui/components/statusbar.go` — Mode indicator, pending count, time
- [ ] `internal/tui/components/tabs.go` — Tab strip
- [ ] `internal/tui/views/dashboard.go` — Port `dashboard.Render` to a `tea.Model` using existing `dashboard.Aggregate`
- [ ] `cmd/root.go` — Detect TTY → `tea.NewProgram(tui.New(...))` instead of `dashboard.Render`
- [ ] Auto-refresh via tick

**Test:** Launch `spec`, see dashboard, press `q` to quit. `spec --help` still works. Piped output (`spec | cat`) falls back to static render.

### Phase 2 — List Views & Navigation
**Goal:** Tab between views, navigate items, drill into spec detail.

- [ ] `internal/tui/views/spec_list.go` — Filterable list with fuzzy search
- [ ] `internal/tui/views/pipeline.go` — Specs grouped by stage
- [ ] `internal/tui/views/triage.go` — Triage queue
- [ ] `internal/tui/views/review.go` — PR review queue
- [ ] `internal/tui/views/spec_detail.go` — Spec deep-dive (read-only initially)
- [ ] `internal/tui/components/table.go` — Reusable table with selection + scrolling
- [ ] View switching via number keys, tab, letter shortcuts
- [ ] Breadcrumb navigation for drill-down

**Test:** Navigate all views, drill into specs, return with Esc. Search filters work.

### Phase 3 — Actions & Mutations
**Goal:** Execute spec operations from within the TUI.

- [ ] `internal/tui/components/modal.go` — Confirmation dialogs
- [ ] `internal/tui/components/toast.go` — Success/error notifications
- [ ] Advance, block, unblock, revert actions on selected spec
- [ ] Focus spec (`f` key)
- [ ] Open in browser (`o` key)
- [ ] Copy spec ID to clipboard (`y` key)
- [ ] Editor suspend/resume for `e` (edit) action
- [ ] Post-action data refresh

**Test:** Advance a spec from the TUI, confirm it persists. Block/unblock round-trip. Editor opens and TUI resumes.

### Phase 4 — Theming & Polish
**Goal:** Theme system, additional themes, visual polish.

- [ ] `internal/tui/views/settings.go` — Config viewer, integration status
- [ ] `internal/tui/views/help.go` — Contextual keybinding overlay
- [ ] Named theme support: gruvbox, dracula, tokyo-night, nord, solarized, rosé-pine
- [ ] `preferences.theme` config support in user config
- [ ] Responsive layout (adapt to terminal width: compact mode < 80 cols)
- [ ] Loading spinners for async operations
- [ ] Stale data indicators
- [ ] `preferences.refresh_interval` config support

**Test:** Switch themes, verify readability on light/dark terminals. Resize terminal, layout adapts. Slow network shows stale indicators.

### Phase 5 — Advanced Interactions
**Goal:** Deeper workflow integration.

- [ ] Inline triage intake (`i` key → huh form inside TUI)
- [ ] New spec creation flow (`n` key)
- [ ] Decision recording (`c` key on spec detail)
- [ ] Build start/resume (`b` key with session status display)
- [ ] Standup generation from TUI
- [ ] Mouse support (click tabs, click items) — opt-in via config

---

## 8. Key Dependencies

| Dependency | Status | Purpose |
|-----------|--------|---------|
| `charmbracelet/bubbletea` | Already in go.mod (indirect via huh) | TUI framework |
| `charmbracelet/bubbles` | Already in go.mod (indirect) | Table, viewport, spinner, help components |
| `charmbracelet/lipgloss` | Already in go.mod (direct) | Styling |
| `catppuccin/go` | Already in go.mod (indirect) | Catppuccin palette |
| `muesli/termenv` | Already in go.mod (indirect) | Terminal capability detection |
| `charmbracelet/huh` | Already in go.mod (direct) | Form inputs within TUI |

**No new dependencies required.** Everything needed is already in the dependency tree. `bubbletea` and `bubbles` just need to be promoted from indirect to direct imports.

---

## 9. Backward Compatibility

| Concern | Resolution |
|---------|------------|
| `spec` (no args) behaviour change | TTY → TUI, non-TTY → static render (same as today). `spec dashboard --static` flag as explicit escape hatch. |
| All CLI subcommands | Unchanged. `spec advance`, `spec list --mine`, etc. work exactly as before. |
| `spec watch` | Retained but marked as superseded by the TUI's built-in auto-refresh. |
| Scripting / piping | Non-interactive detection (`!IsInteractive()`) ensures `spec \| jq` still works. |
| `--help` | Unchanged. `spec --help` prints help, does not launch TUI. |
| MCP server | Unchanged. `spec mcp-server` is unaffected. |

---

## 10. File Impact Summary

| File | Change |
|------|--------|
| `cmd/root.go` | Modify `RunE` to launch TUI when interactive |
| `internal/tui/prompts.go` | Keep as-is (used by CLI subcommands) |
| `internal/tui/*.go` (new) | ~5 new files: tui.go, theme.go, keymap.go, nav.go |
| `internal/tui/views/*.go` (new) | ~7 new files, one per view |
| `internal/tui/components/*.go` (new) | ~7 new files, one per component |
| `internal/dashboard/dashboard.go` | No changes — TUI calls `Aggregate()` directly |
| `internal/config/config.go` | Add `Preferences` struct with `Theme` and `RefreshInterval` |
| `go.mod` | Promote bubbletea, bubbles from indirect to direct |

Estimated **~20 new files**, **~2 modified files**, **0 deleted files**.
