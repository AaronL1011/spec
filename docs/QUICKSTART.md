# spec — Quickstart & Daily Guide

This guide covers installation, identity setup, team configuration, pipeline
configuration, and daily use. It assumes only a terminal and Git.

The interactive dashboard is the primary interface for daily work. The command
interface provides the same operations for automation, CI, and direct terminal
use.

For a high-level overview and contribution guide, see the [README](../README.md).

**Contents**

1. [Install](#1-install)
2. [Set up your identity (user config)](#2-set-up-your-identity-user-config)
3. [Join or create a team (team config)](#3-join-or-create-a-team-team-config)
4. [Configure your pipeline](#4-configure-your-pipeline)
5. [Connect integrations (optional)](#5-connect-integrations-optional)
6. [Dashboard (TUI)](#6-dashboard-tui)
7. [Command interface](#7-command-interface)
8. [Building with a coding agent](#8-building-with-a-coding-agent)
9. [Command reference](#9-command-reference)
10. [Configuration reference](#10-configuration-reference)
11. [Troubleshooting](#11-troubleshooting)

---

## 1. Install

Pick whichever suits you (see the [README](../README.md#install) for all options):

```bash
# Homebrew
brew install aaronl1011/tap/spec

# Go (requires Go 1.25+)
go install github.com/aaronl1011/spec@latest
```

Verify:

```bash
spec version
spec --help
```

Optionally install shell completions and man pages:

```bash
spec completion zsh   > "${fpath[1]}/_spec"   # or use: make install-completions
man spec                                       # after: make install-man
```

---

## 2. Set up your identity (user config)

Run the personal wizard once. It writes `~/.spec/config.yaml` — your name, role,
handle, and editor. This file is **never committed**.

```bash
spec config init --user
```

You'll be prompted for:

| Prompt | Notes |
|---|---|
| **Name** | Your full name |
| **Role** | One of `pm`, `tl`, `designer`, `qa`, `engineer` — drives your queues and section ownership |
| **Handle** | Your comms handle — how `spec` addresses and matches you in the configured comms tool. The format is **platform-specific**: Slack/Discord use an `@`-mention (`@jdoe`), while **Teams identifies users by their organisation email** (their Microsoft 365 / Azure AD address, e.g. `jdoe@acme.com`) |
| **Editor** | Defaults to `$EDITOR`, falls back to `vi` |

Confirm it took:

```bash
spec whoami
```

Your role determines which specs land in your queue (`spec list`) and which spec
sections you're allowed to edit during sync. Need to act as another role for a
single command? Use `--role tl` — but keep it to admin checks, not daily work.

---

## 3. Join or create a team (team config)

The team config (`spec.config.yaml`) defines the team name, the canonical specs
repo, integrations, and the pipeline. It is **committed to the specs repo** so the
whole team shares one source of truth.

### Joining an existing team

If your team already has a specs repo containing `spec.config.yaml`:

```bash
export SPEC_GITHUB_TOKEN="ghp_..."     # token with read access to the specs repo
spec join acme/specs
```

`join` accepts several reference formats:

```bash
spec join acme/specs                   # GitHub (default)
spec join github.com/acme/specs        # explicit provider
spec join gitlab.com/acme/specs        # GitLab
spec join --branch develop acme/specs  # non-default branch
```

The repo is cloned to `~/.spec/repos/<owner>/<repo>/` and every subsequent command
uses it automatically. (GitLab/Bitbucket use `SPEC_GITLAB_TOKEN` /
`SPEC_BITBUCKET_TOKEN`, or pass `--token`.)

### Setting up a new team

From inside your specs repo (or wherever you'll manage specs):

```bash
spec config init
```

The wizard asks you to pick a pipeline preset, then prompts for team name, cycle
label, and the specs repo location. It writes `spec.config.yaml`. Edit the
`integrations` block to connect your tools, then commit the file.

### Verify everything

```bash
spec whoami        # resolved identity
spec config test   # checks user config, team config, and each integration
spec list          # what's waiting for you
```

---

## 4. Configure your pipeline

The pipeline is the heart of `spec` — the stages every spec moves through, the
gates that must pass to advance, and the effects that fire on transition.

### Start from a preset

```bash
spec config init --preset startup
```

| Preset | Best for | Stages |
|---|---|---|
| `minimal` | Solo / tiny teams | triage → draft → build → review → done |
| `startup` | Fast-moving product teams | triage → draft → review → build → pr_review → done |
| `product` | Full teams with design & QA | triage → draft → review → design → engineering → build → pr_review → qa → done |
| `platform` | Infrastructure teams | triage → draft → review → rfc → build → pr_review → security → done → rollout → monitoring |
| `kanban` | Continuous flow | backlog → ready → in_progress → review → done |

### Inspect and edit

```bash
spec pipeline               # current pipeline (compact)
spec pipeline --verbose     # show gates and effects
spec pipeline presets       # list presets with details
spec pipeline add security_review --after build --owner security --icon 🔒
spec pipeline remove design
spec pipeline edit review
spec pipeline validate      # check owners, gate expressions, references
spec pipeline export        # print resolved config as YAML
```

### Customise in `spec.config.yaml`

Use a preset, tweak it, or define stages from scratch:

```yaml
pipeline:
  preset: startup
  skip: [design]                 # drop stages you don't need
  stages:                        # add or override stages
    - name: security_review
      owner: security            # pm | tl | designer | qa | engineer | security | anyone | author
      icon: 🔒
      skip_when: "'internal' in spec.labels"
      gates:
        - section_not_empty: acceptance_criteria
        - expr: "decisions.unresolved == 0"
          message: "Resolve all open decisions before advancing"
      transitions:
        advance:
          effects:
            - notify: "@security-team"
            - sync: outbound
```

**Gates** (must be true to advance):

| Gate | Meaning |
|---|---|
| `section_not_empty: <slug>` | A section has content |
| `pr_stack_exists: true` | A PR stack plan exists in the technical section |
| `prs_approved: true` | All created PRs are approved |
| `expr: "<expression>"` | Custom condition (see below), with optional `message:` |

Expressions can read `spec.id`, `spec.status`, `spec.labels`, `spec.word_count`,
`spec.time_in_stage`, `spec.revert_count`, `decisions.{total,resolved,unresolved}`,
`acceptance_criteria.items.count`, `pr_stack.{exists,total}`, and
`prs.{total,approved,merged}`.

**Effects** (run on transition): `notify: <target>`, `sync: outbound|inbound`,
`log_decision: "<msg>"`, `archive: true`, `webhook: { url }`, `trigger: <pipeline>`.
Messages expand `$spec_id`, `$spec_title`, `$from_stage`, `$to_stage`, `$user`,
`$author`.

**Conditional stages**: attach `skip_when: "<expression>"` to a stage to skip it
for specs the expression matches. Always run `spec pipeline validate` after editing.

---

## 5. Connect integrations (optional)

Every integration is optional. Omit a category or set `provider: none` and `spec`
uses a noop adapter — it works fully with zero integrations as a local spec
lifecycle manager. Tokens use `${VAR}` interpolation, resolved from your shell.

```yaml
integrations:
  comms:   { provider: slack }            # slack | teams | discord | none
  pm:      { provider: jira }             # jira | linear | github-issues | none
  docs:    { provider: confluence }       # confluence | notion | none
  repo:    { provider: github }           # github | gitlab | bitbucket
  agent:   { provider: claude-code }      # claude-code | cursor | copilot | none
  ai:                                      # anthropic | openai | ollama | none
    provider: anthropic
    model: claude-sonnet-4-20250514
    token: ${AI_API_KEY}
  deploy:  { provider: github-actions }   # github-actions | gitlab-ci | argocd | none
```

Set the secrets in your shell (add them to `~/.bashrc` / `~/.zshrc` to persist):

```bash
export SPEC_GITHUB_TOKEN="ghp_..."
export AI_API_KEY="sk-ant-..."
```

Then confirm:

```bash
spec config test
```

---

## 6. Dashboard (TUI)

Running `spec` with no arguments opens the interactive dashboard — a persistent,
auto-refreshing terminal app for reading specs, triaging intake, changing stages,
reviewing work, and starting builds.

```bash
spec                  # open the dashboard
```

In pipes and CI it falls back to a static render; force the static view anywhere
with `spec --static`. Non-dashboard commands also print a one-line awareness hint
when items are pending, for example `⚠ 1 pending · run 'spec' for details`.

### Tabs

Press `1`–`6` to jump to a tab, or `tab` / `shift+tab` to cycle. Press `enter` on a
spec to drill into a readable detail view; `esc` goes back.

| Key | Tab | Shows |
|---|---|---|
| `1` | **Dashboard** | Your prioritised DO / REVIEW / INCOMING / BLOCKED items |
| `2` | **Pipeline** | Every spec grouped by stage |
| `3` | **Specs** | Searchable list of active (and archived) specs |
| `4` | **Triage** | Open triage items |
| `5` | **Reviews** | PRs and plan reviews awaiting you |
| `6` | **Settings** | Edit name, role, theme, refresh interval live |

### Spec actions

Select a spec in any list to run lifecycle actions inline:

| Key | Action | Notes |
|---|---|---|
| `a` | advance | validates gates · confirm modal |
| `v` | revert | |
| `e` | edit in `$EDITOR` | |
| `b` | start/resume build | hands context to the coding agent |
| `x` | toggle block | confirm modal |
| `u` | unblock | confirm modal |
| `f` | toggle focus (★) | single key, toggles on/off |
| `c` | record a decision | |
| `p` | push local edits | |
| `s` | sync with docs | |
| `y` | copy spec ID | |
| `o` | open in browser | Reviews tab only |
| `g a` | archive | confirm modal |
| `g r` | restore | confirm modal |

**Creation:** `n` new spec · `i` new triage item · `g s` standup

### Triage in the dashboard

The **Triage** tab handles intake items. Press `enter` (or `space`) on an item to
open its detail view — title, severity, source, history, and notes — then act on
it inline. Actions are gated on your configured role:

| Key | Action | Who |
|---|---|---|
| `enter` / `space` | open detail view | everyone |
| `n` | add a note | everyone |
| `e` | edit (title, priority, source, body) | pm · engineer |
| `c` | close | pm · engineer |
| `x` | escalate / de-escalate | pm · engineer |
| `p` | promote to a full spec | pm |

The edit form supports field navigation and cursor movement: `tab` moves between
fields, arrow keys move the cursor, `enter` cycles priority, `ctrl+s` saves, and
`esc` cancels.

### Global keys

`?` help (context-aware) · `/` search · `r` refresh · `esc` back / arm exit ·
`esc esc` quit · `ctrl+c` hard quit

> Destructive actions (`a` advance, `x` block, `c` close, `g a` archive, `g r`
> restore) show a confirm modal — press `enter` to confirm or `esc` to cancel. `esc`
> always goes back one level; pressing it twice at a top-level tab quits the app.

The focused spec is marked with a ★ across list views and persists between sessions.
Settings you change in the **Settings** tab (name, role, theme, refresh interval)
apply live and are written straight to your user config.

---

## 7. Command interface

Every dashboard action is also available as a command. Use commands when scripting
a step, integrating with CI or a git hook, or working directly from the shell. The
dashboard and commands share state: a `spec focus` set in one interface is visible
in the other.

`spec focus` a spec once and you can drop the ID from almost every command below.

### Start your day

```bash
spec                  # open the dashboard (default interface)
spec list --mine      # specs you own
```

### Pick up a spec

```bash
spec focus SPEC-042   # set working context (persists across sessions)
spec status           # pipeline position + section completion
spec pull             # fetch the spec into the current service repo's .spec/
```

### Plan the work (engineer)

When a spec reaches your engineering/build stage, add a technical plan of steps:

```bash
spec plan add SPEC-042 "Add token-bucket limiter" --repo api-service
spec plan add SPEC-042 "Write integration tests"  --repo api-service
spec plan                       # review the plan
spec plan edit                  # edit steps in $EDITOR
spec plan ready                 # request TL review
```

The TL reviews:

```bash
spec review SPEC-042 --plan
spec review SPEC-042 --plan --approve
spec review SPEC-042 --plan --request-changes --feedback "Step 2 needs detail"
```

### Execute the build

```bash
spec steps                      # view steps and progress
spec steps next                 # details of the next step (branch name, repo, path)
spec steps start                # start the next step
spec build                      # launch the coding agent with full context
spec do                         # resume where you left off
spec steps complete --pr 123    # mark the step done, link its PR
spec steps block 2 "waiting on auth service deploy"
spec steps unblock 2
```

### Capture decisions

```bash
spec decide --question "JWT or session tokens?"
spec decide --list
spec decide --resolve 1 --decision "JWT for stateless scaling"
```

### Move it through the pipeline

```bash
spec validate                   # dry-run the current stage's gates
spec advance                    # advance (validates gates, runs effects)
spec advance --dry-run          # preview effects without moving
spec revert --to design --reason "AC incomplete"
spec eject --reason "blocked on vendor"   # → blocked
spec resume                     # unblock
```

### Fast-track small fixes

If enabled in team config, skip the ceremony for small bugs:

```bash
spec fix "Login button unresponsive on mobile" --label bug
```

This creates a spec that starts at `build` with a minimal template.

### Wrap up

```bash
spec review                     # post a structured review request with stacked PRs
spec deploy --env production     # trigger deployment (if a deploy adapter is set)
spec standup                    # auto-generated standup from real activity
spec focus --clear              # clear focus when you're done
```

---

## 8. Building with a coding agent

`spec build` and `spec do` hand a coding agent (Claude Code, Cursor, Copilot, …)
structured context — the spec, the PR stack plan, prior diffs, and conventions —
over an MCP server or a consolidated context file.

```bash
spec focus SPEC-042
spec build      # creates branches, assembles context, starts the agent
spec do         # resume; checks current branch → focus → most recent session
```

Under the hood the build engine reads the PR Stack Plan, creates branches like
`spec-042/step-1-token-bucket`, assembles context, starts an MCP server for
MCP-capable agents, and records decisions and step completions in real time.

For MCP-compatible agents, add to `.mcp.json`:

```json
{
  "mcpServers": {
    "spec": { "command": "spec", "args": ["mcp-server"] }
  }
}
```

### AI drafting (optional)

When an `ai` integration is configured, AI generates content for human review —
it never writes to a spec directly (every draft goes through accept / edit / skip):

```bash
spec draft --section problem_statement
spec draft --pr-stack
spec draft --pr
```

---

## 9. Command reference

Use this section when scripting or when you need the exact command and flags. Most
daily actions also have a dashboard keybinding.

`[id]` means the command uses the focused spec when the ID is omitted.

### Daily driver

| Command | Description |
|---|---|
| `spec` | Interactive dashboard (TUI) |
| `spec --static` | Static dashboard render (scripts / CI) |
| `spec focus [id]` | Set (or `--clear`) the focused spec |
| `spec do [id]` | Resume work with full context |
| `spec standup` | Auto-generated standup from real activity |
| `spec watch` | Live-updating pipeline dashboard |

### Intake & lifecycle

| Command | Description |
|---|---|
| `spec intake "title"` | Create a triage item (`--source`, `--priority`) |
| `spec promote <triage-id>` | Promote a triage item to a full spec |
| `spec new --title "…"` | Scaffold a new spec |
| `spec fix <title>` | Fast-track bug fix (`--label`) |
| `spec advance [id]` | Advance to next stage (validates gates) |
| `spec revert [id] --to <stage> --reason "…"` | Send back to an earlier stage |
| `spec eject [id] --reason "…"` | Escape hatch → blocked |
| `spec resume [id]` | Return a blocked spec to its pre-block stage |
| `spec validate [id]` | Dry-run the current stage's gates |
| `spec status [id]` | Pipeline position + section completion |
| `spec list` / `--all` / `--mine` / `--triage` | List specs by queue / stage / owner |
| `spec history` | Recently completed specs |

### Collaboration & knowledge

| Command | Description |
|---|---|
| `spec pull [id]` | Fetch spec into the current repo's `.spec/` |
| `spec push [id]` | Commit & push local spec edits to the specs repo |
| `spec sync [id]` | Bidirectional, section-scoped sync with the docs provider |
| `spec link [id] --section <s> --url <url>` | Attach a resource link |
| `spec edit [id]` | Open in `$EDITOR` (or print the docs URL) |
| `spec decide [id]` | Manage the decision log (`--question`, `--resolve`, `--list`) |
| `spec search "query"` | Full-text search across active + archived specs |
| `spec context "question"` | Keyword search across specs and decisions |

### Planning, build & review

| Command | Description |
|---|---|
| `spec plan [id]` / `plan edit` / `plan add <desc>` / `plan ready` | Manage the build plan |
| `spec steps [id]` / `next` / `start` / `complete` / `block` / `unblock` | Manage build steps |
| `spec build [id]` | Start/resume the build with agent context |
| `spec review [id]` | Post a structured review request (`--plan`, `--approve`, …) |
| `spec deploy [id] [--env …]` | Trigger deployment via the deploy adapter |
| `spec mcp-server [--spec <id>]` | Run a standalone MCP server |
| `spec draft [id] --section/--pr/--pr-stack` | AI drafting |

### Pipeline, metrics & config

| Command | Description |
|---|---|
| `spec pipeline` / `presets` / `add` / `remove` / `edit` / `validate` / `export` | Manage the pipeline |
| `spec metrics` | Quantitative pipeline health |
| `spec retro` | Auto-populate the retrospective with cycle metrics |
| `spec whoami` | Resolved identity and config source |
| `spec join <repo>` | Join a team by cloning its specs repo |
| `spec config init` / `init --user` / `test` | Configuration wizards & validation |
| `spec completion <shell>` | Generate shell completion scripts |

Run `spec <command> --help` for full flags on any command.

---

## 10. Configuration reference

### Team config — `spec.config.yaml` (committed)

```yaml
version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 7"

specs_repo:
  provider: github          # github | gitlab | bitbucket
  owner: acme
  repo: specs
  branch: main
  token: ${SPEC_GITHUB_TOKEN}

integrations:
  comms:  { provider: none }
  pm:     { provider: none }
  docs:   { provider: none }
  repo:   { provider: github, owner: acme, token: ${SPEC_GITHUB_TOKEN} }
  agent:  { provider: none }
  ai:     { provider: none }
  deploy: { provider: none }

sync:
  outbound_on_advance: true
  conflict_strategy: warn   # warn | abort | force | skip

archive:
  directory: archive

dashboard:
  stale_threshold: 48h
  refresh_ttl: 300

pipeline:
  preset: startup           # or define `stages:` directly

# Optional: enable fast-track fixes
fast_track:
  enabled: true
  allowed_roles: [engineer, tl]
  require_labels: [bug, hotfix]
  max_duration: "2d"
```

### User config — `~/.spec/config.yaml` (never committed)

```yaml
user:
  owner_role: engineer       # pm | tl | designer | qa | engineer
  name: "Jane Doe"
  handle: "@jdoe"

preferences:
  editor: $EDITOR
  ai_drafts: true
  standup_auto_post: false
  theme: auto                # auto | catppuccin-mocha | gruvbox-dark | dracula |
                             # tokyo-night | nord | solarized-dark | rose-pine | …
  refresh_interval: 30s      # TUI auto-refresh
  mouse: false               # enable mouse support in the TUI
  multiplexer: none          # tmux | zellij | wezterm | iterm2 | none
  passive_awareness:
    during_build: false      # don't interrupt flow during spec build/do
    dismiss_duration: 2h

# Map repo names to local paths for cross-repo build navigation
workspaces:
  api-service: ~/code/api-service
  web-app: ~/code/web-app
```

### Storage model

```
specs repo (canonical)          ~/.spec/ (local state)
├── SPEC-042.md                 ├── config.yaml      user identity
├── triage/TRIAGE-088.md        ├── spec.db          SQLite: cache, sessions, activity
├── archive/SPEC-001.md         ├── repos/<owner>/<repo>/   specs-repo clone
├── templates/                  └── sessions/<id>/   build session state
└── spec.config.yaml
```

The specs repo is the single source of truth. `spec pull` copies a spec into a
service repo's `.spec/` for local agent context. Specs are not tied to a single
service repo — cross-repo work is the default.

---

## 11. Troubleshooting

| Problem | Fix |
|---|---|
| `no access token` | `export SPEC_GITHUB_TOKEN=…` (or the provider-specific variable) |
| `no role configured` | `spec config init --user` |
| `team config not found` | `spec join <org/repo>`, or `spec config init` to create one |
| `command not found: spec` | Ensure your Go bin dir (or `~/.local/bin`) is on `$PATH` |
| Integration not picked up | Check `spec config test`; confirm the `${VAR}` is exported |
| Can't advance a spec | `spec validate` shows which gate is failing |
| Editing the wrong section | Your `owner_role` must match the section's `<!-- owner: role -->` |

Get help anywhere:

```bash
spec <command> --help
spec config test
spec pipeline --verbose
```

