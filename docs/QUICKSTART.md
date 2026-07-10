# Quickstart: from install to first reviewed spec

This guide follows the primary `spec` experience: run the interactive TUI,
complete onboarding, and work from the dashboard. You only need a terminal,
Git, and access to your team's specs repository.

If you administer the team repository or integrations, use the
[Configuration reference](CONFIGURATION.md) after this guide.

---

## 1. Install

```bash
# Homebrew
brew install aaronl1011/tap/spec

# Or Go 1.25.8+
go install github.com/aaronl1011/spec@latest
```

Check the binary:

```bash
spec version
```

---

## 2. Prepare repository access

Ask your team for the specs repository reference. It may look like:

```text
acme/specs
github.com/acme/specs
gitlab.com/acme/specs
https://bitbucket.org/acme/specs
```

Export the matching access token before starting. It needs read/write access
because `spec` commits workflow changes back to this repository.

```bash
export SPEC_GITHUB_TOKEN="..."
# or SPEC_GITLAB_TOKEN / SPEC_BITBUCKET_TOKEN
```

You can also paste the token into onboarding's password field.

---

## 3. Run `spec`

```bash
spec
```

On a new machine, the interactive wizard appears automatically.

### Step 1: your identity

Enter:

- **name** — shown in activity and decisions;
- **role** — `pm`, `tl`, `designer`, `qa`, or `engineer`;
- **spec handle** — a stable local identity such as `ada` (optional during
  onboarding, editable later in Settings).

Your personal config is written to `~/.spec/config.yaml`. It is local and must
not be committed.

### Step 2: your team

Choose **Join an existing specs repo**, then enter:

- the repository reference;
- its branch (defaults to `main`);
- an access token, or leave the field blank to use the exported variable.

`spec` clones and validates the repository at
`~/.spec/repos/<owner>/<repo>/`, then opens the dashboard without restarting.

> Creating a team? Choose **Create a new team**. The wizard hands you to the
> advanced setup path; run `spec config init` in the repository that will own
> `spec.config.yaml`. Continue with
> [Create a team](CONFIGURATION.md#create-a-team).

---

## 4. Learn the dashboard in two minutes

The TUI has six persistent views:

| Key | View | Purpose |
| --- | --- | --- |
| `1` | Dashboard | Your DO, REVIEW, INCOMING, and BLOCKED queues |
| `2` | Pipeline | Every active spec grouped by stage |
| `3` | Specs | Active and archived spec browser |
| `4` | Triage | Lightweight incoming work |
| `5` | Reviews | Pull requests awaiting review |
| `6` | Settings | Personal identity, theme, refresh, mouse, editor |

Common controls:

```text
j / k or arrows   move
enter             open or select
tab / shift+tab   cycle views
?                 contextual help
/                 search all specs
r                 refresh
esc               back; press twice at top level to exit
ctrl+c            exit immediately
```

Open `?` whenever a screen feels unfamiliar. Help changes with the current
view and with reader modes such as thread focus, composing, and anchor picking.

---

## 5. Find and read a spec

1. Press `3` for **Specs**.
2. Move to a spec and press `enter`.
3. Read the overview: metadata, stage, ownership, progress, and sections.
4. Press `o` to open the reader.

Reader navigation:

| Key | Action |
| --- | --- |
| `[` / `]` | Previous / next section |
| `1`–`9` | Jump to a section |
| `g` / `G` | Top / bottom of the section |
| `n` / `p` | Next / previous matching thread across the document |
| `t` | Show or hide the thread pane |
| `T` | Cycle pane size: peek, review, conversation |
| `tab` | Focus prose or thread pane |
| `o` / `esc` | Return to overview |

The section list shows open-thread counts. Gutter markers connect discussions
to their paragraph, list item, table row, or code block.

### Ask on a section

Press `a`, type the question, then `ctrl+s` to send. Enter inserts a newline;
`esc` cancels.

### Ask on exact text

1. Press `A`.
2. Move block by block with `j` / `k` or the arrows.
3. Press `enter` to choose the highlighted block.
4. Type the question and press `ctrl+s`.

Press `s` in pick mode to ask on the whole section instead, or `esc` to cancel.

### Review conversations

With a thread selected:

- `r` replies;
- `x` resolves;
- `u` toggles read/unread;
- `f` cycles `open`, `all`, `mine`, and `unread` filters.

Unread state is personal to your identity on this machine. It is not committed
to Git.

---

## 6. Create or triage work

From any top-level view:

### Lightweight intake

Press `i` to open the triage form. Fill in title, priority, and optional source.
Use `tab` to move fields; `enter` advances from title, cycles priority, and
submits from the final source field. Press `esc` to cancel.

Triage is for work that still needs classification. In the **Triage** view,
press `enter` or `space` to inspect an item. Depending on your role you can add
a note (`n`), edit (`e`), close (`c`), escalate (`x`), or promote it (`p`).

### Full spec

Press `n`, enter a title, and confirm. `spec` assigns the next ID, scaffolds the
standard template, commits it, and refreshes the views.

Use this when the work is already understood well enough to enter the team
pipeline.

---

## 7. Take ownership and act

Select a spec in Dashboard, Pipeline, or Specs. The main lifecycle keys are:

| Key | Action |
| --- | --- |
| `g c` | Claim or assign |
| `f` | Focus / unfocus |
| `e` | Edit in your configured editor |
| `a` | Advance after validating gates |
| `v` | Revert to an earlier stage with a reason |
| `x` | Block with a reason |
| `u` | Resume a blocked spec |
| `c` | Record a decision |
| `b` | Start or resume an agent build |
| `p` | Push local spec changes |
| `s` | Publish/sync through the docs integration |
| `g a` / `g r` | Archive / restore |

Actions that move or remove work ask for confirmation. Errors appear in the
status bar; press `E` to expand a long error.

### Focus and CLI context

Press `f` on a spec to make it your working context. Shell commands then infer
the ID:

```bash
spec status
spec validate
spec build --check
spec do
```

Focus persists across TUI sessions.

---

## 8. Plan and build

The TUI starts and resumes builds, while the detailed plan editor remains a
command workflow.

```bash
spec focus SPEC-042
spec plan add "Add token-bucket limiter" --repo api-service
spec plan add "Add integration tests" --repo api-service
spec plan ready
spec build --check
```

`spec build --check` validates the plan, workspace paths, routed skills, agent
capabilities, and completion strategy without launching an agent.

Back in the TUI, select the spec and press `b`. The confirmation tells you which
agent will launch. `spec build` assigns the spec to you when appropriate,
provisions isolated worktrees, records durable progress, and can resume later:

```bash
spec do
```

For agent-harness authors, the MCP contract is documented in
[Agent integration](AGENT_INTEGRATION.md).

---

## 9. Personalise the TUI

Press `6` for **Settings**. Select a field and press `enter` to edit:

- name, role, and spec handle;
- theme (previewed live);
- refresh interval;
- mouse support;
- editor.

For enums use `space`, `l`, and `h`; press `enter` to save or `esc` to cancel.
Changes apply immediately and write to `~/.spec/config.yaml`.

The integration rows and config paths are read-only. Team integrations and
pipeline behavior are shared configuration; an administrator changes those in
`spec.config.yaml` and validates with `spec config lint`.

---

## 10. Know when to use commands

Stay in the TUI for ordinary human work. Reach for a command when you need:

- exact flags or automation;
- detailed build-plan editing;
- CI or JSON output;
- team configuration and integration diagnostics;
- MCP or headless agent integration.

Useful checks:

```bash
spec whoami           # resolved identity, team, and config paths
spec config lint      # structural and semantic config diagnostics
spec config test      # show which config and integration categories are present
spec config check     # live PM/Jira preflight and workflow statuses
spec pipeline presets # inspect built-in pipelines
spec --static         # non-interactive dashboard render
```

Every command has targeted help:

```bash
spec --help
spec build --help
spec sync --help
```

---

## 11. Troubleshooting

### The onboarding wizard does not appear

It requires an interactive terminal. Run plain `spec` without piping or
redirecting output. In CI or a pipe, `spec` renders a static dashboard instead.

Manual fallbacks:

```bash
spec config init --user
spec join acme/specs
```

### No access token

Export the provider-specific variable or paste the token into onboarding:

```bash
export SPEC_GITHUB_TOKEN="..."
```

Legacy variables such as `GITHUB_TOKEN` still work with a deprecation warning.

### Team config not found

Run `spec` and join the team, or use `spec join <repo>`. A valid team repository
must contain `spec.config.yaml` at its root.

### Can't advance

The stage's gates are not satisfied. Select the spec and press `a` to see the
error, or inspect all gates explicitly:

```bash
spec validate
spec pipeline --verbose
```

### An integration is listed but does not work

`spec config test` reports configured categories; it does not call remote APIs.
Run `spec config check` for the implemented live PM/Jira preflight and inspect
provider warnings when `spec` starts. See [Configuration](CONFIGURATION.md).

### The wrong work appears in my dashboard

Check your role, handle, and provider identities:

```bash
spec whoami
```

Edit name, role, and handle in Settings. Provider-specific identities and
advanced preferences live in `~/.spec/config.yaml`.

---

## Next

- [TUI guide](TUI.md) — complete keyboard and workflow guide
- [Configuration](CONFIGURATION.md) — create a team, integrations, pipelines
- [Team contract](TEAM_CONTRACT.md) — collaboration conventions
