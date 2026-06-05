# Plan — A BYO Spec Build Port: minimal kernel, pluggable routing & workflow, any harness

> **Goal:** `spec-cli` is a generic control plane. A spec handoff must work for
> **any** team that brings its own **skills**, **harness**, **model**, **routing
> model**, and **VCS/review workflow** — with **zero kernel change**. Your team's
> set (layer/modifier skills, hexagonal standards, stacked-draft-PR on GitHub) is
> *one configuration*, shipped as the default, not the product.
>
> This revises the earlier alignment plan, which correctly fixed your team's
> coupling but over-bound the product: it promoted one routing ontology and one
> PR workflow to product law. Here we demote both to **pluggable adapters behind
> stable ports**, leaving a tiny universal core.

## 0. Principles → architecture

This is a straight application of the repo's own rules (`AGENTS.md`: *"Engines
depend on interfaces, never concrete adapters … noop adapters exist for every
category"*) extended to the two missing seams.

| Principle | How the design honours it |
|---|---|
| **Single responsibility / separation of concerns** | Three layers: kernel (spec/DAG/ledger/git/MCP), routing (node→skills), workflow (VCS/review). Each owns one decision. |
| **Dependency inversion** | The engine depends on `SkillRouter` and `BuildStrategy` **interfaces**, never on the registry or the stacked-PR code. |
| **Open/closed** | New router / strategy / harness / repo provider = new adapter; the kernel is never edited to onboard a team. |
| **Interface segregation** | The MCP surface is grouped; a harness only sees the tools the active strategy provides (a `none` strategy exposes no `spec_open_pr`). |
| **Stable abstractions** | The most-depended-on contract (Tier 0) is the smallest and most stable; volatile policy lives at the adapter edges. |
| **Least assumption / robustness** | The mandatory contract assumes almost nothing; spec-format features degrade gracefully when absent. |
| **Single source of truth + testability** | Each schema has exactly one owner (spec-cli) and a **fixture-based** conformance suite — not coupled to any consumer repo. |

---

## 1. The three layers (ports & adapters)

```
                          ┌─────────────────────────────────────────┐
   harness (BYO)  ◀──MCP──│  Tier 0: KERNEL  (spec, DAG, ledger,     │
   pi/Claude/Cursor/CI    │  git primitives, MCP transport,         │
                          │  node context). Depends on interfaces.  │
                          └───────┬──────────────────────┬──────────┘
                                  │ SkillRouter           │ BuildStrategy
                                  │ (Tier 1)              │ (Tier 2)
                 ┌────────────────┴───────┐     ┌─────────┴───────────────────┐
                 ▼            ▼            ▼     ▼            ▼                 ▼
            registry      plan-ref     none/   stacked-     single-          none
            router        router       disc.   draft-pr     branch        (local)
            (DEFAULT)                          (DEFAULT,
                                                uses RepoAdapter: github/gitlab/…)
```

- **Tier 0 — Kernel.** Universal, BYO-safe, the only mandatory contract. Owns the
  spec, the DAG/ledger, the git **primitives** (branch, worktree, base ref, diff),
  the MCP transport, and deterministic node context. Names no team artifact and no
  workflow. Depends only on the two interfaces below.
- **Tier 1 — `SkillRouter` (pluggable, optional).** Maps a node to skill refs
  (+ optional gates). spec-cli ships a **registry router** (the `registry.v1`
  schema) as the default, plus a **plan-ref router** (skills named inline in the
  plan) and a **noop/discovery router** (returns nothing; the harness/model finds
  skills). Routing is *opaque to the kernel* — a node just carries `skillPaths`.
- **Tier 2 — `BuildStrategy` (pluggable).** Owns the VCS/review **workflow**: how a
  node is provisioned (branch/worktree/base) and finished (push, PR/MR), and the
  **completion definition**. Ships **stacked-draft-pr** (default; uses the existing
  `RepoAdapter`, so GitHub today, GitLab/others later), **single-branch**, and
  **none** (local-only). A strategy declares which finishing tools it exposes.

Harness and model remain BYO via the existing `AgentAdapter` + capability
negotiation; spec-cli is **model-agnostic** (model selection belongs to the
harness/skill, not the kernel).

---

## 2. Tier 0 — the universal contract (the only thing every harness must rely on)

**Resources**

| URI | Guarantee |
|---|---|
| `spec://current/full` | The approved spec. |
| `spec://current/dag` | `build-port/v1`. Mandatory node fields: `id`, `dependsOn[]`, `status`, `skillPaths[]`. Optional/additive: `repo`, `layer`, `qualityGates[]`, `acceptanceCriteria[]`, `branch`. Plus `strategy` + `router` metadata (see §3). |
| `spec://current/acceptance-criteria`, `…/conventions`, `…/prior-diffs` | Best-effort; may be empty. |
| `spec://current/capabilities` | **New.** Advertises the active router, strategy, and the **live tool groups** so a conductor discovers capabilities instead of assuming. |

**Tools (always present)**

`spec_provision_node` · `spec_node_complete` · `spec_node_failed` · `spec_decide` ·
`spec_node_context` · `spec_status`.

`spec_provision_node` returns `{ nodeId, workDir, branch?, baseRef?, skillPaths[], qualityGates[] }` — `branch`/`baseRef` are populated by the strategy (a `none`-VCS strategy may omit them).

**Completion** is reported by the kernel as *graph completion* (all nodes
complete) and, when the strategy defines an artifact, *artifact completion* is
delegated to the strategy (no hardcoded "leaves have PRs" in the kernel).

**`spec_node_context`** returns whatever spec sections exist (problem summary,
node text, AC subset, binding decisions, non-goals, gates) and **degrades
gracefully** — a spec without `§7.3`/`§6` yields a thinner slice, never an error.

This Tier-0 surface is what a BYO harness/skill author programs against. Tiers 1–2
are invisible to it except through the opaque `skillPaths`, `qualityGates`, and
the `capabilities` advertisement.

---

## 3. Tier 1 — `SkillRouter` (routing is a choice, not a mandate)

```go
// internal/adapter: interface; implementations in internal/adapter/<router>/.
type SkillRouter interface {
    // Route returns skill refs (+ gates) for a node, or empty when it can't/won't route.
    Route(node Node) (RouteResult, error)
}
```

- **`registry` (default).** Owns the **`registry.v1`** schema (published by
  spec-cli as `docs/schemas/registry.v1.json`, generated from the structs). This
  is where the earlier divergences are fixed — **inside the default router, not the
  kernel**:
  - `applies_to`: flat prefixed list `["<repo>", "layer:<tag>"]` (canonical).
  - discriminator: `kind: layer|modifier`.
  - skills + manifest under `.agents/skills/` (the cross-harness location);
    `path:` resolved **repo-root-relative**.
  - consume `version`, `kind`, `applies_to`, `quality_gates`, `precedence`;
    `requires`/`produces` advisory; frontmatter routing read from `metadata.*`.
- **`plan-ref`.** Skills named directly on the §7.3 node (`[repo:layer]{skill: foo}`)
  — for teams that don't want a registry.
- **`none` / discovery.** Returns no `skillPaths`; the harness/model discovers
  skills itself (e.g. pi's native `.agents/skills` scan). The default for a team
  that brings no routing model at all.

`build.router` selects it; absent → `registry` if a manifest exists, else `none`.
**A spec with no router still builds** (opaque/empty `skillPaths`).

---

## 4. Tier 2 — `BuildStrategy` (the workflow is a choice)

```go
type BuildStrategy interface {
    Provision(ctx, node, ledger) (Placement, error)   // branch/worktree/baseRef, or local
    Finish(ctx, node, ledger) (Artifact, error)       // push, PR/MR — or no-op
    Complete(ctx, session, graph) (Completion, error) // strategy-defined "done"
    Tools() []ToolSpec                                 // finishing tools to expose (segregation)
}
```

- **`stacked-draft-pr` (default).** Today's behaviour: worktree-per-node, base-ref
  stacking, `spec_push`/`spec_open_pr`/`spec_link_prs`, completion = every
  repo-bearing leaf has a draft PR. Uses the existing `RepoAdapter` (GitHub now;
  GitLab-MR/Gerrit are new `RepoAdapter`s, **not** strategy forks).
- **`single-branch`.** One branch per spec, commits accumulate, push, optional
  single PR; completion = branch pushed (+ optional PR).
- **`none`.** Local worktrees/commits only; exposes no finishing tools; completion =
  graph complete. For experimentation, air-gapped, or non-PR flows.

`build.strategy` selects it; default `stacked-draft-pr`. The strategy's `Tools()`
drive which `spec_*` finishing tools the MCP server registers and what
`spec://current/capabilities` advertises — so a conductor never calls a tool the
strategy doesn't provide.

---

## 5. Ownership

| Concern | Owner |
|---|---|
| Tier-0 kernel + the two port interfaces + `spec_node_context` | **spec-cli** |
| `registry.v1` schema + default routers + default strategies | **spec-cli** (shipped, replaceable) |
| Port/schema versioning, deprecation, **fixture** conformance suite | **spec-cli** |
| Skill **content**, standards, gate **commands** | **ai-squad-skills** (your set) |
| `registry.yaml` **instances** (validated vs published schema) | **ai-squad-skills** |
| Conductor playbooks + worker template | **ai-squad-skills** |
| A *different* team's router/strategy/skills/harness | **that team** (no spec-cli change) |

Rule: **spec-cli owns interfaces and ships default adapters; teams own
configuration + conformant adapters/instances.** Your team's repo is just the
first BYO consumer.

---

## 6. Workstreams

> **Delivery status (spec-cli).** WS-1 ✅, WS-2 ✅ (registry + none routers;
> `plan-ref` deferred), WS-3 ✅ (none strategy + capabilities + tool gating;
> `single-branch` deferred), WS-4 ✅, WS-5 ✅ (synthetic conformance proves BYO
> via the `none`/`none` stack; a `single-branch`/`plan-ref` second stack is the
> remaining nice-to-have). WS-6 (ai-squad-skills consumer migration) in progress.
> Deferred adapters need **zero kernel change** — they slot behind the shipped
> `SkillRouter`/`BuildStrategy` interfaces.

### WS-1 — Extract the two ports (the keystone, enables everything)
Define `SkillRouter` and `BuildStrategy` interfaces the engine depends on; move the
current registry routing behind `registry` and the current PR mechanics behind
`stacked-draft-pr`; wire selection via `build.router`/`build.strategy` with defaults
that **reproduce today's behaviour exactly**. Ship `noop` for each (per `AGENTS.md`).
- **Acceptance:** with default config, every existing build test passes unchanged;
  the engine imports the interfaces, never the concrete router/strategy.

### WS-2 — Make routing real *and* optional (fixes the divergences inside the default router)
Implement the `registry.v1` canonical shapes (D1–D6, D10 from the prior plan)
**inside the `registry` router**; add `plan-ref` and `none` routers; publish
`registry.v1.json`.
- **Acceptance:** a fixture spec with **no** router builds (empty `skillPaths`); the
  `registry` router resolves fixture registries to the expected skills+gates
  (the test that fails today); a `plan-ref` fixture routes from the node line.

### WS-3 — Make the workflow pluggable
Ship `single-branch` and `none` strategies; move completion + finishing-tool
exposure behind `BuildStrategy.Tools()`; add `spec://current/capabilities`.
- **Acceptance:** `build.strategy: none` produces worktrees + node completion,
  exposes **no** finishing tools, and reports graph-complete; `stacked-draft-pr`
  is unchanged.

### WS-4 — Kernel genericity + Tier-0 surface
Remove any team identifier from `internal/`/`cmd/` (the `build-orchestrator` prompt
mention; label template examples as illustrative). Add `spec_node_context` with
graceful degradation; surface `qualityGates`/`acceptanceCriteria` additively; parse
optional `(ac: …)`; `--check` validates router+strategy+coverage and prints the
active adapters. State model-agnosticism in `INTEGRATION-PORT.md`.
- **Acceptance:** `grep -ri 'build-orchestrator|hexagonal|rubocop|qlty|nexl' internal cmd`
  is empty outside labelled examples; `spec_node_context` works on a minimal spec.

### WS-5 — Fixture-based conformance + governance
A **synthetic** conformance suite (no consumer repo): port conformance (an
LLM-free adapter drives a fixture spec through Tier-0 + the default strategy),
router conformance (fixtures per router), strategy conformance (fixtures per
strategy). Semver each interface; document the deprecation policy and a one-release
compat shim for legacy registry shapes.
- **Acceptance:** CI proves a **second, fully different** stack — `plan-ref` router
  + `single-branch` strategy + a stub harness — completes a fixture build with zero
  kernel change. This is the BYO proof.

### WS-6 — `ai-squad-skills` becomes a conformant consumer
Normalise registries to `registry.v1`; move routing frontmatter under `metadata`;
ship a worker agent/template + defensive spawn note; `capability-contract/` becomes
a thin **reference** to spec-cli's published schema/port (no re-definition); the
nexl repo runs the **router conformance** test against the published schema in its
own CI.
- **Acceptance:** nexl's shipped registries pass spec-cli's published-schema
  validation; the build-orchestrator negotiates finishing tools via
  `spec://current/capabilities` rather than assuming PRs.

---

## 7. Migration & compatibility

- **Defaults reproduce today.** `router=registry`, `strategy=stacked-draft-pr` ⇒
  nexl sees no behavioural change.
- **DAG is additive** (`qualityGates`, `acceptanceCriteria`, `strategy`/`router`
  metadata, `capabilities` resource); `schemaVersion` stays `build-port/v1`.
- **Legacy registry shapes** (nested `applies_to`, `modifier:` bool) accepted for
  one release behind the `registry` router with a deprecation warning, then removed.
- `(ac: …)` optional; coverage check is a warning when absent.

## 8. Sequencing

```
WS-1 (ports) ─┬─► WS-2 (routers)  ─┐
              ├─► WS-3 (strategies)├─► WS-5 (fixture conformance + governance) ─► WS-6 (nexl conforms)
              └─► WS-4 (kernel + Tier-0) ─┘
```

WS-1 first — until the ports exist, nothing can be swapped. WS-2/3/4 are parallel
spec-cli adapters on top. WS-5 makes BYO provable and permanent. WS-6 migrates your
team to be the reference consumer.

## 9. Definition of done (BYO-true)

- The **mandatory** contract is Tier-0 only; routing and workflow are adapters
  behind interfaces with shipped defaults and noops.
- spec-cli's kernel names no team skill, standard, repo, layer, or workflow.
- A second stack (different router + different strategy + different harness/model)
  completes a build with **zero kernel change**, proven by a synthetic CI fixture.
- Each schema/interface has one owner (spec-cli), is versioned, and is validated by
  fixture-based conformance; `ai-squad-skills` is a conformant consumer, not a
  co-owner of the contract.
- Your team's set still works out of the box via defaults — BYO costs nothing to
  the team that brought the defaults.
