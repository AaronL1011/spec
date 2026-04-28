# spec — The End-Game Developer Control Plane

`spec` was built for flow. Born from a desire for liberation from tangled webs of project management software - free to solve problems in peace and serenity.

Take back your focus, find a process that really gets shit done.

```
$ spec

Good morning, Aaron.                           engineer · Cycle 7

─── DO ──────────────────────────────────────────────────────────
⚡ SPEC-042  Auth refactor            build          PR 2/4 in progress
⚡ SPEC-039  Rate limiting            pr-review      2 unresolved threads

─── REVIEW ──────────────────────────────────────────────────────
📋 PR #418   Search indexing           api-gateway    requested 3h ago

─── INCOMING ────────────────────────────────────────────────────
📨 TRIAGE-088  Billing alerts          triage         high priority

Run 'spec do' to resume SPEC-042. 12 specs in pipeline.
```

## Install

### From source

Requires Go 1.25+.

```bash
go install github.com/aaronl1011/spec-cli@latest
```

### Build from this repo

```bash
git clone https://github.com/aaronl1011/spec-cli.git
cd spec-cli
make build
# Binary is at ./bin/spec
```

### Prebuilt binaries

Download from [GitHub Releases](https://github.com/aaronl1011/spec-cli/releases) for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64.

### Homebrew

```bash
brew install aaronl1011/tap/spec
```

## Quick Start

### 1. Set up your identity

```bash
spec config init --user
```

This creates `~/.spec/config.yaml` with your name, role, and preferences. You only do this once.

### 2. Join your team (or set up a new one)

**Joining an existing team:**

If your team already has a specs repo with `spec.config.yaml`:

```bash
export GITHUB_TOKEN=ghp_...
spec join acme/specs
```

This clones the specs repo and configures your local environment automatically.

**Setting up a new team:**

In your specs repo (or wherever you want to manage specs):

```bash
spec config init
```

This creates `spec.config.yaml` with your team name, specs repo location, pipeline stages, and integration placeholders. Commit this file to your specs repo.

### 3. Verify

```bash
spec whoami
spec config test
```

### 4. Create your first spec

```bash
spec new --title "Auth token expiration fix"
```

This scaffolds a `SPEC-001.md` in your specs repo with all required sections, auto-assigned ID, and a notification to your team.

### 5. Start working

```bash
spec list              # What's waiting for me?
spec focus SPEC-001    # Set your working context
spec advance           # Move it through the pipeline
spec pull              # Fetch into my service repo
spec build             # Start the build with agent context
spec do                # Resume where you left off
```

Once you `spec focus` a spec, most commands infer the ID automatically — no need to repeat it.

## How It Works

### Focus Mode

Most `spec` commands operate on a single spec. Instead of typing the ID every time, set a **focused spec**:

```bash
spec focus SPEC-042        # Set focus
spec status                # → shows SPEC-042
spec advance               # → advances SPEC-042
spec build                  # → builds SPEC-042
spec focus --clear         # Clear when done
```

The focus persists across terminal sessions. Passing an explicit ID (e.g. `spec status SPEC-007`) overrides the focus for that one command.

`spec do` is even smarter — it checks your current branch first, then falls back to the focus, then to your most recent build session.

### The Spec

A `SPEC.md` is a structured markdown document with YAML frontmatter and role-scoped sections:

```markdown
---
id: SPEC-042
title: Auth refactor
status: build
author: Aaron Lewis
cycle: Cycle 7
repos: [auth-service, api-gateway]
revert_count: 0
---

# SPEC-042 — Auth refactor

## Decision Log
| # | Question / Decision | Options | Decision | Rationale | By | Date |
|---|---|---|---|---|---|---|

## 1. Problem Statement           <!-- owner: pm -->
## 2. Goals & Non-Goals           <!-- owner: pm -->
## 3. User Stories                <!-- owner: pm -->
## 4. Proposed Solution           <!-- owner: pm -->
## 5. Design Inputs               <!-- owner: designer -->
## 6. Acceptance Criteria         <!-- owner: qa -->
## 7. Technical Implementation    <!-- owner: engineer -->
## 8. Escape Hatch Log            <!-- auto: spec eject -->
## 9. QA Validation Notes         <!-- owner: qa -->
## 10. Deployment Notes           <!-- owner: engineer -->
## 11. Retrospective              <!-- auto: spec retro -->
```

The `<!-- owner: role -->` markers define who can write to each section. This powers section-scoped sync (a PM edits §1–4 in Confluence, an engineer edits §7 in the terminal) and gate validation (can't advance past design if §5 is empty).

### The Pipeline

Specs flow through configurable stages. Each stage has an owner role, gates, and transition effects.

**Start with a preset:**

```bash
spec config init --preset startup
```

**Or customize your pipeline:**

```yaml
pipeline:
  preset: startup
  skip: [design]           # Remove stages you don't need
  stages:
    - name: security_review
      owner: security
      skip_when: "'internal' in spec.labels"
      gates:
        - expr: "decisions.unresolved == 0"
          message: "All decisions must be resolved"
      transitions:
        advance:
          effects:
            - notify: "@security-team"
```

**Available presets:** `minimal`, `startup`, `product`, `platform`, `kanban`

**Pipeline commands:**

```bash
spec pipeline              # View current pipeline
spec pipeline presets      # List all presets
spec pipeline add          # Add a stage interactively
spec pipeline validate     # Check for errors
spec advance --dry-run     # Preview transition effects
```

📖 **[Full pipeline documentation →](docs/pipelines.md)**

📖 **[Engineer workflow guide →](docs/engineer-workflow.md)**

📖 **[Small-team pilot runbook →](docs/pilot-runbook.md)**

**Transitions:**

- **Forward** (`spec advance`) — validates gates, runs effects, notifies next owner
- **Backward** (`spec revert --to <stage> --reason "..."`) — requires reason, notifies owners
- **Escape hatch** (`spec eject --reason "..."`) — moves to `blocked`
- **Fast-track** (`spec advance <id> --to done`) — TL-only, skips intermediate stages

### The Dashboard

Running `spec` with no arguments shows the personal dashboard, a prioritised view of specs, PRs, and notifications aggregated from configured integrations.

Every other `spec` command prints a passive awareness line when items are pending:

```
$ spec build
⚠ 1 pending · run 'spec' for details

Building SPEC-042...   # Uses focused spec
```

### Build Orchestration

`spec build` and `spec do` provide structured context to coding agents (Claude Code, Cursor, Copilot, etc.) via an MCP server or consolidated context file:

```bash
spec focus SPEC-042   # Set your working context
spec build            # Start the build — branches, context, agent
spec do               # Resume where you left off
```

Once focused, you can omit the spec ID from most commands. Passing an explicit ID overrides the focus for that invocation.

The build engine:
1. Reads the PR Stack Plan from §7.3
2. Creates branches (`spec-042/step-1-token-bucket`)
3. Assembles context (spec + prior diffs + conventions)
4. Starts an MCP server (for MCP-compatible agents)
5. Spawns the agent — it takes over the terminal
6. Records decisions and step completions in real time

For agents that support MCP (Claude Code, Cursor), add to `.mcp.json`:

```json
{
  "mcpServers": {
    "spec": {
      "command": "spec",
      "args": ["mcp-server"]
    }
  }
}
```

### AI Drafting

AI is a progressive enhancement — every feature works without it. When configured, `spec draft` generates content for human review:

```bash
spec draft --section problem_statement   # Draft a section
spec draft --pr-stack                     # Propose a PR decomposition
spec draft --pr                           # Generate a PR description
```

(These commands use the focused spec. Pass an explicit ID to override.)

Every draft goes through **accept / edit / skip** — AI never writes directly to a spec.

## Commands

### Daily driver

| Command | Description |
|---|---|
| `spec` | Personal dashboard — everything awaiting your attention |
| `spec focus [id]` | Set (or clear with `--clear`) the focused spec |
| `spec do [id]` | Resume work with full context |
| `spec standup` | Auto-generated standup from real activity |

### Intake

| Command | Description |
|---|---|
| `spec intake "title"` | Create a triage item (`--source`, `--priority`) |
| `spec promote <triage-id>` | Promote to a full spec |

### Spec lifecycle

| Command | Description |
|---|---|
| `spec new --title "..."` | Scaffold a new spec |
| `spec advance [id]` | Advance to next stage (validates gates) |
| `spec revert [id] --to <stage> --reason "..."` | Send back to a previous stage |
| `spec eject [id] --reason "..."` | Escape hatch → blocked |
| `spec resume [id]` | Unblock |
| `spec validate [id]` | Dry-run gate checks |
| `spec status [id]` | Pipeline position + section completion |
| `spec list` | Specs awaiting your action |
| `spec list --all` | Full pipeline grouped by stage |
| `spec list --mine` | Specs you own |
| `spec list --triage` | Open triage items |

*Commands marked `[id]` use the focused spec when omitted.*

### Collaboration

| Command | Description |
|---|---|
| `spec pull [id]` | Fetch spec to local `.spec/` directory |
| `spec sync [id]` | Bidirectional sync with docs provider |
| `spec link [id] --section <s> --url <url>` | Attach a resource link |
| `spec edit [id]` | Open in `$EDITOR` |
| `spec decide [id] --question "..."` | Add to decision log |
| `spec decide [id] --resolve N --decision "..."` | Resolve a decision |
| `spec decide [id] --list` | View decision log |

### AI drafting

| Command | Description |
|---|---|
| `spec draft [id] --section <slug>` | Draft a spec section |
| `spec draft [id] --pr` | Draft a PR description |
| `spec draft [id] --pr-stack` | Propose a PR stack plan |

### Technical planning

| Command | Description |
|---|---|
| `spec plan [id]` | View build plan |
| `spec plan edit [id]` | Edit plan in `$EDITOR` |
| `spec plan add [id] <desc>` | Add a step (`--repo`) |
| `spec plan ready [id]` | Request plan review |
| `spec review [id] --plan` | Review technical plan |
| `spec review [id] --plan --approve` | Approve plan |

### Build execution

| Command | Description |
|---|---|
| `spec steps [id]` | View build steps and progress |
| `spec steps next [id]` | Show next step details |
| `spec steps start [id] [n]` | Start working on a step |
| `spec steps complete [id] [n]` | Mark step complete (`--pr N`) |
| `spec steps block [id] [n] <reason>` | Block a step |
| `spec steps unblock [id] [n]` | Unblock a step |

### Build & deploy

| Command | Description |
|---|---|
| `spec build [id]` | Start/resume build with agent context |
| `spec review [id]` | Post structured review request |
| `spec deploy [id] [--env production]` | Trigger deployment |
| `spec fix <title>` | Fast-track bug fix (`--label`) |
| `spec mcp-server [--spec <id>]` | Standalone MCP server |

### Knowledge

| Command | Description |
|---|---|
| `spec search "query"` | Full-text search across all specs |
| `spec context "question"` | Semantic search (keyword fallback without AI) |
| `spec history` | Browse archived specs |

### Pipeline visibility

| Command | Description |
|---|---|
| `spec watch` | Live-updating terminal dashboard |
| `spec retro` | Cycle retrospective with metrics |
| `spec metrics` | Pipeline health numbers |

### Identity & config

| Command | Description |
|---|---|
| `spec whoami` | Your resolved identity |
| `spec join <repo>` | Join an existing team by cloning their specs repo |
| `spec config init` | Team config wizard |
| `spec config init --user` | Personal config wizard |
| `spec config test` | Validate all integrations |

## Configuration

### Team config — `spec.config.yaml`

Committed to the specs repo. Defines team settings, integrations, and the pipeline.

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
  token: ${GITHUB_TOKEN}

integrations:
  comms:
    provider: slack              # slack | teams | discord | none
  pm:
    provider: jira               # jira | linear | github-issues | none
  docs:
    provider: confluence         # confluence | notion | none
  repo:
    provider: github             # github | gitlab | bitbucket
  agent:
    provider: claude-code        # claude-code | cursor | copilot | none
  ai:
    provider: anthropic          # anthropic | openai | ollama | none
    model: claude-sonnet-4-20250514
    token: ${AI_API_KEY}
  deploy:
    provider: github-actions     # github-actions | gitlab-ci | argocd | none

# Pipeline: use a preset or define custom stages
pipeline:
  preset: startup              # minimal | startup | product | platform | kanban
  # Or define stages directly:
  # stages:
  #   - name: triage
  #     owner: pm
  #   - name: build
  #     owner: engineer
  #     gates:
  #       - section_not_empty: acceptance_criteria
  #       - expr: "decisions.unresolved == 0"
  #   - name: done
  #     owner: tl
  # See docs/pipelines.md for full configuration options
```

Every integration is optional. Set `provider: none` or omit it entirely. `spec` works fully with zero integrations — it's a local spec lifecycle manager out of the box.

### User config — `~/.spec/config.yaml`

Personal identity and preferences. Never committed.

```yaml
user:
  owner_role: engineer       # pm | tl | designer | qa | engineer
  name: "Aaron Lewis"
  handle: "@aaron"

preferences:
  editor: $EDITOR
  ai_drafts: true
  standup_auto_post: false
```

### Environment variables

Tokens and secrets use `${VAR}` interpolation in config files. Set them in your shell environment:

```bash
export GITHUB_TOKEN=ghp_...
export AI_API_KEY=sk-ant-...
```

## Storage Model

```
specs repo (canonical)          ~/.spec/ (local state)
├── SPEC-042.md                 ├── config.yaml (user identity)
├── SPEC-043.md                 ├── spec.db (SQLite — cache, sessions, activity)
├── triage/                     ├── repos/ (specs repo clone)
│   └── TRIAGE-088.md           │   └── my-org/specs/
├── archive/                    └── sessions/ (build session state)
│   └── SPEC-001.md                 └── SPEC-042/
├── templates/                           ├── context.md
└── spec.config.yaml                     └── activity.log
```

- The **specs repo** is the single source of truth for all spec content.
- `spec pull` copies specs to service repos for local context (`.spec/SPEC-042.md`).
- `~/.spec/spec.db` stores dashboard cache, build sessions, activity logs, and embeddings.
- Specs are not tied to any single service repo — cross-repo features are the default.

## Adapter Architecture

`spec` uses a config-driven adapter pattern. Engines depend on interfaces, never on concrete implementations. Every integration category has a noop adapter used when unconfigured — no panics, no blocked network calls.

| Category | Interface | Providers |
|---|---|---|
| Comms | `CommsAdapter` | Slack, Teams, Discord |
| PM | `PMAdapter` | Jira, Linear, GitHub Issues |
| Docs | `DocsAdapter` | Confluence, Notion |
| Repo | `RepoAdapter` | GitHub, GitLab, Bitbucket |
| Agent | `AgentAdapter` | Claude Code, Cursor, Copilot |
| AI | `AIAdapter` | Anthropic, OpenAI, Ollama |
| Deploy | `DeployAdapter` | GitHub Actions, GitLab CI, ArgoCD |

To add a new provider, implement the interface in `internal/adapter/<provider>/` and register it in the adapter registry. The engine code doesn't change.

## Development

### Prerequisites

- Go 1.25+
- Git

### Build

```bash
make build          # → ./bin/spec
make install        # → $GOPATH/bin/spec
```

### Test

```bash
make test           # go test ./... -race -count=1
make test-cover     # with coverage report
```

Tests use in-memory SQLite (`:memory:`) and `t.TempDir()` for isolation — no shared state, no external dependencies.

### Lint

```bash
make lint           # go vet + golangci-lint
make vet            # go vet only
```

### Project structure

```
cmd/                    Cobra command definitions (thin — flags + call internal/)
internal/
  config/               Config loading, env var interpolation, resolution chain
  markdown/             Frontmatter R/W, section extraction, decision log, templates
  pipeline/             Stage machine, gates, transitions, role-based access
  git/                  All git operations (only package that shells out to git)
  store/                All SQLite operations (only package that touches the DB)
  adapter/              Interface definitions + noop implementations + registry
  build/                PR stack parser, session state, context assembly, MCP server
  dashboard/            Signal aggregation, cache-first rendering, awareness line
  ai/                   AI service (null-safe), accept/edit/skip flow, prompts
```

### Key architectural rules

- **`cmd/` is thin.** Parse flags, resolve config, call `internal/`. No business logic.
- **Engines depend on interfaces.** Import `internal/adapter`, never `internal/adapter/github`.
- **Only `internal/git/` shells out to git.** No other package calls `exec.Command("git", ...)`.
- **Only `internal/store/` touches SQLite.** No other package opens the DB.
- **No CGo.** The binary is statically linked and cross-compilable (`modernc.org/sqlite`).
- **AI is never required.** Every feature works without an `ai` integration. The AI service returns `("", nil)` when unconfigured; callers always handle this.

### Adding a command

1. Create `cmd/<name>.go` — define the Cobra command, parse flags.
2. Call into `internal/` for all logic.
3. Register with `rootCmd.AddCommand()` in `init()`.

### Adding an adapter

1. Interface is in `internal/adapter/<category>.go`.
2. Create `internal/adapter/<provider>/<category>.go` implementing the interface.
3. Wire it into `cmd/helpers.go` → `buildRegistry()` based on the config provider string.

### Testing guidelines

- Table-driven tests for functions with multiple inputs.
- Golden file tests for the markdown engine.
- Test against interfaces, not implementations.
- Each test creates its own state — `store.OpenMemory()`, `t.TempDir()`.
- Test names describe the scenario: `TestAdvance_GateNotMet_ReturnsError`.

## Versioning & Roadmap

| Version | What ships |
|---|---|
| **v0.1** | Local spec lifecycle — `new`, `list`, `advance`, `decide`, `validate`, `status`, `whoami`, dashboard |
| **v0.2** | Build & AI — `build`, `do`, `draft`, `intake`, `promote`, `pull`, MCP server |
| **v0.3** | Integrations — `sync`, `review`, `link`, comms/docs/repo adapters |
| **v0.4** | Full control plane — `standup`, `watch`, `context`, `retro`, `deploy`, semantic search |

See [SPEC.md](SPEC.md) for the full product specification and PR stack plan.

## License

MIT
