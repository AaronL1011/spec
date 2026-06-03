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

## How discovery works

The engine resolves skills in priority order:

1. Explicit config: `integrations.agent.settings.skill` (comma/newline list).
2. `profile.yaml` `skill:` refs.
3. Any non-hidden entry under `.spec/agent/skills/`.

Missing or empty → no skills are injected and the build proceeds normally.

## How injection works

- Skill-capable agents (e.g. pi) receive each resolved path as `--skill <path>`.
- Non-skill agents (e.g. Claude Code) get the skill bodies folded into the
  assembled system prompt and the consolidated `context.md`.

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
