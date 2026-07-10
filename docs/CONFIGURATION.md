# Configuration reference

Most users should not start here. Install `spec`, run `spec`, and complete the
interactive onboarding flow described in the [Quickstart](QUICKSTART.md).

Use this reference when you:

- create or administer a team;
- connect integrations;
- customise a pipeline;
- manage advanced personal preferences;
- diagnose config resolution.

`spec` combines two configuration scopes:

| File | Scope | Commit it? |
| --- | --- | --- |
| `spec.config.yaml` | Team, repository, integrations, pipeline | Yes |
| `~/.spec/config.yaml` | Identity, TUI preferences, agent, workspaces | No |

---

## Configuration workflows

### Ordinary first run

```bash
spec
```

The TUI wizard creates personal identity and joins an existing team. Joining
clones the specs repository, validates `spec.config.yaml`, and opens the
Dashboard.

### Create a team

Creating a team is an administrative path. In the repository that will own the
team config, run:

```bash
spec config init
```

The interactive selector presents five pipeline presets, then asks for team,
cycle, and specs-repository details. It writes `spec.config.yaml` with optional
integrations disabled.

Afterward:

```bash
spec config lint
spec pipeline --verbose
```

Commit `spec.config.yaml`, create a `specs/` directory, and push the repository
so teammates can join it with plain `spec`.

A non-interactive initializer defaults to `minimal`; choose explicitly in
automation:

```bash
spec config init --preset startup
```

### Repair personal identity without the TUI

```bash
spec config init --user
spec whoami
```

The command wizard asks for name, role, canonical handle, integration-specific
identities for configured providers, and editor.

### Join directly from the shell

```bash
spec join acme/specs
spec join github.com/acme/specs
spec join gitlab.com/acme/specs --branch develop
spec join https://bitbucket.org/acme/specs --token "$TOKEN"
```

When no hostname is supplied, GitHub is assumed.

---

## Validate and diagnose

The config commands have different responsibilities:

| Command | What it checks |
| --- | --- |
| `spec config lint` | Team YAML structure and semantics |
| `spec config test` | Config and integration presence; no remote calls |
| `spec config check` | Live PM/Jira project and workflow preflight |
| `spec whoami` | Effective identity, team, and config paths |
| `spec pipeline validate` | Pipeline owners, gates, effects, references |
| `spec build --check` | Build graph, workspaces, skills, capabilities |

`config lint` reports line-precise errors and warns about unused provider
identity keys. `config check` currently performs the implemented live Jira
check.

Recommended admin check:

```bash
spec config lint
spec config test
spec config check        # when Jira is configured
spec pipeline validate
```

Provider construction warnings appear on commands that use integrations. A
missing required field disables only that adapter; local workflow continues.

---

## Resolution and storage

On each invocation, `spec` loads personal config and finds team config in this
order:

1. `spec.config.yaml` in the current directory;
2. an ancestor directory;
3. `.spec/spec.config.yaml` in the current or ancestor directory;
4. a joined clone under `~/.spec/repos/<owner>/<repo>/`.

The first valid team config wins. Personal config at `~/.spec/config.yaml` is
always loaded.

Even when team config is found in your own checkout, spec documents are read
and written through the managed clone:

```text
~/.spec/repos/<owner>/<repo>/
├── spec.config.yaml
└── specs/
    ├── SPEC-042.md
    ├── SPEC-042.threads.yaml
    ├── triage/TRIAGE-088.md
    └── archive/SPEC-001.md
```

Other local state:

```text
~/.spec/
├── config.yaml          personal identity and preferences
├── spec.db              focus, cache, activity, build ledger, read-state
├── repos/               managed team repositories
└── sessions/            build-session artifacts
```

---

## Environment variables

Any config string can reference `${VAR}`. Values resolve at load time.

```yaml
specs_repo:
  token: ${SPEC_GITHUB_TOKEN}
```

Preferred repository token variables:

| Provider | Variable |
| --- | --- |
| GitHub | `SPEC_GITHUB_TOKEN` |
| GitLab | `SPEC_GITLAB_TOKEN` |
| Bitbucket | `SPEC_BITBUCKET_TOKEN` |

Legacy bare names such as `GITHUB_TOKEN` remain aliases with a deprecation
warning. The exact variable named in the config wins.

Never commit literal secrets. Team config may safely commit `${VAR}` strings;
each user or CI environment provides values.

---

## Team configuration

### Minimal team config

```yaml
version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 7"

specs_repo:
  provider: github
  owner: acme
  repo: specs
  branch: main
  token: ${SPEC_GITHUB_TOKEN}

pipeline:
  preset: startup
```

Defaults applied when omitted:

- specs repository branch: `main`;
- archive directory: `archive`;
- dashboard refresh TTL: `300` seconds;
- dashboard stale threshold: `48h` (legacy/general display value);
- urgency easing: `ease-in`;
- sync conflict strategy: `warn`;
- sync auto-push: `auto`.

### `team`

```yaml
team:
  name: "Platform Team"
  cycle_label: "Cycle 7"
```

`cycle_label` is optional and is stamped on newly scaffolded specs.

### `specs_repo`

```yaml
specs_repo:
  provider: github
  owner: acme
  repo: specs
  branch: main
  token: ${SPEC_GITHUB_TOKEN}
```

The managed clone and ID-claiming workflow support GitHub, GitLab, and
Bitbucket repository references. Individual integrations have their own
implementation status below.

### `sync`

```yaml
sync:
  outbound_on_advance: true
  conflict_strategy: warn   # warn | abort | force | skip
  auto_push: auto           # auto | prompt | off
```

#### `outbound_on_advance`

When true, advancing publishes the spec through the configured docs adapter.
An adapter failure is reported but does not roll back the stage transition.

#### `conflict_strategy`

Controls explicit inbound docs sync:

| Value | Behavior |
| --- | --- |
| `warn` | Keep local conflicting sections and report them |
| `abort` | Stop at the first conflict |
| `force` | Accept remote content for conflicts |
| `skip` | Silently skip conflicts and apply safe sections |

Inbound sync is opt-in. Run `spec sync <id> --direction in` or `both`; it asks
for confirmation. Git Markdown remains the source of truth.

#### `auto_push`

| Value | Behavior |
| --- | --- |
| `auto` | Publish local edits/comments automatically (default) |
| `prompt` | Interactive CLI edits ask; asynchronous TUI/MCP changes publish |
| `off` | Keep changes local until `spec push` or TUI `p` |

Use `spec edit --no-push` for one intentionally local edit.

### `archive`

```yaml
archive:
  directory: archive
```

The path is relative to `specs/`.

---

## Integrations

All integration categories are optional. Empty or `provider: none` resolves to
a noop adapter. One broken integration does not prevent local reading, editing,
or pipeline work.

### Implementation matrix

The configuration schema accepts several future providers. This table states
what actually performs remote work today.

| Category | Operational providers | Accepted but currently disabled/noop |
| --- | --- | --- |
| Comms | `slack`, `teams` | `discord` |
| PM | `jira` | `linear`, `github-issues` |
| Docs | `confluence` | `notion` |
| Repo/reviews | `github` | `gitlab`, `bitbucket` |
| Agent | `claude-code`, `pi` | `cursor`, `copilot` |
| AI | `anthropic`, `ollama` | `openai` |
| Deploy | `github-actions` | `gitlab-ci`, `argocd` |

The specs repository itself may be hosted on GitLab or Bitbucket even though
their review adapters are not yet implemented.

### Comms

#### Slack

```yaml
integrations:
  comms:
    provider: slack
    token: ${SPEC_SLACK_TOKEN}
    default_channel: "#platform"
    standup_channel: "#platform-standup"
```

`token` is required.

#### Microsoft Teams

```yaml
integrations:
  comms:
    provider: teams
    webhook_url: ${TEAMS_WEBHOOK_URL}
    standup_webhook_url: ${TEAMS_STANDUP_WEBHOOK_URL}
    graph_token: ${TEAMS_GRAPH_TOKEN}
    team_id: "abc123"
    channel_id: "xyz456"
```

`webhook_url` enables outbound messages. Graph mention sync is optional, but
`graph_token`, `team_id`, and `channel_id` must be supplied together.

### PM: Jira

```yaml
integrations:
  pm:
    provider: jira
    base_url: ${JIRA_BASE_URL}
    project_key: PLAT
    email: ${JIRA_EMAIL}
    token: ${JIRA_API_TOKEN}
    board_id: 42
    epic_issue_type: Epic
    story_issue_type: Story
    sync_stories: false
    request_timeout: 10s
    labels: [spec-managed]
    components: []
    fields:
      epic_name: customfield_10011
      epic_link: customfield_10014
      team: customfield_10001
      sprint: customfield_10020
      story_points: customfield_10016
    status_map:
      draft: "To Do"
      engineering: "In Progress"
      pr_review: "In Review"
      done: "Done"
```

Required fields: `base_url`, `project_key`, `email`, `token`.

Run `spec config check` before authoring `status_map`. It validates the live
project and prints the exact workflow statuses. Unknown custom fields are never
guessed.

Spec-created Jira epics use a `spec-id:<ID>` label for idempotent lookup. Adopt
an existing epic with:

```bash
spec link SPEC-042 --epic PLAT-123
```

`--epic` is an alternative to the normal `--section` / `--url` link mode.

Unmapped stages make no status-transition call. Failed operations are queued;
`spec sync --pm` reconciles later.

### Docs: Confluence

```yaml
integrations:
  docs:
    provider: confluence
    base_url: ${CONFLUENCE_BASE_URL}   # include /wiki
    space_key: PLAT
    parent_page_id: "123456"
    email: ${CONFLUENCE_EMAIL}
    token: ${CONFLUENCE_API_TOKEN}
    request_timeout: 15s
```

All fields except `request_timeout` are required. `parent_page_id` keeps
spec-created pages under a predictable parent.

Manual end-to-end check:

```bash
spec sync SPEC-042 --direction out
```

Pages are found by a durable spec label, not title. Frontmatter becomes a
metadata panel; subsequent publishes update the existing page.

Inbound Confluence conversion is lossy and explicit only. Section owner markers
provide guardrails, but remote empty sections never delete non-empty local
content.

### Repo/reviews: GitHub

```yaml
integrations:
  repo:
    provider: github
    owner: acme
    token: ${SPEC_GITHUB_TOKEN}
```

`owner` and `token` fall back to `specs_repo`. This adapter populates the
Reviews view and PR status/gates.

### Coding agent

```yaml
integrations:
  agent:
    provider: pi            # or claude-code
    command: pi             # optional override
```

The executable must be on PATH. Users may override the team default in personal
config. Run `spec build --check` to see the effective provider and capabilities.

### AI

#### Anthropic

```yaml
integrations:
  ai:
    provider: anthropic
    token: ${ANTHROPIC_API_KEY}
    model: claude-opus-4-5
```

#### Ollama

```yaml
integrations:
  ai:
    provider: ollama
    model: llama3
    base_url: http://localhost:11434
```

AI is optional. Draft commands produce content for review; core lifecycle and
reader functionality do not depend on AI.

### Deploy: GitHub Actions

```yaml
integrations:
  deploy:
    provider: github-actions
    environments:
      - name: staging
        auto: true
      - name: production
        auto: false
        gate: prs_approved
```

The current adapter dispatches a GitHub Actions workflow and reuses repository
owner/token settings.

---

## Pipeline configuration

The easiest starting point is the interactive selector:

```bash
spec config init
spec pipeline presets
```

### Presets

| Preset | Intended use |
| --- | --- |
| `minimal` | Solo/tiny: triage → draft → build → review → done |
| `startup` | Fast product: draft, TL review, build, PR review |
| `product` | Design, engineering, QA, and optional deployment stages |
| `platform` | RFC review, discussion, approval, implementation |
| `kanban` | Continuous backlog → doing → done flow |

Run `spec pipeline presets` for the exact ordered stage list and features of
each preset.

```yaml
pipeline:
  preset: product
  skip: [design]
```

`stages` can override or extend preset stages. Without a preset, `stages` is the
whole pipeline.

### Stage fields

```yaml
pipeline:
  stages:
    - name: engineering
      owner: [engineer, tl]
      icon: "build"
      optional: false
      skip_when: "'internal' in spec.labels"
      stale_after: 5d
      dashboard:
        do_scope: assignee     # role | assignee | author | none
        claimable: true
      gates:
        - section_not_empty: acceptance_criteria
        - steps_exists: true
      warnings:
        - after: 3d
          message: "Plan needs attention"
          notify: tl
      review:
        reviewers: [tl]
        min_approvals: 1
      transitions:
        advance:
          effects:
            - notify: next_owner
      on_enter: []
      on_exit: []
      auto_archive: false
```

Owners may be a string or list. Built-in user roles are `pm`, `tl`, `designer`,
`qa`, and `engineer`; presets also use special owners such as `anyone` and
`author`.

#### Dashboard scope

| `do_scope` | Who sees the spec in DO |
| --- | --- |
| `role` | Everyone with an owning role (default) |
| `assignee` | Assignees; unassigned work falls back to owners when claimable |
| `author` | Original author |
| `none` | Hidden from DO, still visible in Pipeline and Specs |

Use `spec assign` or TUI `g c` to claim work. `spec build` / `spec do` can
auto-claim assignee-scoped build stages.

#### Gates

```yaml
gates:
  - section_not_empty: problem_statement
  - steps_exists: true
  - prs_approved: true
  - review_approved: true
  - duration: 24h
  - link_exists: pr
  - link_exists:
      section: design
      type: figma
  - expr: "decisions.unresolved == 0"
    message: "Resolve decisions before advancing"
```

Compose gates with `all`, `any`, and `not`. Legacy `section_complete` and
`pr_stack_exists` map to `section_not_empty` and `steps_exists`.

#### Effects

Effects may run on entry, exit, or a transition:

```yaml
effects:
  - notify: next_owner
  - notify:
      targets: [tl, "#platform"]
      template: spec-advanced
  - sync: outbound
  - update_pm: { status: "In Review" }
  - log_decision: "Advanced to review"
  - increment: revert_count
  - archive: true
  - trigger: post-deploy-checks
  - webhook:
      url: ${SPEC_WEBHOOK_URL}
      method: POST
      timeout: 10s
  - when: "'hotfix' in spec.labels"
    notify: "#incidents"
```

Validate after editing:

```bash
spec config lint
spec pipeline validate
spec pipeline --verbose
```

### Dashboard urgency

```yaml
dashboard:
  refresh_ttl: 300
  urgency:
    easing: ease-in          # linear | ease-in | ease-in-strong
  review:
    stale_after: 2d
  blocked:
    visible_to: [tl, engineer]
    scope: owning_role       # all | involved | owning_role
```

A stage only receives a time-urgency gradient when that stage sets
`stale_after`. Editing content does not reset dwell time; stage transitions do.
Review urgency is separately opt-in and uses PR age.

### Build engine

```yaml
build:
  max_parallel: 4
  router: registry            # registry | none
  strategy: stacked-draft-pr  # stacked-draft-pr | none
```

- `registry` routes node skills from `.agents/skills/registry.yaml` (legacy
  `.spec/agent/skills/` also works).
- `stacked-draft-pr` creates a stack of draft PRs.
- `none` strategy keeps work on local branches and completes when nodes finish.

See [Agent integration](AGENT_INTEGRATION.md) for the versioned MCP contract.

### Fast track

```yaml
fast_track:
  enabled: true
  allowed_roles: [engineer, tl]
  max_duration: 48h
  require_labels: [bug]
```

This enables `spec fix` for approved roles and labels.

---

## Personal configuration

### Use Settings for common fields

Press `6` in the TUI to edit name, role, handle, theme, refresh interval, mouse,
and editor. Changes apply live and persist to `~/.spec/config.yaml`.

Advanced fields are edited manually or through `spec config init --user`.

### Identity

```yaml
user:
  owner_role: engineer
  name: "Ada Lovelace"
  handle: ada
  identities:
    github: adalovelace
    slack: "@ada"
    jira: ada.lovelace
```

`handle` is the stable identity used inside spec. `identities` maps provider
names to external handles. Missing provider mappings fall back to `handle`.

`spec whoami` shows exactly which identity each configured adapter receives.

### Preferences

```yaml
preferences:
  editor: code
  dashboard_sections: [do, review, incoming, blocked]
  standup_auto_post: false
  ai_drafts: true
  theme: catppuccin-mocha
  refresh_interval: 30s
  mouse: true
  multiplexer: tmux       # tmux | zellij | wezterm | iterm2 | none
  auto_pull: true
  auto_navigate: true
  passive_awareness:
    show: [review_requests, blocked, mentions]
    hide: [fyi]
    during_build: false
    dismiss_duration: 2h
```

Themes include `auto`, Catppuccin variants, Gruvbox, Dracula, Tokyo Night,
Nord, Solarized, Rose Pine, Kanagawa, Everforest, GitHub, Ayu, Modus, and
Graphite. The Settings selector is the authoritative list.

### Personal agent override

```yaml
agent:
  provider: pi
  command: pi
  conductor_skill: ~/.agents/skills/build-orchestrator
  skill: ~/skills/spec-build
  router: registry
  strategy: stacked-draft-pr
```

A non-empty personal agent overrides `integrations.agent` for that user.

### Workspaces

```yaml
workspaces:
  auth-service: ~/code/auth-service
  api-gateway: ~/code/api-gateway
  frontend: ~/code/frontend
```

Repository names in build-plan steps resolve through this map for cross-repo
worktrees and terminal navigation.

---

### Common diagnostics

**First-run wizard does not appear:** use an interactive terminal. The manual
fallback is `spec config init --user`, then `spec join <repo>`.

**Wrong role or personal queue:** edit Settings and inspect `spec whoami`.

**Team config not found:** run `spec` to join, or `spec join <repo>`.

**Config parses but behaves incorrectly:** run `spec config lint` and
`spec pipeline validate`.

**Configured integration fails:** check required fields and environment
variables. `spec config test` is not a network test.

**Jira status mapping is wrong:** run `spec config check` and use its printed
workflow statuses.

**Build repository is unresolved:** add it under personal `workspaces`, then
run `spec build --check`.

**Changes are not publishing:** inspect `sync.auto_push` and run `spec push`.
