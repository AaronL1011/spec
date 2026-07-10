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
returns empty results and lets local workflows continue.

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
    email: ${JIRA_EMAIL}        # REQUIRED — Atlassian account email for basic auth
    token: ${JIRA_API_TOKEN}

    # --- Optional: bind to a specific board/team and tune behaviour ---
    board_id: 42                # board analytics are scoped here
    team_id: "team-abc"         # Jira Team field (Advanced Roadmaps / Plans)
    epic_issue_type: Epic       # Epic | Initiative | <custom hierarchy level>
    story_issue_type: Story     # issue type used when sync_stories is on
    sync_stories: false         # opt-in: create a Jira story per build step
    request_timeout: 10s
    labels: [spec-managed]      # applied to every spec-created issue
    components: []

    # Custom-field ids vary per Jira instance — set them explicitly, never
    # guessed. Run `spec config check` to discover your instance's fields.
    fields:
      epic_name: customfield_10011   # required on company-managed projects
      epic_link: customfield_10014   # company-managed: links stories to the epic
                                     # (omit on team-managed; the parent field is used)
      team: customfield_10001
      sprint: customfield_10020
      story_points: customfield_10016

    # Map spec pipeline stages to Jira board statuses. A stage that is absent
    # makes no Jira call (clean no-op). Status sync is on by default once this
    # map is set; run `spec config check` to print your workflow statuses.
    status_map:
      draft: "To Do"
      engineering: "In Progress"
      build: "In Progress"
      pr-review: "In Review"
      qa-validation: "In Review"
      done: "Done"
      closed: "Done"
```

| Provider | Required fields | Key optional fields |
|---|---|---|
| `jira` | `base_url`, `project_key`, `email`, `token` | `board_id`, `team_id`, `epic_issue_type`, `fields.*`, `status_map`, `sync_stories` |

**Linking is idempotent.** `spec new`/`spec promote` find-or-create the epic by a
`spec-id:<ID>` marker label, so re-runs never duplicate. Created epics carry a
remote link back to the spec. Adopt an existing epic with
`spec link <id> --epic PLAT-123`.

**Status reflection is deterministic.** On `spec advance`, the new stage is
mapped to a Jira status via `status_map` and the matching transition is
executed (idempotently). Unmapped stages do nothing. A failed transition is
queued and retried; `spec sync --pm` reconciles a drifted board on demand and
`spec status <id>` flags a spec whose Jira card is out of sync.

**Enable runbook:**

1. Set `email` plus `board_id`/`fields` as needed.
2. Run `spec config check` to validate credentials and print the live workflow
   statuses.
3. Author `status_map` from those statuses.
4. Optionally set `sync_stories: true` to mirror build steps as Jira stories.

#### `integrations.docs`

Mirrors every spec to a documentation platform so people outside engineering
(PMs, designers, leadership) can read specs where they already work, without a
Git or markdown tool. The Confluence integration is **outbound and per-spec**:
each spec publishes its full content to one Confluence page and keeps that page
current as it moves through the pipeline. There is no bulk "mirror everything"
command — mirroring happens incrementally, one spec at a time.

```yaml
integrations:
  docs:
    provider: confluence            # confluence | none  (notion: coming soon)
    base_url: ${CONFLUENCE_BASE_URL} # https://<org>.atlassian.net/wiki  (must include /wiki)
    space_key: PLAT                  # human space key; resolved to a numeric space id automatically
    parent_page_id: "123456"         # REQUIRED — spec pages are created as children of this page
    email: ${CONFLUENCE_EMAIL}       # REQUIRED — Atlassian account email for Cloud basic auth
    token: ${CONFLUENCE_API_TOKEN}
    # request_timeout: 15s           # optional per-request timeout (default: 10s)
```

| Provider | Required fields | Optional fields |
|---|---|---|
| `confluence` | `base_url`, `space_key`, `parent_page_id`, `email`, `token` | `request_timeout` |

**If any required field is missing, docs is disabled** (a clean no-op, never a
crash). spec prints a warning naming the missing field to stderr on the next
command that uses integrations — e.g. *"confluence: parent_page_id required so
spec pages mirror under a parent — docs disabled"* — and `spec config test`
lists Docs as not configured.

**Where each value comes from:**

| Field | How to obtain it |
|---|---|
| `base_url` | Your wiki root, including the `/wiki` path: `https://<org>.atlassian.net/wiki`. |
| `email` | The Atlassian account email that owns the API token (Cloud basic auth). |
| `token` | [id.atlassian.com](https://id.atlassian.com/manage-profile/security/api-tokens) → Security → **Create API token**. |
| `space_key` | Space → **Space settings**, or the `.../spaces/PLAT/...` segment in the space URL. spec resolves this to the numeric space id for you. |
| `parent_page_id` | Create (or pick) a parent page such as "Specs", open it, and copy the number from `.../pages/123456/...` in its URL. |

**Token permissions.** The API token's account needs to read the space and to
create/update pages and add labels under `parent_page_id`. A space-admin or
contributor role on the target space is sufficient.

**How a spec maps to a page.**

- **Identity is a durable label** (`spec-id-<slug>`), not the page title. Page
  lookups search by this label, so renaming the page in Confluence never
  orphans the mirror.
- **Titles are human-friendly** — `SPEC-042 — <title>`, derived from the spec
  frontmatter — so the space reads like documentation, not ticket ids.
- **Pages nest under `parent_page_id`**, keeping the space navigable instead of
  dumping pages at the root. First publish resolves the space id, creates the
  child page, and attaches the identity label; later publishes update the page
  in place (with an optimistic version bump).
- **Frontmatter becomes a metadata panel.** The YAML frontmatter is stripped
  from the body and rendered as an info panel at the top of the page (Status,
  Author, Cycle, Version, Epic, Repos, Updated), so readers get context at a
  glance. Spec markdown — headings, lists, tables, code blocks, links, inline
  formatting — is converted to Confluence storage format, with all prose
  XML-escaped so content like `List<T>` or `Q&A` publishes cleanly.

**What triggers a mirror** (see [`sync`](#sync) below):

1. **Automatically on `spec advance`** when `sync.outbound_on_advance: true`
   (the default in generated configs). Each stage transition republishes the
   spec — this is the primary, zero-effort path.
2. **Manually** with `spec sync <id>`. Adding `--dry-run` previews the
   outbound plan locally **without** contacting Confluence. Sync is
   **outbound-only by default** — the spec is the source of truth, and the
   mirror never overwrites it unless inbound is requested explicitly.

Mirror failures are **non-fatal**: if Confluence is unreachable during
`spec advance`, the advance still succeeds and the failure is reported as a sync
effect — the spec lifecycle never blocks on the mirror.

**Reading and contributing.** External readers simply browse the space. For
non-engineer roles (`pm`, `designer`, `qa`), `spec edit <id>` prints the
Confluence page URL (resolved via the same label) instead of opening `$EDITOR`,
pointing each contributor to where they read and comment.

> **Inbound (Confluence → repo) is best-effort and opt-in.** Pulling Confluence
> edits back into the specs repo requires an explicit `spec sync <id>
> --direction in` (or `both`) plus an interactive confirmation — it never runs
> by default. It is section-scoped by `<!-- owner: role -->` markers but
> depends on HTML comments the Confluence editor can strip, and the conversion
> is lossy. Safety rails: the H1 title section is never an inbound target, and
> an empty remote section never deletes non-empty local content (even with
> `--force`). Keep using the specs repo as the source of truth.

**Setup runbook:**

1. Create an Atlassian API token; export `CONFLUENCE_BASE_URL` (with `/wiki`),
   `CONFLUENCE_EMAIL`, and `CONFLUENCE_API_TOKEN`.
2. Create a parent "Specs" page in the target space and copy its numeric id.
3. Fill in `integrations.docs` with `provider: confluence`, `space_key`, and
   `parent_page_id`.
4. Run `spec config test` to confirm Docs shows as configured (no "disabled"
   warning). Then mirror one spec for real with `spec sync <id> --direction
   out` and confirm its page appears under the parent in Confluence — the first
   push resolves the space id, creates the child page, and attaches the
   identity label, so it validates credentials, space, and parent end to end.
5. Keep `sync.outbound_on_advance: true`, then `spec advance` as normal — each
   spec now mirrors itself.

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
  outbound_on_advance: true   # Mirror the spec to the docs platform on every advance (default: false; generated configs set true)
  conflict_strategy: warn     # warn | abort | force | skip (default: warn) — applies to inbound pulls
  auto_push: auto             # auto | prompt | off (default: auto)
```

`outbound_on_advance` drives the [Confluence mirror](#integrationsdocs): when
`true`, every `spec advance` republishes the full spec outbound. The
`conflict_strategy` settings only affect **inbound** pulls (docs → repo), where
the specs repo always wins on conflict:

| `conflict_strategy` | Behaviour on a conflicting inbound section |
|---|---|
| `warn` | Print a warning and leave the local version; `spec sync` exits with conflicts listed (default) |
| `abort` | Stop the sync run as soon as a conflict is detected |
| `force` | Accept the remote change, overwriting local (also `spec sync --force`) |
| `skip` | Silently skip conflicting sections, applying the non-conflicting ones (also `spec sync --skip`) |

| `auto_push` | Behaviour |
|---|---|
| `auto` | Local edits (`spec edit`, `spec plan edit`) and comments (TUI/MCP threads & decisions) are committed and pushed to the specs repo automatically (default) |
| `prompt` | Interactive commands confirm before publishing; async surfaces (TUI, MCP) behave as `auto` |
| `off` | Edits stay local until you run `spec push` (the original manual model) |

Use `spec edit --no-push` to keep a single edit local regardless of policy when
batching several changes before one push.

---

### `archive`

```yaml
archive:
  directory: archive          # Path inside specs repo for archived specs (default: archive)
```

---

### `dashboard`

Controls the personal dashboard (`spec` with no args) — its staleness cue, cache,
and which blocked specs surface in the **BLOCKED** section.

```yaml
dashboard:
  stale_threshold: 48h        # Age after which a spec is marked stale (default: 48h)
  refresh_ttl: 300            # Cache TTL in seconds (default: 300)
  urgency:                    # Time-urgency gradient (see below)
    easing: ease-in           # linear | ease-in (default) | ease-in-strong
  review:                     # Time-urgency gradient for the REVIEW section
    stale_after: 2d           # PR review-age window. Omit/none = never coloured (default)
  blocked:                    # Scope the BLOCKED section (default: every role sees every blocked spec)
    visible_to: [tl, engineer]  # Roles allowed to see BLOCKED. Empty/omitted = all roles.
    scope: owning_role          # all (default) | involved | owning_role
```

| Field | Default | Description |
|---|---|---|
| `stale_threshold` | `48h` | Time-in-stage after which a spec is flagged stale. |
| `refresh_ttl` | `300` | Seconds the dashboard caches aggregated data. |
| `urgency.easing` | `ease-in` | Curve shaping the [time-urgency gradient](#time-urgency-gradient): `linear`, `ease-in`, or `ease-in-strong`. |
| `review.stale_after` | *(unset)* | Opt-in review-age window for the [time-urgency gradient](#time-urgency-gradient) on REVIEW rows. Omit, `none`, or `0` = never coloured. |
| `blocked.visible_to` | all roles | Roles that may see the BLOCKED section at all. A role not listed sees no BLOCKED section. |
| `blocked.scope` | `all` | Which blocked specs a permitted role sees: `all` (every blocked spec), `involved` (only specs you author or are assigned), `owning_role` (only specs whose pre-block stage your role owned). |

#### Time-urgency gradient

A task's **whole row** on the dashboard **DO** section and the **pipeline screen**
progressively shifts colour — from primary text through yellow, amber, and orange
to red — the longer it dwells in its current stage. This creates gentle,
estimation-free time pressure: a felt "ship it or trim scope" nudge.

The intensity is `ease(dwell / stale_after)`, where:

- **`dwell`** is measured from `stage_entered_at` (stamped in the spec's
  frontmatter on every stage transition), falling back to the `updated` date for
  legacy specs. Editing a spec does **not** reset it — only advancing/reverting does.
- **`stale_after`** is set [per pipeline stage](#stage-fields). **A stage with no
  `stale_after` is never stale and shows no colouring** — there is no global
  fallback window. Set it only on stages where dwell matters (e.g. `build`,
  `engineering`), and leave it off holding stages like `done` or `monitoring`.
- **`easing`** shapes the curve. `ease-in` (default) keeps rows cool for most of
  the window and intensifies near the deadline; `linear` ramps evenly;
  `ease-in-strong` stays cool even longer then spikes.

The colours derive from the active theme (no hardcoded hues), so the gradient
stays correct on light, dark, and accessibility palettes; monochrome themes
(e.g. `graphite`) ramp by brightness, and `NO_COLOR` disables it entirely.

The **REVIEW** section uses the same gradient and `easing`, but on a different
clock: intensity is `ease(pr_age / review.stale_after)`, where `pr_age` is how
long the pull request has been open (the same timestamp behind the "*2d*" label
on the row). It is opt-in — set `dashboard.review.stale_after` to enable it;
left unset, REVIEW rows are never coloured. (Note: `pr_age` is measured from
when the PR was opened, not from when review was requested of you.)

`owning_role` reads the `blocked_from` frontmatter field, which `spec eject`
records automatically when a spec is blocked. Because the team standup already
rolls up every blocker, a common pattern is `scope: involved` here so each
person's dashboard shows only *their own* stuck work while the TL gets the
team-wide view from standup.

The **DO** section is scoped separately, per stage — see
[Dashboard scope](#dashboard-scope) under pipeline configuration.

---

### `pipeline`

Defines the lifecycle every spec moves through. See [Pipeline configuration](#pipeline-configuration) below for the full reference.

---

### `build`

Tunes `spec build`'s DAG orchestration and selects the build adapters. All keys
are optional.

```yaml
build:
  max_parallel: 4              # Max ready nodes fanned out per wave (default: 4)
  router: registry            # Skill routing: registry (default) | none
  strategy: stacked-draft-pr  # VCS/review workflow: stacked-draft-pr (default) | none
```

| Field | Default | Description |
|---|---|---|
| `max_parallel` | `4` | Bounds orchestrator fan-out across a wave's ready nodes. |
| `router` | `registry` | Tier-1 `SkillRouter`. `registry` routes per-node from `.agents/skills/registry.yaml` (see `docs/schemas/registry.v1.json`); `none` routes nothing and lets the harness discover skills. |
| `strategy` | `stacked-draft-pr` | Tier-2 `BuildStrategy`. `stacked-draft-pr` stacks a draft PR per node; `none` keeps work on local branches and exposes no finishing tools. |

Both `router` and `strategy` can be overridden per-user under `agent` (below).

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
  stale_after: 5d             # Dwell window for the time-urgency gradient. Omit = never stale.
  gates: []                   # Conditions that must pass before advancing. See Gates.
  warnings: []                # Time-based alerts. See Warnings.
  transitions:                # Custom advance/revert behaviour. See Transitions.
    advance: {}
    revert: {}
  on_enter: []                # Effects fired when entering this stage. See Effects.
  on_exit: []                 # Effects fired when leaving this stage. See Effects.
  auto_archive: false         # true = move spec to archive/ when entering.
  review: {}                  # Technical plan review requirement. See Stage review.
  dashboard: {}               # Who sees this stage's specs in the DO section. See Dashboard scope.
```

**`owner`** accepts a single role string or an array:

```yaml
owner: pm
owner: [pm, tl]
```

Valid roles: `pm`, `tl`, `designer`, `qa`, `engineer`

**`stale_after`** sets this stage's dwell window for the
[time-urgency gradient](#time-urgency-gradient). Accepts `m`/`h`/`d`/`w` units
(`30m`, `48h`, `5d`, `2w`). Omit it, or set `none`/`0`, to make the stage never
stale (no colouring). There is no global fallback — only stages with an explicit
`stale_after` show the gradient.

### Dashboard scope

By default a spec appears in the **DO** section of the dashboard for *everyone*
whose role owns its current stage. On a team with several engineers that floods
each dashboard with the whole role's work. Per-stage `dashboard.do_scope`
narrows DO to the person actually responsible, using two spec concepts:

- **author** — who originated the spec (frontmatter `author`, set at `spec new`).
- **assignees** — who is responsible for moving it *now* (frontmatter
  `assignees`). Set with `spec assign`, claimed in the TUI with `g c`, or
  claimed automatically when you run `spec build` / `spec do` on an
  assignee-scoped stage.

```yaml
- name: engineering
  owner: engineer
  dashboard:
    do_scope: assignee        # role (default) | assignee | author | none
    claimable: true           # default true
```

| `do_scope` | Who sees the spec in DO |
|---|---|
| `role` (default) | Anyone whose role owns the stage. Today's behaviour. |
| `assignee` | The spec's assignee(s) only. While unassigned the spec falls back to the whole owning role (a shared "claimable" queue) unless `claimable: false`. |
| `author` | The spec author only, regardless of role. |
| `none` | Nobody — the spec is visible only in the dashboard / `spec list`, never in DO. |

**`claimable`** (default `true`) only applies to `assignee` scope. `true`
surfaces unassigned specs to the whole owning role so anyone can pick them up;
`false` hides them from everyone until someone is explicitly assigned.

The full pipeline stays visible to everyone via the dashboard and `spec list` —
scope only narrows the focused DO section, never hides work outright.

**Identity matching.** Assignees and author are matched against the user's
configured `name` *and* `handle` (case-insensitive, a leading `@` is ignored),
so `@maximo`, `maximo`, and `Maximo` all resolve to the same person.

#### Assigning work

| Action | CLI | TUI |
|---|---|---|
| Claim a spec for yourself | `spec assign SPEC-123` | `g c`, then Enter |
| Assign to others | `spec assign SPEC-123 @greg @maximo` | `g c`, edit the handles |
| Unassign everyone | `spec assign SPEC-123 --clear` | `g c`, type `-` |
| Auto-claim on starting work | `spec build` / `spec do` | `b` (build) |

Auto-claim fires only on the build/`do` path, so a `planning`-style stage that is
`assignee`-scoped is picked up explicitly with `spec assign` (or `g c`). The DO
row shows the assignee (e.g. `@maximo`, or `@maximo +1`) or `unclaimed` for an
assignee-scoped stage waiting to be picked up.

#### Worked example

Keep a planning stage personal to its author until it reaches a role-scoped
review stage, where the reviewing role picks it up:

```yaml
dashboard:
  blocked:
    visible_to: [tl, engineer]
    scope: owning_role

pipeline:
  stages:
    - name: planning
      owner: engineer
      dashboard: { do_scope: assignee }   # unclaimed → shared queue; claimed → personal
    - name: plan_review
      owner: [tl, engineer]               # role-scoped (default): opens up for review
    - name: build
      owner: engineer
      dashboard: { do_scope: assignee }   # only the engineer building it
    - name: done
      owner: tl
      dashboard: { do_scope: none }       # terminal — keep finished specs out of DO
```

Result: an unclaimed `planning` spec shows to every engineer as a queue; once
claimed it shows only to its assignee; at `plan_review` it opens to the whole
reviewing role; a spec blocked from `build` shows in BLOCKED for the TL and
engineers (build is engineer-owned) but not for the PM or designer.

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
  handle: "ada"           # Spec-canonical handle (stable, user-chosen)
  identities:             # Optional: your handle on each integration
    github: adalovelace   #   a handle differs on every service
    slack: "@ada"
    jira: ada.lovelace
```

`owner_role` drives all role-aware commands (`spec list`, dashboard queue, passive awareness).
Missing `owner_role` prints a setup prompt on any role-aware command.

**`handle`** is your *spec-canonical* identity — a stable token that identifies
you inside spec (frontmatter author/assignees, thread author, decision log). It
never leaves spec, so it can be anything you like.

**`identities`** maps an integration **provider** (`github`, `slack`, `teams`,
`jira`, …) to your handle on that service. Because a person's handle differs on
every platform, adapter calls (e.g. "PRs awaiting my review" on GitHub) resolve
through this map. It is optional and additive: any provider you don't list
falls back to `handle`, so existing configs keep working unchanged. `spec
config init --user` only prompts for the providers your team actually
configured, and `spec config lint` warns about identity keys no integration
uses.

**Identity matching.** A spec authored or assigned under *any* of your
identities (canonical handle, display name, or a per-provider handle) is
recognised as yours in the dashboard and awareness line — so display-name vs
`@handle` vs login drift across teams never hides your own work.

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
  router: registry           # optional: override build.router for your runs
  strategy: stacked-draft-pr # optional: override build.strategy for your runs
```

This takes precedence over `integrations.agent` in `spec.config.yaml`.

The build integration distinguishes two skill roles:

- `conductor_skill` — orchestrator-level skills handed to an MCP-capable agent
  for the whole-DAG build. Start-dir scoping avoids cross-repo skill name
  collisions.
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
| `spec whoami` | Show resolved identity: role, name, handle, per-integration identities, config file paths |
| `spec join <repo>` | Clone a specs repo and bootstrap local config |

### `spec whoami` output

```
Name:   Ada Lovelace
Role:   engineer
Handle: ada
Config: ~/.spec/config.yaml
Team:   Platform Team
Cycle:  Cycle 7
Team config: ~/code/specs/spec.config.yaml
Identities:
  repo    (github): adalovelace
  comms   (slack): @ada
```

The `Identities` block shows the exact handle each adapter receives. When a
provider is unmapped, the canonical handle is shown in its place.

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
    parent_page_id: "123456"
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
