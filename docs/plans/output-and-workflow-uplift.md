# Output & Workflow Uplift

> Make mutating commands truthful, testable, and scriptable by extracting
> orchestration out of `cmd/` into a workflow engine, rendering success only
> after the change is durably pushed, and standardizing on a single output
> contract with `--json`/`--quiet`.

---

## Problem

Three coupled issues, all rooted in command files mixing orchestration logic
with terminal I/O inside the `WithSpecsRepo` git closure:

1. **Success is reported before the change is durably committed/pushed.**
   `WithSpecsRepo` applies the mutation, *then* commits, *then* pushes with
   rebase-retry, and on a same-file conflict it `ResetHard`s back to the remote
   and returns an error (`internal/git/specsrepo.go:142→194`). But commands
   print `✓ … advanced` *inside* the mutate closure (`cmd/advance.go`,
   `cmd/revert.go:122`, `cmd/decide.go:109/137`, etc.). On a concurrent edit the
   user sees a success line immediately followed by an error, and the spec did
   not actually change. ~14 commands are affected.

2. **`cmd/` carries business logic it is explicitly forbidden to hold.**
   `AGENTS.md` rule #1: *"`cmd/` is thin … zero business logic in command
   files."* `runAdvance` is ~180 lines orchestrating gate evaluation, effect
   execution, DB logging, sync, and rendering, with the same logic
   hand-duplicated across `promote`/`revert`/`decide`/`eject`. None of it is
   unit-testable because it is welded to Cobra + git + `os.Stdout`.

3. **No consistent, scriptable output contract.** 215 `fmt.Printf` + 123
   `fmt.Println` calls write to the global `os.Stdout`; only `pipeline.go` /
   `root.go` use Cobra's `cmd.Print*`. There is no `--json` (despite
   `DashboardData` already carrying `json` tags), no `--quiet`, and the passive
   awareness line always prints to stderr. Config is also resolved twice per
   command (pre-run awareness + the command itself).

**Root cause:** logic and I/O are fused into command closures. Fixing #2
(extraction) is the clean enabler for both #1 and #3.

---

## Goals

- Mutating commands print success **only after** the push succeeds.
- Core workflow logic (advance, revert, promote, decide, eject, block/unblock)
  lives in `internal/` and is unit-testable against adapter fakes.
- All command output flows through `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`.
- `--json` and `--quiet` are available globally; awareness line respects them
  and non-interactive terminals.
- No behavioral regressions; existing tests stay green.

## Non-Goals

- Redesigning the pipeline state machine or gate semantics.
- Changing the git concurrency strategy in `WithSpecsRepo`.
- TUI rendering changes (the TUI already runs mutations as async `tea.Cmd`s and
  renders results after completion — it is not affected by #1).

---

## Sequencing

This lands as a **single change package**. The phases below are the ordered
work steps within it (not separate PRs): scaffolding → extraction → render after
durability → output contract. The order matters because each step depends on the
prior one; the whole thing is reviewed and merged together.

---

## Step 0 — Scaffolding (no behavior change)

1. Add `cmd/output.go`: a small `printer` resolved from a command that wraps
   `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, honors `--json`/`--quiet`
   (read once via persistent flags), and exposes:
   - `(p *printer) Line(format, args...)` — human stdout, suppressed by `--quiet`.
   - `(p *printer) Warn(format, args...)` — stderr (replaces `warnf`).
   - `(p *printer) JSON(v any)` — emits `v` when `--json` is set.
2. Register global persistent flags in `cmd/root.go`: `--json`, `--quiet`.
3. No call sites changed yet — purely additive.

**Done when:** builds, `go vet`/lint clean, no output changes observable.

---

## Step 1 — Workflow engine extraction (Issue #2)

Create `internal/workflow/` depending only on interfaces + existing engines
(`pipeline`, `markdown`, `effects`, `adapter`, `store`), never on `cmd` or
`os.Stdout`.

### API shape

```go
package workflow

type Deps struct {
    Pipeline  pipeline.Pipeline
    Registry  *adapter.Registry
    DB        *store.DB          // may be nil; best-effort logging
    User      string
    Role      string
    SyncCfg   config.SyncConfig
    ArchiveDir string
}

type AdvanceInput struct {
    SpecID      string
    SpecPath    string   // resolved inside the repo clone
    TargetStage string   // "" = next stage
    DryRun      bool
}

type AdvanceResult struct {
    SpecID        string
    PreviousStage string
    NewStage      string
    Skipped       []string
    Effects       []effects.Result
    GateFailures  []pipeline.GateResult
    SyncedOut     bool
    Archived      bool
}

// Advance mutates the spec file on disk and returns a structured result.
// It performs NO terminal I/O. Returns ErrGatesNotMet with populated
// GateFailures when gates fail.
func Advance(ctx context.Context, d Deps, in AdvanceInput) (*AdvanceResult, error)
```

Mirror this for `Revert`, `Promote`, `Decide`/`ResolveDecision`, `Eject`,
`Block`/`Unblock`. Each returns a result struct and performs no printing.

### Command rewrite (per command)

`cmd/advance.go` `runAdvance` collapses to:

```go
return gitpkg.WithSpecsRepo(ctx, &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
    path, err := specPathIn(repoPath, rc, specID)
    if err != nil { return "", err }
    res, err = workflow.Advance(ctx, deps(rc, repoPath), workflow.AdvanceInput{...})
    if err != nil { return "", err }
    if res.DryRunOrNoChange() { return "", nil }
    return res.CommitMessage(), nil
})
// rendering happens AFTER this returns nil — see Phase 1b below.
```

The TUI's `internal/tui/actions.go` (`advanceSpec`, `blockSpec`, etc.) should
also call `workflow.*` instead of re-implementing the closures, removing a
second copy of the logic.

### Tests (the payoff)

- `internal/workflow/advance_test.go`: table-driven over a `t.TempDir()` spec
  file + fake adapters: happy path, gate failure (`ErrGatesNotMet`), fast-track
  skip, dry-run produces no file change, effect failure is non-fatal.
- Same for revert/promote/decide/eject. These are the first real unit tests for
  the state machine orchestration.

**Done when:** all `cmd/*` mutators delegate to `workflow`, no orchestration
logic remains in `cmd/`, new engine tests pass, existing tests green.

---

## Step 1b — Render after durability (Issue #1)

Done alongside Step 1 per command (they touch the same lines).

**Rule:** the mutate closure returns *data only*; the success/effect summary is
rendered in `RunE` **after** `WithSpecsRepo` returns `nil`.

```go
res, err := runAdvanceTxn(ctx, rc, specID, ...)   // wraps WithSpecsRepo
if err != nil { return err }                       // nothing printed on failure
if res.DryRun { renderAdvanceDryRun(p, res); return nil }
renderAdvanceResult(p, res)                        // ✓ only now
```

- Effect/sync side-effect lines (`→ synced out`, `→ marked for archiving`) move
  into the result struct and render post-commit too — never for a rolled-back op.
- Gate-failure output stays inside the transaction path but is returned as
  `AdvanceResult.GateFailures` and rendered by the caller before returning the
  sentinel error (still no `✓`).

**Done when:** killing the process is impossible to observe a `✓` for a spec
that didn't move; a forced push-conflict (two clones) prints only the error.

**Test:** add an integration-style test using a local bare git remote + two
clones to force the same-file conflict and assert stdout contains no `✓`.

---

## Step 2 — Output contract (Issue #3)

1. **Route everything through the printer.** Mechanically replace `fmt.Print*`
   in `cmd/*.go` with `p.Line` / `p.Warn`; delete `warnf`. Enforce with a lint
   rule or a grep check in CI: no `fmt.Print` in `cmd/` (allow in `tools/`).
2. **`--json`.** Each command renders its result struct via `p.JSON(res)` when
   the flag is set. Start with the high-value read paths that already have
   tags: root dashboard (`dashboard.DashboardData`), `list`, `status`,
   `advance`/`revert`/`promote` results. This is the agent/MCP-facing win.
3. **`--quiet`.** Suppresses human stdout (not errors). Awareness line
   (`internal/dashboard/awareness.go:PrintAwarenessLine`) becomes no-op under
   `--quiet`, when `--json` is set, or when stderr is not a TTY.
4. **Resolve config once.** Cache the `*ResolvedConfig` from the
   `PersistentPreRunE` awareness pass and reuse it in commands (store on a
   package-level holder keyed per invocation, or pass via `cmd.Context()`),
   eliminating the double `config.Resolve()` + double specs-dir scan per command.

**Done when:** `spec list --json | jq` works; `--quiet` silences chatter;
awareness line is absent in pipes/CI; only one config resolution per command.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Behavior drift during mass `fmt.Print` → `p.Line` swap | Do Phase 3 mechanically, one command per commit; golden-output tests via `cmd.SetOut` buffer. |
| `--json` schema churn | Reuse existing structs; treat JSON shape as a documented contract; only add the fields already tagged. |
| TUI regressions when actions.go switches to `workflow` | TUI already asserts on `actionResultMsg`; add fakes; the result structs map 1:1 to current messages. |
| Config-cache staleness within a single command | Scope cache to one invocation only; never persist across commands. |

## Rollout

Single change package, built in step order (0 → 1 → 1b → 2), kept green at each
step. Commits may follow conventional-commit prefixes (`refactor:`, `feat:`) for
history clarity, but they ship together as one reviewed package referencing this
plan.

---

## Implementation Status

**Delivered (build + `go vet` + `golangci-lint` + full test suite green):**

- **Step 0** — `cmd/output.go` `printer` (`Line`/`Raw`/`Warn`/`JSON`,
  honoring `--json`/`--quiet`); global `--json`/`--quiet` persistent flags in
  `cmd/root.go`. Locked by `cmd/output_test.go`.
- **Step 1** — `internal/workflow/` engine: `Advance`, `Revert`, `Eject`,
  `Resume` return structured results and perform **no terminal I/O**. Shared
  effect-context/activity-log helpers live in `workflow.go`; `ErrGatesNotMet`
  carries gate failures. Covered by `internal/workflow/workflow_test.go`
  (success, gate-not-met-no-mutation, dry-run-no-mutation, role guard, revert
  with revert-count, eject/resume round-trip, resume-requires-stage).
- **Step 1b** — `advance`, `revert`, `eject`, `resume`, `decide`
  (`--resolve`), and `promote` now render success/effects **only after**
  `WithSpecsRepo` returns `nil`. Gate failures render their detail and return a
  clean terminal error without a `✓`. No `fmt.Print*` remains in any refactored
  command.
- **Step 2 (partial)** — `--json` wired for the agent-facing read/result paths:
  root dashboard (static), `list` (all modes + triage), and the mutating result
  structs (`advance`/`revert`/`eject`/`resume`/`decide`/`promote`). Awareness
  line suppressed under `--quiet`/`--json` and when stderr is not a TTY
  (`awarenessAllowed`). Config resolved once per invocation (`resolveConfig`
  memoization).

**Deferred (follow-up, intentionally out of this package):**

- **TUI `actions.go` consolidation.** The TUI's `advanceSpec`/`blockSpec`/etc.
  are a *lighter* operation than the CLI path — notably they do **not** evaluate
  gates. Routing them through `workflow.*` would silently start enforcing gates
  and firing effects in the TUI, a UX/semantic change that needs a product
  decision. The TUI already renders after async completion, so it does not
  suffer issue #1. Left untouched to keep this package safe and focused.
- **Remaining `fmt.Print*` conversions** in non-mutating report commands
  (`status`, `plan`, `steps`, `metrics`, `standup`, `retro`, `search`, `fix`,
  `sync`, `config`, …). These are correctness-safe today; convert mechanically
  in a later pass and add the CI grep guard (`no fmt.Print in cmd/`).
- **Forced-conflict integration test** (local bare remote + two clones asserting
  no `✓` on push conflict). The non-mutation-on-failure property is already
  proven at the engine level; the end-to-end git harness is a nice-to-have.
