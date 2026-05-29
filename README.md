# spec — The End-Game Developer Control Plane

`spec` was built for flow. Born from a desire for liberation from tangled webs of
project-management software — free to solve problems in peace and serenity.

It unifies spec management, pipeline orchestration, build context, and team
coordination into a single, fast, local-first CLI. Run `spec` with no arguments
and you get an interactive terminal dashboard of everything awaiting your attention.

```
 Dashboard   Pipeline   Specs   Triage   Reviews   Settings

 Good morning, Aaron.                              engineer · Cycle 7

 ─── DO ───────────────────────────────────────────────────────────
 ★ SPEC-042  Auth refactor            build          PR 2/4 in progress
 ⚡ SPEC-039  Rate limiting            pr-review      2 unresolved threads

 ─── REVIEW ───────────────────────────────────────────────────────
 📋 PR #418   Search indexing          api-gateway    requested 3h ago

 ─── INCOMING ─────────────────────────────────────────────────────
 📨 TRIAGE-088  Billing alerts         triage         high priority

 12 specs in pipeline · 1 pending          ↑↓ move · enter open · ? help
```

> **New here?** Jump straight to the **[QUICKSTART guide →](QUICKSTART.md)** to go from
> zero to productive in about 15 minutes.

---

## Why spec?

- **One place for the work.** Specs, pipeline state, decisions, reviews, and build
  context live together — not scattered across five SaaS tabs.
- **A pipeline that fits your team.** Stages, gates, and automated effects are
  config-driven. Start from a preset, customise as you grow.
- **Markdown in git is the source of truth.** No proprietary database, no lock-in.
  A spec is a structured `SPEC-NNN.md` you can read, diff, and review.
- **Local-first and resilient.** Every integration is optional. Unconfigured tools
  use noop adapters — nothing panics, nothing blocks. `spec` works fully offline.
- **Agent-ready.** `spec build` assembles structured context for coding agents
  (Claude Code, Cursor, Copilot) over an MCP server or a context file.
- **AI is a bonus, never a requirement.** Drafting features enhance the flow when
  configured and quietly step aside when they aren't.

---

## Install

### Homebrew

```bash
brew install aaronl1011/tap/spec
```

### Go install

Requires Go 1.25+.

```bash
go install github.com/aaronl1011/spec@latest
```

### Prebuilt binaries

Download from [GitHub Releases](https://github.com/aaronl1011/spec/releases) for
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64.

### Build from source

```bash
git clone https://github.com/aaronl1011/spec.git
cd spec
make build      # → ./bin/spec
```

Verify:

```bash
spec version
spec --help
```

### Shell completions & man pages

```bash
make install              # build + install to ~/.local/bin
make install-completions  # auto-detects bash / zsh / fish
make install-man          # install man pages (spec(1), spec-advance(1), …)
```

---

## The 30-second tour

```bash
spec config init --user        # set up your identity (once)
spec join acme/specs           # join your team's specs repo
spec                           # open the interactive dashboard
spec new --title "Auth fix"    # scaffold a spec
spec focus SPEC-042            # set your working context
spec do                        # resume where you left off
```

Once you `spec focus` a spec, most commands infer the ID automatically — no need to
repeat it. The full walkthrough lives in the **[QUICKSTART guide →](QUICKSTART.md)**.

---

## Core concepts

### The spec

A `SPEC-NNN.md` is a structured markdown document with YAML frontmatter and
role-scoped sections. `<!-- owner: role -->` markers define who can write to each
section, which powers section-scoped sync (a PM edits the problem statement in
Confluence, an engineer edits the technical plan in the terminal) and gate
validation (you can't advance past design if the design inputs are empty).

### The pipeline

Specs flow through configurable stages — each with an owner role, gates that must
pass before advancing, and effects that fire on transition (notify a channel, sync
to docs, log a decision). Start from a preset (`minimal`, `startup`, `product`,
`platform`, `kanban`) and customise from there.

### Focus mode

Most commands operate on a single spec. Set a focused spec once with `spec focus`
and it persists across terminal sessions, so `spec status`, `spec advance`,
`spec build`, and friends all just work. Pass an explicit ID to override for one
command.

### The TUI

Running `spec` in an interactive terminal launches a persistent dashboard with six
tabs — **Dashboard, Pipeline, Specs, Triage, Reviews, Settings** — full keyboard
navigation, drill-down spec reading, and inline actions (advance, block, focus,
build, decide, …). In non-interactive contexts (pipes, CI) it falls back to a
static render; force it with `--static`.

For the complete command reference, configuration schema, and keybindings, see the
**[QUICKSTART guide →](QUICKSTART.md)**.

---

## Architecture

`spec` uses a config-driven adapter pattern. Engines depend on interfaces, never on
concrete implementations. Every integration category has a noop adapter used when
unconfigured.

| Category | Interface | Providers |
|---|---|---|
| Comms | `CommsAdapter` | Slack, Teams, Discord |
| PM | `PMAdapter` | Jira, Linear, GitHub Issues |
| Docs | `DocsAdapter` | Confluence, Notion |
| Repo | `RepoAdapter` | GitHub, GitLab, Bitbucket |
| Agent | `AgentAdapter` | Claude Code, Cursor, Copilot |
| AI | `AIAdapter` | Anthropic, OpenAI, Ollama |
| Deploy | `DeployAdapter` | GitHub Actions, GitLab CI, ArgoCD |

```
cmd/                  Cobra command definitions (thin — flags + call internal/)
internal/
  config/             Config loading, env var interpolation, resolution chain
  markdown/           Frontmatter R/W, section extraction, decision log, templates
  pipeline/           Stage machine, gates, transitions, role-based access
  git/                All git operations (only package that shells out to git)
  store/              All SQLite operations (only package that touches the DB)
  adapter/            Interface definitions + noop implementations + registry
  build/              PR stack parser, session state, context assembly, MCP server
  dashboard/          Signal aggregation, cache-first rendering, awareness line
  tui/                Bubble Tea TUI — views, components, keymap, themes
  ai/                 AI service (null-safe), accept/edit/skip flow, prompts
```

The single source of truth is the **specs repo** (canonical markdown). Local state
lives under `~/.spec/` (user config, a SQLite cache of dashboard/sessions/activity,
and the specs-repo clone).

---

## Development

### Prerequisites

- Go 1.25+
- Git

### Common tasks

```bash
make build        # → ./bin/spec
make install      # → $BINDIR/spec (default ~/.local/bin)
make test         # go test ./... -race -count=1
make test-cover   # coverage report → coverage.html
make lint         # go vet + golangci-lint
make fmt          # gofmt -s -w .
make docs         # regenerate man pages into docs/man/
```

### Architectural rules

These are enforced by convention and reviewed in every PR (see
[`AGENTS.md`](AGENTS.md) for the full standard):

- **`cmd/` is thin.** Parse flags, resolve config, call `internal/`. No business logic.
- **Engines depend on interfaces.** Import `internal/adapter`, never `internal/adapter/github`.
- **Only `internal/git/` shells out to git.** No other package calls `exec.Command("git", …)`.
- **Only `internal/store/` touches SQLite.** No other package opens the DB.
- **No CGo.** The binary is statically linked and cross-compilable (`modernc.org/sqlite`).
- **AI is never required.** Every feature works without an `ai` integration; the AI
  service returns `("", nil)` when unconfigured and callers always handle that.

### Testing guidelines

- Table-driven tests for functions with multiple interesting inputs.
- Golden file tests for the markdown engine.
- Test against interfaces, not implementations.
- Each test creates its own state — `store.OpenMemory()`, `t.TempDir()`. No shared fixtures.
- Test names describe the scenario: `TestAdvance_GateNotMet_ReturnsError`.

---

## Contributing

Contributions are welcome. To keep the codebase coherent:

1. **Read [`AGENTS.md`](AGENTS.md)** — it defines the Go standards, naming, error
   handling, and design principles (KISS, loose coupling, robustness) we hold to.
2. **Adding a command:** create `cmd/<name>.go` (flags only), call into `internal/`
   for all logic, and register it with `rootCmd.AddCommand()` in `init()`.
3. **Adding an adapter:** implement the category interface from
   `internal/adapter/<category>.go` in `internal/adapter/<provider>/`, then wire it
   into the registry by provider string — engine code never changes.
4. **Commits:** use [Conventional Commits](https://www.conventionalcommits.org/)
   (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`). One logical change
   per commit.
5. **Before opening a PR:** run `make fmt lint test` clean. PR descriptions should
   reference the spec (`Implements US-12` / `Addresses §7.9`).

The full product specification and PR stack plan live in [`SPEC.md`](SPEC.md).

---

## Project layout & further reading

| Document | Purpose |
|---|---|
| **[QUICKSTART.md](QUICKSTART.md)** | Setup, configuration, and day-to-day usage |
| [SPEC.md](SPEC.md) | Full product specification and PR stack plan |
| [AGENTS.md](AGENTS.md) | Coding standards for contributors and AI agents |
| [CHANGELOG.md](CHANGELOG.md) | Release history |
| `docs/man/` | Generated man pages (`make docs` to regenerate) |

---

## License

[MIT](LICENSE)
