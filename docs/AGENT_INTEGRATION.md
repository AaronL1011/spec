# Agent build integration port

> Advanced reference for agent-harness authors. Most users start and resume
> builds with `b` in the TUI. Use `spec build --check` to diagnose setup.

This document defines the versioned MCP port behind `spec build`. A harness
that can speak MCP and dispatch work into a directory can drive a build without
depending on spec internals or bundled skills.

`spec` owns the dependency graph, durable node ledger, Git/worktree mechanics,
and GitHub calls. The harness supplies implementation judgment and worker
dispatch.

For user setup, see [Configuration](CONFIGURATION.md#coding-agent) and the
[TUI guide](TUI.md#actions-on-a-selected-spec).

## 1. Launch and capability negotiation

`spec build <id>` (or TUI `b`) launches the effective agent once for the whole
DAG. `spec do <id>` resumes durable state.

The agent adapter advertises these capabilities:

- **MCP** — `spec` writes an ephemeral MCP config pointing at
  `spec mcp-server --spec <id>` and frames the run as conductor traversal.
- **Skills** — `spec` passes conductor-level skills. Per-node worker skills
  travel through `spec_provision_node`, not the conductor launch.
- **Headless** — enables autonomous `spec fix --auto` and CI runs.
- **SystemPrompt** — allows `spec` to provide a thin base instruction; the
  orchestration playbook remains a skill.

Without MCP, `spec` uses a solo fallback: it writes one consolidated context
file containing the spec, conventions, prior diffs, and skill bodies. The
single agent implements the work directly. The conductor contract below
applies to MCP-capable agents.

Preflight without launching:

```bash
spec build <id> --check
```

This validates the DAG, workspaces, routed skills, skill-name collisions,
capabilities, and completion definition.

## 2. Data plane: resources

Read-only MCP resources:

### `spec://current/dag`

The versioned `build-port/v1` graph. Read it once to plan traversal. Schema:
`docs/schemas/dag.v1.json`.

```json
{
  "schemaVersion": "build-port/v1",
  "specId": "SPEC-042",
  "maxParallel": 4,
  "nodes": [
    {
      "id": "n1",
      "number": 1,
      "repo": "svc",
      "layer": "rails-api",
      "dependsOn": [],
      "status": "pending",
      "branch": "",
      "skillPaths": ["/abs/skill"]
    }
  ],
  "waves": [["n1"], ["n2", "n3"], ["n4"]]
}
```

Always check `schemaVersion`. Fields are additive within `build-port/v1`.

`waves` is ordered and contains node IDs. Resolve each ID against `nodes`.
Every node in a wave has dependencies satisfied by earlier waves, so the wave
is safe to fan out up to `maxParallel`.

A non-empty graph `error` means the build plan is not a valid DAG and must not
launch.

### Other resources

- `spec://current/full` — full approved spec.
- `spec://current/acceptance-criteria` — definition of done.
- `spec://current/conventions` — project conventions, when present.
- `spec://current/prior-diffs` — cumulative completed-node diffs.
- `spec://current/decisions` — decision log.
- `spec://current/capabilities` — active router, strategy, finishing tools,
  and completion semantics. Schema:
  `docs/schemas/capabilities.v1.json`.

## 3. Skill routing

`skillPaths` is opaque to the build kernel. It comes from the selected router:
`build.router` in team config or `agent.router` in personal config.

### `registry` (default)

Routes each node using `.agents/skills/registry.yaml` (legacy
`.spec/agent/skills/` is also recognized).

Canonical `registry/v1` entries use:

- `kind: layer|modifier`;
- flat `applies_to` values such as `service` or `layer:rails-api`;
- a repository-relative `path`;
- optional top-level `modifiers` and `conventions`.

Modifiers compose for matching repositories even if no layer skill matches.
Repository conventions such as `pr_title` remain mechanical and independent of
the active router.

### No skill router

Set the router to `none`. `skillPaths` is empty and the harness may discover
skills
itself.

Treat the paths supplied by the DAG as authoritative. Hand them to the worker;
do not infer which routing policy produced them.

Nodes may also include:

- `acceptanceCriteria` — the resolved criterion slice for the node;
- `qualityGates` — verification commands from the registry.

Both are advisory worker context.

## 4. Build strategy

The selected strategy (`build.strategy` or personal `agent.strategy`) defines
VCS/review mechanics and completion.

### `stacked-draft-pr` (default)

Each node gets a branch stacked on its parent and finishes as a draft PR.
Finishing tools are exposed. A build completes when every node is complete and
every repository-bearing stack leaf has a recorded draft PR.

### Local-only strategy

Set the strategy to `none`. Work remains on local branches. Finishing tools are
absent. The build completes
when all nodes complete.

Read `finishingTools` and `completion` from
`spec://current/capabilities`; never assume the default strategy.

## 5. Control plane: tools

Tools are idempotent and keyed by `node_id`. `spec` owns base-ref calculation.

### `spec_provision_node(node_id)`

Computes the base ref, creates the branch and worktree, sets the node to
in-progress, and returns:

```json
{
  "nodeId": "n1",
  "workDir": "/abs/worktree",
  "branch": "spec-042/n1",
  "baseRef": "main",
  "skillPaths": []
}
```

Provision in wave order. A same-repository child branches from its parent, so
the parent must be provisioned first.

### `spec_node_context(node_id)`

Returns the deterministic worker slice: description, dependencies, routed
skills, acceptance criteria, and quality gates. Give this to the worker instead
of asking it to reread the full spec.

### Node checkpoint tools

- `spec_node_complete(node_id)` — mark complete and capture the diff into
  cumulative context.
- `spec_node_failed(node_id, reason)` — persist failure. Do not dispatch
  downstream dependents.

### Finishing tools

Available only when listed by the active strategy:

- `spec_push(node_id)` — push the node branch from its worktree.
- `spec_open_pr(node_id, type?, summary?, title?, body?)` — open a draft PR
  from the node branch to its recorded base.
- `spec_link_prs()` — re-chain the whole stack.
- `spec_link_prs(node_id, base)` — retarget one node after parent merges.

For `spec_open_pr`, pass `type` and `summary` to apply the repository's
`pr_title` convention. Supported placeholders include `{type}`, `{epic}`, and
`{desc}`. Pass `title` to override the convention. The tool records the PR and
annotates the build plan.

### Decision tools

- `spec_decide(question)` — record a decision forced by implementation.
- `spec_decide_resolve(number, decision, rationale)` — resolve one.

## 6. Conductor loop

```text
1. Read spec://current/full and spec://current/dag.
2. Stop if the graph is empty or errored, a workspace is unresolved,
   a blocking decision remains, or work lacks a resolvable capability.
3. For each wave, in order:
   a. Provision each node serially.
   b. Dispatch one worker per worktree, in parallel up to maxParallel.
   c. Checkpoint success or failure with the node tools.
   d. Wait for the entire wave before starting dependents.
4. If the strategy exposes finishing tools, push and open draft PRs,
   roots first, then call spec_link_prs.
5. Report and stop at draft; humans mark ready and merge.
```

## 7. Worker dispatch contract

- **One writer per worktree.** `spec` already isolates each node. Never run two
  workers in one `workDir` and never add a second isolation layer.
- **Lean context.** A worker needs `workDir`, node context, and `skillPaths`.
  It does not need the MCP server or conductor skills.
- **Workers report; conductor checkpoints.** Workers do not push, open PRs,
  call control-plane tools, or spawn more workers.

Prefer a fresh lightweight worker over inheriting the conductor's complete
context.

## 8. Completion, resume, and failure

Completion is strategy-defined and advertised by capabilities.

Under `stacked-draft-pr`, all nodes and all repository-bearing leaves need draft
PRs. The `pr_stack_exists` compatibility gate enforces the same
`AllLeavesHaveDraftPR` definition.

Under `none`, all nodes complete is sufficient.

State is durable. Re-running `spec do <id>` leaves completed nodes alone and
dispatches only ready unfinished nodes. A failed node blocks its descendants,
not independent branches.

## 9. Versioning and conformance

- The DAG declares `build-port/v1`.
- Changes within a major version are additive.
- Tool names, idempotency, and advertised completion semantics are stable.
- Consumers must tolerate unknown optional fields.

The non-LLM conformance kit in `internal/build/conformance_test.go` exercises a
synthetic fixture under both the default stack and
`router=none` / `strategy=none`.

## 10. Adapter summary

- Skill routing: `build.router`; default `registry`; alternative `none`.
- Build strategy: `build.strategy`; default `stacked-draft-pr`; alternative
  `none`.
- Registry schema: `docs/schemas/registry.v1.json`.
- Capability schema: `docs/schemas/capabilities.v1.json`.

The spec/DAG/ledger/Git/MCP kernel is mandatory and contains no agent policy.
Routers and strategies are replaceable boundary adapters.
