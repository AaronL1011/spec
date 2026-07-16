# spec — developer workflow in the terminal

`spec` is a terminal control plane for turning an idea into reviewed,
implemented work.

It keeps specifications, pipeline state, decisions, review threads, build
plans, and coding-agent context together. Markdown in Git remains the source
of truth; the interactive terminal UI is where people read and move the work.

```bash
brew install aaronl1011/tap/spec
spec
```

On first run, `spec` opens an onboarding wizard. It collects your identity,
connects you to your team's specs repository, and then opens the dashboard —
no config files to find or commands to memorise first.

![Demonstration of a spec review](docs/demos/demo.gif)

> New here? Follow the **[TUI-first quickstart](docs/QUICKSTART.md)**.

---

## What spec gives you

- **A daily terminal workspace.** Run `spec` to see what to do, what needs
  review, what is incoming, and what is blocked.
- **A readable review cockpit.** Move through a spec section by section,
  traverse every discussion thread, anchor comments to exact blocks, filter
  review work, and track unread replies.
- **A configurable delivery pipeline.** Teams define stages, ownership,
  advancement gates, warnings, and transition effects.
- **Git-backed specifications.** Specs are structured Markdown, reviewable in
  normal Git tooling and usable without a hosted spec database.
- **Integrated build context.** Build plans become a dependency graph that
  `spec build` can hand to Pi or Claude Code through MCP.
- **Progressive enhancement.** Git is enough to start. Jira, Confluence,
  GitHub, Slack, Teams, deployment, and AI integrations are optional.
- **A scriptable CLI when you need it.** TUI actions have command equivalents
  for automation, CI, hooks, and advanced workflows.

---

## Install

### Homebrew

```bash
brew install aaronl1011/tap/spec
```

### Go

Requires Go 1.25.8 or newer.

```bash
go install github.com/aaronl1011/spec@latest
```

### Prebuilt binaries

Download a release for Linux, macOS, or Windows from
[GitHub Releases](https://github.com/aaronl1011/spec/releases).

### Build from source

```bash
git clone https://github.com/aaronl1011/spec.git
cd spec
make build                 # writes ./bin/spec
```

Verify the install:

```bash
spec version
```

---

## Start here: run `spec`

```bash
spec
```

If this is your first run, the wizard guides you through two steps:

1. **Identity** — name, role, and an optional stable spec handle.
2. **Team** — join an existing specs repository using `org/repo` or a full
   GitHub, GitLab, or Bitbucket URL.

Set the provider token before starting, or paste it into the password field:

```bash
export SPEC_GITHUB_TOKEN="..."     # or SPEC_GITLAB_TOKEN / SPEC_BITBUCKET_TOKEN
spec
```

After joining, the wizard opens the dashboard directly.

Creating a brand-new team is an administrative path. Choose **Create a new
team** in onboarding, then run `spec config init` in the repository that will
hold `spec.config.yaml`. See [Configuration](docs/CONFIGURATION.md).

### Your first minute in the TUI

```text
j / k or arrows   move
enter             open the selected item
1 … 7             Dashboard, Pipeline, Specs, Triage, Reviews, Security, Settings
?                 help for the current screen
/                 search specs
esc               go back; twice at top level exits
```

A useful first pass:

1. Press `3` to browse all specs.
2. Select a spec and press `enter` for its overview.
3. Press `o` to open the reader.
4. Use `]` / `[` for sections and `n` / `p` for discussion threads.
5. Press `esc` to return, then `1` for your personal dashboard.

See the full [TUI guide](docs/TUI.md) for lifecycle actions, triage, the review
cockpit, and Settings.

---

## The workflow

### Capture work

Use `i` in the TUI for a lightweight triage item, or `n` for a full spec. A PM
can promote a triage item later with `p` from its detail pane.

### Shape and review the spec

Specs move through the team's configured pipeline. Ownership and gates make the
next action visible without hiding the underlying document.

In the reader:

- `A` selects a paragraph, list item, table row, or code block and asks a
  precisely anchored question;
- `n` / `p` traverse matching threads across the whole document;
- `r`, `x`, and `u` reply, resolve, or preserve something as unread;
- `f` cycles open, all, mine, and unread views.

### Plan and build

Engineers add build steps and dependencies to the technical plan. `b` in the
TUI, or `spec build`, validates the plan and launches the configured coding
agent with deterministic spec and repository context.

### Advance with confidence

`a` advances the selected spec only after its current gates pass. Configured
effects can publish documentation, update Jira, notify a channel, or trigger a
webhook. Integration failures degrade without corrupting pipeline state.

---

## TUI and CLI: clear responsibilities

**Use the TUI for human workflow:** onboarding, finding work, reading specs,
triage, discussion, lifecycle actions, builds, and personal settings.

**Use commands for explicit or automated tasks:** scripts, CI, configuration
administration, detailed build-plan editing, integration preflight, and MCP.

Once a spec is focused, commands can usually omit its ID:

```bash
spec focus SPEC-042
spec status
spec validate
spec build --check
spec do
```

The TUI and CLI share the same Git-backed specs, local focus, read-state, and
build ledger.

---

## Core concepts

### Specs repository

The team repository contains the shared config at its root and the documents
under `specs/`:

```text
spec.config.yaml
specs/
├── SPEC-042.md
├── SPEC-042.threads.yaml
├── triage/TRIAGE-088.md
└── archive/SPEC-001.md
```

Joining creates a managed local clone at
`~/.spec/repos/<owner>/<repo>/`. `spec` reads and writes through that clone and
publishes changes according to the team's `sync.auto_push` policy.

### Pipeline

A pipeline is an ordered set of stages. Each stage may define:

- one or more owning roles;
- gates that must pass before advancement;
- dashboard scope and claim behavior;
- warnings, review requirements, and auto-advance rules;
- effects on entry, exit, advance, or revert.

Choose a built-in preset or define stages directly. The interactive preset
selector is available through `spec config init`.

### Identity and ownership

Your local identity includes a name, role, stable spec handle, and optional
provider-specific identities. It controls personal queues, section ownership,
assignment matching, thread authorship, and integration calls.

Personal settings live in `~/.spec/config.yaml` and are never committed. Shared
team behavior lives in `spec.config.yaml` and is committed.

### Focus

Focus is the CLI's working context. In the TUI press `f` on a spec; from the
shell run `spec focus SPEC-042`. Commands such as `status`, `advance`, `plan`,
`steps`, and `build` then infer that ID.

### Security

The **Security** tab (press `6`) lists open dependency-vulnerability alerts from
your scanner. The scanner is provider-agnostic — set it once, like the editor
preference:

```yaml
integrations:
  security:
    provider: dependabot   # dependabot | renovate | snyk | custom
    scope: org             # org-wide alerts, or "repo" for a specific set
    # repos: lib-a, lib-b  # repo scope: one or more repositories to watch
    token: ${SPEC_GITHUB_TOKEN}

security:
  sla:                     # deadline = alert age + window, per severity
    critical: 1d
    high: 1w
    medium: 2w
    low: 30d
  dashboard_surface_within: 24h
```

Each alert's severity sets an SLA deadline; rows warm from neutral through amber
to red as that deadline nears, and pin hottest once overdue. Security items live
only in the Security tab (their fix PRs are filtered out of Reviews) — except
when an alert is within a day of breaching, when it also surfaces at the top of
the dashboard. v1 reads GitHub Dependabot alerts; `spec config init` prompts for
the provider.

---

## Documentation

| Guide | Use it for |
| --- | --- |
| **[Quickstart](docs/QUICKSTART.md)** | First run and first TUI workflow |
| **[TUI guide](docs/TUI.md)** | Reader, review, triage, actions, Settings |
| **[Configuration](docs/CONFIGURATION.md)** | Team and pipeline config |
| **[Agent integration](docs/AGENT_INTEGRATION.md)** | MCP build-port contract |
| **[Team contract](docs/TEAM_CONTRACT.md)** | Collaboration conventions |
| [SPEC.md](SPEC.md) | Product specification and architecture |
| [CHANGELOG.md](CHANGELOG.md) | Release history |

Use `spec --help`, `spec <command> --help`, and `?` inside the TUI for the
reference closest to your current task.

---

## Development and contributing

Requires Go 1.25.8+ and Git.

```bash
make build
make test
make lint-strict
make docs
```

Read [`AGENTS.md`](AGENTS.md) before contributing. It defines architecture,
Go, testing, lint, and commit conventions. Contributions use conventional
commits and should reference the relevant product specification or discussion.

---

## License

[MIT](LICENSE)
