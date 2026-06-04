# Plan — Harden the build integration into a tidy Integration Port

> **Goal:** turn the current implicit, leaky `spec build` ⇄ agent seam into an
> explicit, versioned, documented **Build Integration Port** that any agent
> build system (pi, Claude Code, Cursor, a bespoke runner, CI) can integrate
> against without reading spec-cli's source or the `ai-squad-skills` skills.

This plan addresses the faults found in the integration review (the
skill-injection seam, completion semantics, config cross-wiring, and silent
degraded mode) and reframes the fixes around one organising idea: **spec-cli
exposes a port; harnesses implement an adapter.**

---

## 1. The organising idea: a Build Integration Port

Today the "contract" between spec-cli and an agent build system is *implicit* —
spread across the MCP resources/tools (`internal/build/mcp.go`), the baked-in
prompts (`internal/build/context.go`), the skill-routing fallbacks
(`internal/build/registry.go`, `provision.go`), and the `ai-squad-skills`
`SKILL.md` files. A new harness can only integrate by reverse-engineering all of
these.

The target state is a single, named **port** with three planes:

| Plane | What spec-cli guarantees | Consumed by |
|-------|--------------------------|-------------|
| **Data plane** | `spec://current/*` resources — DAG, spec, ACs, conventions, prior diffs — with a **stable, versioned JSON schema** for the DAG. | Any MCP client. |
| **Control plane** | Idempotent MCP tools: `spec_provision_node`, `spec_node_complete`, `spec_node_failed`, `spec_push`, `spec_open_pr`, `spec_link_prs`, `spec_decide`. Server owns all git/GitHub mechanics. | The conductor (orchestrator). |
| **Capability plane** | Per-node skill *routing* (which capability the node needs) — surfaced as data, never injected into the conductor. | The worker dispatcher. |

The harness side implements an **adapter** that:
1. reads the DAG,
2. walks waves, dispatching one worker per node into the provisioned `workDir`,
3. checkpoints each node via the control plane,
4. finishes with the PR tools.

`ai-squad-skills` becomes *one reference adapter* (the pi/Claude skill set), not
the only way to drive a build.

**Design rule going forward:** spec-cli supplies *deterministic mechanism +
data*; it never describes a competing execution model in prose, and it never
pushes capability/policy content into the conductor's context.

---

## 2. Faults this plan closes

Mapped from the integration review (severity in brackets):

- **A [High]** Two contradictory execution models in one session — baked-in
  prompts describe a single-agent self-do loop; the orchestrator skill describes
  conductor+workers.
- **B [High]** Cross-repo skill **name collision** — `unionSkills` injects
  duplicate-named worker skills (`deep-review`, `diagnose`) into the conductor.
- **C [High]** "Build complete" ≠ artifact — `reconcile()` reports success from
  node status alone, ignoring whether draft PRs exist.
- **D [High]** Silent degraded mode — without the external skill, a build
  provisions/completes nodes but never pushes or opens PRs, still reporting ✓.
- **E [Med]** Conductor↔node config cross-wiring — `agent.skill` means
  "conductor skills" to the skill suite but "node worker fallback" to spec-cli.
- **F [Med]** Conductor context pollution — worker skill descriptions loaded
  into the conductor that must never self-load them.
- **G [Low]** Vestigial `InvokeResult.StepSignalled` (computed, discarded).
- **H [Low]** Worker isolation cost — forked workers inherit MCP + skill union
  they are told not to use.
- **I [Low]** Misleading provisioning pseudocode (provision shown inside the
  parallel loop).
- **J [Low]** Single-repo `FailingTests` context for multi-repo DAGs.
- **K [Low]** Headless fan-out fragility.

---

## 3. The port specification (target contract)

This is the artifact a third-party build system reads. It will live at
`docs/INTEGRATION-PORT.md` and be the **only** document required to write a new
adapter.

### 3.1 Capability negotiation

spec-cli already models agent capabilities (`adapter.Capabilities{MCP, Headless,
Skills, SystemPrompt}`). Extend this into an explicit handshake so the engine
adapts what it emits:

- `MCP` true → conductor drives via tools; spec-cli emits **only** the DAG +
  resources and the two conductor-level skills (see §4.1). No worker skill
  bodies in the conductor.
- `MCP` false → spec-cli folds the consolidated context file + skill bodies into
  the prompt (existing `!caps.Skills` path in `provision()`), and the single
  agent self-drives. This is the *only* place the self-do model is legitimate.
- A new `caps.Subagents` (or `caps.FanOut`) bit lets the engine tailor the
  kickoff: when false, the conductor is told to walk waves sequentially.

### 3.2 The DAG resource is the versioned schema

`spec://current/dag` (`mcp.go dagJSON`) becomes the formal interface. Add:

- `schemaVersion` (string, e.g. `"build-port/v1"`) at the document root.
- A published JSON Schema at `docs/schemas/dag.v1.json`, validated in tests.
- A documented stability policy: additive changes only within `v1`.

### 3.3 Per-node skill routing as data, never injection

`skillPaths` already appears per-node in the DAG and in the
`spec_provision_node` result. The contract states plainly: **the conductor reads
node `skillPaths` and hands them to the worker it dispatches; spec-cli does not
add node skills to the conductor's own skill set.**

### 3.4 Completion is artifact-defined

The port defines "done" as: every node `complete` **and** every leaf node
carries a recorded draft-PR URL (`AllLeavesHaveDraftPR`, `gate.go`). The build
command reports against this definition, not node status alone.

---

## 4. Workstreams (PR stack)

Sequenced so each PR is independently shippable and reviewable. Node ids map to
a §7.3-style stack if this becomes a spec.

### PR 1 — Stop polluting the conductor; route skills as data only

**Closes B, F (and removes the collision class entirely).**

- In `engine.go provision()`, when `caps.Skills && caps.MCP`, pass pi **only**
  the conductor skills (build-orchestrator + pr-finisher equivalents resolved
  from a new dedicated config key, see PR 4), **not** `buildCtx.SkillPaths`.
- Keep `unionSkills` / `buildCtx.Skills` strictly for the `!caps.Skills`
  fallback (bodies folded into the prompt + context file) — that path has no
  name-collision risk because bodies are concatenated, not registered by name.
- Per-node `skillPaths` continue to flow through `spec_provision_node` only.

**Acceptance:**
- Building a cross-repo spec whose nodes route same-named modifier skills no
  longer passes duplicate `--skill` names to the agent (unit test on the
  emitted `InvokeRequest.SkillPaths`).
- `provision()` for an MCP+Skills agent emits ≤ the conductor skill count,
  independent of node count.

### PR 2 — Make the engine's prompts defer to the conductor (kill the model clash)

**Closes A, D.**

- Rewrite `buildSystemPrompt` / `buildKickoffPrompt` (`context.go`) for the
  `MCP` case to **name the conductor contract** rather than describe a self-do
  loop: "Conduct this build via the Build Integration Port — read
  `spec://current/dag`, walk waves, dispatch one worker per node into its
  provisioned `workDir`, checkpoint via the node tools, finish with the PR
  tools. If you cannot act as the conductor, stop and report." Optionally emit
  the harness-specific invocation hint (e.g. `/skill:build-orchestrator` for pi)
  driven by a small per-provider template.
- Keep the self-do prose only behind the `!caps.MCP` branch.

**Acceptance:**
- The MCP-mode system prompt contains no "do the work yourself" instruction and
  explicitly references the PR/finish tools (golden test on prompt text).
- A build run with no conductor skill available surfaces an actionable "install
  the build adapter / skill" message instead of silently self-driving.

### PR 3 — Artifact-defined completion in `reconcile()`

**Closes C.**

- After nodes complete, `reconcile()` (`engine.go`) checks `AllLeavesHaveDraftPR`
  (or the ledger `PRURL`s) before printing ✓.
- New terminal states:
  - all nodes complete **and** leaves have draft PRs → `✓ Build complete: N
    nodes, M draft PRs`.
  - nodes complete, PRs missing → `Nodes complete but no draft PRs — run the
    finisher / 'spec do <id>'` (non-success exit semantics for CI).
- Reuse the existing gate verifier; do not duplicate logic.

**Acceptance:**
- Table test: ledger with all nodes complete but no `PRURL` → reconcile reports
  the "no draft PRs" state, not success.
- Ledger with PRs on all leaves → success message includes PR count.

### PR 4 — Disentangle conductor config from node-routing config

**Closes E.**

- Introduce an explicit config key for the **conductor adapter** skills, e.g.
  `integrations.agent.settings.orchestrator_skill` (or `build.conductor`), read
  in `buildEngineOptions`. This is what PR 1/PR 2 pass to the conductor.
- Keep `agent.skill` (`opts.SkillRefs`) strictly as the **node worker fallback**
  used inside `resolveSkills` — and document that it is only consulted when a
  node fails registry routing.
- Update `ai-squad-skills/scripts/configure-spec.sh` to write conductor skills
  to the new key, not `agent.skill`.
- Add a migration note + a one-line warning when conductor skill paths are
  detected in `agent.skill` (the old, cross-wired location).

**Acceptance:**
- A node that fails registry routing no longer inherits the conductor skills
  (unit test on `skillsForNode` fallback with the new config split).
- `configure-spec.sh --dry-run` produces the new key; re-running is idempotent.

### PR 5 — `spec build --check` pre-flight (the integration's front door)

**New UX; de-risks A–E for users.**

- A dry-run that, without launching any agent:
  - resolves and validates the DAG (waves, cycles, unknown edges),
  - resolves each node's `workDir` (workspace presence) and reports missing
    workspaces with the exact config key,
  - prints the routed skill(s) per node and **flags name collisions** across
    nodes,
  - reports the resolved conductor skill(s) and whether the agent advertises
    `MCP`/`Skills`/`Subagents`,
  - states the completion definition (nodes + leaf PRs).
- Implemented in `cmd/build.go` behind `--check`; pure functions reused from the
  engine so the check and the real run cannot diverge.

**Acceptance:**
- `spec build SPEC-XXX --check` exits non-zero on missing workspace / invalid
  DAG / skill collision and zero when the build is launchable.
- Output names every node's `repo:layer`, routed skill, and `workDir`.

### PR 6 — Tidy the adapter surface

**Closes G, H, J, K.**

- Remove `InvokeResult.StepSignalled` and the `scanForNodeComplete` scan, or
  repurpose the headless stream parse to drive the artifact check in PR 3
  (decide one; do not leave it computed-and-discarded). (G)
- Document worker-context guidance in the port doc: workers should be dispatched
  with a **lean context** (no MCP, no conductor skills); recommend a
  fresh/`delegate`-style worker over a forked one. spec-cli can't enforce this
  but the reference adapter should follow it. (H)
- `runTests` / `FailingTests`: either gate it behind single-repo builds or run
  per-node test commands during provisioning; at minimum document the
  single-repo limitation in the port doc. (J)
- Headless: document the supported headless model (sequential walk, or a
  conductor that fans out) and make `invokeHeadless` completion derive from the
  artifact check, not the discarded boolean. (K)

**Acceptance:**
- `grep StepSignalled` returns nothing (or the field has a live consumer).
- Port doc has a "Worker dispatch" section with the lean-context recommendation.

### PR 7 — Publish the port spec + conformance kit

**Makes "any build system can integrate" real.**

- `docs/INTEGRATION-PORT.md`: the single integration doc — capability handshake,
  DAG schema (`v1`), every tool's request/response shape and idempotency
  guarantee, the wave-walk algorithm, completion definition, and the worker
  dispatch contract. Lift the canonical tool table out of the skill files so the
  skills reference the port, not vice-versa.
- `docs/schemas/dag.v1.json` + a schema-validation test against `dagJSON`.
- A **conformance harness**: a tiny scripted "adapter" (no LLM) that drives a
  fixture spec end-to-end through the MCP tools (provision → complete → push →
  open_pr via the noop repo adapter) and asserts ledger + artifact state. This
  is the executable definition of the port and the regression guard for every
  future change.

**Acceptance:**
- The conformance adapter builds a fixture multi-node, multi-repo spec to "all
  leaves have draft PRs" using only the documented port — no skill files, no pi.
- CI runs the conformance harness; a breaking change to the DAG schema or tool
  contract fails it.

---

## 5. Sequencing & dependencies

```
PR1 (skill routing as data) ─┐
PR2 (prompts defer)          ├─► PR5 (--check)  ─► PR7 (port spec + conformance)
PR3 (artifact completion)    │
PR4 (config split) ──────────┘
PR6 (adapter tidy) ───────────────────────────────► PR7
```

- **PR 1–4** are the load-bearing seam fixes; ship first.
- **PR 5** depends on the resolution logic being factored out of the engine
  (shared by check + run) — do that factoring inside PR 1/PR 3.
- **PR 7** is the capstone: it can only be written once the contract it
  documents is the real one (post PR 1–6).

## 6. Backward compatibility & migration

- The DAG resource stays additive (`schemaVersion` added, nothing removed) → no
  break for existing MCP clients.
- `agent.skill` keeps working as the node fallback; the new conductor key is
  opt-in, with a warning when conductor paths sit in the old location. The
  `ai-squad-skills` installer is updated in lockstep (PR 4).
- Non-MCP / non-skill agents keep the consolidated-context fallback unchanged.

## 7. Risks

| Risk | Mitigation |
|------|------------|
| Prompt rewrites (PR 2) regress real builds | Golden tests on prompt text per capability; behind capability branches. |
| Conformance harness becomes a maintenance burden | Keep it LLM-free and fixture-driven; it doubles as the fastest build regression test. |
| Config split (PR 4) confuses existing users | Warning + migration note + installer update; old key still honoured for node fallback. |
| Third parties depend on undocumented DAG fields | Freeze `v1` schema; additive-only policy enforced by schema test. |

## 8. Definition of done for the whole effort

- A new agent build system can implement a working adapter from
  `docs/INTEGRATION-PORT.md` alone, verified by the conformance harness.
- No worker skill content reaches the conductor; cross-repo builds carry no
  skill name collisions.
- `spec build` reports success only when the pipeline artifact (stacked draft
  PRs on every leaf) exists, and never silently self-drives without a conductor.
- `spec build --check` is the documented first step for wiring up any harness.
