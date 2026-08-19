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
| `spec config test` | Config and integration presence, resolved [terminal stages](#terminal-stages); no remote calls |
| `spec config check` | Live PM/Jira project and workflow preflight |
| `spec whoami` | Effective identity, team, and config paths |
| `spec pipeline validate` | Pipeline owners, gates, effects, references |
| `spec build --check` | Build graph, workspaces, skills, capabilities |

`config lint` reports line-precise errors and warns about unused provider
identity keys and about an `auto_archive` stage placed before required stages.
`config check` currently performs the implemented live Jira check.

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

### `templates`

Customise the spec and triage skeletons scaffolded by `spec new`,
`spec promote`, and `spec intake`. The entire block is optional — with no
config and no committed files, the built-in templates are used.

```yaml
templates:
  spec: templates/spec.md       # path inside the specs repo (default shown)
  triage: templates/triage.md   # path inside the specs repo (default shown)
  frontmatter_defaults:         # optional keys seeded into every new spec
    service_area: payments
    compliance: sox
```

A template file committed at the configured path overrides the built-in
default. Placeholders use `<% name %>` syntax:

| Placeholder | Spec | Triage | Value |
|---|---|---|---|
| `<% id %>` | ✓ | ✓ | `SPEC-NNN` / `TRIAGE-NNN` |
| `<% title %>` | ✓ | ✓ | The item title |
| `<% date %>` | ✓ | ✓ | Creation date (`YYYY-MM-DD`) |
| `<% author %>` | ✓ | | From git config |
| `<% cycle %>` | ✓ | | From `team.cycle_label` |
| `<% source %>` | ✓ | ✓ | Triage ID, `direct`, or `tui` |
| `<% priority %>` | | ✓ | Intake priority |
| `<% source_ref %>` | | ✓ | Ticket #, alert ID, permalink |
| `<% reported_by %>` | | ✓ | From `spec whoami` |

`frontmatter_defaults` entries are inserted into the frontmatter of every new
spec in declaration order. Computed fields (`id`, `title`, `created`, …)
always win — a default whose key already exists in the rendered frontmatter
is skipped.

**Manage templates with `spec template`:**

| Command | Effect |
|---|---|
| `spec template init` | Commit the built-in defaults to the specs repo as a starting point (`--force` to overwrite) |
| `spec template validate [spec\|triage]` | Parse, render, and structurally check the committed template |
| `spec template show [spec\|triage]` | Print the effective template new specs will scaffold from |

**Templates are fluid — changes are forgiving.** Every scaffold resolves the
template from the freshly synced specs repo, so new specs always use the
latest committed template. A template that is broken mid-edit (parse error,
unresolved placeholder, or a missing gate-critical section —
`problem_statement`, `user_stories`, `acceptance_criteria`) never blocks spec
creation: scaffolding falls back to the built-in default with a warning, and
`spec template validate` reports exactly what to fix. Existing specs are
never touched by template changes — a spec keeps the skeleton it was born
with, and section-based features (gates, sync, `spec answer`) operate on the
spec's own headings, not the current template.

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

The agent is configured **once, in personal config** (`~/.spec/config.yaml`) and
serves two planes:

| Plane | Powers | Requires |
| --- | --- | --- |
| Completions | `spec draft`, TUI `d` | `Generate` capability |
| Sessions | `spec build`, `spec draft --interactive`, TUI `b`/`D` | `MCP` capability |

A harness serves both. A completion endpoint serves the first only. Nothing in
team config selects an agent: it depends on what each person has installed and
pays for, and a shared default would break for everyone who chose differently.

```yaml
# ~/.spec/config.yaml
agent:
  provider: pi              # pi | claude-code | openai-compatible | anthropic | none
  command: pi               # optional binary override
```

Verify any configuration end to end:

```bash
spec agent check
```

This reports the reachable binary or endpoint, which planes are available, and
the latency of a real contained round-trip. Use it before relying on an agent
mid-draft.

#### Harnesses

```yaml
agent:
  provider: claude-code     # or: pi
```

The binary must be on PATH. Harnesses reuse their own authentication, so no token
belongs in spec's config. Both planes are available.

Headless completions are contained: no tools, no MCP servers, no session
persistence, an empty working directory, and closed stdin. A drafting call cannot
touch the repository.

#### Completion endpoints

```yaml
agent:
  provider: openai-compatible
  generate:
    base_url: http://localhost:11434/v1
    model: qwen2.5-coder:14b
```

Any server speaking the OpenAI `/chat/completions` shape works. Presets fill in
`base_url` when omitted:

| Provider | Default `base_url` |
| --- | --- |
| `ollama` | `http://localhost:11434/v1` |
| `llama-server` | `http://localhost:8080/v1` |
| `lmstudio` | `http://localhost:1234/v1` |
| `openai` | `https://api.openai.com/v1` |
| `openai-compatible` | none — set it explicitly |

Hosted providers need a token, given as an environment reference:

```yaml
agent:
  provider: openai
  generate:
    model: gpt-4o
    token: ${SPEC_LLM_TOKEN}
```

Anthropic's native API is also supported:

```yaml
agent:
  provider: anthropic
  generate:
    model: claude-sonnet-4-5
    token: ${SPEC_LLM_TOKEN}
```

Endpoint providers serve completions only, so `spec build` degrades to its solo
fallback and `spec draft --interactive` falls back to one-shot drafting.

#### Generation settings

```yaml
agent:
  provider: openai-compatible
  generate:
    base_url: http://localhost:1234/v1
    model: qwen2.5-coder:14b
    max_tokens: 4096
    token: ${SPEC_LLM_TOKEN}
    timeout: 120s
```

| Key | Applies to | Notes |
| --- | --- | --- |
| `model` | all | Passed through verbatim; spec does not translate names |
| `base_url` | endpoints | Ignored by harnesses |
| `token` | endpoints | Must be `${VAR}`; a literal is refused |
| `max_tokens` | endpoints | Harness CLIs expose no cap |
| `timeout` | all | Default `120s`; local models on cold cache are slow |

`spec agent check` names any setting that cannot take effect for the resolved
provider, so a value that looks effective but is not gets reported rather than
silently ignored.

#### Turning drafting off

```yaml
preferences:
  agent_drafts: false
```

This hides draft affordances while leaving sessions working — the planes are
independent. Defaults to enabled, so drafting works as soon as an agent exists.

#### Letting a session move stages

Section writes are always available in a session. Stage transitions are not,
because they fire the pipeline's team-visible effects (Jira sync, Slack posts):

```yaml
preferences:
  agent_authoring:
    transitions: true
```

While disabled, `spec_advance` and `spec_revert` are absent from `tools/list`
entirely rather than failing when called.

#### Migrating from `integrations.ai` / `integrations.agent`

Both keys were removed. Move the agent into personal config and rename `ai` to
`agent.generate`:

```yaml
# before — team config
integrations:
  agent: {provider: pi}
  ai: {provider: ollama, model: llama3, base_url: http://localhost:11434}

# after — personal config (~/.spec/config.yaml)
agent:
  provider: pi                      # keep the harness for both planes
  generate:                         # or, for a completion endpoint:
    base_url: http://localhost:11434/v1
    model: llama3
```

Note the `/v1` suffix: the completion adapter speaks the OpenAI-compatible API
rather than Ollama's native one, so an existing `base_url` needs it appended.

`preferences.ai_drafts` is now `preferences.agent_drafts`.

`spec config lint` flags every removed and renamed key with the replacement.
Removed keys warn rather than hard-fail, so a stale team config does not break
commands for anyone still on an older binary.

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
      auto_archive: false   # completes + archives here (see Terminal stages)
```

Owners may be a string or list. Built-in user roles are `pm`, `tl`, `designer`,
`qa`, and `engineer`; presets also use special owners such as `anyone` and
`author`.

#### Terminal stages

A **terminal stage** is where a spec is considered complete. It is not a field
you set — there is no `terminal: true`. The set is derived from the stages you
already declared:

1. Every stage with `auto_archive: true`.
2. The **last stage without `optional: true`** (the last required stage).
3. Only if neither rule matches — which needs every stage to be optional — any
   stage literally named `done` or `closed`.

With the default pipeline that gives `closed` (rule 1) and `done` (rule 2).

Reaching a terminal stage is what ends lead time and cycle time, counts toward
throughput (`spec metrics`, `spec retro`), and — if bounties are enabled —
**freezes a claimed bounty into an immutable award**. So it is worth knowing
which stages yours are:

```bash
spec pipeline              # names them, with the rule that qualified each
spec pipeline --verbose    # tags each stage [terminal: <reason>]
spec config test           # in the resolved-config report
```

Because the set is derived, editing an unrelated stage can move it:

- Adding a stage **without** `optional: true` after your finish stage makes the
  new stage terminal instead, so specs finishing at the old one complete
  nothing.
- Marking your finish stage `optional: true` **without** `auto_archive: true`
  moves completion *backwards* to the stage before it.

`spec config lint` warns when an `auto_archive` stage still has required stages
after it, since that completes and archives a spec before mandatory work. It
cannot catch the two cases above — check `spec pipeline` after reordering
stages.

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
  - children_complete: true
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

##### Letting an initiative close (`children_complete`)

A spec that carries vision for a set of deliverable slices (see
[spec hierarchy](#spec-hierarchy)) has no PR stack of its own, so the delivery
gates wedge it permanently. `children_complete` passes when the spec has **at
least one** slice and every slice has reached a terminal stage. A spec with no
slices evaluates **false**, never vacuously true, so adding this gate under
`any:` can only ever relax an initiative — never an ordinary spec.

This is opt-in; upgrading does not rewrite your pipeline. To adopt it, relax
**both** delivery stages — relaxing only the first leaves the initiative wedged
one stage later:

```yaml
pipeline:
  stages:
    - name: pr-review
      owner_role: engineer
      gates:
        - any:
            - pr_stack_exists: true
            - children_complete: true
    - name: qa-validation
      owner_role: qa
      gates:
        - any:
            - prs_approved: true
            - children_complete: true
```

The same rollup is available to expression gates and `skip_when` as
`children.total`, `children.complete`, `children.open` and `children.blocked`.
In an expression, always test `children.total > 0` alongside `children.open ==
0`; the latter alone is true for every spec that has no slices.

A team that never edits its config still gets links, rollups, agent context
inheritance and validation — its initiatives simply stop at `pr-review`.

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

### Bounties

```yaml
bounty:
  enabled: true               # default false — no bounty surface at all
  grantable_by: [tl, pm]      # roles allowed to grant and clear
  max_active: 3               # concurrently bountied specs
  require_reason: true        # a grant must say why
  shimmer: true               # animate the TUI marker
  finish: gold                # gold | platinum | prismatic
```

A bounty marks a spec as **worth claiming**. It is a pull signal, not an
instruction: it never assigns anyone, reorders a queue, changes a gate, or sends
a notification. A bountied spec renders its gem glyph (`◈`) and SPEC-ID in gold
wherever it appears, while the rest of the row keeps its time-urgency colour, so
"worth taking" and "has been sitting too long" stay separately readable.

```bash
spec bounty set SPEC-042 --reason "unblocks the billing migration"
spec bounty list
spec bounty clear SPEC-042            # --force if already claimed
```

In the TUI, `g b` on a selected spec opens the same prompt; submitting `-`
clears the bounty.

#### Marker finish

The marker is shaded like a lit surface rather than filled with a flat colour:
the tone falls away from the glyph toward the end of the ID, and a narrow
highlight crosses it about every six seconds. Each spec's pass is offset by a
hash of its ID, so several bountied rows twinkle independently instead of
blinking together.

| `finish` | reads as |
| --- | --- |
| `gold` (default) | warm and legible on every palette |
| `platinum` | cool white metal — understated to the point of blending into primary text on most themes |
| `prismatic` | near-white stone whose highlight rotates through hue as it crosses, like dispersion in a cut gem |

`shimmer: false` keeps the shading but parks the highlight. Monochrome themes
(`graphite`) override all three finishes with a luminance ramp, and `NO_COLOR`
or a low-colour terminal falls back to the glyph plus bold weight.

Three rules are worth knowing before you turn this on:

- **The cap is the feature.** Past `max_active`, granting fails and lists the
  specs you must choose between. An uncapped marker debases into a second
  priority field.
- **A granter cannot claim their own bounty.** The assignment still happens;
  the award simply does not attach, so no one self-awards.
- **Bounties belong on unglamorous, critical work.** The specs that need a nudge
  are the ones with no intrinsic appeal. Gold-starring the fun greenfield spec
  inverts the mechanism and starves the boring queue further.

The record lives in the spec's own frontmatter (`bounty.granted_by`,
`claimed_by`, `earned_by`, `earned_at`) and travels with it into `archive/`, so
awards survive clones and machine loss. Advancing a claimed, bountied spec into
a [terminal stage](#terminal-stages) freezes the award; it is immutable from then
on. Terminal stages are derived from your pipeline rather than configured
directly, so run `spec pipeline` to confirm where awards will settle before you
turn bounties on.

```bash
spec bounty ledger                       # all recorded awards
spec bounty ledger --since 2026-07-01    # a quarter
spec bounty ledger --cycle "Cycle 7"     # one delivery cycle
spec bounty ledger --json                # for a spreadsheet or a script
```

The ledger is derived from the specs repo and its archive on every run — no
local database is consulted, so a fresh clone reports the same numbers. What an
earned bounty is worth is deliberately outside the tool: `spec` records who
claimed and finished the work, and the reward is a decision you make with it.

---

## Spec hierarchy

A spec may declare a **parent** in its frontmatter, making it a *deliverable
slice* of an *initiative*:

```yaml
---
id: SPEC-009
title: Token bucket limiter
parent: SPEC-004
---
```

There is no new document type and no `kind:` field. An initiative is an
ordinary spec that happens to have slices; "is this an initiative?" is always
derived from the graph, never declared, so the two can never disagree. A spec
with no `parent:` behaves exactly as it always did, including every gate —
there is nothing to migrate.

```bash
spec new --title "Redis backend" --parent SPEC-004   # scaffold a slice
spec link SPEC-009 --parent SPEC-004                 # attach an existing spec
spec link SPEC-009 --parent ""                       # detach
spec list --parent SPEC-004                          # the initiative's slices
spec status SPEC-004                                 # slices + rollup
spec status SPEC-009                                 # the parent line
```

### The rules

The tree is **two levels deep with one parent**, enforced when the link is
made rather than detected afterwards:

| Rule | At link time | `spec validate` |
| --- | --- | --- |
| Parent must exist | refused | **error** |
| Parent must not be the spec itself | refused | **error** |
| Parent must not itself have a parent | refused | warning |
| A spec with slices may not gain a parent | refused | warning |
| Parent must not be in a terminal stage | refused | warning |
| Parent must not be archived | refused | warning |

Frontmatter is hand-editable, so `spec validate` re-checks all six as a
backstop. The severity split is deliberate: a dangling or self-referential
parent makes every hierarchy query undefined and blocks `spec advance`, while
an initiative that closed last week must not retroactively wedge a slice
someone is working on today. `spec link --parent ""` always works and is the
escape hatch for every refusal.

One spec never mutates another's stage. If a slice is reverted after its
initiative closed, `spec validate` reports `SPEC-004 closed · SPEC-009 reopened
to build` and a human decides what to do.

### What a slice inherits

A slice's coding agent is given the initiative's **TL;DR, §1 Problem Statement
and §4 Proposed Solution** as a delimited, read-only block — vision is written
once and never copy-pasted. The parent's §5–§8 are deliberately excluded: its
acceptance criteria and technical implementation describe nothing the slice
should build, and injecting them invites an agent to implement the wrong scope.

To let an initiative reach a terminal stage without a PR stack of its own, see
[`children_complete`](#letting-an-initiative-close-children_complete).

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
  agent_drafts: true
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

### Personal agent

```yaml
agent:
  provider: pi
  command: pi
  conductor_skill: ~/.agents/skills/build-orchestrator
  skill: ~/skills/spec-build
  router: registry
  strategy: stacked-draft-pr
  generate:
    model: qwen2.5-coder:14b
```

This is the only place an agent is configured; team config has no agent key. See
[Coding agent](#coding-agent) for the full reference and `spec agent check` to
verify it.

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

**Drafting says no completion plane:** the provider serves sessions only. Run
`spec agent check` to see which planes are available, and configure a completion
endpoint under `agent.generate` if you want `spec draft`.

**`spec draft` reports a removed key:** `integrations.ai` and
`integrations.agent` no longer exist. See
[Migrating](#migrating-from-integrationsai--integrationsagent).

**Draft keys are missing in the TUI:** either no agent is configured or
`preferences.agent_drafts` is false. `spec agent check` distinguishes the two.

**Build repository is unresolved:** add it under personal `workspaces`, then
run `spec build --check`.

**Changes are not publishing:** inspect `sync.auto_push` and run `spec push`.
