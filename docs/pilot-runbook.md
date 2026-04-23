# spec Pilot Runbook (Team of 4)

This runbook is for a small-team pilot of `spec` with one tech lead and three contributors.
It is designed to validate workflow value quickly while keeping setup and ceremony light.

## Pilot Scope

Use this pilot scope for the first two weeks:

- In scope: `config`, `new`, `list`, `status`, `advance`, `decide`, `plan`, `steps`, `pull`, `build`, `do`, dashboard (`spec` with no args)
- Optional in scope: `intake`, `promote`, `draft`
- Out of scope for pilot: `sync`, `deploy`, `watch`, `retro`, `metrics`, `context` (can be enabled later)

This keeps the pilot focused on the core lifecycle and execution loop.

## Team Roles

Example role split for a team of 4:

- `tl`: tech lead (pilot owner, stage transitions, policy owner)
- `pm`: product owner or proxy (can be the TL if needed)
- `engineer`: two contributors

If one person plays multiple roles, use explicit role overrides only when needed:

```bash
spec --role tl list
```

## Day-0 Setup (60-90 minutes)

### 1) Install and verify

```bash
spec version
spec --help
```

### 2) Per-user identity setup

Each team member runs:

```bash
spec config init --user
spec whoami
```

### 3) Team config setup

In the specs repo:

```bash
spec config init --preset startup
spec pipeline validate
```

Use a minimal, pilot-friendly team config profile:

```yaml
version: "1"

team:
  name: "Pilot Team"
  cycle_label: "Pilot Sprint"

specs_repo:
  provider: github
  owner: your-org
  repo: your-specs-repo
  branch: main
  token: ${GITHUB_TOKEN}

integrations:
  comms:
    provider: none
  pm:
    provider: none
  docs:
    provider: none
  repo:
    provider: github
  agent:
    provider: cursor
  ai:
    provider: none
  deploy:
    provider: none

pipeline:
  preset: startup
  skip: [design]
```

### 4) Dry-run smoke test

Run once as TL:

```bash
spec new --title "Pilot smoke test"
spec list --all
spec status SPEC-001
spec advance SPEC-001 --dry-run
```

## Working Agreement (Pilot Policy)

- Use `spec` as the first command each morning.
- Keep all active work represented as specs or triage items.
- Do not use `--role` for normal work. Restrict it to TL/admin checks.
- Use short, explicit decision logs with `spec decide` for notable trade-offs.
- Prefer small plans and small steps for easier pilot signal collection.

## Daily Operating Rhythm

### Individual contributor flow

```bash
spec
spec list --mine
spec pull SPEC-0XX
spec plan add SPEC-0XX "Implement API validation"
spec steps start SPEC-0XX
spec do SPEC-0XX
spec steps complete SPEC-0XX --pr 123
```

### TL flow

```bash
spec
spec list --all
spec status SPEC-0XX
spec validate SPEC-0XX
spec advance SPEC-0XX
```

## Weekly Cadence (15 minutes)

Run once per week, led by TL:

1. Review active specs and blocked items.
2. Check if work is actually flowing through `spec` instead of side channels.
3. Capture top 3 friction points (command UX, stage model, missing affordances).
4. Decide one small adjustment for next week (pipeline, conventions, or team policy).

## Success Criteria (2-Week Pilot)

Target thresholds:

- At least 80% of active work tracked through `spec`
- Median intake-to-build-start time reduced or unchanged with better clarity
- Fewer "who owns this now?" handoff questions
- Team reports net positive workflow value (3 out of 4 members)
- No critical workflow breakages (data loss, blocked transitions with no workaround)

## Failure Signals

Treat these as intervention triggers:

- Team stops using `spec` after initial setup
- Stages feel too heavy for real work cadence
- Frequent ambiguity about current status or owner
- Spec content drifts from implementation reality

If any trigger appears, simplify immediately:

- Reduce stage count
- Remove non-essential gates
- Use only core commands for one week

## Rollback / Safe Exit

Pilot rollback is low risk because specs are markdown in git.

If you pause the pilot:

1. Keep the specs repo as source of truth.
2. Continue manual execution in existing tools.
3. Preserve `SPEC.md` files and decision logs for continuity.
4. Disable optional integrations by setting providers to `none`.

## Pilot Closeout Template

At the end of week 2, record:

- What improved
- What stayed neutral
- What regressed
- Which commands became daily habits
- Which commands were confusing or unnecessary
- Go/No-go for broader rollout

Recommended closeout decision:

- **Go**: adopt with current scope and iterate
- **Go with conditions**: adopt after 1-2 targeted fixes
- **No-go**: pause and revisit with reduced process footprint

