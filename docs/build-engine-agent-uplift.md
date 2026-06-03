# Build Engine & Agent Harness Uplift

> Ad-hoc spec. Status: **draft**. Author: aaron. Date: 2026-06-03.
> Scope: make `spec build`/`spec do` deliver the bidirectional-MCP build experience
> the SPEC promises, add a first-class **pi.dev** agent adapter, and introduce a
> clean **seam for reproducible agent skills** to live (skill content authored
> separately by a teammate).

---

## 1. Problem Statement

The build phase is where engineers spend ~80% of their time (SPEC §4.10), and the
agent integration is the DX that is supposed to make `spec` indispensable. Today
the pieces exist but are wired more loosely than the design intends, and the
headline "MCP-primary, bidirectional" experience is effectively dead on the
default path.

Concretely, tracing `spec build` → `internal/build/engine.go` → the adapters:

- **G1 — MCP is never actually engaged during a build.** `engine.go` prints
  `"MCP server started on stdio."` but starts no server and writes no MCP config.
  The Claude adapter (`internal/adapter/claude/agent.go`) spawns a bare `claude`
  with no args, no `--mcp-config`/`.mcp.json`, and no context. MCP only works if
  the user hand-created `.mcp.json`, and even then it runs `spec mcp-server`
  **without `--spec`**, so none of the `spec://current/*` resources or the
  build-mode tools (`spec_step_complete`) are available. SPEC ACs (lines
  1068–1071) are unmet in practice.
- **G2 — The post-exit `Step complete? [y/n]` prompt** (`engine.go`, raw
  `fmt.Scanln`) contradicts the AC that MCP completions advance without a
  post-exit prompt, risks double-advancing, and hangs in a tmux split pane or
  `tea.ExecProcess` (the TUI path) where stdin is not interactive.
- **G3 — Prior-diffs are always empty.** `context.go` reads `step-N.diff` from
  the session dir, but nothing ever writes those files, so the cumulative
  cross-PR context (a core §4.10 differentiator) never materialises.
- **G4 — `FailingTests` is declared but never populated** (`context.go`), despite
  being step 4 of the SPEC build-engine flow.
- **G5 — No pi.dev adapter.** `internal/adapter/resolve` maps `pi` (and
  `cursor`/`copilot`) to `noop.Agent` with "not yet implemented", even though pi
  is named as a first-class target throughout the SPEC.
- **G6 — No reproducibility mechanism.** The agent "system prompt" is a hardcoded
  one-liner (`context.go: buildSystemPrompt`). There is no skill or agent-profile
  concept, so implementations and fixes are not reproducible or consistent across
  engineers and projects.

Net effect: an engineer running `spec build SPEC-042` gets a bare agent shell with
no spec context, no way for the agent to record decisions or advance steps, and a
brittle stdin prompt afterwards. The orchestration value proposition is not
delivered.

## 2. Goals & Non-Goals

### Goals

- `spec build <id>` and `spec do` engage the MCP server automatically, focused on
  the active spec, for any MCP-capable agent — zero manual `.mcp.json` editing.
- Ship a working **pi.dev** agent adapter that drives pi via its native flags
  (`--mcp-config`, `--skill`, `--append-system-prompt`, `--session-id`, headless
  `-p --mode json`).
- Establish a **clean seam for agent skills** (`.spec/agent/`): the engine
  discovers and injects skills if present, and works unchanged if absent — so a
  teammate can author the build/fix playbooks later without touching the engine.
- Agents self-advance steps via `spec_step_complete`; eliminate the brittle
  post-exit prompt on the MCP path.
- Make prior-step diffs real so cumulative context works across a PR stack.
- Keep the workflow smooth from the TUI: one action launches a fully-provisioned
  build session and the detail view reflects MCP-driven progress on return.

### Non-Goals

- Rewriting the MCP server protocol. The existing `internal/mcp` + `internal/build/mcp.go`
  resources/tools are adequate; we wire and focus them, not redesign them.
- Building adapters for Cursor/Copilot in this uplift (interface will support
  them; implementations are follow-up).
- Making AI/agents a hard dependency. Non-MCP agents and no-agent flows must keep
  working via the consolidated context file.
- **Authoring the `spec-build`/`spec-fix` skill content.** This work ships the
  seam (discovery + injection + reserved location), not the playbooks. A teammate
  authors the skill bodies against the seam as a follow-up.
- Embedding/knowledge-search work (`spec_search` stays keyword-based here).

## 3. User Stories

| # | As a... | I want to... | So that... |
|---|---|---|---|
| U1 | Engineer | run `spec build SPEC-042` and have my agent already know the spec, ACs, and conventions | I don't re-prompt from scratch |
| U2 | Engineer | have the agent mark a step done from inside its session | the session advances without me babysitting a `[y/n]` prompt |
| U3 | Engineer | use pi as my build agent | I get the same MCP + skill-injection experience as Claude Code |
| U4 | Tech lead | drop a build/fix skill into `.spec/agent/skills/` later | the engine picks it up and every engineer/agent produces consistent results — with no engine change |
| U5 | Engineer | run `spec fix "..." --auto` | small fixes are produced hands-off by a headless agent following the fix playbook |
| U6 | Engineer | resume a multi-PR build | the agent sees diffs from prior steps as cumulative context |
| U7 | Engineer | press `b` on a spec in the dashboard | I confirm, then launch a fully-provisioned build agent and watch step/AC progress without leaving the TUI |
| U8 | Engineer | be asked to confirm before an agent starts | I never spawn a file-editing agent by an accidental keypress |

## 4. Proposed Solution

### 4.1 Concept Overview

Shift context **provisioning** into the build engine and make adapters thin shims
over each harness CLI. The engine assembles context, generates an ephemeral MCP
config pointing at `spec mcp-server --spec <id>`, resolves the agent profile +
skills, and hands a structured request to the adapter. The adapter only translates
that request into the CLI invocation for its harness.

```
spec build <id>
  └─ build.Engine
       ├─ load/create session, checkout branch        (existing)
       ├─ assemble BuildContext (+ prior diffs, conventions)
       ├─ write context.md (fallback) + ephemeral mcp-config.json
       ├─ resolve agent profile + skills IF present (.spec/agent/), else defaults
       ├─ Invoke(InvokeRequest)  ───────────────► AgentAdapter (claude | pi | noop)
       │                                              ├─ claude: write .mcp.json, spawn claude
       │                                              └─ pi: pi --mcp-config … --skill … --append-system-prompt …
       └─ on return: capture step diff, advance only if not already MCP-advanced
```

The MCP server itself is unchanged in capability — `spec mcp-server --spec <id>`
already serves `spec://current/*` and the `combinedHandler` already merges
build-mode tools. We just make the build path actually launch and focus it.

### 4.2 Architecture / Approach

**(a) Widen the `AgentAdapter` interface.** The current two-method interface
cannot carry the spec id, MCP config path, skills, system prompt, or run mode.
Replace `Invoke(ctx, contextFile, workDir)` + `SupportsMCP()` with a
request/result pair and a capabilities descriptor (keeps "accept interfaces,
return structs" per AGENTS.md):

```go
// internal/adapter/agent.go
type AgentAdapter interface {
    Invoke(ctx context.Context, req InvokeRequest) (*InvokeResult, error)
    Capabilities() Capabilities
}

type InvokeRequest struct {
    SpecID        string
    WorkDir       string
    ContextFile   string   // consolidated markdown fallback
    MCPConfigPath string   // engine-generated; runs `spec mcp-server --spec <id>`
    SystemPrompt  string   // assembled build instructions
    SkillPaths    []string // reproducibility skills
    Prompt        string   // kickoff prompt
    Headless      bool     // -p mode for `spec fix --auto` / CI
}

type InvokeResult struct {
    StepSignalled bool // agent advanced the step via MCP during the session
}

type Capabilities struct {
    MCP          bool
    Headless     bool
    Skills       bool
    SystemPrompt bool
}
```

`noop.Agent` and `claude.Agent` are updated; `Capabilities()` replaces
`SupportsMCP()` call sites in `engine.go`.

**(b) Engine-owned provisioning.** In `internal/build`, add `provision.go`:
- `writeMCPConfig(specID, path)` → emits `{"mcpServers":{"spec":{"command":"spec","args":["mcp-server","--spec","<id>"]}}}` to a per-session file (`SessionDir(specID)/mcp-config.json`).
- Engine generates it before `Invoke`, passes the path in `InvokeRequest.MCPConfigPath`.
- For Claude (which discovers `.mcp.json` from the workspace), the claude adapter
  writes/merges `<workDir>/.mcp.json` from `MCPConfigPath` and restores it on exit
  (or writes to a stable, git-ignored path). For pi, the engine path is passed
  straight through as `--mcp-config`.

**(c) pi.dev adapter** (`internal/adapter/pi/agent.go`). pi's CLI maps cleanly:

| Need | pi flag |
|---|---|
| MCP server (focused on spec) | `--mcp-config <MCPConfigPath>` |
| Reproducible skill | `--skill <path>` (repeatable; additive even with `--no-skills`) |
| Build instructions | `--append-system-prompt <SystemPrompt or @file>` |
| Session continuity per spec | `--session-id spec-<id>` |
| Kickoff | trailing prompt arg / piped stdin |
| Headless autonomous | `-p --mode json` |

Interactive invoke (default): inherit stdio, spawn pi, block until exit.
Headless invoke (`Headless`): run `-p --mode json`, stream events, and set
`InvokeResult.StepSignalled=true` when a `tool_execution_end` event for
`spec_step_complete` (success) is observed. `Capabilities{MCP:true, Headless:true,
Skills:true, SystemPrompt:true}`.

`resolveAgent` in `internal/adapter/resolve` gains a `case "pi"` returning the new
adapter; the `command` setting overrides the binary name (default `pi`).

**(d) Skill seam (plumbing only — skills authored separately).** This work does
**not** ship the build/fix skill content. It establishes a clean, well-defined
place for skills to live and wires the engine to discover and inject whatever is
there, so a teammate can author the actual playbooks later without touching the
engine. Authoring `spec-build`/`spec-fix` is explicitly a follow-up (see
Non-Goals and Open Questions).

The convention:

```
.spec/
  conventions.md             # already read by engine
  agent/
    profile.yaml             # OPTIONAL: model, thinking, skill refs (engine reads if present)
    skills/                  # OPTIONAL: skill files/dirs a teammate fills in later
      .gitkeep                # ships empty; reserves the location
```

What this work ships (the seam):

- **Discovery.** The engine resolves skills from, in order: explicit
  `integrations.agent.settings.skill` paths, then `profile.yaml`'s `skill` refs,
  then any files/dirs under `.spec/agent/skills/`. Missing/empty → no skills, and
  the build proceeds normally.
- **Injection.** Resolved paths flow into `InvokeRequest.SkillPaths`. The pi
  adapter passes each as `--skill <path>`; Claude maps them into its skills dir;
  for non-skill agents the engine concatenates the skill bodies into the
  consolidated `context.md`. If `SkillPaths` is empty, every path is a no-op.
- **System prompt.** The engine assembles `InvokeRequest.SystemPrompt` = a
  minimal base build instruction + current-step scope + conventions. This base
  prompt stays intentionally thin; the *behavioural playbook* is what the
  separately-authored skill provides.
- **Profile.** `profile.yaml` is OPTIONAL and read leniently — `model`,
  `thinking`, and `skill` refs only. Absent file ⇒ engine defaults; unknown keys
  ignored. No skill content is required for a profile to be valid.

```yaml
# .spec/agent/profile.yaml — all fields optional
model: anthropic/claude-sonnet-4
thinking: medium
skill:
  build: .spec/agent/skills/spec-build      # paths a teammate will create later
  fix: .spec/agent/skills/spec-fix
```

Config surfaces under `integrations.agent.settings` (`config.ProviderConfig.Get`):
`model`, `skill`, `headless`, `command`.

> **Out of scope here:** writing the `spec-build`/`spec-fix` skill bodies and the
> step-by-step build loop they encode. Those are authored by a teammate against
> this seam. The acceptance criteria below test that the seam works with skills
> present and absent — not the content of any specific skill.

**(e) Completion flow fix.** Use `InvokeResult.StepSignalled` and re-read session
state after `Invoke`. Only fall back to the interactive `[y/n]` prompt when the
agent is **not** MCP-capable **and** the current step is still `in-progress`.
On the MCP path, advancement happens via the tool; the engine just reports status.

**(f) Real prior diffs.** On step completion, capture `git.Diff(ctx, workDir,
baseRef)` (already exists, `internal/git/branch.go`) into
`SessionDir(specID)/step-N.diff`. `AssembleContext` already consumes these, so
cumulative context starts working with no reader changes. Populate
`BuildContext.FailingTests` opportunistically from a configured test command's
output when present (best-effort, non-fatal).

**(g) TUI build experience.** Covered in detail in §4.3.

### 4.3 TUI Build Experience

The build is triggered from the dashboard/spec-detail view by pressing **`b`** on a
selected spec (`keymap.go: Build`). Today `b` calls `buildSpec` immediately
(`app.go:1210`); this uplift routes it through a **confirmation modal first**, so
launching an agent is a deliberate action (see change 1 below). Once confirmed,
two launch modes already exist in `internal/tui/buildcmd.go` and are kept:

- **Multiplexer pane (non-suspending).** When `User.Preferences.Multiplexer` is
  `tmux`/`zellij`/`wezterm`, the build runs in a new split pane via `spawnPane`.
  The TUI stays live and gets an immediate `actionResultMsg{Detail: "launched in
  <mux> pane"}`.
- **Suspend-and-run (fallback).** Otherwise `tea.ExecProcess` suspends the TUI,
  runs `spec build <id>` in the current terminal, and resumes on exit, emitting
  `actionResultMsg{Action: "build", Err: ...}`.

This uplift makes the following concrete TUI changes:

1. **Confirm before invoking build.** `b` no longer launches directly. It follows
   the existing modal pattern used by Advance/Unblock (`app.go`): set
   `a.pendingAction = "build"` + `a.pendingSpecID`, then
   `a.modal.ShowConfirm("Build "+specID, msg)`. `executeActionWithInput` gains a
   `case "build"` that runs `a.startAction("building "+specID,
   buildSpec(a.rc, specID))` only after the user confirms. Pressing `esc` /
   declining cancels with no side effects and no spawned process.
   - The confirm body states what will happen and which agent runs, e.g.
     `Launch the pi build agent for SPEC-042 (step 2/4)? It connects to the spec
     MCP server and can edit files and record decisions.` The agent name comes
     from `integrations.agent.provider`; the step comes from `build.LoadSession`
     when a session exists.
   - The pre-flight stage guard (status must be `build`/`engineering`) is checked
     **before** showing the modal, so an invalid spec surfaces an inline
     status-bar error and never reaches the confirm step.
2. **No stdin hang in panes.** Because the `[y/n]` post-exit prompt is removed on
   the MCP path (§4.2e), a build launched into a split pane no longer blocks on a
   prompt that has no interactive stdin. The agent advances steps via
   `spec_step_complete`; the pane simply shows the agent session.
3. **Pre-flight validation stays in the TUI, provisioning stays in the engine.**
   `buildSpec` keeps its existing stage guard (status must be `build`/`engineering`,
   `buildcmd.go`) and returns an actionable `actionResultMsg` error inline (shown
   on the status bar) *before* spawning. All context/MCP-config/skill provisioning
   happens inside `spec build` (the engine), so the TUI never needs to know about
   MCP wiring.
4. **Status-bar feedback.** Launch state flows through the existing
   `startAction("building <id>", …)` → `actionResultMsg` path: a busy label while
   launching, then a success toast (`<id> build: launched in tmux pane`) or an
   error summary with detail. No new status-bar machinery.
5. **Detail refresh on return.** On any `actionResultMsg{Action: "build"}` the app
   calls `refreshActiveView()` (the existing handler at `app.go:429`), and the
   spec-detail view re-reads session state so the **PR-step progress** and
   **acceptance-criteria count** reflect what the agent did via MCP. For the
   non-suspending pane mode, the periodic `tickMsg` refresh (and the existing
   `specdetail_refresh` path) picks up step/AC changes while the agent works in
   the adjacent pane.
6. **Build status surfaced in spec-detail.** The spec-detail header gains a
   compact build line when a session exists for the spec: `Build: step 2/4 —
   [api-gateway] rate-limit middleware · ACs 3/7`. Data comes from the build
   session (`build.LoadSession`) + AC parse already used by the CLI
   (`showACProgress`); no new persistence. This makes the dashboard the single
   place to see whether a build is mid-flight, which step, and how close to done.
7. **Help affordance.** The `b`/build binding already appears in the key help
   (`keymap.go`, `help.go`); the help text is clarified to "start/resume build
   (MCP agent)" so users understand it launches the configured agent with full
   context rather than an empty shell.

Explicitly **out of scope for the TUI**: rendering the agent's live token stream
inside the TUI, or replacing the pane/exec launch with an embedded terminal. The
agent runs in its own pane/term; the TUI's job is launch + reflect progress.

## 5. Design Inputs

- SPEC.md §4.10 (Build Engine deep-dive), §4.13 (two agent concepts), decision
  rows 014/015, ACs at lines 1058–1074.
- pi CLI surface (verified): `--mcp-config`, `--skill` (repeatable, additive with
  `--no-skills`), `--append-system-prompt` (text or file), `--session-id`,
  `--print/-p`, `--mode json` event stream (`tool_execution_end` carries
  `toolName`/`result`/`isError`).
- pi skills are Agent Skills standard (`SKILL.md` or root `.md`) with `name` +
  `description` frontmatter; loadable cross-harness.
- Claude Code is MCP-native via workspace `.mcp.json`.
- `internal/git` already exposes `Diff(ctx, dir, baseRef)` and `RevParse`.

## 6. Acceptance Criteria

- [ ] `AgentAdapter` uses `Invoke(ctx, InvokeRequest) (*InvokeResult, error)` +
  `Capabilities()`; `noop` and `claude` updated; build compiles and `make lint-strict` passes.
- [ ] Running `spec build <id>` generates a per-session MCP config and the agent
  can read `spec://current/full` and call `spec_step_complete` with no manual
  `.mcp.json` setup (verified for Claude and pi).
- [ ] A `pi` provider resolves to the pi adapter (not noop); `spec build` launches
  pi with `--mcp-config`, `--append-system-prompt`, and one `--skill` per
  resolved skill path (zero `--skill` flags when none are present).
- [ ] **Skill seam:** with `.spec/agent/skills/` empty (the shipped state), a build
  runs normally with no skills injected; dropping a skill file/dir in (or setting
  `agent.settings.skill`) causes it to be passed to the agent — with no engine
  code change. A malformed/missing profile is ignored gracefully.
- [ ] When the agent calls `spec_step_complete`, the session advances and `spec do`
  resumes on the next step with **no** post-exit `[y/n]` prompt.
- [ ] For a non-MCP agent, the consolidated `context.md` is written and includes
  the skill body and current-step scope; the `[y/n]` fallback still works.
- [ ] After completing step N, `SessionDir/step-N.diff` exists and step N+1's
  assembled context includes it under `spec://current/prior-diffs`.
- [ ] `spec fix "<title>" --auto` runs a headless pi session (injecting the fix
  skill if one is present), and advances the step via MCP; exits non-zero on
  agent error. (Behaviour is correct with or without a skill authored.)
- [ ] Pressing `b` shows a confirmation modal naming the agent and spec; declining
  or pressing `esc` spawns nothing and leaves no session/process side effects.
- [ ] After confirming, in a tmux/zellij pane the build launches without hanging on
  stdin, and the TUI stays live with a `launched in <mux> pane` status toast.
- [ ] In suspend-and-run (no multiplexer) mode, exiting the agent resumes the TUI
  and the status bar shows a build success/error result.
- [ ] The spec-detail view shows a build line (`step N/M — [repo] desc · ACs x/y`)
  whenever a build session exists, and it updates on return / next refresh tick
  to reflect steps the agent advanced via MCP.
- [ ] A pre-flight stage-guard failure (`b` on a non-build spec) surfaces as an
  inline status-bar error and never spawns an agent.
- [ ] Adapter failures degrade gracefully: a missing `pi`/`claude` binary returns
  an actionable error and never panics.

## 7. Technical Implementation

### 7.1 Architecture Notes

- `cmd/` stays thin: `build.go`, `do.go`, `fix.go` only resolve config + spec path
  and call the engine. New `--auto`/`--headless` flag plumbs into `InvokeRequest.Headless`.
- Provisioning, skill/profile **resolution** (not authoring), and diff capture
  live in `internal/build`. Skill discovery is a small, well-named helper so the
  follow-up skill-authoring work has an obvious place to plug into.
- Only `internal/git` shells out (diff capture uses existing helpers).
- Adapters import `internal/adapter` only; the MCP-config JSON is built in
  `internal/build` and passed as a path, keeping adapters free of build internals.
- pi adapter's headless event parsing is isolated; interactive mode just inherits stdio.

### 7.2 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `--mcp-config` merges vs replaces pi's discovered servers | Med | Med | Confirm against pi docs/behaviour before locking; if it replaces, include any baseline servers in the generated file |
| Claude `.mcp.json` write pollutes workspace | Med | Low | Write to a stable path, add to `.gitignore` guidance, restore/remove on exit |
| Headless completion detection misses the event | Low | Med | Treat absence as "not signalled" and fall back to session-state re-read; never silently mark done |
| Interface change ripples across callers/tests | High | Low | Mechanical; `Capabilities()` replaces `SupportsMCP()` at the two call sites |
| Double-advance (agent + post-exit prompt) | Med | Med | Gate prompt on `!MCP && step still in-progress`; re-read session after Invoke |

### 7.3 Change Package

This ships as a **single, cohesive change** (one branch, one PR): the build path
is broken until provisioning, the widened interface, and the agents land
together, so splitting them would leave intermediate states that don't deliver a
working build. Suggested commit `feat: MCP-native build engine + pi adapter +
reproducible agent skills`.

Files touched, in build order within the single change:

- `internal/adapter/agent.go` — widen `AgentAdapter` to `Invoke(ctx,
  InvokeRequest) (*InvokeResult, error)` + `Capabilities()`.
- `internal/adapter/noop/`, `internal/adapter/claude/agent.go` — conform to the
  new interface; Claude writes/restores workspace `.mcp.json` from the request.
- `internal/build/provision.go` (new) — MCP-config generation + system-prompt /
  skill assembly; `internal/build/engine.go` — call provisioning, pass the
  request, fix the completion flow, capture `step-N.diff`, populate
  `FailingTests` (best-effort).
- `internal/adapter/pi/agent.go` (new) + `internal/adapter/resolve` — pi adapter
  and `case "pi"`.
- `.spec/agent/` defaults — `profile.yaml` resolution + shipped `spec-build` /
  reserve `.spec/agent/skills/` (empty, `.gitkeep`) and read OPTIONAL
  `profile.yaml`; `internal/config` settings keys (`model`, `skill`, `headless`,
  `command`). No skill bodies are authored here.
- `cmd/build.go`, `cmd/do.go`, `cmd/fix.go` — thin wiring; `--auto`/`--headless`
  flag → `InvokeRequest.Headless`. `internal/tui/app.go` — `b` routes through a
  confirm modal (`pendingAction="build"`, `executeActionWithInput` case).
  `internal/tui/buildcmd.go` — no stdin hang on
  the MCP path; `internal/tui/specdetail.go` — build status line
  (`step N/M · ACs x/y`) from `build.LoadSession`; `keymap.go`/`help.go` — build
  help text clarified. Detail refreshes on the existing `actionResultMsg` /
  `tickMsg` paths.

The acceptance criteria in §6 define done for the whole package; it is not
considered shippable until every checkbox passes and `make lint-strict` is clean.

## 8. Escape Hatch Log

*No escapes logged.*

## 9. QA Expectations

**Happy path:**
- `spec build SPEC-042` with `agent.provider: pi` launches pi, agent reads spec
  via MCP, records a decision, runs tests, calls `spec_step_complete`; `spec do`
  resumes on the next step.
- Same flow with `agent.provider: claude-code`.

**Edge cases:**
- No `.spec/agent/` present → defaults used, build still works.
- Agent binary missing → actionable error, no panic.
- Non-MCP agent → context.md fallback includes any present skill bodies + scope;
  `[y/n]` prompt works.
- Skill seam: empty `.spec/agent/skills/` → build runs, no `--skill` flags; add a
  skill → it's injected, no engine change.
- Missing/malformed `profile.yaml` → engine defaults, build proceeds.
- Multi-PR stack → step-2 context includes step-1 diff.
- TUI launch in tmux pane → no stdin hang; TUI stays live; detail line updates on next tick.
- TUI suspend-and-run mode → agent runs inline, TUI resumes cleanly on exit.
- `b` pressed on a non-build-stage spec → inline status-bar error, no modal, no spawn.
- `b` → confirm modal → decline/`esc` → no agent launched, TUI unchanged.

**Out of scope:**
- Cursor/Copilot adapters; embeddings-based `spec_search`; deploy-phase changes.
- Authoring the `spec-build`/`spec-fix` skill bodies (follow-up; tested only as
  "the seam injects whatever is present").

## 10. Retrospective

*To be completed after build.*

## Decision Log

| # | Question / Decision | Decision Made | Rationale | Date |
|---|---|---|---|---|
| 1 | Where should context provisioning live? | In `internal/build` (engine), not adapters | Adapters stay thin shims over CLIs; one provisioning path serves all MCP agents and fixes G1 generically | 2026-06-03 |
| 2 | Evolve `AgentAdapter` or add methods? | Replace with `InvokeRequest`/`InvokeResult` + `Capabilities()` | Two-method interface can't carry spec id / mcp config / skills / run mode; struct request keeps signatures stable for future fields | 2026-06-03 |
| 3 | How to deliver reproducible behaviour? | Ship the **seam** (discover + inject skills, reserve `.spec/agent/skills/`, optional `profile.yaml`); author skill bodies separately | Lets a teammate own the playbooks without touching the engine; keeps this change focused; engine works identically with skills present or absent | 2026-06-03 |
| 4 | How does the agent signal step completion? | Via MCP `spec_step_complete`; engine gates the `[y/n]` prompt on non-MCP + in-progress | Matches SPEC AC line 1071; removes the stdin hang in panes | 2026-06-03 |
| 5 | Should the TUI embed the agent's live output? | No — keep the existing pane/`tea.ExecProcess` launch; TUI only launches + reflects session progress | Embedding a terminal/token stream is a large surface for little gain; the multiplexer pane already gives a live view; keeps the TUI change small and robust | 2026-06-03 |
| 6 | Should `b` launch the agent immediately? | No — require a confirm modal first, reusing the Advance/Unblock pattern | A build spawns a file-editing agent and a subprocess/pane; that should be deliberate, not a single accidental keypress; matches existing destructive-action UX | 2026-06-03 |

## Open Questions

- Does pi's `--mcp-config` **merge with** or **replace** discovered MCP servers?
  (Confirm before locking the generated-config contents.)
- Preferred Claude `.mcp.json` strategy: write-and-restore in workspace, or a
  stable git-ignored path? Leaning git-ignored stable path to avoid churn.
- Should the headless `--auto` path be gated behind the existing fast-track role
  checks in `cmd/fix.go`, or available to any build? (Leaning: reuse fast-track gating.)
- Default model/thinking for `profile.yaml` — bake in a conservative default or
  require explicit config?
- **Follow-up handoff (skill authoring):** the teammate will author skills under
  `.spec/agent/skills/` (Agent Skills format: `SKILL.md` with `name` +
  `description`). Open for them: the exact build loop the skill encodes (read ACs
  → implement → record decisions → lint/test → `spec_step_complete`), and whether
  `spec-build` and `spec-fix` are one parameterised skill or two. This spec only
  guarantees the engine will discover and inject those files.
