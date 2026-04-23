---
id: SPEC-005
title: Frictionless engineer workflows
status: build
version: 0.1.0
author: Aaron
cycle: Cycle 0
repos:
    - spec-cli
revert_count: 0
source: direct
created: "2026-04-23"
updated: "2026-04-23"
---

# SPEC-005 - Frictionless engineer workflows

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Should `spec do` work in `engineering` stage for exploratory prototyping? | (1) Allow in engineering for exploration, (2) Strictly require `build` stage | **(2) Build stage only** | `spec do` spawns an agent to execute a plan; in `engineering` there's no approved plan yet; exploratory work uses `spec edit` and `spec draft` | @aaron | 2026-04-23 |
| 002 | Should plan review support sync (live) sessions? | (1) Async only, (2) Async + sync option, (3) Sync by default | **(1) Async only** | Sync meetings are coordination overhead; async review fits the "terminal is your office" philosophy; teams that need sync can do it outside `spec` | @aaron | 2026-04-23 |
| 003 | Where should build plan status live? | (1) Session file only, (2) §7.3 markdown prose, (3) Frontmatter YAML | **(3) Frontmatter YAML** | Structured data enables `spec steps` commands; frontmatter is already parsed; avoids regex parsing of prose; single source of truth | @aaron | 2026-04-23 |
| 004 | Who can approve a technical plan and advance to `build`? | (1) Hardcode TL role, (2) Configurable via pipeline, (3) Any reviewer | **(2) Pipeline configurable** | Teams vary — some need TL approval, others use peer review, some auto-advance on section completion; pipeline config already has `owner_role` and gate expressions | @aaron | 2026-04-23 |
| 005 | Should `spec` implement git-level PR stacking or use a simple checklist? | (1) Integrate Graphite/ghstack, (2) Simple checklist (no git stacking), (3) Minimal branch chaining | **(2) Simple checklist** | Traditional PR stacking is a single-repo problem; `spec` steps span multiple repos; real stacking tools exist for teams that need them; keep `spec` focused on workflow orchestration, not git plumbing; rename "PR Stack" to "Build Plan" for clarity | @aaron | 2026-04-23 |
| 006 | Step 1 complete: Extended frontmatter schema - added BuildStep, ReviewState types with helper methods (CurrentStep, AllStepsComplete, StepsExist, IsReviewApproved, etc). All tests passing. | | | | agent | 2026-04-23 |
| 007 | Step 2 complete: Extended user config schema - added Workspaces map, Multiplexer, AutoPull, AutoNavigate, PassiveAwarenessConfig with helper methods. All tests passing. | | | | agent | 2026-04-23 |
| 008 | Step 3 complete: Extended pipeline config schema - added StageReviewConfig, AutoAdvanceConfig, StepsExists gate (with PRStackExists backward compat), ReviewApproved gate. All tests passing. | | | | agent | 2026-04-23 |
| 009 | Step 4 complete: Extended team config schema - added FastTrackConfig with Enabled, AllowedRoles, MaxDuration, RequireLabels, PipelineVariant, ExcludedStages. All tests passing. | | | | agent | 2026-04-23 |
| 010 | Step 5 complete: Created internal/planning/ package with Plan, Step, ReviewState types. Includes FromMeta(), ToFrontmatter(), step management (Add/Start/Complete/Block/Unblock), review workflow (Request/Approve/RequestChanges), validation, and progress tracking. All tests passing. | | | | agent | 2026-04-23 |
| 011 | Step 6 complete: Created cmd/plan.go with subcommands: `spec plan [id]` (show), `spec plan edit [id]`, `spec plan add [id] <desc>`, `spec plan ready [id]`. Added markdown.UpdateFrontmatter() helper. All tests passing. | | | | agent | 2026-04-23 |
| 012 | Phase 2 complete (Steps 5-9): Technical Planning - Created internal/planning/ package, `spec plan` command (show/edit/add/ready), and `spec review --plan --approve/--request-changes`. All tests passing. | | | | agent | 2026-04-23 |
| 013 | Step 10 complete: Created internal/steps/ package with Engine, BranchName(), WorkspacePath(), StartStep(), CompleteStep(), BlockStep(), UnblockStep(), Progress(), GetNextStep(). All tests passing. | | | | agent | 2026-04-23 |
| 014 | Phase 3 complete (Steps 10-13): Build Steps - Created internal/steps/ package and cmd/steps.go with subcommands: show, next, start, complete, block, unblock. Includes branch name generation, workspace path resolution, and step transitions. All tests passing. | | | | agent | 2026-04-23 |
| 015 | Decision: Simplify Phase 4 (Inbox & Awareness). Skip dedicated `spec inbox` command. Instead: (1) add `--mine` filter to `spec dashboard`, (2) add passive awareness line to key commands. Rationale: KISS - avoid command proliferation, dashboard already shows spec state, just needs personal filtering. | | | | agent | 2026-04-23 |
| 016 | Phase 4 complete (simplified): Added `--mine` flag to `spec list`, created internal/awareness/ package with Summary and Print(). Integrated passive awareness line into `spec do`. Skipped dedicated inbox command per KISS decision. All tests passing. | | | | agent | 2026-04-23 |
| 017 | Phase 5 complete: Added `spec fix` command for fast-track bug fixes. Respects FastTrackConfig (enabled, allowed_roles, require_labels, max_duration). Creates minimal spec template starting at build stage. All tests passing. | | | | agent | 2026-04-23 |
| 018 | Decision: Skip `spec upgrade` command (YAGNI). Fast-track specs can be manually edited if scope grows. Revisit if teams actually need this escape hatch. | | | | agent | 2026-04-23 |

## 1. Problem Statement           <!-- owner: pm -->

`spec` promises that "your terminal is your office" — but today, engineers still do too much manual orchestration. The pipeline is configurable (SPEC-004), the MCP integration works (SPEC-003), but the daily build loop has unnecessary friction:

### Too Many Commands to Start Work

An engineer sees `SPEC-042` on their dashboard and wants to work on it. Today's flow:

```bash
$ spec pull SPEC-042      # fetch from specs repo
$ spec build SPEC-042     # start build session
$ spec do                 # actually spawn agent
```

Three commands. The system has all the information to do this in one: the spec exists, the engineer's role matches, the stage is `build`. Every extra command is friction that breaks flow state.

### Multi-Repo Work Requires Manual Context Switching

A spec spanning `auth-service`, `api-gateway`, and `frontend` requires the engineer to:

1. Complete work in `auth-service`
2. Read the prompt: "Please switch to ~/code/api-gateway"
3. Open a new terminal or `cd`
4. Run `spec do` again
5. Repeat for `frontend`

This is the exact context-switching pain `spec` claims to eliminate. The tool knows where repos live. It should handle navigation.

### Build Plans Are Unstructured Prose

§7.3 Build Plan is freeform markdown:

```markdown
1. [auth-service] Add token bucket rate limiter
2. [auth-service] Integrate Redis backend
3. [api-gateway] Add rate limit middleware
```

This is parsed with regex. If the format drifts, `spec build` breaks. Engineers can't programmatically add, remove, or reorder steps. There's no state tracking — "step 2 is done" lives in a separate session file that can desync from the prose.

### Pipeline Advancement is Manual Even When Obvious

When all PRs are approved and merged, the engineer must remember to run `spec advance`. The system knows the gates are satisfied. Why wait for a human to type a command?

Forgotten `spec advance` calls cause phantom delays — specs sit in `pr_review` for hours after the work is done because no one pushed the button.

### Decision Capture Has High Friction

Recording a decision requires:

```bash
$ spec decide SPEC-042 --question "REST vs gRPC?"
# ... time passes, discussion happens ...
$ spec decide SPEC-042 --resolve 3 --decision "gRPC" --rationale "Lower latency"
```

The engineer must remember the question number, switch out of their agent, and type a verbose command. The `--rationale` flag is awkward for multi-line explanations.

Result: engineers skip decision logging, and institutional knowledge evaporates.

### No UX for Technical Planning Phase

Before building, engineers must plan: read the spec, understand the architecture, identify risks, decompose into PRs. Today there's no structured UX for this:

- No command to "enter planning mode" for a spec
- No visibility into what sections need completion before build
- No structured way to request plan review
- No gate ensuring the plan is approved before implementation starts

Engineers either skip planning (and discover scope issues mid-build) or do it ad-hoc with no tool support. The `engineering` stage exists in the pipeline but has no dedicated commands.

### Engineers Can't Self-Service Small Fixes

An engineer finds a bug while building a feature. To fix it properly, they must:

1. Run `spec intake "Fix null pointer"` (creates triage, owned by PM)
2. Wait for PM to promote it or ask a TL to fast-track
3. Finally work on it

For a 20-minute fix, this is absurd. Engineers should be able to create, own, and complete small fixes without ceremony.

### Who is affected?

| Role | Pain |
|------|------|
| **Engineer** | Multiple commands to start work; manual repo switching; can't self-service small fixes; decision logging is tedious |
| **Tech Lead** | Specs stall waiting for manual `advance`; no visibility into build plan progress |
| **Team** | Institutional knowledge lost because decisions aren't captured; velocity metrics skewed by forgotten advances |

## 2. Goals & Non-Goals           <!-- owner: pm -->

### Goals

- **Structured planning phase**: `spec plan SPEC-042` is the entry point for technical planning. Engineers see what sections need completion, draft with AI assistance, and request async review when ready.
- **Configurable plan approval**: Who can approve a plan (TL, peer, auto) is defined in pipeline config, not hardcoded. Teams configure what works for them.
- **One command to start building**: `spec do SPEC-042` pulls the spec, validates the stage is `build` (plan approved), starts MCP, and spawns the agent. Zero prerequisites once planning is complete.
- **Seamless multi-repo flow**: When a build plan spans repos, `spec` navigates automatically — opens new terminals/panes, changes directories, continues the session.
- **Structured build plans**: build plans are data (YAML), not prose. Steps can be added, removed, and reordered via commands. State is authoritative.
- **Auto-advance when gates pass**: Optionally, specs advance automatically when gates are satisfied. No human button-pushing for obvious transitions.
- **Low-friction decision capture**: `spec decide` has an interactive mode. The agent can resolve decisions mid-session. Multi-line rationale is easy.
- **Engineer self-service for small fixes**: Engineers can create, own, and complete bug fixes without PM/TL ceremony when appropriate.
- **Inbox actions**: Pending items shown by passive awareness can be dismissed, snoozed, or acted on inline — not just "run `spec` for details."

### Non-Goals

- **Fully automated pipelines**: Auto-advance is opt-in per stage. Humans remain in the loop for judgment calls. `spec` doesn't become a CI system.
- **Replacing project management**: PMs still own triage, prioritisation, and spec authorship. Engineer self-service is for small fixes, not feature work.
- **Magic repo discovery**: Engineers declare their repo locations in config. `spec` doesn't scan the filesystem or guess.
- **Real-time collaboration**: `spec` is a single-user CLI. Multi-user editing, live cursors, and conflict resolution are out of scope.
- **IDE integration**: The focus is terminal-native. IDE plugins may come later but are not part of this spec.

## 3. User Stories                <!-- owner: pm -->

### Technical Planning

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-28 | Engineer | Run `spec plan SPEC-042` to enter planning mode | I have a clear entry point for the `engineering` stage |
| US-29 | Engineer | See which §7 sections need completion before I can advance | I know exactly what's expected of me |
| US-30 | Engineer | Use `spec draft` to get AI-assisted architecture notes and build plan | I start from a draft rather than a blank page |
| US-31 | Engineer | Run `spec plan ready SPEC-042` when my plan is complete | I signal that I'm ready for review without manual stage advancement |
| US-32 | Reviewer | Review a technical plan asynchronously via `spec review --plan` | I validate the approach without scheduling a meeting |
| US-33 | Reviewer | Approve, request changes, or comment on a plan | I give structured feedback that the engineer can act on |
| US-34 | Engineer | See plan review status and feedback in `spec status` | I know if I'm approved, waiting, or have changes requested |
| US-35 | TL | Configure who can approve plans in pipeline config | My team's review process is enforced by the tool |

### One Command to Build

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-01 | Engineer | Run `spec do SPEC-042` and immediately be in my agent with full context | I don't think about pulling, building, or session management |
| US-02 | Engineer | Run `spec do` with no arguments and resume my most recent work | I continue where I left off with zero friction |
| US-03 | Engineer | Have `spec do` auto-pull if my local copy is stale | I always work against current spec content without manual sync |
| US-36 | Engineer | Get a clear error if I run `spec do` on a spec not yet at `build` stage | I understand I need to complete planning first |

### Multi-Repo Flow

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-04 | Engineer | Declare my repo locations once in config | `spec` knows where to find `auth-service`, `api-gateway`, etc. |
| US-05 | Engineer | Have `spec` open a new terminal pane in the next repo when I complete a build plan step | I don't manually navigate or re-run commands |
| US-06 | Engineer | See a unified build plan status across all repos | I know the overall progress without checking each repo |
| US-07 | Engineer | Opt out of auto-navigation and handle repo switching myself | I have control when I prefer manual flow |

### Structured Build Plans

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-08 | Engineer | Have build plan defined as structured data, not prose | Parsing is reliable and state is authoritative |
| US-09 | Engineer | Run `spec steps add "Add rate limiter" --repo auth-service` | I modify the build plan without editing markdown |
| US-10 | Engineer | Run `spec steps reorder 3 --after 1` | I adjust the plan as I learn more |
| US-11 | Engineer | Run `spec steps status` and see step completion, branches, PR links | I have a clear view of multi-PR progress |
| US-12 | Engineer | Have step completion tracked authoritatively in the build plan | Session state and build plan state can't desync |

### Auto-Advance

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-13 | TL | Configure stages to auto-advance when gates pass | Specs flow without manual button-pushing |
| US-14 | Engineer | See a notification when my spec auto-advances | I know transitions happened without checking |
| US-15 | TL | Disable auto-advance for stages that need human judgment | Automation doesn't bypass necessary review |
| US-16 | Any | Have auto-advance respect business hours / quiet periods | Notifications don't fire at 3am |

### Decision Capture

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-17 | Engineer | Run `spec decide SPEC-042` and see an interactive list of open decisions | I can review and resolve without remembering IDs |
| US-18 | Engineer | Resolve a decision with a multi-line rationale via `$EDITOR` | Complex reasoning is captured properly |
| US-19 | Engineer | Have my agent resolve decisions via MCP tool with full context | Decisions are captured in flow without CLI switching |
| US-20 | Engineer | See open decisions when I start a build session | I'm prompted to address outstanding questions |

### Engineer Self-Service

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-21 | Engineer | Run `spec fix "Null pointer in auth handler"` to create a fast-track bug fix | I own and complete small fixes without ceremony |
| US-22 | Engineer | Have fast-track specs skip design/QA stages automatically | The process matches the work size |
| US-23 | TL | Set limits on fast-track (e.g., max 1 day, must be labelled `bug`) | Engineers can self-service without bypassing process entirely |
| US-24 | Engineer | Convert a fast-track fix to a full spec if scope grows | I can "upgrade" when I discover the fix is bigger than expected |

### Inbox Actions

| # | As a... | I want to... | So that... |
|---|---------|--------------|------------|
| US-25 | Engineer | Dismiss a pending item for 2 hours when I see the passive awareness line | I stay focused without losing track of the item |
| US-26 | Engineer | Act on a pending review request directly from the awareness prompt | I don't have to run `spec` then another command |
| US-27 | Engineer | Configure which pending items show in passive awareness | I filter noise (e.g., don't show triage items) |

## 4. Proposed Solution           <!-- owner: pm -->

### 4.1 Concept Overview

This spec optimizes the **engineering workflow** — from receiving a spec through planning, building, and completing it. The principle: **`spec` should infer the next action and ask for confirmation, not require the engineer to remember and invoke each step.**

Seven capabilities:

1. **Technical Planning** — `spec plan` for the `engineering` stage with structured section completion and async review.
2. **Unified `spec do`** — One command that handles pull, session, MCP, and agent spawning (requires `build` stage).
3. **Workspace Mode** — Multi-repo orchestration with automatic navigation.
4. **Structured Build Plans** — YAML-based build plan with manipulation commands.
5. **Auto-Advance** — Optional automatic stage transitions when gates pass.
6. **Interactive Decisions** — Low-friction decision capture with TUI and enhanced MCP tools.
7. **Fast-Track Fixes** — Engineer self-service for small bug fixes.

### 4.2 Technical Planning Phase

The `engineering` stage is where technical planning happens. `spec plan` is the entry point:

```
$ spec plan SPEC-042

SPEC-042 — Auth refactor
Stage: engineering (you are the owner)

Sections to complete:
  ✓ §7.1 Architecture Notes (draft exists, 0 words)
  ○ §7.2 Dependencies & Risks
  ○ §7.3 Build Plan

Commands:
  spec edit SPEC-042                    Open in $EDITOR
  spec draft SPEC-042 --section <slug>  AI-assisted drafting
  spec draft SPEC-042 --build-plan        AI-proposed PR decomposition
  spec plan ready SPEC-042              Request review when complete

Run 'spec plan SPEC-042 --edit' to open in editor now.
```

**Section completion tracking:**

| Section | Complete When |
|---------|---------------|
| §7.1 Architecture Notes | Non-empty, >50 words |
| §7.2 Dependencies & Risks | Non-empty (risks identified or "none") |
| §7.3 Build Plan | `steps:` frontmatter has ≥1 step |

**Requesting review:**

```
$ spec plan ready SPEC-042

Validating technical plan...
  ✓ §7.1 Architecture Notes (342 words)
  ✓ §7.2 Dependencies & Risks (2 risks identified)
  ✓ §7.3 Build Plan (4 steps)

All sections complete. Requesting review...

✓ Review requested
  Reviewers: @mike, @sarah (from pipeline config)
  SPEC-042 will advance to 'build' when approved.
```

**Who reviews is pipeline-configurable:**

```yaml
# spec.config.yaml
pipeline:
  stages:
    - name: engineering
      owner: engineer
      review:
        required: true
        reviewers: [tl]              # role-based
        # OR: reviewers: [@mike, @sarah]  # named individuals
        # OR: reviewers: [author]    # self-review (for solo/trusted)
        min_approvals: 1
      gates:
        - section_not_empty: architecture_notes
        - section_not_empty: dependencies_risks
        - steps_exists: true
        - review_approved: true
```

**Async review flow:**

```
$ spec review SPEC-042 --plan

SPEC-042 — Auth refactor
Technical plan by @aaron · Requested 2h ago

─── §7.1 Architecture Notes ────────────────────────────
  Rate limiting will be implemented at the auth-service layer
  using a token bucket algorithm backed by Redis...
  [truncated, 342 words]

─── §7.2 Dependencies & Risks ──────────────────────────
  • Risk: Redis connection pooling under high load
  • Risk: Migration path for existing rate limit configs

─── §7.3 Build Plan ─────────────────────────────────
  1. [auth-service] Add token bucket rate limiter
  2. [auth-service] Integrate into auth flow
  3. [api-gateway] Add rate limit headers
  4. [frontend] Handle 429 responses

[a]pprove · [r]equest changes · [c]omment · [q]uit
> a

✓ Plan approved
  SPEC-042 advanced to 'build'
  @aaron notified
```

**Review outcomes:**

| Action | Effect |
|--------|--------|
| **Approve** | If `min_approvals` met, spec advances to `build`. Author notified. |
| **Request changes** | Spec stays in `engineering`. Author notified with feedback. |
| **Comment** | Adds comment to decision log. No state change. |

**Plan status in dashboard:**

```
$ spec

─── DO ────────────────────────────────────────────
📝 SPEC-042  Auth refactor       engineering   awaiting review
   Plan submitted 2h ago · Reviewers: @mike, @sarah
   
─── REVIEW ────────────────────────────────────────
📋 SPEC-039  Search indexing     engineering   plan review requested
   From @carlos · Run 'spec review SPEC-039 --plan'
```

### 4.3 Unified `spec do`

`spec do SPEC-042` is the entry point for the `build` stage (plan already approved):

```
$ spec do SPEC-042

✓ Pulling SPEC-042 (updated 2h ago)
✓ Stage: build (plan approved by @mike)
✓ Session: step 2/4 — Integrate Redis backend

⚠ 2 open decisions awaiting resolution (#003, #004)
  [v]iew · [s]kip · [r]esolve now

> s

Starting MCP server on stdio...
Spawning claude-code in ~/code/auth-service on branch spec-042/step-2-redis...
```

**Behavior:**

| Condition | Action |
|-----------|--------|
| Spec not in `.spec/` | Auto-pull from specs repo |
| Local copy is stale | Warn and offer to pull, or auto-pull if `preferences.auto_pull: true` |
| Stage is not `build` | Error with guidance (see below) |
| Stage owner doesn't match user role | Error: "This spec is owned by QA at this stage." |
| No existing session | Create session, start at step 1 |
| Existing session | Resume at current step |
| Open decisions exist | Prompt to view/skip/resolve before spawning agent |

**`spec do` requires `build` stage:**

```
$ spec do SPEC-042

✗ SPEC-042 is in 'engineering' stage
  
  Complete technical planning first:
    spec plan SPEC-042        # See what's needed
    spec plan ready SPEC-042  # Request review when done
  
  'spec do' is available once the plan is approved.
```

`spec do` with no arguments resumes the most recently active session (must be at `build` stage).

### 4.4 Workspace Mode

Engineers declare repo locations in user config:

```yaml
# ~/.spec/config.yaml
workspaces:
  auth-service: ~/code/auth-service
  api-gateway: ~/code/api-gateway
  frontend: ~/code/frontend
```

When a build plan step completes and the next step is in a different repo:

```
✓ Step 2 complete: Integrate Redis backend (auth-service)

Next: Step 3 — Add rate limit middleware (api-gateway)
Location: ~/code/api-gateway

  [c]ontinue in new pane · [m]anual (I'll switch myself) · [q]uit for now

> c

Opening new terminal pane...
```

**Pane opening** uses the configured terminal multiplexer:

```yaml
# ~/.spec/config.yaml
preferences:
  multiplexer: tmux   # tmux | zellij | wezterm | iterm2 | none
```

If `none` or undetected, falls back to printing the command and waiting for the user.

### 4.5 Structured Build Plans

build plans move from §7.3 prose to structured YAML in frontmatter:

```yaml
---
id: SPEC-042
steps:
  - repo: auth-service
    description: Add token bucket rate limiter
    branch: spec-042/step-1-rate-limiter
    pr: 415
    status: complete
  - repo: auth-service
    description: Integrate Redis backend
    branch: spec-042/step-2-redis
    pr: 418
    status: in-progress
  - repo: api-gateway
    description: Add rate limit middleware
    status: pending
  - repo: frontend
    description: Add rate limit error handling
    status: pending
---
```

**Commands:**

```bash
# View build plan status
$ spec steps
SPEC-042 — Auth refactor

  1. ✓ [auth-service] Add token bucket rate limiter    PR #415 merged
  2. ► [auth-service] Integrate Redis backend          PR #418 open
  3. ○ [api-gateway] Add rate limit middleware         pending
  4. ○ [frontend] Add rate limit error handling        pending

# Add a step
$ spec steps add "Add metrics emission" --repo auth-service --after 2

# Reorder
$ spec steps move 4 --after 2

# Remove
$ spec steps remove 3

# Mark complete (usually done via MCP, but available manually)
$ spec steps complete 2
```

**Migration:** §7.3 remains as human-readable documentation. On first `spec build`, if `steps:` frontmatter is missing but §7.3 has content, offer to parse and migrate.

### 4.6 Auto-Advance

Stages can opt into automatic advancement:

```yaml
# spec.config.yaml
pipeline:
  stages:
    - name: pr_review
      auto_advance:
        when: "prs.all_approved and prs.threads_resolved"
        notify: [author, next_owner]
        quiet_hours: "22:00-08:00"  # defer until morning
```

**Behavior:**

1. On events that might change gate status (PR approved, PR merged, threads resolved), `spec` evaluates the `when` expression.
2. If true and outside quiet hours, advance happens automatically.
3. Notifications are sent per config.
4. If inside quiet hours, advance is queued for the next window.

**Event sources:**
- Webhook from GitHub/GitLab (if configured)
- Polling on `spec` invocation (always works, no webhook needed)
- Manual trigger via `spec check-auto-advance SPEC-042`

**Safeguards:**
- Auto-advance is opt-in per stage. Default is manual.
- `auto_advance.require_approval: tl` can require a TL to have approved the PR.
- `auto_advance.exclude_labels: [needs-discussion]` can exclude specs with certain labels.

### 4.7 Interactive Decisions

`spec decide SPEC-042` with no flags enters interactive mode:

```
$ spec decide SPEC-042

SPEC-042 — Auth refactor
Open decisions:

  #003  REST vs gRPC for rate limit service?
        Asked by @aaron on Apr 21

  #004  Should we support burst allowance?
        Asked by @agent on Apr 22

  [number] to resolve · [a]dd new · [q]uit

> 3

Resolving #003: REST vs gRPC for rate limit service?

Decision: gRPC
Rationale (opens $EDITOR for multi-line):
```

**MCP enhancement:**

Add `spec_list_open_decisions` tool so agents can see what's unresolved:

```json
{
  "tool": "spec_list_open_decisions",
  "result": [
    {"id": 3, "question": "REST vs gRPC?", "asked_by": "@aaron", "date": "2026-04-21"},
    {"id": 4, "question": "Burst allowance?", "asked_by": "@agent", "date": "2026-04-22"}
  ]
}
```

Enhance `spec_decide_resolve` to accept multi-line rationale.

### 4.8 Fast-Track Fixes

`spec fix` creates a minimal, engineer-owned spec that skips ceremony:

```
$ spec fix "Null pointer in auth handler" --repo auth-service

Created: SPEC-048 (fast-track bug fix)
Pipeline: triage → build → pr_review → done
Owner: you (engineer)
Branch: spec-048/fix-null-pointer

Start working? [Y/n]
```

**Constraints (configurable by TL):**

```yaml
# spec.config.yaml
fast_track:
  enabled: true
  allowed_roles: [engineer, tl]
  max_duration: 2d          # auto-escalate if not done in 2 days
  require_labels: [bug, hotfix]
  pipeline_variant: bug     # uses the 'bug' pipeline variant
  excluded_stages: [design, qa_expectations]  # these are skipped
```

If scope grows, `spec upgrade SPEC-048` converts to a full spec and notifies PM.

### 4.9 Inbox Actions

The passive awareness line becomes actionable:

```
$ spec build SPEC-042
⚠ 1 review pending: PR #418 (auth-service)
  [d]ismiss 2h · [v]iew · [o]pen · [enter] continue

> d

Dismissed until 14:30. Building SPEC-042...
```

**Actions:**
- `d` — Dismiss for 2 hours (configurable)
- `v` — Show details inline
- `o` — Open in browser
- `enter` — Continue with original command

**Filtering:**

```yaml
# ~/.spec/config.yaml
preferences:
  passive_awareness:
    show: [review_requests, spec_owned, mentions]
    hide: [triage, fyi]
    during_build: false   # don't interrupt during spec do/build
```

## 5. Design Inputs               <!-- owner: designer -->

### Core Principle: Get Out of the Way

The build loop is sacred. Every prompt, every confirmation, every extra line of output is friction. Design should:

- **Be scannable in <2 seconds** — Engineer glances, understands, continues
- **Default to action** — `[Y/n]` not `[y/N]`. Enter should do the common thing.
- **Hide when possible** — Success states are silent or one line. Errors are verbose.
- **Respect flow state** — No interruptions during `spec do`. Awareness waits for natural breaks.

### `spec plan` Output

**Entry point for engineering stage:**
```
$ spec plan SPEC-042

SPEC-042 — Auth refactor
Stage: engineering

Sections to complete:
  ✓ §7.1 Architecture Notes     342 words
  ○ §7.2 Dependencies & Risks   empty
  ○ §7.3 Build Plan          no steps

spec edit SPEC-042                    # Open in editor
spec draft SPEC-042 --build-plan        # AI-assisted decomposition  
spec plan ready SPEC-042              # Request review
```

Compact. Shows progress at a glance. Commands are suggestions, not required reading.

**Ready for review:**
```
$ spec plan ready SPEC-042

✓ All sections complete
✓ Review requested from @mike (tl)

  SPEC-042 will advance to 'build' when approved.
  Check status: spec status SPEC-042
```

**Validation failure:**
```
$ spec plan ready SPEC-042

✗ Cannot request review — sections incomplete:
  ○ §7.2 Dependencies & Risks (empty)
  ○ §7.3 Build Plan (no steps)

  Complete with 'spec edit SPEC-042' or 'spec draft --build-plan'
```

### `spec review --plan` Output

**Reviewer's view:**
```
$ spec review SPEC-042 --plan

SPEC-042 — Auth refactor
Plan by @aaron · Submitted 2h ago

─── §7.1 Architecture Notes ────────────────────────────
  [content, scrollable if long]

─── §7.2 Dependencies & Risks ──────────────────────────
  • Risk: Redis connection pooling
  • Risk: Config migration

─── §7.3 Build Plan ─────────────────────────────────────
  1. [auth-service] Add token bucket rate limiter
  2. [auth-service] Integrate into auth flow
  3. [api-gateway] Add rate limit headers  
  4. [frontend] Handle 429 responses

[a]pprove · [r]equest changes · [c]omment · [v]iew full spec · [q]uit
```

Single keypress actions. Content is truncated intelligently; `[v]` shows full spec if needed.

### `spec do` Output

**Happy path (resuming):**
```
$ spec do

Resuming SPEC-042 — Auth refactor
Step 2/4: Integrate Redis backend (auth-service)

Spawning claude-code...
```

Three lines. No box drawing. No emoji overload. The agent takes over.

**Happy path (starting fresh):**
```
$ spec do SPEC-042

Starting SPEC-042 — Auth refactor
Step 1/4: Add token bucket rate limiter (auth-service)
Branch: spec-042/step-1-rate-limiter

Spawning claude-code...
```

**With open decisions (blocking prompt):**
```
$ spec do SPEC-042

SPEC-042 has 2 open decisions:
  #003 REST vs gRPC for rate limit service?
  #004 Should we support burst allowance?

[s]kip and continue · [r]esolve now · [v]iew details
> 
```

Decisions are surfaced *before* spawning the agent so the engineer can address them or consciously skip.

**Error (wrong stage):**
```
$ spec do SPEC-042

✗ SPEC-042 is in 'draft' stage (owner: pm)
  You can work on it when it reaches 'build'.
  
  Check status: spec status SPEC-042
  View your queue: spec list
```

### `spec steps` Output

**Default view:**
```
$ spec steps

SPEC-042 — Auth refactor (2/4 complete)

  1. ✓ Add token bucket rate limiter          auth-service   PR #415 merged
  2. ► Integrate Redis backend                auth-service   PR #418 review
  3. ○ Add rate limit middleware              api-gateway    —
  4. ○ Add rate limit error handling          frontend       —

Current: step 2 · Run 'spec do' to continue
```

**Legend:**
- `✓` — Complete (green)
- `►` — In progress (bold/yellow)
- `○` — Pending (dim)
- `✗` — Blocked (red)

**Verbose view (`spec steps -v`):**
```
$ spec steps -v

SPEC-042 — Auth refactor

  1. ✓ Add token bucket rate limiter
     repo: auth-service
     branch: spec-042/step-1-rate-limiter
     PR: #415 (merged Apr 21)
     
  2. ► Integrate Redis backend
     repo: auth-service  
     branch: spec-042/step-2-redis
     PR: #418 (2 approvals, 1 thread open)
     
  ...
```

### `spec decide` Interactive Mode

```
$ spec decide SPEC-042

SPEC-042 — 2 open decisions

  3. REST vs gRPC for rate limit service?
     @aaron · Apr 21 · no options recorded
     
  4. Should we support burst allowance?
     @agent · Apr 22 · options: (a) yes with config (b) no, keep simple

[3/4] resolve · [a]dd new · [q]uit
> 3

Decision for #003:
> gRPC

Open editor for rationale? [Y/n]
> y

[opens $EDITOR with template]

✓ Decision #003 resolved
```

### `spec fix` Output

```
$ spec fix "Null pointer in auth handler" --repo auth-service

✓ Created SPEC-048 — Null pointer in auth handler
  Type: fast-track bug fix
  Pipeline: build → pr_review → done (3 stages)
  Branch: spec-048/fix-null-pointer

Start working? [Y/n] 
```

On `Y`, immediately runs `spec do SPEC-048`.

### Passive Awareness (Actionable)

**During normal commands:**
```
$ spec list
⚠ 1 pending: PR #418 needs your review [d/v/o/↵] 
```

Waits for single keypress. No enter required. Timeout (3s) continues automatically.

**During build (if enabled):**
Suppressed entirely. `preferences.passive_awareness.during_build: false`

**Dismissed state persists:**
```
$ spec list
⏸ 1 pending (snoozed until 14:30)

...
```

Dim, unobtrusive. Shows snooze is active.

### Multi-Repo Navigation

**Prompt when switching repos:**
```
✓ Step 2 complete — Integrate Redis backend

Next: Step 3 — Add rate limit middleware
      ~/code/api-gateway

[c]ontinue in new pane · [m]anual · [q]uit
> 
```

Single keypress. If `c`, new pane opens and this terminal shows:

```
→ Opened new pane for api-gateway
  This terminal is now idle. Close it or use for something else.
```

### Error States

**All errors include next action:**

```
✗ Workspace 'api-gateway' not configured
  
  Add to ~/.spec/config.yaml:
    workspaces:
      api-gateway: ~/code/api-gateway
  
  Or run: spec config add-workspace api-gateway ~/code/api-gateway
```

```
✗ Cannot auto-advance SPEC-042: 1 PR thread unresolved
  
  PR #418: "Can we use token bucket instead?" (@carlos)
  
  Resolve at: https://github.com/org/auth-service/pull/418
  Or advance manually: spec advance SPEC-042 --force
```

### Color Usage

| Element | Color | When |
|---------|-------|------|
| Success indicator | Green | `✓` |
| In-progress | Yellow/Bold | `►`, current step |
| Pending | Dim | `○`, future steps |
| Error | Red | `✗`, blocking issues |
| Prompt keys | Cyan | `[d/v/o/↵]` |
| Commands in help | Dim | `Run 'spec list'` |
| Snoozed/dismissed | Dim | `⏸` |

## 6. Acceptance Criteria         <!-- owner: qa -->

### US-28: `spec plan` entry point
- [ ] `spec plan SPEC-042` shows spec title, current stage, and section completion status
- [ ] Section completion shows: §7.1 Architecture Notes, §7.2 Dependencies & Risks, §7.3 Build Plan
- [ ] Each section shows word count or step count as appropriate
- [ ] Sections are marked ✓ (complete) or ○ (incomplete) based on content
- [ ] Suggested commands are shown: `spec edit`, `spec draft`, `spec plan ready`
- [ ] `spec plan SPEC-042 --edit` opens the spec in `$EDITOR` directly
- [ ] `spec plan` on a spec not in `engineering` stage shows error with guidance

### US-29: Section completion tracking
- [ ] §7.1 Architecture Notes is complete when non-empty and >50 words
- [ ] §7.2 Dependencies & Risks is complete when non-empty (explicit "none identified" counts)
- [ ] §7.3 Build Plan is complete when `steps:` frontmatter has ≥1 step
- [ ] Completion status updates immediately when spec is edited

### US-30: AI-assisted planning
- [ ] `spec draft SPEC-042 --section architecture_notes` generates architecture notes from spec context
- [ ] `spec draft SPEC-042 --section dependencies_risks` generates risk analysis
- [ ] `spec draft SPEC-042 --build-plan` generates build plan from §4 Proposed Solution
- [ ] All drafts go through accept/edit/skip flow
- [ ] Accepted build plan draft is written to `steps:` frontmatter, not §7.3 prose

### US-31: `spec plan ready`
- [ ] `spec plan ready SPEC-042` validates all three §7 sections are complete
- [ ] If validation fails, shows which sections are incomplete with remediation
- [ ] If validation passes, marks spec as ready for review
- [ ] Reviewers are determined from pipeline config `stages[engineering].review.reviewers`
- [ ] Notifications sent to configured reviewers
- [ ] Spec status shows "awaiting review" after `plan ready`

### US-32: Async plan review
- [ ] `spec review SPEC-042 --plan` shows the technical plan for review
- [ ] Content includes §7.1, §7.2, §7.3 sections
- [ ] Long content is truncated with option to view full
- [ ] Reviewer can approve, request changes, or comment
- [ ] Review actions are single-keypress (no Enter required)

### US-33: Review outcomes
- [ ] Approve: if `min_approvals` met, spec advances to `build`; author notified
- [ ] Approve: if `min_approvals` not yet met, approval recorded; author notified of progress
- [ ] Request changes: spec stays in `engineering`; author notified with feedback
- [ ] Comment: adds entry to decision log; no state change
- [ ] All review actions are logged in spec history

### US-34: Plan review status
- [ ] `spec status SPEC-042` shows review status: pending, approved, changes requested
- [ ] Shows reviewer names and their actions (approved, requested changes, commented)
- [ ] Shows how many approvals vs required (`1/2 approvals`)
- [ ] If changes requested, shows the feedback

### US-35: Configurable reviewers
- [ ] Pipeline config supports `stages[].review.reviewers` as role list (e.g., `[tl]`)
- [ ] Pipeline config supports `stages[].review.reviewers` as named users (e.g., `[@mike]`)
- [ ] Pipeline config supports `stages[].review.reviewers: [author]` for self-review
- [ ] Pipeline config supports `stages[].review.min_approvals` (default 1)
- [ ] Pipeline config supports `stages[].review.required: false` to skip review entirely
- [ ] When `review.required: false`, `spec plan ready` advances directly to `build`

### US-36: `spec do` stage validation
- [ ] `spec do SPEC-042` on a spec in `engineering` stage shows clear error
- [ ] Error message explains planning must be complete first
- [ ] Error message shows commands: `spec plan`, `spec plan ready`
- [ ] `spec do` only works when spec is at `build` stage

### US-01: `spec do SPEC-X` single command
- [ ] `spec do SPEC-042` with no local copy pulls from specs repo automatically
- [ ] `spec do SPEC-042` with stale local copy prompts to update (or auto-updates if `preferences.auto_pull: true`)
- [ ] `spec do SPEC-042` validates spec is at `build` stage; errors with guidance if not
- [ ] `spec do SPEC-042` validates user role matches stage owner; errors with guidance if not
- [ ] `spec do SPEC-042` creates a new session if none exists, starting at step 1
- [ ] `spec do SPEC-042` resumes existing session at current step if session exists
- [ ] `spec do SPEC-042` starts MCP server and spawns configured agent
- [ ] Total commands to go from dashboard to working in agent: **1**

### US-02: `spec do` with no arguments
- [ ] `spec do` with no arguments resumes the most recently active session
- [ ] If no active session exists, shows list of specs awaiting build by user and prompts to select
- [ ] "Most recent" is determined by `last_activity` timestamp in session state

### US-03: Auto-pull stale specs
- [ ] `spec do` compares local `.spec/` timestamp with specs repo
- [ ] If remote is newer, prompts: "Spec updated 2h ago. Pull latest? [Y/n]"
- [ ] If `preferences.auto_pull: true`, pulls without prompting
- [ ] After pull, continues with session as normal

### US-04: Workspace configuration
- [ ] `~/.spec/config.yaml` supports `workspaces:` map of repo name → local path
- [ ] `spec config add-workspace <name> <path>` adds entry interactively
- [ ] Invalid paths are rejected with clear error at config load time
- [ ] Missing workspace for a repo in build plan shows error with remediation command

### US-05: Auto-navigate to next repo
- [ ] When build plan step completes and next step is in a different repo, user is prompted
- [ ] Option `[c]ontinue in new pane` opens new terminal pane in the target repo
- [ ] New pane automatically runs `spec do` to continue session
- [ ] Option `[m]anual` prints the path and waits for user to switch themselves
- [ ] Option `[q]uit` ends session cleanly; next `spec do` resumes at the new step

### US-06: Unified build plan status
- [ ] `spec steps` shows all steps across all repos with status indicators
- [ ] Status indicators: `✓` complete, `►` in-progress, `○` pending, `✗` blocked
- [ ] PR numbers and status (open/merged/review) shown for steps with PRs
- [ ] Total progress shown: "2/4 complete"

### US-07: Opt-out of auto-navigation
- [ ] `preferences.auto_navigate: false` disables new pane opening
- [ ] With auto-navigate disabled, `[c]` option is not shown; only `[m]` and `[q]`
- [ ] Manual message includes full path and command to run

### US-08: Structured build plan in frontmatter
- [ ] Spec frontmatter supports `steps:` array with `repo`, `description`, `status` per step
- [ ] `status` values: `pending`, `in-progress`, `complete`, `blocked`
- [ ] Optional fields: `branch`, `pr` (number), `blocked_reason`
- [ ] Stack is the authoritative source of step state (not session file)

### US-09: `spec steps add`
- [ ] `spec steps add "<description>" --repo <repo>` appends a new step
- [ ] `--after <N>` inserts after step N instead of appending
- [ ] `--before <N>` inserts before step N
- [ ] New step has `status: pending` by default
- [ ] Stack is written to spec frontmatter immediately

### US-10: `spec steps reorder`
- [ ] `spec steps move <N> --after <M>` moves step N to after step M
- [ ] `spec steps move <N> --before <M>` moves step N to before step M
- [ ] `spec steps move <N> --first` moves step N to position 1
- [ ] Cannot move completed steps (error with guidance)

### US-11: `spec steps status`
- [ ] `spec steps` (alias: `spec steps status`) shows formatted build plan
- [ ] `spec steps -v` shows verbose details (branches, PR links, timestamps)
- [ ] `spec steps --json` outputs machine-readable format

### US-12: Authoritative step state
- [ ] `spec_step_complete` MCP tool updates steps frontmatter, not just session
- [ ] Session `current_step` is derived from build plan state on load (first non-complete step)
- [ ] Manual `spec steps complete <N>` updates frontmatter
- [ ] Completing a step out of order shows warning but allows it

### US-13: Auto-advance configuration
- [ ] Stage config supports `auto_advance.when: "<expression>"`
- [ ] Stage config supports `auto_advance.notify: [<targets>]`
- [ ] Stage config supports `auto_advance.quiet_hours: "HH:MM-HH:MM"`
- [ ] Auto-advance is disabled by default (must explicitly configure)

### US-14: Auto-advance execution
- [ ] When `auto_advance.when` expression becomes true, spec advances automatically
- [ ] Notifications sent per `auto_advance.notify` config
- [ ] Activity logged: "Auto-advanced from pr_review to qa_validation (gates satisfied)"
- [ ] Works via polling on `spec` invocation (no webhook required)

### US-15: Auto-advance disable per stage
- [ ] Stages without `auto_advance` config require manual `spec advance`
- [ ] `auto_advance.enabled: false` explicitly disables even if `when` is set
- [ ] Dashboard shows "ready to advance" indicator for manual-advance stages with satisfied gates

### US-16: Quiet hours
- [ ] Auto-advance during quiet hours is deferred, not skipped
- [ ] Deferred advances execute when quiet hours end
- [ ] `spec status` shows "Auto-advance pending (quiet hours until 08:00)"

### US-17: Interactive decision mode
- [ ] `spec decide SPEC-042` with no flags shows interactive TUI
- [ ] TUI lists all open decisions with question, author, date
- [ ] Selecting a decision prompts for resolution
- [ ] `[a]dd` option creates new decision
- [ ] `[q]uit` exits cleanly

### US-18: Multi-line rationale
- [ ] When resolving, user is prompted "Open editor for rationale? [Y/n]"
- [ ] `Y` opens `$EDITOR` with a template including the question for context
- [ ] Saved content becomes the rationale
- [ ] `n` allows single-line rationale at prompt

### US-19: MCP decision tools
- [ ] `spec_list_open_decisions` tool returns array of open decisions
- [ ] `spec_decide_resolve` accepts multi-line `rationale` parameter
- [ ] Decisions resolved via MCP appear in decision log with "agent" as decided_by

### US-20: Open decisions prompt at session start
- [ ] `spec do` with open decisions shows them before spawning agent
- [ ] Prompt offers: `[s]kip and continue`, `[r]esolve now`, `[v]iew details`
- [ ] `s` continues without resolving; decisions remain open
- [ ] `r` enters interactive decision mode; after resolution, continues to agent

### US-21: `spec fix` command
- [ ] `spec fix "<title>"` creates a new spec with fast-track pipeline
- [ ] Fast-track skips stages per `fast_track.excluded_stages` config
- [ ] Spec is owned by creator (engineer) from creation
- [ ] Branch is created automatically: `spec-<id>/fix-<slug>`
- [ ] Prompts to start working immediately after creation

### US-22: Fast-track pipeline
- [ ] Fast-track specs use `fast_track.pipeline_variant` if configured
- [ ] Default fast-track pipeline: `triage → build → pr_review → done`
- [ ] Design, QA expectations, and other ceremony stages are skipped

### US-23: Fast-track limits
- [ ] `fast_track.allowed_roles` restricts who can create fast-tracks
- [ ] `fast_track.max_duration` triggers escalation if exceeded
- [ ] `fast_track.require_labels` requires specific labels (e.g., `bug`)
- [ ] Attempting fast-track without meeting criteria shows clear error

### US-24: Upgrade fast-track to full spec
- [ ] `spec upgrade SPEC-048` converts fast-track to full spec
- [ ] Missing sections are populated with templates
- [ ] Pipeline switches to default (full) variant
- [ ] PM is notified of the upgrade
- [ ] Upgrade can happen at any stage

### US-25: Dismiss pending items
- [ ] Passive awareness line accepts `[d]` keypress to dismiss
- [ ] Dismiss duration is configurable (default 2h)
- [ ] Dismissed items don't appear in passive awareness until duration expires
- [ ] Dismiss state is stored in `~/.spec/dismissed.yaml`

### US-26: Act on pending items inline
- [ ] `[v]iew` shows details without running full `spec` dashboard
- [ ] `[o]pen` opens the item in browser (PR link, spec in docs provider)
- [ ] Actions complete quickly and return to original command

### US-27: Filter passive awareness
- [ ] `preferences.passive_awareness.show` whitelists item types
- [ ] `preferences.passive_awareness.hide` blacklists item types
- [ ] `preferences.passive_awareness.during_build: false` suppresses during `spec do`/`spec build`
- [ ] Item types: `review_requests`, `spec_owned`, `mentions`, `triage`, `fyi`, `blocked`

### General
- [ ] All new commands support `--help` with examples
- [ ] All new commands support `--json` for machine-readable output where applicable
- [ ] All errors include actionable next steps
- [ ] No regressions in existing command behavior

## 7. Technical Implementation    <!-- owner: engineer -->

### 7.1 Architecture Notes

#### Overview

SPEC-005 extends the existing `spec` architecture with seven new capabilities. The changes are primarily additive — new commands, new config fields, extended frontmatter — with minimal disruption to existing code paths.

**Guiding principles:**
- New commands in `cmd/` remain thin (parse flags, call `internal/`)
- Shared logic lives in new `internal/` packages, not duplicated across commands
- Config extensions are backward-compatible (new fields have sensible defaults)
- Frontmatter extensions are optional (specs without `steps:` still work)

#### Component Map

```
cmd/
├── plan.go           NEW   spec plan, spec plan ready
├── steps.go          NEW   spec steps (add/move/remove/complete)
├── fix.go            NEW   spec fix
├── upgrade.go        NEW   spec upgrade
├── do.go             MOD   auto-pull, stage validation, decision prompt
├── decide.go         MOD   interactive TUI mode
├── review.go         MOD   --plan flag for technical plan review
└── root.go           MOD   actionable passive awareness

internal/
├── planning/         NEW   technical planning phase logic
│   ├── planning.go         section completion tracking
│   ├── review.go           plan review request/approval workflow
│   └── validation.go       §7 section validation rules
│
├── workspace/        NEW   multi-repo orchestration
│   ├── workspace.go        workspace config loading
│   ├── navigate.go         terminal multiplexer integration
│   └── multiplexer.go      tmux/zellij/wezterm/iterm2 adapters
│
├── steps/            NEW   structured build plan operations
│   ├── steps.go            steps CRUD operations
│   ├── frontmatter.go      steps ↔ frontmatter serialization
│   └── migrate.go          §7.3 prose → YAML migration
│
├── inbox/            NEW   actionable passive awareness
│   ├── inbox.go            pending item aggregation
│   ├── dismiss.go          dismiss state persistence
│   └── actions.go          inline action handlers
│
├── fasttrack/        NEW   fast-track fix workflow
│   ├── fix.go              spec fix creation logic
│   └── upgrade.go          fast-track → full spec conversion
│
├── config/
│   ├── user.go       MOD   add workspaces, passive_awareness prefs
│   └── pipeline.go   MOD   add review config, auto_advance config
│
├── markdown/
│   └── frontmatter.go MOD  add BuildStep type, steps field
│
├── build/
│   ├── session.go    MOD   derive current_step from steps frontmatter
│   └── do.go         NEW   unified spec do orchestration
│
├── mcp/
│   └── tools.go      MOD   add spec_list_open_decisions tool
│
├── pipeline/
│   ├── autoadvance.go NEW  auto-advance polling and execution
│   └── gates.go      MOD   add review_approved gate type
│
└── tui/
    └── decide.go     NEW   interactive decision TUI
```

#### Extended Frontmatter Schema

```go
// internal/markdown/frontmatter.go

type SpecMeta struct {
    // ... existing fields ...
    
    // Stack is the structured build plan plan.
    // Replaces unstructured §7.3 prose.
    Stack []BuildStep `yaml:"steps,omitempty"`
    
    // Review tracks plan review state for engineering stage.
    Review *ReviewState `yaml:"review,omitempty"`
    
    // FastTrack marks this as a fast-track bug fix.
    FastTrack bool `yaml:"fast_track,omitempty"`
}

type BuildStep struct {
    Repo          string `yaml:"repo"`
    Description   string `yaml:"description"`
    Branch        string `yaml:"branch,omitempty"`
    PR            int    `yaml:"pr,omitempty"`
    Status        string `yaml:"status"` // pending, in-progress, complete, blocked
    BlockedReason string `yaml:"blocked_reason,omitempty"`
}

type ReviewState struct {
    RequestedAt time.Time        `yaml:"requested_at,omitempty"`
    Reviewers   []string         `yaml:"reviewers,omitempty"`
    Approvals   []ReviewApproval `yaml:"approvals,omitempty"`
    Status      string           `yaml:"status"` // pending, approved, changes_requested
    Feedback    string           `yaml:"feedback,omitempty"`
}

type ReviewApproval struct {
    Reviewer   string    `yaml:"reviewer"`
    ApprovedAt time.Time `yaml:"approved_at"`
}
```

#### Extended User Config Schema

```go
// internal/config/user.go

type UserConfig struct {
    User        UserIdentity      `yaml:"user"`
    Preferences PreferencesConfig `yaml:"preferences"`
    Workspaces  map[string]string `yaml:"workspaces,omitempty"` // NEW: repo name → local path
}

type PreferencesConfig struct {
    // ... existing fields ...
    
    Multiplexer      string                 `yaml:"multiplexer,omitempty"`       // NEW: tmux|zellij|wezterm|iterm2|none
    AutoPull         bool                   `yaml:"auto_pull,omitempty"`         // NEW: auto-pull stale specs
    AutoNavigate     *bool                  `yaml:"auto_navigate,omitempty"`     // NEW: auto-open new pane (default true)
    PassiveAwareness *PassiveAwarenessConfig `yaml:"passive_awareness,omitempty"` // NEW
}

type PassiveAwarenessConfig struct {
    Show        []string `yaml:"show,omitempty"`         // whitelist item types
    Hide        []string `yaml:"hide,omitempty"`         // blacklist item types  
    DuringBuild bool     `yaml:"during_build,omitempty"` // show during spec do (default false)
    DismissDuration string `yaml:"dismiss_duration,omitempty"` // default "2h"
}
```

#### Extended Pipeline Config Schema

```go
// internal/config/pipeline.go

type StageConfig struct {
    // ... existing fields ...
    
    // Review configures plan review requirements for this stage.
    Review *StageReviewConfig `yaml:"review,omitempty"` // NEW
    
    // AutoAdvance configures automatic stage advancement.
    AutoAdvance *AutoAdvanceConfig `yaml:"auto_advance,omitempty"` // NEW
}

type StageReviewConfig struct {
    Required     bool     `yaml:"required,omitempty"`     // default true
    Reviewers    []string `yaml:"reviewers,omitempty"`    // roles or @names
    MinApprovals int      `yaml:"min_approvals,omitempty"` // default 1
}

type AutoAdvanceConfig struct {
    Enabled    *bool    `yaml:"enabled,omitempty"`     // default true if When is set
    When       string   `yaml:"when,omitempty"`        // expression
    Notify     []string `yaml:"notify,omitempty"`      // notification targets
    QuietHours string   `yaml:"quiet_hours,omitempty"` // "HH:MM-HH:MM"
}
```

#### Extended Team Config Schema

```go
// internal/config/config.go

type Config struct {
    // ... existing fields ...
    
    // FastTrack configures engineer self-service for bug fixes.
    FastTrack *FastTrackConfig `yaml:"fast_track,omitempty"` // NEW
}

type FastTrackConfig struct {
    Enabled         bool     `yaml:"enabled,omitempty"`          // default false
    AllowedRoles    []string `yaml:"allowed_roles,omitempty"`    // default [engineer, tl]
    MaxDuration     string   `yaml:"max_duration,omitempty"`     // e.g., "2d"
    RequireLabels   []string `yaml:"require_labels,omitempty"`   // e.g., [bug, hotfix]
    PipelineVariant string   `yaml:"pipeline_variant,omitempty"` // variant to use
    ExcludedStages  []string `yaml:"excluded_stages,omitempty"`  // stages to skip
}
```

#### Technical Planning Flow

```go
// internal/planning/planning.go

// SectionStatus tracks completion state for §7 sections.
type SectionStatus struct {
    Slug      string
    Name      string
    Complete  bool
    WordCount int    // for prose sections
    StepCount int    // for steps section
    Message   string // e.g., "342 words" or "4 steps"
}

// CheckSections evaluates §7 section completion.
func CheckSections(spec *markdown.SpecMeta, content string) []SectionStatus {
    return []SectionStatus{
        checkArchitectureNotes(content),   // §7.1: non-empty, >50 words
        checkDependenciesRisks(content),   // §7.2: non-empty
        checkBuildPlan(spec),            // §7.3: steps has ≥1 step
    }
}

// AllSectionsComplete returns true if all §7 sections are complete.
func AllSectionsComplete(sections []SectionStatus) bool

// internal/planning/review.go

// RequestReview marks the spec as ready for plan review.
func RequestReview(spec *markdown.SpecMeta, cfg *config.StageConfig) error {
    // 1. Validate all sections complete
    // 2. Determine reviewers from cfg.Review.Reviewers
    // 3. Set spec.Review = &ReviewState{Status: "pending", ...}
    // 4. Write updated frontmatter
    // 5. Send notifications to reviewers
}

// ApprovePlan records an approval and advances if min_approvals met.
func ApprovePlan(spec *markdown.SpecMeta, reviewer string, cfg *config.StageConfig) error

// RequestChanges records change request and notifies author.
func RequestChanges(spec *markdown.SpecMeta, reviewer, feedback string) error
```

#### Workspace Navigation

```go
// internal/workspace/navigate.go

// Navigator handles cross-repo navigation.
type Navigator struct {
    Workspaces  map[string]string // from user config
    Multiplexer string            // tmux, zellij, etc.
}

// OpenInNewPane opens a new terminal pane in the target repo.
// Returns an error if multiplexer is not available.
func (n *Navigator) OpenInNewPane(repo, command string) error

// internal/workspace/multiplexer.go

// Multiplexer abstracts terminal multiplexer operations.
type Multiplexer interface {
    // SplitPane opens a new pane and runs the command.
    SplitPane(workDir, command string) error
    // Available returns true if this multiplexer is running.
    Available() bool
}

func NewTmuxMultiplexer() Multiplexer    // tmux split-window -c <dir> <cmd>
func NewZellijMultiplexer() Multiplexer  // zellij run --cwd <dir> -- <cmd>
func NewWeztermMultiplexer() Multiplexer // wezterm cli split-pane --cwd <dir> -- <cmd>
func NewItermMultiplexer() Multiplexer   // osascript for iTerm2 (macOS only)
```

#### Unified `spec do` Flow

```go
// internal/build/do.go

// DoOptions configures the unified spec do command.
type DoOptions struct {
    SpecID    string
    AutoPull  bool
    SkipDecisions bool
}

// Do executes the unified spec do workflow.
func Do(ctx context.Context, opts DoOptions) error {
    // 1. Resolve spec (from .spec/ or pull from specs repo)
    // 2. Validate stage == "build"
    // 3. Validate user role matches stage owner
    // 4. Load or create session (derive current_step from steps frontmatter)
    // 5. Check for open decisions → prompt if any
    // 6. Start MCP server
    // 7. Spawn agent via adapter
    // 8. On agent exit, check if step completed → prompt for next repo if needed
}

// ResolveSpec finds the spec, pulling if necessary.
func ResolveSpec(specID string, autoPull bool) (*markdown.SpecMeta, string, error) {
    // Check .spec/<id>.md exists
    // If not, pull from specs repo
    // If exists but stale, prompt or auto-pull based on prefs
}
```

#### Auto-Advance Polling

```go
// internal/pipeline/autoadvance.go

// CheckAutoAdvance evaluates auto-advance conditions for all specs.
// Called on every `spec` invocation (dashboard, list, etc.).
func CheckAutoAdvance(ctx context.Context, cfg *config.Config, db *store.DB) error {
    // 1. Load all specs at stages with auto_advance configured
    // 2. For each, evaluate the `when` expression
    // 3. If true and outside quiet hours, advance
    // 4. If true and inside quiet hours, queue for later
    // 5. Send notifications per config
}

// ParseQuietHours parses "HH:MM-HH:MM" format.
func ParseQuietHours(s string) (start, end time.Time, err error)

// InQuietHours returns true if the current time is within quiet hours.
func InQuietHours(start, end time.Time) bool
```

#### New Gate Type: `review_approved`

```go
// internal/pipeline/gates.go

// Add to gate evaluation:
case gate.ReviewApproved != nil && *gate.ReviewApproved:
    if spec.Review == nil || spec.Review.Status != "approved" {
        return GateResult{
            Passed:  false,
            Message: "Plan review not yet approved",
        }
    }
```

#### MCP Tool: `spec_list_open_decisions`

```go
// internal/mcp/tools.go

func (s *Server) registerTools() {
    // ... existing tools ...
    
    s.tools["spec_list_open_decisions"] = Tool{
        Name:        "spec_list_open_decisions",
        Description: "List unresolved decisions for the current spec",
        Handler:     s.handleListOpenDecisions,
    }
}

func (s *Server) handleListOpenDecisions(args map[string]any) (any, error) {
    decisions := s.spec.DecisionLog.OpenDecisions()
    return decisions, nil
}
```

#### Interactive Decision TUI

```go
// internal/tui/decide.go

import "github.com/charmbracelet/huh"

// RunDecideInteractive shows the interactive decision resolution TUI.
func RunDecideInteractive(spec *markdown.SpecMeta, content string) error {
    decisions := parseOpenDecisions(content)
    if len(decisions) == 0 {
        fmt.Println("No open decisions.")
        return nil
    }
    
    // Show list, let user select one to resolve
    // On selection, prompt for decision + rationale
    // Rationale can open $EDITOR for multi-line
}
```

### 7.2 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Terminal multiplexer integration is brittle across platforms | Medium | Medium | Detect availability at runtime; graceful fallback to manual navigation; extensive testing on macOS/Linux |
| Frontmatter `steps:` field conflicts with existing §7.3 prose | Low | Medium | Migration is opt-in on first `spec build`; keep §7.3 as human-readable documentation; steps is authoritative |
| Auto-advance polling adds latency to every `spec` invocation | Medium | Low | Polling is async after command completes; cache recent gate evaluations; <50ms target |
| Interactive TUI doesn't work in non-TTY environments (CI, piped) | Low | Low | Detect non-TTY and fall back to flag-based interface; clear error messages |
| Review state in frontmatter creates merge conflicts | Medium | Medium | Review state is transient (cleared on stage advance); conflicts resolved by latest write wins |
| `spec fix` bypasses important process for complex bugs | Medium | High | Configurable guardrails (max_duration, require_labels); `spec upgrade` escape hatch; TL can disable fast_track |
| Workspace paths become stale when repos move | Low | Low | Validate paths on config load; clear error with remediation on missing workspace |
| Quiet hours timezone handling is complex | Medium | Low | Use local timezone; document behavior; allow explicit TZ in config if needed later |

**New Dependencies:**

| Dependency | Purpose | Already Used | Risk |
|------------|---------|--------------|------|
| `github.com/charmbracelet/huh` | Interactive TUI forms | Yes (SPEC-004) | Low |
| `github.com/expr-lang/expr` | Expression evaluation | Yes (SPEC-004) | Low |
| None new | — | — | — |

No new external dependencies required. All functionality builds on existing packages.

### 7.3 Build Plan

#### Phase 1: Foundation

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 1 | spec-cli | Extended frontmatter schema | Add `Stack`, `ReviewState` types to `internal/markdown/frontmatter.go`; add read/write support; tests |
| 2 | spec-cli | Extended user config schema | Add `Workspaces`, `PassiveAwareness`, `Multiplexer`, `AutoPull` to `internal/config/user.go`; tests |
| 3 | spec-cli | Extended pipeline config schema | Add `StageReviewConfig`, `AutoAdvanceConfig` to `internal/config/pipeline.go`; add `review_approved` gate type; tests |
| 4 | spec-cli | Extended team config schema | Add `FastTrackConfig` to `internal/config/config.go`; tests |

#### Phase 2: Technical Planning

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 5 | spec-cli | Planning package foundation | Create `internal/planning/`; section completion tracking (`CheckSections`, `AllSectionsComplete`); tests |
| 6 | spec-cli | `spec plan` command | Create `cmd/plan.go`; show section completion status; `--edit` flag; tests |
| 7 | spec-cli | Plan review workflow | Add `internal/planning/review.go`; `RequestReview`, `ApprovePlan`, `RequestChanges`; tests |
| 8 | spec-cli | `spec plan ready` subcommand | Extend `cmd/plan.go`; validate sections; request review; notify reviewers; tests |
| 9 | spec-cli | `spec review --plan` | Extend `cmd/review.go`; show plan content; single-keypress approve/request changes/comment; tests |

#### Phase 3: Structured Build Plans

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 10 | spec-cli | Steps package foundation | Create `internal/steps/`; CRUD operations on `BuildStep`; frontmatter serialization; tests |
| 11 | spec-cli | `spec steps` command | Create `cmd/steps.go`; show steps status with indicators; `-v` verbose mode; `--json`; tests |
| 12 | spec-cli | `spec steps add/move/remove` | Extend `cmd/steps.go`; steps manipulation subcommands; tests |
| 13 | spec-cli | `spec steps complete` | Extend `cmd/steps.go`; manual step completion; warning for out-of-order; tests |
| 14 | spec-cli | Steps ↔ session integration | Update `internal/build/session.go` to derive `current_step` from steps frontmatter; update MCP `spec_step_complete` to write to frontmatter; tests |
| 15 | spec-cli | §7.3 prose → YAML migration | Add `internal/steps/migrate.go`; offer migration on first `spec build` if steps empty but §7.3 has content; tests |

#### Phase 4: Unified `spec do`

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 16 | spec-cli | `spec do` auto-pull | Extend `cmd/do.go`; check spec freshness; prompt or auto-pull based on prefs; tests |
| 17 | spec-cli | `spec do` stage validation | Extend `cmd/do.go`; validate stage == `build`; clear error with guidance for wrong stage; tests |
| 18 | spec-cli | Open decisions prompt | Extend `cmd/do.go`; check for open decisions before spawning agent; [s]kip/[r]esolve/[v]iew; tests |
| 19 | spec-cli | `spec do` no-args resume | Extend `cmd/do.go`; find most recent active session; prompt if multiple; tests |

#### Phase 5: Workspace Mode

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 20 | spec-cli | Workspace package foundation | Create `internal/workspace/`; workspace config loading; path validation; tests |
| 21 | spec-cli | Multiplexer detection | Add `internal/workspace/multiplexer.go`; detect tmux/zellij/wezterm/iterm2; tests |
| 22 | spec-cli | Multiplexer adapters | Implement `SplitPane` for each multiplexer; macOS iTerm2 via osascript; tests |
| 23 | spec-cli | Cross-repo navigation | Integrate with `spec do`; prompt [c]ontinue/[m]anual/[q]uit on repo change; open new pane; tests |
| 24 | spec-cli | `spec config add-workspace` | Extend `cmd/config.go`; interactive workspace addition; tests |

#### Phase 6: Auto-Advance

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 25 | spec-cli | Auto-advance engine | Create `internal/pipeline/autoadvance.go`; `CheckAutoAdvance` polling; quiet hours parsing; tests |
| 26 | spec-cli | Auto-advance integration | Call `CheckAutoAdvance` from dashboard/root command; async after main output; tests |
| 27 | spec-cli | Deferred advance queue | Store pending advances for quiet hours; execute on next invocation outside quiet hours; tests |

#### Phase 7: Interactive Decisions

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 28 | spec-cli | Interactive decision TUI | Create `internal/tui/decide.go`; list open decisions; select to resolve; tests |
| 29 | spec-cli | `spec decide` interactive mode | Extend `cmd/decide.go`; no-flags enters TUI; multi-line rationale via $EDITOR; tests |
| 30 | spec-cli | MCP `spec_list_open_decisions` | Extend `internal/mcp/tools.go`; add new tool; tests |

#### Phase 8: Fast-Track Fixes

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 31 | spec-cli | Fast-track package | Create `internal/fasttrack/`; fix creation logic; config validation; tests |
| 32 | spec-cli | `spec fix` command | Create `cmd/fix.go`; create minimal spec; auto-branch; prompt to start working; tests |
| 33 | spec-cli | `spec upgrade` command | Create `cmd/upgrade.go`; convert fast-track to full spec; notify PM; tests |
| 34 | spec-cli | Fast-track escalation | Add duration check; notify on max_duration exceeded; tests |

#### Phase 9: Inbox Actions

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 35 | spec-cli | Inbox package | Create `internal/inbox/`; pending item aggregation; dismiss state persistence; tests |
| 36 | spec-cli | Actionable passive awareness | Extend `cmd/root.go`; single-keypress actions; timeout auto-continue; tests |
| 37 | spec-cli | Dismiss state | Add `~/.spec/dismissed.yaml`; respect dismiss duration; tests |
| 38 | spec-cli | Passive awareness filtering | Respect `preferences.passive_awareness` config; suppress during build; tests |

#### Phase 10: Polish & Documentation

| # | Repo | Description | Scope |
|---|------|-------------|-------|
| 39 | spec-cli | Help text updates | Update `--help` for all new/modified commands; add examples |
| 40 | spec-cli | Documentation | Update `README.md`; add `docs/workflows.md` with planning → build → review flow |
| 41 | spec-cli | Integration tests | End-to-end tests for planning → review → build → complete flow |

## 8. Escape Hatch Log            <!-- auto: spec eject -->

## 9. QA Validation Notes         <!-- owner: qa -->

## 10. Deployment Notes           <!-- owner: engineer -->

## 11. Retrospective              <!-- auto: spec retro -->
