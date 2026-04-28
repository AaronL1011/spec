# spec Quickstart — Pilot Onboarding

Welcome to the `spec` pilot program. This guide gets you from zero to productive in ~15 minutes.

## Prerequisites

- **Git** installed and configured
- **GitHub access token** with repo read permissions
- Terminal access (macOS, Linux, or WSL)

---

## 1. Install spec

### Option A: Go install (recommended)

Requires Go 1.21+:

```bash
go install github.com/aaronl1011/spec@latest
```

### Option B: Download binary

Download from [GitHub Releases](https://github.com/aaronl1011/spec/releases) for your platform.

### Verify installation

```bash
spec version
spec --help
```

---

## 2. Set up your access token

Create a GitHub personal access token with `repo` scope, then export it:

```bash
export GITHUB_TOKEN="ghp_..."
```

Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) for persistence:

```bash
echo 'export GITHUB_TOKEN="ghp_..."' >> ~/.zshrc
source ~/.zshrc
```

---

## 3. Set up your identity

Run the user config wizard:

```bash
spec config init --user
```

You'll be prompted for:
- **Name**: Your full name
- **Role**: Your primary role (`engineer`, `tl`, `pm`, `qa`, `designer`)
- **Handle**: Your comms handle (e.g., `@aaron` or `aaron@company.com`)
- **Editor**: Your preferred editor (defaults to `$EDITOR`)

Verify:

```bash
spec whoami
```

---

## 4. Join the team

Join the existing specs repo:

```bash
spec join NEXL-LTS/ai-product-specs
```

This clones the specs repo to `~/.spec/repos/` and configures your environment.

Verify:

```bash
spec config test
spec list
```

---

## 5. Daily workflows

### Morning check-in

Start every day with the dashboard:

```bash
spec                    # What needs my attention?
spec list --mine        # Specs assigned to me
```

### Working on a spec

```bash
spec focus SPEC-042     # Set your working context
spec status             # View current state
spec pull               # Fetch to local .spec/ directory
```

### Building (with coding agent)

```bash
spec plan               # View the build plan
spec steps              # View build steps
spec steps start        # Start next step
spec build              # Launch agent with full context
spec do                 # Resume where you left off
```

### Completing work

```bash
spec steps complete --pr 123    # Mark step done, link PR
spec advance                     # Move to next pipeline stage
```

### Quick decisions

```bash
spec decide --question "Should we use JWT or session tokens?"
spec decide --list                # View open decisions
spec decide --resolve 1 --decision "JWT for stateless scaling"
```

### Switching specs

```bash
spec focus SPEC-043     # Switch to another spec
spec focus --clear      # Clear focus when done
```

---

## 6. Key commands reference

| Command | What it does |
|---------|--------------|
| `spec` | Dashboard — your daily starting point |
| `spec focus [id]` | Set working context (most commands infer the ID) |
| `spec list` | Specs awaiting your action |
| `spec status` | Current spec's pipeline position |
| `spec pull` | Fetch spec to local directory |
| `spec plan` | View build plan |
| `spec steps` | View/manage build steps |
| `spec build` | Start build with agent context |
| `spec do` | Resume work (smart: checks branch → focus → recent) |
| `spec advance` | Move to next pipeline stage |
| `spec decide` | Manage decision log |

---

## 7. Pilot ground rules

During the pilot, please:

1. **Use `spec` as your first command each morning** — builds the habit
2. **Keep active work in specs** — no shadow tracking in other tools
3. **Log notable decisions** — helps us evaluate the decision log feature
4. **Report friction** — note commands that feel awkward or missing
5. **Stay in your role** — avoid `--role` overrides unless testing

---

## 8. Getting help

```bash
spec <command> --help   # Command-specific help
spec pipeline           # View current pipeline stages
spec config test        # Verify integrations
```

**Docs:**
- [Engineer workflow guide](engineer-workflow.md)
- [Pipeline configuration](pipelines.md)
- [Full pilot runbook](pilot-runbook.md)

**Issues?** Ping @aaron or open an issue in the specs repo.

---

## Quick troubleshooting

| Problem | Fix |
|---------|-----|
| `no access token` | Export `GITHUB_TOKEN` |
| `no role configured` | Run `spec config init --user` |
| `spec not found` | Check `$PATH` includes Go bin directory |
| `already joined` | You're set — run `spec list` |
| `team config not found` | Run `spec join NEXL-LTS/ai-product-specs` |

---

## What success looks like

By end of pilot, you should feel that:

- ✓ You always know what to work on next
- ✓ Spec status is clear without asking anyone
- ✓ Decisions are captured, not lost in chat
- ✓ Context switching between specs is painless
- ✓ The tool stays out of your way

Let's ship some specs. 🚀
