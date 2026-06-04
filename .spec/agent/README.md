# `.spec/agent/` — the agent skill & profile seam

This directory is the reserved, well-defined home for reproducible agent
behaviour. The build engine **discovers and injects** whatever lives here and
works unchanged when it is empty — so build/fix playbooks can be authored later
without touching engine code.

## Layout

```
.spec/agent/
  profile.yaml   # OPTIONAL: model, thinking, and skill refs (read leniently)
  skills/        # OPTIONAL: skill files/dirs (Agent Skills standard)
    .gitkeep     # ships empty; reserves the location
```

## Two skill roles

The build seam distinguishes **conductor** skills from **node-worker** skills
(see `docs/INTEGRATION-PORT.md`):

- **Conductor skills** drive the whole-DAG orchestration. For an MCP-capable
  agent they are resolved from the **start dir only**, in priority order:
  1. `integrations.agent.settings.conductor_skill` (comma/newline list),
  2. `integrations.agent.settings.skill` (fallback),
  3. `profile.yaml` `skill:` refs,
  4. any non-hidden entry under `.spec/agent/skills/`.
  They are passed to the agent as `--skill <path>` and are deliberately
  **start-dir scoped** so cross-repo skills can never collide in the conductor.
- **Node-worker skills** implement one node. They are routed per node by the
  repo's `.spec/agent/skills/registry.yaml` (repo/layer tags) and reach workers
  **only** through `spec_provision_node` (`skillPaths`) — never injected into the
  conductor. `integrations.agent.settings.skill` is the per-node fallback used
  when registry routing does not match.

Missing or empty → no skills are injected and the build proceeds normally.

## How injection works

- MCP + skill-capable agents (e.g. pi) receive the **conductor** skills as
  `--skill <path>`; node-worker skills travel via `spec_provision_node`.
- Non-MCP / non-skill agents (e.g. Claude Code in solo mode) get every resolved
  skill body folded into the assembled system prompt and `context.md`.

## profile.yaml (all fields optional)

```yaml
model: anthropic/claude-sonnet-4
thinking: medium
skill:
  build: .spec/agent/skills/spec-build
  fix: .spec/agent/skills/spec-fix
```

A missing or malformed profile is ignored gracefully — the engine falls back to
its defaults. Skills themselves follow the Agent Skills standard: a directory
containing `SKILL.md` (or a single `.md` file) with `name` + `description`
frontmatter.

> **Note:** the `spec-build` / `spec-fix` skill bodies are authored separately
> against this seam. This directory ships intentionally empty.
