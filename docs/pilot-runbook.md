# spec Pilot Runbook (Team of 4)

This runbook is for a small-team pilot of `spec` with one tech lead and three contributors.
It is designed to validate workflow value quickly while keeping setup and ceremony light.

## Pilot Scope

Use this pilot scope for the first two weeks:

- In scope: `config`, `new`, `list`, `status`, `advance`, `decide`, `plan`, `steps`, `pull`, `build`, `do`, `sync`, dashboard (`spec` with no args)
- Optional in scope: `intake`, `promote`, `draft`, `watch`, `retro`, `metrics`
- Out of scope for pilot: `deploy`, `context` (can be enabled later)

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

### 4) Sync integration setup

For a Jira + Confluence + Microsoft Teams + GitHub pilot, use environment variables for secrets:

```bash
export GITHUB_TOKEN="..."
export JIRA_BASE_URL="https://your-domain.atlassian.net"
export JIRA_EMAIL="you@example.com"
export JIRA_API_TOKEN="..."
export CONFLUENCE_BASE_URL="https://your-domain.atlassian.net/wiki"
export CONFLUENCE_EMAIL="you@example.com"
export CONFLUENCE_API_TOKEN="..."
export TEAMS_WEBHOOK_URL="https://..."
```

Optional Teams mention ingestion requires Graph access:

```bash
export TEAMS_GRAPH_TOKEN="..."
export TEAMS_TEAM_ID="..."
export TEAMS_CHANNEL_ID="..."
```

Update the `integrations` and `sync` sections in `spec.config.yaml`:

```yaml
integrations:
  comms:
    provider: teams
    webhook_url: ${TEAMS_WEBHOOK_URL}
    standup_webhook_url: ${TEAMS_STANDUP_WEBHOOK_URL} # optional
    graph_token: ${TEAMS_GRAPH_TOKEN}                 # optional
    team_id: ${TEAMS_TEAM_ID}                         # optional
    channel_id: ${TEAMS_CHANNEL_ID}                   # optional

  pm:
    provider: jira
    base_url: ${JIRA_BASE_URL}
    project_key: PLAT
    email: ${JIRA_EMAIL}
    token: ${JIRA_API_TOKEN}

  docs:
    provider: confluence
    base_url: ${CONFLUENCE_BASE_URL}
    space_key: ENG
    email: ${CONFLUENCE_EMAIL}
    token: ${CONFLUENCE_API_TOKEN}

  repo:
    provider: github
    owner: your-org
    token: ${GITHUB_TOKEN}

  deploy:
    provider: none

sync:
  outbound_on_advance: true
  conflict_strategy: warn # warn | abort | force | skip
```

Permission checklist:

- Jira: account can create issues, browse issues, and transition issues in `project_key`.
- Confluence: account can create, read, and edit pages in `space_key`. `base_url` must include `/wiki`.
- Teams: webhook can post to the pilot channel; messages must stay under 28 KB.
- Teams mentions: optional Graph token can read channel messages for `team_id` and `channel_id`.
- GitHub: token can read repo metadata and read/write pull requests for pilot repos.

Sync operating policy:

- Treat the specs repo as the source of truth. Confluence is the readable external workspace.
- Keep Confluence pages structured by `spec`; avoid manual page restructuring during the pilot.
- Start with `conflict_strategy: warn`; use `--force` only when intentionally accepting remote Confluence content.
- Each user must have `user.owner_role` configured before inbound sync; `spec sync` enforces section ownership.

### 5) Dry-run smoke test

Run once as TL:

```bash
spec new --title "Pilot smoke test"
spec list --all
spec status SPEC-001
spec advance SPEC-001 --dry-run
```

If sync is enabled, run one Confluence smoke test before inviting the full team:

```bash
spec sync SPEC-001 --direction out --dry-run
spec sync SPEC-001 --direction out
```

Then edit one TL-owned section in Confluence and verify inbound behavior:

```bash
spec sync SPEC-001 --direction in --dry-run
spec sync SPEC-001 --direction in
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

