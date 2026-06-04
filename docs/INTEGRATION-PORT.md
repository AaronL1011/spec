# spec Build Integration Port

> The single document required to integrate any agent build system with
> `spec build`. If your harness speaks MCP and can dispatch a unit of work into
> a directory, it can drive a spec build using only what is below — no spec-cli
> source, no specific skill files.

`spec-cli` is the **control plane**: it owns the dependency graph, the durable
node ledger, all git/worktree mechanics, and the GitHub calls. Your build system
is the **conductor**: it reads the graph, dispatches workers, and checkpoints
progress through the port. spec-cli supplies *deterministic mechanism and data*;
it never designs and never injects capability content into your conductor.

## 1. Launch & capability negotiation

`spec build <id>` (or `spec do <id>` to resume) launches the configured agent
once for the whole DAG. What spec-cli emits depends on the capabilities the
agent adapter advertises:

| Capability | Effect |
|------------|--------|
| `MCP` | spec-cli writes an ephemeral MCP config pointing at `spec mcp-server --spec <id>` and frames the run as a **conductor** traversal of this port. **Recommended.** |
| `Skills` | spec-cli passes the *conductor-level* skills (start-dir scoped, from `conductor_skill` config or `.spec/agent/skills/` discovery). Per-node worker skills are **not** passed here — they travel via `spec_provision_node`. |
| `Headless` | enables `spec fix --auto` / CI autonomous runs. |
| `SystemPrompt` | spec-cli supplies a thin base instruction; the orchestration playbook is a skill. |

If `MCP` is false, spec-cli falls back to a **solo** model: it writes a
consolidated context file (full spec, conventions, prior diffs, all skill
bodies folded in) and the single agent implements the spec itself. The conductor
contract below applies to the `MCP` path.

Run `spec build <id> --check` to preflight without launching the agent: it
validates the DAG, resolves each node's workspace and routed skills, flags skill
name collisions, and prints the agent capabilities and completion definition.

## 2. Data plane — resources

Read these MCP resources (read-only):

| URI | Content |
|-----|---------|
| `spec://current/dag` | The build graph. **Versioned JSON**, schema `build-port/v1` (see `docs/schemas/dag.v1.json`). Read once to plan the walk. |
| `spec://current/full` | The full approved spec. |
| `spec://current/acceptance-criteria` | Definition of done. |
| `spec://current/conventions` | Project conventions (when present). |
| `spec://current/prior-diffs` | Cumulative diffs of completed nodes; pass relevant slices to downstream workers. |
| `spec://current/decisions` | Decision log. |

### The DAG document (`build-port/v1`)

```json
{
  "schemaVersion": "build-port/v1",
  "specId": "SPEC-042",
  "maxParallel": 4,
  "nodes": [
    { "id": "n1", "number": 1, "repo": "svc", "layer": "rails-api",
      "dependsOn": [], "status": "pending", "branch": "", "skillPaths": ["/abs/skill"] }
  ],
  "waves": [["n1"], ["n2", "n3"], ["n4"]]
}
```

- Always check `schemaVersion` (major `build-port/v1`). Fields are added only
  additively within a major version.
- `waves` is ordered; each wave is an array of node **ids** — resolve them
  against `nodes[]`. Every node in a wave has its dependencies satisfied by
  earlier waves, so a wave is safe to fan out up to `maxParallel`.
- A non-empty `error` field means the §7.3 PR stack is not a valid DAG; the
  build is not launchable.

## 3. Control plane — tools

All tools are **idempotent** and keyed by `node_id`. spec-cli owns the git base
ref, so you never compute or pass one.

| Tool | Contract |
|------|----------|
| `spec_provision_node(node_id)` | Computes the base ref, creates the branch + worktree, sets status → in-progress, returns `{ nodeId, workDir, branch, baseRef, skillPaths }`. Provision a node before dispatching its worker. **Provision in wave order** — a same-repo child branches off its parent, so the parent must be provisioned first. |
| `spec_node_complete(node_id)` | Marks the node done and captures its diff into cumulative context. |
| `spec_node_failed(node_id, reason)` | Records a failure for resume/reporting. Do not start downstream dependents of a failed node. |
| `spec_push(node_id)` | Pushes the node's branch from its worktree. |
| `spec_open_pr(node_id, title?, body?)` | Opens a **draft** PR (head = node branch, base = recorded base ref). Records `{number,url}` and annotates §7.3. |
| `spec_link_prs()` / `spec_link_prs(node_id, base)` | Re-chains the stack, or retargets one node's base as parents merge. |
| `spec_decide(question)` / `spec_decide_resolve(number, decision, rationale)` | Record/resolve decisions forced during the build. |

## 4. The conductor loop

```
1. Read spec://current/full and spec://current/dag.
2. Preflight: stop and report if the DAG is empty/errored, a node's workspace
   is unresolved, the Decision Log has blocking entries, or a node has no
   resolvable capability and its work is not self-evident.
3. For each WAVE in dag.waves, in order:
     For each NODE in the wave (provision serially, then dispatch in parallel
     up to maxParallel):
        a. spec_provision_node(node.id) -> { workDir, branch, baseRef, skillPaths }
     Then, for the provisioned nodes:
        b. Dispatch ONE worker, cwd = workDir, given the node prompt + skillPaths.
        c. success -> spec_node_complete(node.id);  failure -> spec_node_failed(node.id, reason)
     Wait for the whole wave before the next (downstream nodes depend on
     upstream diffs, now captured as cumulative context).
4. When all nodes are complete, push and open stacked DRAFT PRs:
   spec_push + spec_open_pr per node (roots first), then spec_link_prs.
5. Report. Stop at draft — humans mark ready and merge.
```

### Worker dispatch contract

- **One writer per worktree.** spec-cli isolates every node in its own worktree;
  never run two workers in the same `workDir`, and never double-isolate (the
  worktree is already isolated).
- **Lean worker context.** A worker needs its `workDir`, the node's spec slice,
  and its `skillPaths` (which it loads by reading the files). It does **not**
  need the MCP server or the conductor's skills. Prefer a fresh/lightweight
  worker over one that inherits the conductor's full context.
- **Workers report; the conductor checkpoints.** Workers never push, never open
  PRs, never call the node tools, and never spawn workers.

## 5. Completion semantics

A build is **complete** only when every node is `complete` **and** every
repo-bearing stack *leaf* (a node nothing else depends on) has a recorded draft
PR. spec-cli reports this distinction directly: nodes-complete-but-no-PRs is
reported as unfinished with a pointer to run the finisher. The `pr_stack_exists`
advance gate enforces the same definition (`AllLeavesHaveDraftPR`).

## 6. Resume & failure

- State is durable. Re-invoking (`spec do <id>`) resumes from the ledger:
  completed nodes are already marked, only ready unfinished nodes are
  re-dispatched.
- A failed node blocks its downstream dependents but not independent DAG
  branches.

## 7. Versioning & conformance

- The DAG resource carries `schemaVersion` (`build-port/v1`); within a major
  version, changes are additive only.
- The tool names, idempotency guarantees, and the completion definition above
  are the stable contract. Adapters should target the major version and treat
  unknown additive fields as optional.

## 8. Reference adapters

- **`ai-squad-skills`** (`build-orchestrator` + `pr-finisher` skills) is the
  reference conductor for pi and Claude Code.
- A non-LLM conformance adapter (driving a fixture spec through the tools)
  exercises this contract end-to-end and is the regression guard for changes to
  the port.
