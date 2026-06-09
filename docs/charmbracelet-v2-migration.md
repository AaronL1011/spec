# Charmbracelet v2 Migration Plan

> **Status:** Implemented · **Owner:** TUI · **Scope:** `internal/tui/`, `cmd/root.go`, `go.mod`, CI toolchain
> **Effort estimate:** Large (single holistic change package) · **Risk:** High (breaking API across the whole TUI)
> **Delivery:** one atomic merge — the whole v2 stack lands together, not as a stack of incremental PRs.

## 0. Implementation status

The migration described below has been executed in a single change package.

- **Modules:** moved to `charm.land/bubbletea/v2 v2.0.7`, `charm.land/lipgloss/v2 v2.0.3`,
  `charm.land/bubbles/v2 v2.1.0`, `charm.land/glamour/v2 v2.0.0`, `charm.land/huh/v2 v2.0.3`;
  added `github.com/charmbracelet/colorprofile`; dropped `github.com/muesli/termenv`.
- **Go directive:** bumped to `go 1.25.8` (required by glamour v2).
- **`App.View()`** now returns `tea.View`, with `AltScreen` and `MouseMode` declared per render;
  `cmd/root.go` no longer passes `WithAltScreen`/`WithMouseCellMotion`.
- **Input:** key handling switched to `tea.KeyPressMsg` matched via `msg.String()`/`msg.Text`;
  mouse handling dispatches on `MouseClickMsg`/`MouseWheelMsg`/`MouseMotionMsg`.
- **lipgloss colours:** `Theme` fields are now `image/color.Color`; dark-background detection
  uses `lipgloss.HasDarkBackground`; `NO_COLOR`/profile detection uses `colorprofile.Detect`.
- **bubbles:** `viewport.New(opts...)` + `SetWidth/SetHeight`; textinput cursor via the v2
  virtual-cursor model.
- **Verification:** `go build ./...`, `go vet ./...`, the full `go test ./...` suite, and the
  pinned `golangci-lint v2.12.2` (`make lint-strict`) all pass. One standup test now strips ANSI
  before asserting text, reflecting the v2 behaviour that styles always emit ANSI (downsampling
  moved to the program's write boundary). The static dashboard path uses plain `fmt.Print` (no
  lipgloss), so its colour behaviour is unaffected.

The sections below remain as the design record and rationale.

This document is the audit and migration plan to move the spec TUI off the
current pinned/pseudo-version Charmbracelet libraries onto the latest **v2**
release line and its new conventions.

---

## 1. Executive summary

The spec TUI is built on a snapshot of the Charmbracelet v1/pre-v2 stack
(bubbletea `v1.3.6`, lipgloss `v1.1.1-<pseudo>`, bubbles `v0.21.1-<pseudo>`,
glamour `v0.10.0`, huh `v1.0.0`). Charmbracelet has since shipped a stable **v2**
of the entire stack with a fundamentally different rendering and input model.

The migration is **not** a drop-in version bump. Three changes touch nearly every
file in `internal/tui/`:

1. **Module paths moved to `charm.land/...`** (e.g. `charm.land/bubbletea/v2`). The
   old `github.com/charmbracelet/...` import paths no longer resolve for v2.
2. **`Model.View()` now returns a `tea.View` struct, not a `string`.** Alt-screen,
   mouse mode, cursor, background colour and window title are all properties of
   that struct rather than program options or commands.
3. **Key and mouse messages are now interfaces** (`KeyPressMsg`, `MouseClickMsg`,
   …) with a structured `Key`/`Mouse` payload. The old `msg.Type`/`msg.Runes`
   and `msg.Action`/`msg.Button` struct fields are gone.

Plus a colour-model change in lipgloss v2 (`lipgloss.Color` is now a function
returning `image/color.Color`; `AdaptiveColor` is removed; termenv is dropped),
and a Go toolchain bump forced by glamour v2.

**Recommendation:** migrate the entire v2 stack as a **single, atomic change
package** that merges in one go. The libraries are too tightly coupled to split
safely — lipgloss v2 colour downsampling assumes a v2 bubbletea program, bubbles
v2 / huh v2 / glamour v2 all depend on the v2 core, and an intermediate state
would leave two copies of lipgloss in the build and a non-compiling `View()`
contract. The work is *sequenced internally* (see §5) but reviewed and merged as
one unit, gated by the existing snapshot/golden tests and `make lint-strict`.

---

## 2. Version audit

### 2.1 Current versions (from `go.mod`)

| Library | Current | Import path |
|---|---|---|
| bubbletea | `v1.3.6` | `github.com/charmbracelet/bubbletea` |
| bubbles | `v0.21.1-0.20250623…` (pseudo) | `github.com/charmbracelet/bubbles` |
| lipgloss | `v1.1.1-0.20250404…` (pseudo) | `github.com/charmbracelet/lipgloss` |
| glamour | `v0.10.0` | `github.com/charmbracelet/glamour` |
| huh | `v1.0.0` | `github.com/charmbracelet/huh` |
| x/ansi | `v0.10.2` | `github.com/charmbracelet/x/ansi` |
| termenv | `v0.16.0` | `github.com/muesli/termenv` |
| Go directive | `go 1.25.0` | — |

The pseudo-versions are untagged commits between v1 and v2 — exactly the
"outdated pseudo-snapshot" state this migration retires.

### 2.2 Target versions (latest stable, verified in module cache)

| Library | Target | Import path | Notes |
|---|---|---|---|
| bubbletea | `v2.0.7` | `charm.land/bubbletea/v2` | requires Go 1.25.0 |
| bubbles | `v2.1.0` | `charm.land/bubbles/v2` | requires Go 1.25.0 |
| lipgloss | `v2.0.3` | `charm.land/lipgloss/v2` | requires Go 1.25.0 |
| glamour | `v2.0.0` | `charm.land/glamour/v2` | **requires Go 1.25.8** |
| huh | `v2.0.3` | `charm.land/huh/v2` | pulls in the v2 stack |
| ultraviolet | (transitive) | `github.com/charmbracelet/ultraviolet` | new input/render core |
| colorprofile | `v0.4.3` | `github.com/charmbracelet/colorprofile` | colour downsampling/`NO_COLOR` |
| x/ansi | `v0.11.x` | `github.com/charmbracelet/x/ansi` | **stays on github** (not moved to charm.land) |

> **Module-path note:** the `charmbracelet/x/*` helper packages (`x/ansi`,
> `x/term`, etc.) keep their `github.com/charmbracelet/x/...` paths. Only the
> primary libraries (bubbletea, bubbles, lipgloss, glamour, huh) moved to
> `charm.land`. `muesli/termenv` is dropped entirely by the v2 stack.

### 2.3 Migration surface (code inventory)

- `internal/tui/` — **~11,900 LOC** production, **~7,350 LOC** tests, ~80 files.
- Import counts (production + tests): 32× bubbletea, 18× lipgloss, 10× bubbles/key,
  plus single uses of viewport, textinput, textarea, cursor, glamour, huh, x/ansi.
- `cmd/root.go` — the only `tea.NewProgram(...).Run()` call site.
- Bubbles components in use: `viewport` (specdetail), `textinput`+`textarea`+`cursor`
  (triageedit), `key` (everywhere via `keymap.go`).
- Tests: **56** `tea.KeyMsg{...}` literals and **3** mouse-message literals to rewrite.
- Only `App` implements `tea.Model` (`Init/Update/View`). All sub-views/components
  expose plain `View() string` helpers and are composed by `App` — this materially
  shrinks the `View()→tea.View` change to a single top-level method.

---

## 3. Breaking changes and required code changes

### 3.1 Module paths (`charm.land/...`)

Every import must change path **and** gain the `/v2` suffix.

```go
// before
import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
    "github.com/charmbracelet/bubbles/key"
    "github.com/charmbracelet/bubbles/viewport"
    "github.com/charmbracelet/glamour"
    "github.com/charmbracelet/huh"
)

// after
import (
    tea "charm.land/bubbletea/v2"
    "charm.land/lipgloss/v2"
    "charm.land/bubbles/v2/key"
    "charm.land/bubbles/v2/viewport"
    "charm.land/glamour/v2"
    "charm.land/huh/v2"
)
```

`github.com/charmbracelet/x/ansi` (used as `xansi` for `StringWidth`/`Truncate`)
stays unchanged.

### 3.2 `Model.View()` returns `tea.View`

In v2 the `Model` interface is:

```go
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() View   // <- was: View() string
}
```

`tea.View` is a struct carrying the rendered content **and the terminal state**
that used to live in program options/commands:

```go
type View struct {
    Content              string
    Cursor               *Cursor          // explicit cursor placement
    AltScreen            bool             // replaces WithAltScreen()
    MouseMode            MouseMode        // replaces WithMouseCellMotion()
    BackgroundColor      color.Color
    ForegroundColor      color.Color
    WindowTitle          string
    ReportFocus          bool
    OnMouse              func(MouseMsg) Cmd
    KeyboardEnhancements KeyboardEnhancements
    // …
}
```

**Required change — `internal/tui/app.go` `App.View()`:**

```go
// before
func (a App) View() string {
    // … compose header/tabs/content/statusbar …
    return out
}

// after
func (a App) View() tea.View {
    // … compose header/tabs/content/statusbar into `out` (string) …
    v := tea.NewView(out)
    v.AltScreen = true
    if a.mouseEnabled() {
        v.MouseMode = tea.MouseModeCellMotion
    }
    // Optionally: v.Cursor = a.activeCursor() for text inputs (see §3.6).
    return v
}
```

All other view/component `View() string` methods (6 view models + the component
helpers) **stay returning `string`** — they are composed into `Content` by the
parent. Only the single top-level `App.View()` changes signature.

### 3.3 Program setup — alt screen and mouse move into `View`

`WithAltScreen()` and `WithMouseCellMotion()` no longer exist as program options.

```go
// before — cmd/root.go
opts := []tea.ProgramOption{tea.WithAltScreen()}
if rc.User != nil && rc.User.Preferences.Mouse {
    opts = append(opts, tea.WithMouseCellMotion())
}
p := tea.NewProgram(app, opts...)
_, err := p.Run()

// after — cmd/root.go
p := tea.NewProgram(app) // alt-screen + mouse now set on App.View()
_, err := p.Run()
```

The mouse preference must be threaded into `App` state so `View()` can choose
`MouseModeCellMotion` vs `MouseModeNone`. This also means the in-app mouse toggle
(currently noted as needing a restart in `app.go`) can become live — set a field
and the next `View()` reflects it.

### 3.4 Key messages: `KeyMsg` is now an interface

v1 delivered a `tea.KeyMsg` **struct** with `.Type` (a `KeyType`) and `.Runes`.
v2 delivers `tea.KeyPressMsg` (and optionally `tea.KeyReleaseMsg`); `tea.KeyMsg`
is an **interface** (`{ fmt.Stringer; Key() Key }`). The payload is a `Key` struct:

```go
type Key struct {
    Text        string // printable text, e.g. "a", "A", "!" (empty for special keys)
    Mod         KeyMod // ModCtrl, ModAlt, …
    Code        rune   // KeyEnter, KeyTab, KeyEscape, or a rune like 'a'
    ShiftedCode rune
    BaseCode    rune
    IsRepeat    bool
}
```

**Mapping table:**

| v1 pattern | v2 replacement |
|---|---|
| `case tea.KeyMsg:` | `case tea.KeyPressMsg:` (add `tea.KeyReleaseMsg` only if needed) |
| `msg.Type == tea.KeyEnter` | `msg.Key().Code == tea.KeyEnter` |
| `msg.Type == tea.KeyEscape` | `msg.Key().Code == tea.KeyEsc` (alias `KeyEscape`) |
| `msg.Type == tea.KeyTab` | `msg.Key().Code == tea.KeyTab` |
| `msg.Type == tea.KeySpace` | `msg.Key().Code == tea.KeySpace` |
| `msg.Type == tea.KeyBackspace` | `msg.Key().Code == tea.KeyBackspace` |
| `msg.Type == tea.KeyShiftTab` | `msg.Key().Code == tea.KeyTab && msg.Key().Mod&tea.ModShift != 0` |
| `msg.Type == tea.KeyCtrlC` | `msg.Key().Code == 'c' && msg.Key().Mod&tea.ModCtrl != 0` (or `msg.String() == "ctrl+c"`) |
| `msg.Type == tea.KeyRunes && string(msg.Runes) == "a"` | `msg.Key().Text == "a"` |
| `string(msg.Runes)` | `msg.Key().Text` |

The key **constants** (`KeyEnter`, `KeyTab`, `KeyEsc`, `KeySpace`, `KeyBackspace`,
…) still exist but are now `rune` codes used against `Key.Code`, not message types.

**`key.Matches` still works** — bubbles v2 `key.Matches[Key fmt.Stringer]` compares
`k.String()` against the binding keys, and `KeyPressMsg` implements `String()`.
So the ~30 `key.Matches(msg, a.keys.X)` call sites are largely **source-compatible**
once `msg` is a `KeyPressMsg`. The breakage is concentrated in the direct
`msg.Type`/`msg.Runes` comparisons (≈20 production sites + the test literals).

Affected production files (non-exhaustive): `app.go` (the bulk),
`intake.go`, `settings.go`, `triageedit.go`, `standup.go`, `decide.go`,
`review.go`, `triageclose.go`, `triagepromote.go`, `newspec.go`.

### 3.5 Mouse messages: `MouseMsg` is now an interface

v1 delivered a `tea.MouseMsg` struct with `.Action`, `.Button`, `.X`, `.Y` and a
`tea.MouseEvent` companion. v2 delivers concrete `MouseClickMsg`,
`MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg`; `MouseMsg` is the interface
`{ fmt.Stringer; Mouse() Mouse }` where:

```go
type Mouse struct {
    X, Y   int
    Button MouseButton
    Mod    KeyMod
}
```

**Required rewrite — `internal/tui/mouse.go`:** the current code switches on
`msg.Action == tea.MouseActionPress`, `msg.Button == tea.MouseButtonLeft`,
`tea.MouseEvent(msg).IsWheel()`, and reads `msg.X`/`msg.Y`. New shape:

```go
// before
func (a App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    isWheel := tea.MouseEvent(msg).IsWheel()
    leftPress := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
    // … msg.X, msg.Y …
}

// after — dispatch on the concrete message type
func (a App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    m := msg.Mouse() // X, Y, Button, Mod
    switch ev := msg.(type) {
    case tea.MouseWheelMsg:
        return a.handleWheel(ev)            // ev.Button: MouseWheelUp/Down/Left/Right
    case tea.MouseClickMsg:
        if m.Button == tea.MouseButtonLeft {
            // region dispatch using m.X, m.Y
        }
    }
    return a, nil
}
```

`MouseButtonWheelUp/Down` become `MouseWheelUp/Down` on `MouseWheelMsg`; verify the
exact enum names against `charm.land/bubbletea/v2/mouse.go` when implementing.
`MouseActionPress`/`MouseActionMotion` are removed (the message *type* now encodes
press vs motion vs release vs wheel).

### 3.6 lipgloss v2 colour model

This is the most pervasive non-mechanical change.

**`lipgloss.Color` is no longer a type.** It is a function:
`func Color(s string) color.Color`. The named type `lipgloss.Color` (formerly
`type Color string`) is gone, and so are `AdaptiveColor` and `CompleteColor`.

**Impact on `internal/tui/theme.go`:** the `Theme` struct stores colours as
`lipgloss.Color` fields and styles take `lipgloss.Color` arguments. Both must move
to the standard library `image/color.Color` interface:

```go
// before
import "github.com/charmbracelet/lipgloss"
type Theme struct {
    Base    lipgloss.Color
    Surface lipgloss.Color
    // …
}
// constructed as: Base: lipgloss.Color("#1a1b26")

// after
import (
    "image/color"
    "charm.land/lipgloss/v2"
)
type Theme struct {
    Base    color.Color
    Surface color.Color
    // …
}
// constructed as: Base: lipgloss.Color("#1a1b26")  // returns color.Color now
```

`lipgloss.NewStyle().Foreground(c)` / `.Background(c)` accept `color.Color`, so the
*call sites* in `theme.go`, `prompts.go`, and components mostly compile unchanged —
the work is in the **field/parameter types** (`func text(fg lipgloss.Color)` →
`func text(fg color.Color)`), which then cascades through `Styles`,
`components.StatusStyles`, etc.

**Adaptive/auto theming** (`autoTheme()` light/dark): replace the removed
`AdaptiveColor` pattern with `lipgloss.LightDark(isDark)`:

```go
lightDark := lipgloss.LightDark(hasDark)
fg := lightDark(lipgloss.Color("#000000"), lipgloss.Color("#ffffff"))
```

**Layout/measurement helpers are unchanged:** `lipgloss.Width`, `lipgloss.Height`,
`lipgloss.JoinVertical/Horizontal`, `lipgloss.Place`, `lipgloss.NewStyle`,
`lipgloss.Right` all exist in v2 with the same signatures. The 34 layout call sites
in components need only the import-path change.

### 3.7 Colour profile / `NO_COLOR` / dark-background detection (termenv removal)

lipgloss v2 drops the `muesli/termenv` dependency. The two termenv touch-points
in spec must be re-homed:

1. **`renderer.go: colourDisabled()`** uses `termenv.NewOutput`, `termenv.EnvNoColor`,
   `termenv.Ascii`. Replace with the `colorprofile` package:
   ```go
   import "github.com/charmbracelet/colorprofile"
   func colourDisabled() bool {
       p := colorprofile.Detect(os.Stdout, os.Environ())
       return p == colorprofile.NoTTY || p == colorprofile.Ascii
   }
   ```
   (`colorprofile.Detect` already honours `NO_COLOR`; confirm the exact `Profile`
   enum names — `Ascii`, `ANSI`, `ANSI256`, `TrueColor`, `NoTTY` — at implementation.)

2. **`theme.go: hasDarkBackground()`** uses `termenv.HasDarkBackground()`. v2 offers
   `lipgloss.HasDarkBackground(in, out term.File)`. Better still, bubbletea v2 can
   query the terminal and deliver a `BackgroundColorMsg` to `Update`, avoiding the
   blocking OSC probe the current code works around with `sync.Once`. Options:
   - **Minimal:** `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` behind the same
     `sync.Once` guard.
   - **Idiomatic v2:** request the background colour via the program and update the
     theme when `BackgroundColorMsg` arrives (removes the probe-blocking hazard the
     current comment describes). Defer to the polish phase.

> **Downsampling note:** in v2, styles no longer carry a renderer and do **not**
> downsample colours themselves. The bubbletea program downsamples `View.Content`
> to the detected `colorprofile.Profile` at write time. For the non-TUI static
> dashboard path (`dashboard.Render`, which writes lipgloss output directly to
> stdout outside a `tea.Program`), confirm colours still degrade correctly under
> `NO_COLOR`/dumb terminals — may need an explicit `colorprofile.NewWriter`.

### 3.8 bubbles v2 — viewport

`viewport.New` changed from positional `(width, height int)` to functional options,
and the `Width`/`Height` fields are now set via methods.

```go
// before — specdetail.go
vp := viewport.New(80, 20)
vp.KeyMap = viewport.KeyMap{}
// …
m.readerViewport.Width  = m.effectiveWidth()
m.readerViewport.Height = max(h, 3)

// after
vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
vp.KeyMap = viewport.KeyMap{}
// …
m.readerViewport.SetWidth(m.effectiveWidth())
m.readerViewport.SetHeight(max(h, 3))
```

`SetContent`, `TotalLineCount`, scrolling helpers remain; verify whether `Height`
is still readable as a field or needs a getter (`m.readerViewport.Height()`).

### 3.9 bubbles v2 — textinput / textarea / cursor

v2 introduces a **virtual vs real cursor** distinction. The v1 idiom
`title.Cursor.SetMode(cursor.CursorStatic)` is replaced:

```go
// before — triageedit.go
import "github.com/charmbracelet/bubbles/cursor"
title := textinput.New()
_ = title.Cursor.SetMode(cursor.CursorStatic)

// after
title := textinput.New()
title.SetVirtualCursor(false) // use the real terminal cursor (placed via View.Cursor)
// or keep a virtual cursor and configure its blink mode on title.Cursor (cursor.Model)
```

When using the real cursor (`SetVirtualCursor(false)`), the active input must
surface its cursor to the top-level `tea.View`: `textinput.Model.Cursor() *tea.Cursor`
returns the cursor to assign to `View.Cursor` in `App.View()` while that field is
focused. The `cursor` package still exists (`cursor.New`, `cursor.CursorStatic`),
but the `Cursor` field on textinput is now the unexported `virtualCursor`.

Note: `internal/tui/threadpane.go` renders its ask/reply input line **manually**
(no bubbles textinput), so it is unaffected except for the import-path change of
any `tea.Key*` references.

### 3.10 glamour v2

API is largely stable: `NewTermRenderer`, `WithStandardStyle`, `WithWordWrap`,
`WithPreservedNewLines`, `WithEmoji`, and `styles.LightStyle`/`styles.DarkStyle`
all exist in v2. The migration is:

- Import path: `github.com/charmbracelet/glamour` → `charm.land/glamour/v2`,
  `github.com/charmbracelet/glamour/styles` → `charm.land/glamour/v2/styles`.
- **Go toolchain bump:** glamour v2 declares `go 1.25.8`. This forces the whole
  module's Go directive up (see §3.12).

`renderer.go`'s width cache, `WithStandardStyle(g.style)` + `WithWordWrap(width)`
construction, and the plain/Glamour split all carry over unchanged.

> **Decoupling option:** glamour only produces ANSI strings and shares no live
> types with the bubbletea program. It *could* remain on v1 (`glamour v1.0.0`,
> which still pins old lipgloss) to avoid the Go 1.25.8 bump. **Not recommended** —
> it keeps a second copy of lipgloss in the build. Prefer moving glamour to v2 and
> bumping Go.

### 3.11 huh v2

`prompts.go` uses standalone `huh.NewForm(...).Run()` dialogs. The v2 API matches
v1 for the constructs in use (`NewForm`, `NewGroup`, `NewSelect[T]`, `NewInput`,
`NewOption`, `Option[T]`), with `NewSelect[T comparable]` (the current
`huh.NewSelect[string]()` satisfies `comparable`). Changes:

- Import path: `github.com/charmbracelet/huh` → `charm.land/huh/v2`.
- huh v2 runs on the v2 bubbletea/lipgloss/bubbles stack internally — must land
  **after** or **with** the core migration so a single v2 stack is in the build.
- The local lipgloss styles in `prompts.go` (`titleStyle`, etc.) follow §3.6.

### 3.12 Go toolchain bump

`go.mod` currently declares `go 1.25.0`. glamour v2 requires `go 1.25.8`. Action:

- Bump the `go` directive in `go.mod` to `1.25.8` (or the toolchain `go1.25.8`).
- CI uses `go-version-file: go.mod` (`.github/workflows/ci.yaml`) so it follows
  automatically — but confirm the CI runner has access to the toolchain and that
  the pinned `golangci-lint v2.12.2` (`Makefile`) supports 1.25.8. The `// Toolchain`
  jump can be done with `go get go@1.25.8`.

---

## 4. Expected improvements & behavioural changes in v2

Worth capturing as motivation and as things to watch for in QA:

- **Unified, structured input model (ultraviolet).** Keys and mouse events carry
  richer, mode-aware data (`Key.Text`, `Key.Mod`, `Key.ShiftedCode`,
  Kitty-keyboard-protocol fields, `IsRepeat`). Enables cleaner modifier handling
  and disambiguating shifted keys — useful for the g-prefix/double-esc state
  machines in `app.go`.
- **`View`-driven terminal state.** Alt-screen, mouse mode, cursor, background,
  window title and focus reporting become declarative per-render. The current
  "mouse toggle needs a restart" limitation goes away; window title could be set
  from the active spec.
- **Correct, automatic colour downsampling** at the program boundary via
  `colorprofile`, with first-class truecolor and an idiomatic `NO_COLOR` story —
  replacing the hand-rolled `colourDisabled()` probe.
- **Real-cursor support** for text inputs (no more virtual-cursor-only rendering),
  improving accessibility and screen-reader/terminal-native behaviour.
- **No blocking background-colour probe.** v2's `BackgroundColorMsg` removes the
  `sync.Once` workaround in `theme.go` that exists solely because the OSC reply
  never arrives once bubbletea owns stdin.
- **Smaller, termenv-free dependency tree** for the styling layer (lipgloss v2 drops
  `muesli/termenv`), at the cost of new transitive deps (`ultraviolet`,
  `colorprofile`, `clipperhouse/*`).
- **Keyboard enhancements** (key release/repeat events) are opt-in via
  `View.KeyboardEnhancements` if we want them later.

### Risks / regressions to watch

- **Rendering drift** in snapshot/golden output (spacing, colour codes,
  truncation) — the snapshot suite (`snapshot_test.go`, `settings_test.go`) is the
  primary guard; expect to re-baseline some goldens deliberately.
- **Mouse enum/name changes** (`MouseButtonWheelUp` → `MouseWheelUp`) are easy to
  get subtly wrong — unit-test wheel and click dispatch.
- **Alpha/beta churn is over** (v2 is stable), but watch the still-`v0` transitive
  deps (`ultraviolet`, `colorprofile`) for minor breakage on patch bumps.
- **huh standalone forms** spin up their own bubbletea program; ensure they don't
  conflict with the main program's terminal state on entry/exit.
- **Static (non-TUI) dashboard path** writes lipgloss directly to stdout — verify
  colour degradation without a `tea.Program` wrapping it.

---

## 5. Migration strategy — one atomic change package

The whole v2 stack lands as a **single merge**. It does not compile in any
half-migrated state (the `View()` contract, the colour types, and the
`charm.land` module graph all flip together), so splitting it across PRs would
either break the build mid-stack or carry two copies of lipgloss. Treat the
phases below as an **internal work order within one branch/commit set**, not as
separately mergeable units. The branch is green (builds, `make lint-strict`,
tests) only at the end.

Per `AGENTS.md`, lint must pass at the pinned `golangci-lint` version before the
package is "done" (watch `errorlint` for the new interface type-switches, and
`unparam`/`wastedassign` while refactoring).

> **Why not a stack:** lipgloss v2 colour downsampling assumes a v2 bubbletea
> program; bubbles v2, huh v2 and glamour v2 all require the v2 core; and
> `App.View()` cannot return both `string` and `tea.View`. There is no
> intermediate commit that is simultaneously correct, on a single lipgloss, and
> shippable. One holistic change is the lower-risk path.

**Suggested work order inside the single branch** (do, then verify the whole
package at the end):

1. **Toolchain.** Bump `go.mod` Go directive to `1.25.8`; confirm CI picks it up
   (`go-version-file: go.mod`) and `golangci-lint v2.12.2` supports it.
2. **Module graph.** Swap every charmbracelet import to its `charm.land/.../v2`
   path in one sweep (bubbletea, lipgloss, bubbles, glamour, huh); leave
   `github.com/charmbracelet/x/ansi` as-is. Add `colorprofile`; drop
   `muesli/termenv`. Expect a large but mechanical compile-error list to work
   through next.
3. **lipgloss colour model (§3.6/§3.7).** `Theme` fields and colour-typed params
   `lipgloss.Color` → `image/color.Color`; adaptive theming → `lipgloss.LightDark`;
   `colourDisabled()` → `colorprofile`; `hasDarkBackground()` →
   `lipgloss.HasDarkBackground`.
4. **bubbletea core (§3.2/§3.3).** `App.View() string` → `App.View() tea.View`
   with `AltScreen`/`MouseMode`; `cmd/root.go` drops `WithAltScreen`/
   `WithMouseCellMotion` and threads the mouse pref into `App`.
5. **Key handling (§3.4).** `tea.KeyMsg` struct → `tea.KeyPressMsg` + `Key()`;
   keep `key.Matches` call sites; replace all `msg.Type`/`msg.Runes` comparisons.
6. **Mouse handling (§3.5).** Rewrite `mouse.go` for interface-based mouse
   messages (`MouseClickMsg`/`MouseWheelMsg`).
7. **bubbles (§3.8/§3.9).** `viewport.New` options + `SetWidth/SetHeight`
   (`specdetail.go`); `textinput`/`textarea`/`cursor` virtual-vs-real cursor and
   `View.Cursor` wiring (`triageedit.go`).
8. **glamour + huh (§3.10/§3.11).** Confirm `renderer.go` cache and `prompts.go`
   forms behave on the v2 stack.
9. **Tests.** Update all message literals (56 key + 3 mouse) to v2 constructors;
   re-baseline snapshot/golden output intentionally where rendering shifts.
10. **Idiomatic v2 (fold into the same package, not deferred):** background colour
    via `BackgroundColorMsg` instead of the `sync.Once` probe, live mouse toggle
    via `View.MouseMode`, and `go mod tidy` to prune the dependency tree. Keep any
    larger UX experiments (window title, keyboard enhancements) out of scope so
    the single merge stays a faithful port, not a redesign.

**Reviewing one large change:** to keep an atomic merge reviewable, structure the
branch as a small number of well-labelled commits matching the work order above
(mechanical import sweep separate from semantic rewrites), and lean on the
snapshot/golden diff as the reviewer's anchor for behavioural parity. The commits
aid review; the **merge is still a single unit**.

---

## 6. Validation checklist

- [ ] `go build ./...` clean.
- [ ] `make lint-strict` clean at pinned `golangci-lint` (esp. `errorlint` on the new
      type-switches, `unparam`/`wastedassign` on refactors).
- [ ] `go test ./internal/tui/...` green; snapshot/golden diffs reviewed and
      re-baselined intentionally (no accidental drift).
- [ ] Manual smoke: alt-screen enter/exit, mouse on/off (live toggle), wheel scroll
      in spec detail, tab clicks, double-esc-to-quit, g-prefix sequences.
- [ ] `NO_COLOR=1 spec` and a dumb terminal render without ANSI; truecolor terminal
      renders full palette.
- [ ] huh dialogs (`spec` preset/intake prompts) enter/exit cleanly.
- [ ] Static path (`spec --static` / piped) still renders and degrades colour.
- [ ] `go mod tidy`; verify `charm.land/*` + `ultraviolet` + `colorprofile` present
      and old `github.com/charmbracelet/{bubbletea,lipgloss,bubbles,glamour,huh}` and
      `muesli/termenv` gone.

---

## 7. Open questions

- Do we want keyboard-enhancement (key release/repeat) events, or stay press-only to
  minimise behavioural change?
- Should the static dashboard renderer adopt `colorprofile.NewWriter` for explicit
  downsampling, or is implicit stdout behaviour sufficient?
- How far do the idiomatic-v2 touches (background-colour msg, live mouse toggle)
  go inside this single package before they count as scope creep beyond a faithful
  port?
- Pin strategy for the still-`v0` transitive deps (`ultraviolet`, `colorprofile`) —
  exact pin vs. allow patch bumps?
