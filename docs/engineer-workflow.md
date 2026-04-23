# Engineer Workflow Guide

This guide covers the engineer-focused features for technical planning, build execution, and fast-track fixes.

## Overview

The engineer workflow has three main phases:

1. **Planning** — Create and get approval for your technical build plan
2. **Building** — Execute steps, track progress, manage blockers
3. **Fast-track** — Self-service small bug fixes (optional)

---

## Technical Planning

### Creating a Build Plan

When a spec reaches the `engineering` stage, add your technical plan:

```bash
# Add steps to your build plan
spec plan add SPEC-042 "Add user authentication endpoint"
spec plan add SPEC-042 "Write integration tests"
spec plan add SPEC-042 "Update API documentation"

# View the plan
spec plan SPEC-042
```

For multi-repo work, specify the target repo:

```bash
spec plan add SPEC-042 "Add endpoint" --repo api-service
spec plan add SPEC-042 "Add UI components" --repo web-app
```

### Editing Plans

Edit the full plan in your editor:

```bash
spec plan edit SPEC-042
```

This opens the spec file where you can edit the `steps:` section in the frontmatter:

```yaml
steps:
  - repo: api-service
    description: Add user authentication endpoint
    status: pending
  - repo: api-service
    description: Write integration tests
    status: pending
  - repo: web-app
    description: Add UI components
    status: pending
```

### Requesting Plan Review

When your plan is ready, request review from the TL:

```bash
spec plan ready SPEC-042
```

This sets `review.status: pending` and notifies reviewers. The TL reviews with:

```bash
# View the plan
spec review SPEC-042 --plan

# Approve
spec review SPEC-042 --plan --approve

# Or request changes
spec review SPEC-042 --plan --request-changes --feedback "Step 2 needs more detail"
```

---

## Build Execution

### Viewing Steps

Once your plan is approved, view your steps:

```bash
spec steps SPEC-042

Build Steps: SPEC-042

Progress: 0/3 steps complete

  ○ 1. [api-service] Add user authentication endpoint
  ○ 2. [api-service] Write integration tests
  ○ 3. [web-app] Add UI components

Next: spec steps start SPEC-042 1
```

### Working Through Steps

```bash
# Start the next step (creates branch name)
spec steps start SPEC-042

# Or start a specific step
spec steps start SPEC-042 1

# When done, mark complete (optionally with PR number)
spec steps complete SPEC-042 --pr 123

# Check what's next
spec steps next SPEC-042
```

### Handling Blockers

```bash
# Block a step with a reason
spec steps block SPEC-042 2 "Waiting on API team to deploy auth service"

# Later, unblock it
spec steps unblock SPEC-042 2
```

### Cross-Repo Navigation

Configure workspace paths in `~/.spec/config.yaml`:

```yaml
workspaces:
  api-service: ~/code/api-service
  web-app: ~/code/web-app
```

Then `spec steps next` shows the local path:

```
Next step for SPEC-042:

  Step 2: Add UI components
  Repo: web-app
  Path: ~/code/web-app
  Branch: spec-042/step-2-add-ui-components

To start: spec steps start SPEC-042
```

---

## Fast-Track Fixes

For small bug fixes that don't need full ceremony:

```bash
spec fix "Fix login button not responding on mobile" --label bug
```

This creates a spec that:
- Starts at `build` stage (skips draft, design, QA planning)
- Has `fast_track: true` in frontmatter
- Uses a minimal template (Problem/Solution/Testing)

### Configuration

Enable fast-track in `spec.config.yaml`:

```yaml
fast_track:
  enabled: true
  allowed_roles: [engineer, tl]
  require_labels: [bug, hotfix]
  max_duration: "2d"
```

| Option | Description |
|--------|-------------|
| `enabled` | Must be `true` to use `spec fix` |
| `allowed_roles` | Who can create fast-track specs |
| `require_labels` | At least one required (prevents misuse) |
| `max_duration` | Suggested time limit (advisory) |

---

## Passive Awareness

When you run `spec do`, you'll see a one-liner about items needing attention:

```
$ spec do SPEC-042
📥 2 reviews pending, 1 spec blocked (spec list --mine)

Resuming SPEC-042...
```

View all your specs:

```bash
spec list --mine
```

### Configuration

Control awareness in `~/.spec/config.yaml`:

```yaml
preferences:
  passive_awareness:
    during_build: false      # Don't show during spec do
    dismiss_duration: "2h"   # How long dismissals last
```

---

## Typical Workflow

### 1. Spec Arrives at Engineering Stage

```bash
spec list                    # See specs in your queue
spec show SPEC-042           # Read the spec
```

### 2. Create Technical Plan

```bash
spec plan add SPEC-042 "Step 1 description"
spec plan add SPEC-042 "Step 2 description"
spec plan SPEC-042           # Review your plan
spec plan ready SPEC-042     # Request TL review
```

### 3. Wait for Approval

TL reviews and approves (or requests changes).

### 4. Execute Build

```bash
spec do SPEC-042             # Resume work context

spec steps start SPEC-042    # Start first step
# ... do the work ...
spec steps complete SPEC-042 --pr 101

spec steps start SPEC-042    # Start next step
# ... continue ...
```

### 5. All Steps Done

```bash
spec steps SPEC-042
# 🎉 All steps complete!

spec advance SPEC-042        # Move to pr-review stage
```

---

## Command Reference

| Command | Description |
|---------|-------------|
| `spec plan [id]` | View build plan |
| `spec plan edit [id]` | Edit plan in $EDITOR |
| `spec plan add [id] <desc>` | Add a step |
| `spec plan ready [id]` | Request plan review |
| `spec review <id> --plan` | View pending plan review |
| `spec review <id> --plan --approve` | Approve plan |
| `spec review <id> --plan --request-changes` | Request changes |
| `spec steps [id]` | View all steps |
| `spec steps next [id]` | Show next step details |
| `spec steps start [id] [n]` | Start working on step |
| `spec steps complete [id] [n]` | Mark step complete |
| `spec steps block [id] [n] <reason>` | Block a step |
| `spec steps unblock [id] [n]` | Unblock a step |
| `spec list --mine` | Show specs you own |
| `spec fix <title>` | Create fast-track spec |
