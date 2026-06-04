# spec Configuration Reference

spec uses two config files.

| File | Scope | Committed? |
|---|---|---|
| `spec.config.yaml` (specs repo root) | Team settings, integrations, pipeline | ✅ Yes |
| `~/.spec/config.yaml` | Personal identity and preferences | ❌ No |

**Quick start**

```sh
# Set up team config (run once per specs repo)
spec config init

# Set up your personal identity (run once per machine)
spec config init --user

# Verify all integrations are reachable
spec config test
```

---

## How config is resolved

spec walks the following chain on every command, stopping at the first match:

1. `spec.config.yaml` in the current directory
2. `spec.config.yaml` in any parent directory (repo-root search)
3. `spec.config.yaml` inside `~/.spec/repos/<owner>/<repo>/` (specs repo clone)
4. `~/.spec/config.yaml` is always loaded regardless of step 1–3

> **Note:** spec always reads and writes specs through the managed clone at
> `~/.spec/repos/<owner>/<repo>/specs/`, even when `spec.config.yaml` was found
> in your own checkout.

### Joining an existing team

```sh
spec join acme/specs                   # GitHub (default)
spec join github.com/acme/specs
spec join gitlab.com/acme/specs
spec join https://github.com/acme/specs

# Flags
--branch <name>   Branch to clone (default: main)
--token  <token>  Access token (default: $SPEC_GITHUB_TOKEN)
```

---

## Environment variable interpolation

Any value in either config file can reference an environment variable using `${VAR}` syntax.
The variable is substituted at load time; the raw `${VAR}` string is left in place if the variable is unset.

```yaml
specs_repo:
  token: ${SPEC_GITHUB_TOKEN}
```

**Token variable aliasing.** spec supports both the `SPEC_`-prefixed style and the legacy bare style for
`*_TOKEN` variables. If `SPEC_GITHUB_TOKEN` is unset, spec falls back to `GITHUB_TOKEN` with a
deprecation warning (and vice-versa). Prefer the `SPEC_`-prefixed form going forward.

| Preferred | Legacy (deprecated) |
|---|---|
| `SPEC_GITHUB_TOKEN` | `GITHUB_TOKEN` |
| `SPEC_GITLAB_TOKEN` | `GITLAB_TOKEN` |
| `SPEC_BITBUCKET_TOKEN` | `BITBUCKET_TOKEN` |

---

## Team config — `spec.config.yaml`

Committed to the root of your specs repo. Defines everything shared across the team.

```yaml
version: "1"
```

`version` is required. Currently `"1"`.

---

### `team`

```yaml
team:
  name: "Platform Team"       # Display name shown in the dashboard and notifications
  cycle_label: "Cycle 7"      # Stamped on every new spec's frontmatter
```

---

### `specs_repo`

Tells spec where the canonical specs repository lives.

```yaml
specs_repo:
  provider: github             # github | gitlab | bitbucket
  owner: my-org                # GitHub org or user
  repo: specs                  # Repository name
  branch: main                 # Branch to read/write (default: main)
  token: ${SPEC_GITHUB_TOKEN}  # Access token — use an env var
```

| Field | Required | Default | Description |
|---|---|---|---|
| `provider` | Yes | — | `github`, `gitlab`, or `bitbucket` |
| `owner` | Yes | — | Org or user that owns the repo |
| `repo` | Yes | — | Repository name |
| `branch` | No | `main` | Branch spec reads from and writes to |
| `token` | Yes | — | PAT or app token with repo read/write access |

---

### `integrations`

All integrations are optional. Unconfigured providers use a no-op adapter that
silently returns empty results — no panic, no crash.

#### `integrations.comms`

Sends notifications and standup posts.

```yaml
integrations:
  comms:
    provider: slack             # slack | teams | none

    # --- Slack ---
    token: ${SPEC_SLACK_TOKEN}
    default_channel: "#platform"
    standup_channel: "#platform-standup"

    # --- Microsoft Teams ---
    webhook_url: ${TEAMS_WEBHOOK_URL}
    standup_webhook_url: ${TEAMS_STANDUP_WEBHOOK_URL}
    # Optional: Graph API for mention sync (all three required together)
    graph_token: ${TEAMS_GRAPH_TOKEN}
    team_id: "abc123"
    channel_id: "xyz456"
```

| Provider | Required fields | Optional fields |
|---|---|---|
| `slack` | `token` | `default_channel`, `standup_channel` |
| `teams` | `webhook_url` | `standup_webhook_url`, `graph_token` + `team_id` + `channel_id` |

> **Teams mention sync:** `graph_token`, `team_id`, and `channel_id` must all be
> set or all be absent. Providing only some of them enables outbound webhooks but
> logs a warning about incomplete mention sync configuration.

#### `integrations.pm`

Connects spec to your project-management tool.

```yaml
integrations:
  pm:
    provider: jira              # jira | none  (linear, github-issues: coming soon)
    base_url: ${JIRA_BASE_URL}  # e.g. https://myorg.atlassian.net
    project_key: PLAT
    email: ${JIRA_EMAIL}
    token: ${JIRA_API_TOKEN}
```

| Provider | Required fields |
|---|---|
| `jira` | `base_url`, `project_key`, `email`, `token` |

#### `integrations.docs`

Syncs spec sections to/from a documentation platform.

```yaml
integrations:
  docs:
    provider: confluence        # confluence | none  (notion: coming soon)
    base_url: ${CONFLUENCE_BASE_URL}
    space_key: PLAT
    email: ${CONFLUENCE_EMAIL}
    token: ${CONFLUENCE_API_TOKEN}
```

| Provider | Required fields |
|---|---|
| `confluence` | `base_url`, `space_key`, `email`, `token` |

#### `integrations.repo`

Aggregates pull requests and CI status.

```yaml
integrations:
  repo:
    provider: github            # github | none  (gitlab, bitbucket: coming soon)
    token: ${SPEC_GITHUB_TOKEN} # Falls back to specs_repo.token if omitted
    owner: my-org               # Falls back to specs_repo.owner if omitted
```

#### `integrations.agent`

The coding agent used by `spec build` / `spec do`.

```yaml
integrations:
  agent:
    provider: claude            # claude | pi | none
    # Claude Code: no extra fields required — uses the claude CLI in PATH
    # pi: no extra fields required — uses the pi CLI in PATH
```

#### `integrations.ai`

AI service for `spec draft` and AI-assisted commands.

```yaml
integrations:
  ai:
    provider: anthropic         # anthropic | ollama | none  (openai: coming soon)

    # --- Anthropic ---
    token: ${ANTHROPIC_API_KEY}
    model: claude-opus-4-5      # optional; defaults to latest

    # --- Ollama (local) ---
    model: llama3
    base_url: http://localhost:11434  # optional; default shown
```

#### `integrations.deploy`

Triggers deployments from `spec deploy`.

```yaml
integrations:
  deploy:
    provider: github-actions    # github-actions | none  (gitlab-ci, argocd: coming soon)
    environments:
      - name: staging
        auto: true              # deploy automatically on stage entry
      - name: production
        auto: false
        gate: prs_approved      # gate name that must pass before deploy
```

#### `integrations.intake`

Automatically creates triage items from external sources.

```yaml
integrations:
  intake:
    sources:
      - provider: slack         # slack channel → triage
        channel: "#incidents"
        filter: "P0 OR P1"      # optional message filter
        auto_create: true
        token: ${SPEC_SLACK_TOKEN}

      - provider: jira          # Jira issue → triage
        trigger: "label:needs-spec"
        auto_create: false
```

| Field | Description |
|---|---|
| `provider` | Source type (`slack`, `jira`) |
| `auto_create` | When `true`, triage items are created without prompting |
| `channel` | Slack channel to watch |
| `filter` | Text/query filter applied to incoming items |
| `trigger` | Provider-specific trigger expression |
| `token` | Override token for this source |

---

### `sync`

Controls how spec sections stay in sync with the docs provider.

```yaml
sync:
  outbound_on_advance: true   # Push to docs platform whenever a spec advances (default: false)
  conflict_strategy: warn     # warn | overwrite | skip (default: warn)
```

| `conflict_strategy` | Behaviour |
|---|---|
| `warn` | Print a warning and leave the local version (default) |
| `overwrite` | Remote wins; local changes are discarded |
| `skip` | Silently skip conflicting sections |

---

### `archive`

```yaml
archive:
  directory: archive          # Path inside specs repo for archived specs (default: archive)
```

---

### `dashboard`

```yaml
dashboard:
  stale_threshold: 48h        # Age after which a spec is marked stale (default: 48h)
  refresh_ttl: 300            # Cache TTL in seconds (default: 300)
```

---

### `pipeline`

Defines the lifecycle every spec moves through. See [Pipeline configuration](#pipeline-configuration) below for the full reference.

---

### `fast_track`

Enables `spec fix` — a shortened self-service pipeline for small bug fixes.

```yaml
fast_track:
  enabled: true
  allowed_roles: [engineer, tl]   # default: [engineer, tl]
  max_duration: 48h               # escalate to TL/PM after this duration
  require_labels: [bug, hotfix]   # spec must carry these labels
```

| Field | Default | Description |
|---|---|---|
| `enabled` | `false` | Enable fast-track mode |
| `allowed_roles` | `[engineer, tl]` | Roles that can create fast-track specs |
| `max_duration` | — | Duration string (`2d`, `48h`) before escalation fires |
| `require_labels` | — | Labels that must appear on the spec |

---

## Pipeline configuration

`pipeline` defines the stages, gates, and automation every spec moves through.

### Presets

Start from a built-in preset instead of writing stages from scratch:

```yaml
pipeline:
  preset: product           # product (default when none configured) | minimal
  skip: [design]            # optionally drop stages from the preset
```

| Preset | Stages |
|---|---|
| `product` | `triage → draft → tl-review → design → qa-expectations → engineering → build → pr-review → qa-validation → done → deploying* → monitoring* → closed*` |
| `minimal` | Lean set without design/QA ceremony stages |

`*` optional stages (can be skipped without error)

### Full custom pipeline

```yaml
pipeline:
  stages:
    - name: draft
      owner: pm
      icon: 📝
      gates:
        - section_not_empty: problem_statement
```

### Stage fields

```yaml
- name: engineering           # Required. Lowercase, underscores allowed.
  owner: engineer             # Role (or list of roles) that own this stage.
  icon: 🔧                   # Emoji shown in pipeline views.
  optional: false             # true = skippable without error.
  skip_when: "label == 'bug'" # Expression; when true, stage is auto-skipped on advance.
  gates: []                   # Conditions that must pass before advancing. See Gates.
  warnings: []                # Time-based alerts. See Warnings.
  transitions:                # Custom advance/revert behaviour. See Transitions.
    advance: {}
    revert: {}
  on_enter: []                # Effects fired when entering this stage. See Effects.
  on_exit: []                 # Effects fired when leaving this stage. See Effects.
  auto_archive: false         # true = move spec to archive/ when entering.
  review: {}                  # Technical plan review requirement. See Stage review.
```

**`owner`** accepts a single role string or an array:

```yaml
owner: pm
owner: [pm, tl]
```

Valid roles: `pm`, `tl`, `designer`, `qa`, `engineer`

### Gates

Gates block `spec advance` until all conditions are satisfied.

```yaml
gates:
  - section_not_empty: problem_statement   # Named spec section must have content
  - steps_exists: true                     # Build plan must have ≥1 step
  - prs_approved: true                     # All PRs in the build plan are approved
  - review_approved: true                  # Technical plan review is approved
  - duration: 24h                          # Must wait this long after entering the stage
  - link_exists: pr                        # A link of type "pr" must exist in the spec
  - link_exists:
      section: design
      type: figma                          # A Figma link must exist in the design section
  - expr: "label == 'shipped'"             # Arbitrary boolean expression
    message: "Spec must be labelled 'shipped' before closing."
```

**Logical operators** — compose gates with `all`, `any`, `not`:

```yaml
gates:
  - all:
      - section_not_empty: problem_statement
      - section_not_empty: acceptance_criteria
  - any:
      - prs_approved: true
      - expr: "fast_track == true"
  - not:
      section_not_empty: design_inputs   # advance allowed only when design is empty
```

| Gate | Type | Description |
|---|---|---|
| `section_not_empty` | string | Section slug must contain non-whitespace content |
| `steps_exists` | bool | Build plan has ≥1 step (`true`) |
| `prs_approved` | bool | All PRs in the build plan are approved |
| `review_approved` | bool | Technical plan review is approved |
| `duration` | duration string | Minimum time in the current stage (`48h`, `5d`) |
| `link_exists` | string or object | A link (optionally of a specific type) exists |
| `expr` | string | Boolean expression evaluated against spec metadata |
| `all` | array of gates | All nested gates must pass |
| `any` | array of gates | At least one nested gate must pass |
| `not` | gate | Nested gate must fail |

> `section_complete` and `pr_stack_exists` are accepted for backward compatibility and map to `section_not_empty` and `steps_exists` respectively.

### Warnings

Non-blocking time-based alerts surfaced in the dashboard and passive awareness line.

```yaml
warnings:
  - after: 5d
    message: "Draft is stale — needs PM attention."
    notify: tl                # optional: notify a role or channel
  - after: 48h
    message: "Engineering spec is blocking the build."
    notify: "#platform"
```

### Transitions

Override the default advance and revert target stages, add extra gates, require
reason fields, or fire side effects on a specific transition.

```yaml
transitions:
  advance:
    to: [qa-validation]        # Restrict where advance can go
    gates:
      - prs_approved: true
    require: []                # e.g. ["reason"] to mandate a --reason flag
    effects:
      - notify: next_owner
  revert:
    require: [reason]          # Force --reason on revert
    effects:
      - notify: tl
      - update_pm:
          status: In Progress
```

### Effects

Effects are side effects fired on `on_enter`, `on_exit`, or a transition.

```yaml
effects:
  - notify: next_owner              # string shorthand: notify the next stage owner
  - notify: tl
  - notify: "#platform-channel"

  - notify:                         # object form for multiple targets or template
      targets: [next_owner, tl]
      channel: "#platform"
      template: spec-advanced

  - sync: outbound                  # outbound | inbound — trigger a docs sync
  - sync: inbound

  - update_pm:
      status: "In Review"           # Push status to Jira/Linear

  - log_decision: "Spec advanced to review"  # Append to decision log

  - increment: revert_count         # Increment a frontmatter counter

  - archive: true                   # Move spec to archive/

  - trigger: post-deploy-checks     # Invoke a named workflow or action

  - webhook:
      url: https://hooks.example.com/spec-event
      method: POST                  # default: POST
      headers:
        Authorization: "Bearer ${WEBHOOK_TOKEN}"
      body:
        event: advanced
      timeout: 10s                  # default: 10s

  - when: "label == 'hotfix'"       # Conditional — effect only fires when true
    notify: "#incidents"
```

### Stage review

Require an explicit approval before a stage can be advanced. Used primarily for
the `engineering` stage to enforce technical plan sign-off.

```yaml
- name: engineering
  owner: engineer
  review:
    required_approvers: [tl]      # Roles that must approve
    min_approvals: 1
```

### Conditional stage skipping

Individual stages can be skipped per-spec by attaching a `skip_when` expression.
When the expression evaluates true for a spec, that stage is skipped on advance
(the skip is recorded in the decision log).

```yaml
pipeline:
  preset: product
  stages:
    - name: design
      owner: designer
      skip_when: "'urgent' in spec.labels"   # skip design for urgent work
```

---

## User config — `~/.spec/config.yaml`

Personal identity and preferences. Never committed. Created by `spec config init --user`.

### `user`

```yaml
user:
  owner_role: engineer    # Your role: pm | tl | designer | qa | engineer
  name: "Ada Lovelace"    # Display name
  handle: "ada"          # @mention handle
```

`owner_role` drives all role-aware commands (`spec list`, dashboard queue, passive awareness).
Missing `owner_role` prints a setup prompt on any role-aware command.

### `preferences`

```yaml
preferences:
  editor: code                         # Editor for spec edit (default: $EDITOR, then vi)
  dashboard_sections: [do, review, incoming, blocked]  # Sections shown in dashboard
  standup_auto_post: false             # Auto-post standup without confirmation
  ai_drafts: true                      # Enable AI-assisted drafting (default: true when AI configured)
  theme: auto                          # TUI colour theme: auto | catppuccin | ... (default: auto)
  auto_navigate: true                  # Navigate to spec in TUI after create/advance

  passive_awareness:
    show: [review_requests, blocked]   # Whitelist item types (empty = show all)
    hide: [fyi]                        # Blacklist item types
    during_build: false                # Show awareness line while spec build is running
```

**Dashboard sections** — valid values: `do`, `review`, `incoming`, `blocked`, `fyi`

**Passive awareness item types** — valid values: `review_requests`, `spec_owned`, `mentions`, `triage`, `fyi`, `blocked`

### `agent` (user override)

Override the team's configured coding agent with your own preference.

```yaml
agent:
  provider: pi              # claude | pi | none
  conductor_skill: ~/.agents/skills/build-orchestrator,~/.agents/skills/pr-finisher
  skill: ~/skills/spec-build   # per-node worker fallback (see below)
```

This takes precedence over `integrations.agent` in `spec.config.yaml`.

The build seam distinguishes two skill roles (see `docs/INTEGRATION-PORT.md`):

- `conductor_skill` — orchestrator-level skills handed to an MCP-capable agent
  to drive the whole-DAG build. Start-dir scoped, so cross-repo skills cannot
  collide in the conductor.
- `skill` — the per-node worker fallback, used only when a node does not match
  the repo's `.spec/agent/skills/registry.yaml`. Routed node-worker skills reach
  workers via `spec_provision_node`, never the conductor.

### `workspaces`

Map specs repo names to local filesystem paths for cross-repo navigation in multi-repo build plans.

```yaml
workspaces:
  auth-service: ~/code/auth-service
  api-gateway: ~/code/api-gateway
  frontend: ~/code/frontend
```

---

## Management commands

| Command | Description |
|---|---|
| `spec config init` | Interactive wizard to create/update `spec.config.yaml` |
| `spec config init --user` | Interactive wizard to create/update `~/.spec/config.yaml` |
| `spec config test` | Ping every configured integration and report pass/fail |
| `spec whoami` | Show resolved identity: role, name, handle, config file paths |
| `spec join <repo>` | Clone a specs repo and bootstrap local config |

### `spec whoami` output

```
Role:        engineer
Name:        Ada Lovelace
Handle:      ada
User config: ~/.spec/config.yaml
Team config: ~/code/specs/spec.config.yaml
Specs repo:  ~/.spec/repos/acme/specs/specs/
```

If `owner_role` is missing:

```
No role configured. Run 'spec config init --user' to set up your identity.
```

### `spec config test` output

Tests each configured integration in sequence and prints a pass/fail line:

```
Configuration test results:
  ✓  comms      (slack)
  ✓  pm         (jira)
  ✗  docs       (confluence) — connection refused: https://myorg.atlassian.net
  ✓  repo       (github)
  –  agent      not configured
  ✓  ai         (anthropic)
```

---

## Minimal examples

### Minimal team config — no integrations

```yaml
version: "1"

team:
  name: "My Team"

specs_repo:
  provider: github
  owner: my-org
  repo: specs
  token: ${SPEC_GITHUB_TOKEN}
```

### Full team config — Atlassian + GitHub + Slack

```yaml
version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 7"

specs_repo:
  provider: github
  owner: my-org
  repo: specs
  branch: main
  token: ${SPEC_GITHUB_TOKEN}

integrations:
  comms:
    provider: slack
    token: ${SPEC_SLACK_TOKEN}
    default_channel: "#platform"
    standup_channel: "#platform-standup"

  pm:
    provider: jira
    base_url: ${JIRA_BASE_URL}
    project_key: PLAT
    email: ${JIRA_EMAIL}
    token: ${JIRA_API_TOKEN}

  docs:
    provider: confluence
    base_url: ${CONFLUENCE_BASE_URL}
    space_key: PLAT
    email: ${CONFLUENCE_EMAIL}
    token: ${CONFLUENCE_API_TOKEN}

  repo:
    provider: github
    token: ${SPEC_GITHUB_TOKEN}
    owner: my-org

  agent:
    provider: claude

  ai:
    provider: anthropic
    token: ${ANTHROPIC_API_KEY}

sync:
  outbound_on_advance: true
  conflict_strategy: warn

dashboard:
  stale_threshold: 48h
  refresh_ttl: 300

pipeline:
  preset: product
  skip: []

fast_track:
  enabled: true
  allowed_roles: [engineer, tl]
  max_duration: 48h
  require_labels: [bug]
```

### Minimal user config

```yaml
user:
  owner_role: engineer
  name: "Ada Lovelace"
  handle: "ada"

preferences:
  editor: code
```

### Full user config

```yaml
user:
  owner_role: engineer
  name: "Ada Lovelace"
  handle: "ada"

agent:
  provider: pi

preferences:
  editor: code
  dashboard_sections: [do, review, incoming, blocked]
  standup_auto_post: false
  ai_drafts: true
  theme: catppuccin
  auto_navigate: true
  passive_awareness:
    show: [review_requests, blocked, mentions]
    during_build: false

workspaces:
  auth-service: ~/code/auth-service
  api-gateway: ~/code/api-gateway
```
