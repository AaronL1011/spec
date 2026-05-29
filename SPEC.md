---
id: SPEC-001
title: spec - Developer Control Plane
status: draft
version: 0.3.0
author: -
cycle: TBD
created: 2026-04-17
updated: 2026-04-21
---

# SPEC-001 - `spec` - Developer Control Plane

> *Your terminal is your office. `spec` is everything else.*

---

## Decision Log

> *Record all significant decisions, questions and changes here for asynchronous reference.*

| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Plugin vs. adapter architecture for integrations? | (1) Hardcoded integrations per tool, (2) Adapter pattern with community plugins, (3) Config-driven with provider abstractions | **(3) Config-driven with provider abstractions** | Keeps core logic decoupled from any specific tool; new providers added via adapters without modifying core; config is the single place teams declare their stack | - | 2026-04-17 |
| 002 | Local CLI vs. hosted service? | (1) Pure CLI with local config, (2) SaaS with team workspace, (3) CLI + optional cloud sync | **(1) Pure CLI with local config for v1** | Ship fast with minimal infrastructure; architecture should leave the door open for optional cloud sync in future (team dashboard, cross-repo visibility) but v1 is local-only | - | 2026-04-17 |
| 003 | Spec storage - where is the golden source of truth? | (1) Per-service repo `.spec/` directory, (2) External tool only (Confluence/Notion), (3) Canonical in external tool mirrored to repo, (4) Dedicated specs repo as canonical source | **(4) Dedicated specs repo** | Specs are a team artefact, not a repo artefact - a PM writes the problem statement before anyone knows which repos are involved; a dedicated specs repo makes cross-repo specs the default, simplifies `spec list`/`search`/`history`, and gives PMs/designers a single place to contribute without touching service repos | - | 2026-04-17 |
| 004 | How is a user's `owner_role` resolved at runtime? | (1) Declared once in `~/.spec/config.yaml` (user-level), (2) Declared in repo-level `spec.config.yaml` per team member, (3) Prompted interactively on first `spec` command if not set | **(1) + (3) User-level config with interactive fallback** | Role is personal, not repo-scoped; declared once in `~/.spec/config.yaml`; if missing, `spec` prompts the user to configure on first role-dependent command | - | 2026-04-17 |
| 005 | Should `spec list` query live from the docs/PM integration or from a local cache? | (1) Always live - query Confluence/Jira on invocation, (2) Local cache synced on `spec pull`, (3) Live with offline fallback to cache | **(3) Live with offline fallback to cache** | Best of both: fresh data when online, graceful degradation offline; cache is populated as a side effect of live queries; with specs repo as canonical, `spec list` can read frontmatter directly from git with remote enrichment (PR status, ticket status) | - | 2026-04-17 |
| 006 | Should specs span multiple repos? | (1) Spec lives in one service repo only, (2) Spec lives in a shared workspace directory, (3) Dedicated specs repo with `spec pull` into service repos | **(3) Dedicated specs repo with `spec pull`** | End-to-end features often span multiple repos; tying a spec to one repo forces an awkward "which repo owns this?" decision; a dedicated specs repo makes multi-repo the default; engineers `spec pull` into their working repo for local context and agent builds | - | 2026-04-17 |
| 007 | How do PMs and designers contribute without touching the specs repo? | (1) They edit markdown directly in the specs repo, (2) One-way outbound sync only (repo → Confluence), (3) Bidirectional section-scoped sync via external tools | **(3) Bidirectional section-scoped sync** | PMs should write in Confluence, designers should link from Figma/Teams; `spec sync` pulls their changes inward, scoped by `<!-- owner: role -->` markers so sections can't overwrite each other; outbound sync publishes the full spec for reading | - | 2026-04-17 |
| 008 | Product framing - spec lifecycle tool or developer control plane? | (1) Spec lifecycle manager that integrates with tools, (2) Unified developer interface that uses specs as coordination primitive | **(2) Developer control plane** | The core pain isn't "managing spec documents" - it's context-switching across a fragmented tool stack; devs don't want a better spec tool, they want to never leave the terminal; `spec` should subsume the *interfaces* of Jira/Slack/GitHub/Confluence for the developer persona while those tools remain the data stores; the spec lifecycle is the coordination backbone, not the product surface | - | 2026-04-21 |
| 009 | Should `spec` with no arguments show help or a dashboard? | (1) Standard CLI help text, (2) Personal dashboard aggregating all signals, (3) Dashboard with `--help` for help | **(3) Dashboard default with `--help` for help** | The bare `spec` invocation is the highest-frequency command - it should be the first thing a dev runs each morning; showing help wastes the most valuable real estate in the product; dashboard replaces opening Jira + GitHub + Slack to check status | - | 2026-04-21 |
| 010 | Push notifications vs. pull-based awareness? | (1) Push only (webhooks on transitions), (2) Pull only (check on next invocation), (3) Push for urgency + passive pull on every invocation | **(3) Push + pull** | Push notifications (Teams/Slack) handle urgent transitions but interrupt flow state; pull-based awareness (a one-line status bar on every `spec` invocation) surfaces pending items when the dev is *ready* - the Unix "you have mail" model; both are needed | - | 2026-04-21 |
| 011 | Should intake/triage be a first-class pipeline stage? | (1) Specs always start at `draft` (PM creates them), (2) Add `triage` stage before `draft` for lightweight intake, (3) Separate intake queue outside the pipeline | **(2) `triage` stage as pipeline entry point** | Half of engineering work originates from bugs, alerts, and ad-hoc requests - not PM-authored specs; a lightweight `triage` stage lets anyone create an intake item with just a title, source, and priority; the PM then fleshes it into a full spec at `draft` or the TL fast-tracks it; this captures the full "feature request and bug alert intake" workflow | - | 2026-04-21 |
| 012 | Should the pipeline extend past `done` into deployment? | (1) Pipeline ends at `done`, deployment is external, (2) Optional deployment stages after `done`, (3) Mandatory deployment stages | **(2) Optional post-merge stages** | "Done" in real life means deployed, validated in staging, promoted to production, and monitored for regressions - not just "PRs merged"; optional `deploying` and `monitoring` stages close the loop without forcing teams that deploy differently to conform; the `optional: true` flag keeps the pipeline configurable | - | 2026-04-21 |
| 013 | What role should AI/LLM play in `spec`? | (1) No AI - purely deterministic, (2) AI-required for core features, (3) AI as progressive enhancement - every feature works without it, AI drafts content for human review | **(3) Progressive enhancement with human-in-the-loop** | `spec` must never depend on an LLM provider for core functionality; AI is a drafting assistant, not a decision maker; every AI-powered feature returns a draft that the user accepts, edits, or skips; the `ai` integration is entirely optional - omitting it or setting `provider: none` is a first-class configuration, not a degraded state; this also means `spec` works offline, air-gapped, and in enterprises that can't send data to external APIs | - | 2026-04-21 |
| 014 | Should `agent` (coding tool) and `ai` (spec's own LLM) be the same integration? | (1) Single `agent` config for both, (2) Separate `agent` and `ai` integrations | **(2) Separate integrations** | They serve different purposes - `agent` is an external coding tool that `spec` orchestrates by writing files and launching processes (Claude Code, Cursor, etc.); `ai` is an LLM API that `spec` itself calls for content drafting, summarisation, and semantic search; conflating them forces teams to use their coding agent's API for unrelated tasks; separating them lets a team use Cursor for builds and Ollama for drafting, or Claude Code for builds and no AI at all | - | 2026-04-21 |
| 015 | How should `spec` deliver context to coding agents? | (1) Write to agent-specific config files (CLAUDE.md, .cursor/rules, etc.), (2) MCP server that agents connect to, (3) Consolidated context file passed as CLI arg, (4) MCP primary + context file fallback | **(4) MCP primary + context file fallback** | Writing to proprietary config files is brittle, pollutes the workspace, and requires a format-specific adapter for every new agent; MCP inverts the relationship - `spec` exposes structured context via a standard protocol and any MCP-compatible agent connects automatically; Claude Code, Cursor, Copilot, and Pi all support MCP already; the MCP server also enables bidirectional communication (agent can record decisions, mark steps done mid-session); consolidated context file is the fallback for non-MCP agents | - | 2026-04-21 |

---

## 1. Problem Statement <!-- owner: pm -->

Engineers are drowning in tools. The average product engineering team operates across 5-10 distinct platforms daily - Jira for tickets, Confluence for docs, Slack or Teams for communication, GitHub or GitLab for code, Figma for design, PagerDuty for alerts, CI/CD dashboards for deploys, and more. Each tool has its own interface, its own notification model, its own mental context. None of them talk to each other in a way that respects developer flow state.

The result is death by a thousand context switches. An engineer's day looks like this:

1. Open Slack - scan 4 channels for anything urgent
2. Open Jira - check the board, figure out what's assigned
3. Open Confluence - find the spec doc someone mentioned yesterday
4. Open GitHub - check PR review requests, CI status
5. Open the terminal - finally start coding
6. Get a Slack notification - context switch, lose 20 minutes
7. Open Jira again - update the ticket status so the PM stops asking
8. Repeat

This isn't just annoying. It's structurally broken:

- **Decisions are scattered.** A critical architecture decision lives in a Slack thread from three weeks ago that nobody can find. The Confluence page says something different. The Jira ticket has no context at all.
- **Handoffs are invisible.** The PM finishes a spec draft but the TL doesn't know it's ready. The designer posts mockups in Figma but the engineer doesn't know they exist. QA adds acceptance criteria after the code is already written.
- **Specs go stale.** There is no single authoritative version of what's being built or why. The Google Doc, the Confluence page, and the README.md in the repo all tell different stories.
- **Agentic tools are starved of context.** Engineers using AI coding agents re-prompt from scratch every session because there's no structured way to feed the agent a complete, current spec with decisions, constraints, and acceptance criteria.
- **Institutional knowledge evaporates.** When someone asks "why did we build authentication this way?" the answer is buried across 6 Slack threads, 2 Confluence pages, a PR description, and someone's memory. New hires have no structured onboarding path through past decisions.
- **Work arrives from everywhere.** Bug reports from support, alerts from monitoring, feature requests from stakeholders, tech debt items engineers notice mid-build - all of these enter the system through different doors with no unified intake or prioritisation.

The fundamental insight is this: **developers don't hate their tools. They hate having to go to their tools.** The terminal is where engineers think, build, and ship. Every time they leave it, they pay a cognitive tax. The solution isn't a better Jira or a better Confluence - it's a unified interface that brings all of those signals into the one place developers already live.

### Who is affected?

| Role | Pain Today |
|---|---|
| Engineer | Context-switches across 5+ tools daily; rebuilds agent prompts from scratch; never sure if a spec is current; updates ticket status manually as busywork |
| Tech Lead | Manually stages work for engineers; has no single view of pipeline health; chases status updates across tools |
| PM | No standard spec template; hard to track spec status across cycles; can't tell if engineers have started or are blocked without asking |
| Designer | No defined entry/exit point in the spec pipeline; uploads to Figma but has no signal that engineering saw it |
| QA | Acceptance criteria added late or forgotten; no structured validation workflow; discovers requirements gaps after code is written |
| New Hire | No structured knowledge base to onboard from; system understanding requires archaeology across Confluence, Slack history, and tribal knowledge |

---

## 2. Goals & Non-Goals <!-- owner: pm -->

### Goals

- **Be the developer's single interface to the product lifecycle.** From intake to deployment, every interaction an engineer has with specs, tickets, reviews, notifications, and decisions should be possible without leaving the terminal.
- **Aggregate signals, not duplicate data.** `spec` reads from and writes to the tools a team already uses (Jira, Confluence, GitHub, Slack, etc.). It replaces their *interfaces* for developers, not their *data stores*.
- **Make `spec` (no arguments) the first command of the day.** A personal, prioritised dashboard that replaces opening Jira + Slack + GitHub to figure out what needs attention.
- **Standardise the `SPEC.md` format** as the coordination primitive - the single document that captures problem, solution, decisions, design, acceptance criteria, and implementation notes with clear ownership per section.
- **Automate handoffs** between contributors and roles via notifications and status transitions so nobody waits for a meeting to know it's their turn.
- **Surface the right context to agentic coding tools** at the right phase, with session continuity so engineers don't re-prompt from scratch.
- **Accumulate a searchable knowledge base** of every spec, decision, and rationale the team has ever produced - queryable from the terminal.
- **Capture the full lifecycle** - from bug report and feature request intake, through speccing, refinement, build, review, deployment, and retrospective.
- **Be adoptable by any team** using any common stack combination (Atlassian, GitHub/Linear/Slack, Notion/GitHub/Discord, etc.) via config-driven adapters.
- **Support specs that span multiple service repos** without requiring a "home repo" decision.
- **Ship incrementally** - each version is independently useful, from local-only spec management (v0.1) to full control plane (v1.0).

### Non-Goals

- `spec` **does not replace** Jira, Linear, Confluence, Slack, or GitHub as data stores. It subsumes their developer-facing interfaces. PMs can keep living in Jira. Designers can keep living in Figma. `spec` meets each role where they are.
- `spec` is **not an AI coding agent** - it orchestrates agents and provides them structured context. It is not one itself.
- `spec` **does not depend on an LLM provider**. AI features are progressive enhancements - every command works fully without an `ai` integration configured. AI drafts content for human review; it never writes directly to specs, makes decisions, or gates pipeline transitions.
- `spec` **does not enforce** any specific cycle length, team structure, or methodology beyond the configurable pipeline stages.
- `spec` is **not a CI/CD system** - it triggers deployments via adapters, it does not run them.
- `spec` is **not a monitoring tool** - it can surface alerts via intake adapters, but observability remains in dedicated tools.

---

## 3. User Stories <!-- owner: pm -->

*(QA to add acceptance criteria per story)*

### Intake & Triage

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-01 | Anyone | Create a lightweight triage item from the CLI when I spot a bug, receive an alert, or hear a feature request | Work enters the system immediately without requiring a full spec upfront |
| US-01a | Engineer | Have `spec` auto-summarise a PagerDuty alert or Slack thread into a triage description when AI is configured | I capture intake items in seconds without manually distilling noisy sources |
| US-02 | PM | Flesh out a triage item into a full spec draft, with AI-drafted sections where configured | The lightweight intake becomes a structured, reviewable document without starting from a blank page |
| US-03 | TL | Fast-track a triage item directly to engineering for urgent fixes | Critical bugs skip the full spec ceremony when appropriate |

### Speccing & Refinement

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-04 | PM | Create a new spec from a standard template | All stakeholders start from consistent structure |
| US-05 | PM | Notify the TL that a spec draft is ready for feasibility review | The process moves forward without a meeting |
| US-06 | TL | Advance a spec to the next pipeline stage | Downstream contributors are unblocked |
| US-07 | Designer | Know exactly what section of the spec I own and when it is needed | I can contribute at the right moment without attending a kickoff meeting |
| US-08 | QA | Add acceptance criteria to a spec before engineering begins | Engineers and agents know what "done" looks like upfront |
| US-09 | PM | Edit my spec sections in Confluence and have changes flow back to the specs repo | I don't have to learn git or markdown tooling to contribute |
| US-10 | Designer | Attach a Figma link or design notes to a spec from my usual tools | I can contribute design context without switching to a developer workflow |
| US-11 | Any team member | Record a decision or question against a spec from the command line | Decisions are captured in the moment rather than forgotten or buried in chat |
| US-11a | Any team member | Request an AI draft of any spec section based on existing context | I get a starting point instead of a blank page, which I then review and edit |

### The Dashboard - `spec` as daily driver

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-12 | Engineer | Run `spec` with no arguments and see everything that needs my attention | I never need to open Jira, Slack, or GitHub to start my day |
| US-13 | Engineer | See a passive "you have mail" indicator on every `spec` command when something is pending | I'm aware of incoming work without being interrupted mid-flow |
| US-14 | Any team member | Run `spec list` and see only specs currently awaiting action from my role | I have a personal action queue without filtering noise from the whole pipeline |
| US-15 | TL | Watch a live-updating pipeline view in my terminal | I can monitor team throughput without opening a project management dashboard |

### Build & Implementation

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-16 | Engineer | Pull a fully-staged spec into my local service repo | I can immediately begin technical planning with full context |
| US-17 | Engineer | Resume my build with one command that remembers where I left off | I drop back into flow state instantly without re-reading the spec or finding my branch |
| US-18 | Engineer | Have `spec` orchestrate a multi-PR, multi-repo build sequence from my PR stack plan | I execute complex features step-by-step with the right context at each step |
| US-18a | Engineer | Have `spec` generate a proposed PR stack plan from the spec's solution and architecture sections | I get a sensible starting decomposition that I review and adjust instead of planning from scratch |
| US-19 | Engineer | Declare an escape hatch when a blocker is found | Work is redirected cleanly without silently stalling |
| US-20 | QA | Send a spec back to a previous stage when expectations aren't met | Work is redirected to the right phase without losing context |

### Review & Deployment

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-21 | Engineer | Submit my stacked PRs for team review with one command | The review rotation can proceed asynchronously with full context |
| US-21a | Engineer | Have `spec` generate PR descriptions from the diff, spec context, and PR stack position | My PRs are well-documented without manual write-up, and reviewers get the context they need |
| US-22 | Engineer | Trigger deployment from the CLI and track it through staging to production | I never need to open a CI/CD dashboard or deployment portal |
| US-23 | QA | Validate in staging and advance or revert the spec from the CLI | The validation workflow is structured and tracked |

### Knowledge & Continuity

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-24 | Any team member | Search historical specs and decision logs with natural language | I can understand why the system was built the way it was |
| US-24a | Any team member | Ask a natural language question and get an AI-synthesised answer grounded in spec history | I get a direct answer with citations, not just a list of matching documents |
| US-25 | New hire | Browse archived specs in reverse-chronological order | I can build system understanding before writing code |
| US-26 | Team | Use `spec` regardless of whether our stack is Atlassian, Linear, or Notion | We are not locked into a single vendor |

### Identity & Configuration

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-27 | Any team member | Declare my role once and have `spec` remember it | I don't have to specify who I am on every command |
| US-28 | Engineer | Work on a spec that spans multiple service repos | I don't need to decide which repo "owns" a cross-cutting feature |

### Async Ceremonies

| # | As a... | I want to... | So that... |
|---|---|---|---|
| US-29 | Engineer | Auto-generate my standup from actual activity | I report accurately without relying on memory, and the standup meeting becomes optional |
| US-30 | TL | Run a retrospective that auto-populates cycle metrics | I have data-driven process improvement without spreadsheets |

---

## 4. Proposed Solution <!-- owner: pm -->

### 4.1 Concept Overview

`spec` is a **developer control plane** - a unified, CLI-based interface to the entire product development lifecycle. It uses the `SPEC.md` document as its coordination primitive, but its surface area extends far beyond document management. From the developer's perspective, `spec` *is* their project management tool, their notification centre, their review coordinator, and their deployment trigger - even though under the hood it reads from and writes to the tools the team already uses.

Its core responsibilities:

1. **Dashboard** - aggregate signals from all integrated tools into a single, prioritised terminal view. Replace the morning ritual of checking Jira + Slack + GitHub with one command.
2. **Intake** - capture work from any source (bug reports, alerts, feature requests, tech debt) into a lightweight triage queue before it becomes a full spec.
3. **Scaffolding** - generate new `SPEC.md` files from a standard template with auto-assigned IDs.
4. **Pipeline management** - transition specs through configurable stages with gate validation, role-based ownership, and automated handoff notifications.
5. **Sync** - bidirectional, section-scoped synchronisation between the specs repo and external tools (Confluence, Notion, Figma, etc.) so each role contributes from their preferred tool.
6. **Build orchestration** - provide structured context to agentic coding tools via MCP server (primary) or consolidated context file (fallback), with session continuity, multi-PR sequencing, and bidirectional communication (agent can record decisions and mark steps done mid-session).
7. **Decision capture** - structured CLI for recording questions, decisions, and rationale to the spec's decision log - the institutional memory that survives team turnover.
8. **Review coordination** - aggregate stacked PRs across repos and post structured review requests.
9. **Deployment** - trigger deploys via CI/CD adapters and track specs through staging to production without leaving the terminal.
10. **Knowledge base** - the specs repo is a searchable archive of every spec, decision, and rationale the team has ever produced. Semantic search makes it queryable.
11. **Async ceremonies** - auto-generate standups from real activity; capture retrospective metrics from pipeline data.
12. **Passive awareness** - a "you have mail" status line on every `spec` invocation so pending items surface when the dev is ready, not when a notification interrupts flow state.
13. **AI-assisted drafting** - optionally use an LLM to draft spec sections, PR descriptions, triage summaries, and PR stack plans. Every AI feature follows the same contract: draft → human review (accept / edit / skip). AI is never required - omitting the `ai` integration is a first-class configuration.

### 4.2 The Developer Experience

The defining design principle of `spec` is: **every interaction a developer has with the product lifecycle should be possible from the terminal.** External tools are invisible plumbing. The developer sees only `spec`.

#### Morning start

```
$ spec

Good morning, Aaron.                           engineer · Cycle 7

─── DO ──────────────────────────────────────────────────────────
⚡ SPEC-042  Auth refactor            build          PR 2/4 in progress
   Next: api-gateway rate limit middleware
🔴 SPEC-039  Rate limiting            pr-review      2 unresolved threads
   @carlos: "can we use token bucket instead?"

─── REVIEW ──────────────────────────────────────────────────────
📋 PR #418   Search indexing           api-gateway    requested 3h ago

─── INCOMING ────────────────────────────────────────────────────
📨 SPEC-046  Billing alerts           triage         high priority
💬 2 mentions in #platform (Slack) referencing SPEC-042

─── BLOCKED ─────────────────────────────────────────────────────
🚫 SPEC-037  Payment retry            5d - upstream dep (payments-v2)

Run 'spec do' to resume SPEC-042. 12 specs in pipeline.
```

That replaces opening Jira, Slack, GitHub, and email. One command, every signal, zero browser tabs.

#### Resuming work

```
$ spec do

Resuming SPEC-042 - Auth refactor
PR 2/4: Integrate Redis backend (auth-service)
Status: 3/5 acceptance criteria passing

MCP server started on stdio.
Context available:
  • spec://current/full (SPEC-042 - §7 Technical Implementation)
  • spec://current/prior-diffs (PR #412 - token bucket rate limiter)
  • spec://current/conventions

Spawning claude-code in ~/code/auth-service on branch spec-042/redis-backend...
```

One command. Back in flow state. No re-reading the spec, no finding the branch, no remembering what you were doing.

#### Passive awareness

```
$ spec build SPEC-042
⚠ 1 spec awaiting your review (SPEC-039). Run 'spec' for details.

Building SPEC-042...
```

Every `spec` invocation includes a one-line indicator if something is pending. Non-intrusive. The Unix "you have mail" model.

#### End of day

```
$ spec standup

Your standup - Aaron Lewis - 2026-04-21
────────────────────────────────────────────────
Yesterday:
  • SPEC-042: Completed PR #415 (Redis integration)
  • SPEC-042: Resolved decision #005 (token bucket algorithm)
  • Reviewed PR #418 for @carlos (search indexing)

Today:
  • SPEC-042: PR 3/4 (api-gateway middleware)

Blockers:
  • SPEC-037: Blocked 5d on payments-v2 upstream

Post to #platform-standup? [y/N]
```

Auto-generated from git commits, spec transitions, decision log entries, and PR activity. Accurate because it's derived from real artifacts, not memory.

### 4.3 Storage Model

```
specs repo (canonical source of truth)
  ├── SPEC-042.md              ← active specs at root
  ├── SPEC-043.md
  ├── triage/                  ← lightweight intake items
  │    └── TRIAGE-088.md
  ├── archive/                 ← completed specs
  │    └── SPEC-001.md
  ├── templates/               ← spec and triage templates
  │    ├── spec.md
  │    └── triage.md
  └── spec.config.yaml         ← team config

        ┌─── spec sync ───┐
        │                  │
        ▼                  ▼
  Confluence/Notion    Jira/Linear
  (PM & Designer       (epic_key,
   read/write §1-5)    status sync)

        ┌─── spec pull ───┐
        │                  │
        ▼                  ▼
  auth-service/        api-gateway/
  .spec/SPEC-042.md    .spec/SPEC-042.md
  (local read-only     (local read-only
   copy for builds)     copy for builds)
```

**Key principles:**
- The **specs repo** is the golden source of truth and the knowledge base. All spec content is committed here. Git history provides full versioning and audit trail.
- **`spec sync`** provides bidirectional, section-scoped sync with external tools. Inbound changes are scoped by `<!-- owner: role -->` markers - a PM's Confluence edits can only update PM-owned sections. Outbound sync publishes the full spec for reading. `spec sync` also handles writing local changes from service repos back to the specs repo (scoped to the current user's role).
- **`spec pull`** copies a spec from the specs repo into a service repo's `.spec/` directory for local context and agent builds.
- Specs are **not tied to any single service repo**, making cross-repo features the default.
- **Triage items** live in `triage/` and are promoted to full specs at the repo root when fleshed out.
- **Archival** is a `git mv` from the specs repo root to `archive/` when a spec reaches its terminal stage. `spec search` and `spec context` read from both active and archived specs.

### 4.4 Sync Model

Sync is **section-scoped** and **role-aware**. The `<!-- owner: role -->` markers in each section heading define who can write to that section from which tool.

| Direction | Source | Target | Sections | Trigger |
|---|---|---|---|---|
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: pm -->` sections (§1-4) | `spec sync <id>` |
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: designer -->` sections (§5) | `spec sync <id>` |
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: qa -->` sections (§6, §9) | `spec sync <id>` |
| Inbound | Service repo `.spec/` | Specs repo | Sections matching user's role | `spec sync <id>` (from service repo) |
| Outbound | Specs repo | Confluence/Notion | All sections (full spec) | `spec sync <id>` or auto on `spec advance` |
| Outbound | Specs repo | Jira/Linear | Frontmatter (`status`, `epic_key`) | Auto on `spec advance` |
| Inbound | Figma / Comms | Specs repo | §5 Design Inputs (links, annotations) | `spec link <id>` or comms bot |

**Conflict resolution:** The specs repo always wins on conflict. If a section was modified in both the specs repo and Confluence since the last sync, `spec sync` warns and requires `--force` to accept the inbound change or `--skip` to keep the repo version.

### 4.5 Pipeline Stages

```
triage → draft → tl-review → design → qa-expectations → engineering → build → pr-review → qa-validation → done → deploying → monitoring → closed
  ↑        ↑         ↑          ↑           ↑                ↑          ↑         ↑              ↑                    ↘
  └────────┴─────────┴──────────┴───────────┴────────────────┴──────────┴─────────┴──────────────┘                  blocked
                                       spec revert --to <stage>                                                 (escape hatch)

  [--- core pipeline, required ---]                                                  [-- post-merge, optional --]
```

**Forward transitions** (`spec advance`) follow the linear happy path. Each transition validates configurable gate conditions before the advance is permitted.

**Backward transitions** (`spec revert --to <stage> --reason "..."`) allow the current stage owner to send a spec back to any previous stage. No gates are checked on reversion. The reason is logged to the decision log and both the current and target stage owners are notified. Reversions are tracked in frontmatter (`revert_count`) as a process health metric.

**Escape hatch** (`spec eject --reason "..."`) transitions to `blocked` from any stage. `spec resume` returns to the pre-block stage.

**Fast-track** (`spec advance <id> --to <stage>`) allows TLs to skip stages for urgent fixes (e.g., triage → engineering for a critical bug). Skipped stages are logged. Requires `owner_role: tl`.

**Post-merge stages** (`deploying`, `monitoring`, `closed`) are optional (configured with `optional: true`). Teams that handle deployment externally can end their pipeline at `done`. Teams that want full lifecycle tracking enable these stages and `spec` triggers deploys and tracks stability via adapters.

### 4.6 Architecture Overview

```
┌───────────────────────────────────────────────────────────────────┐
│                          spec CLI                                  │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │                    Dashboard / Inbox                          │ │
│  │  Aggregates signals from all adapters into a prioritised     │ │
│  │  terminal view. Passive "you have mail" on every invocation. │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │                   AI Service Layer                           │ │
│  │  Optional. Every method returns null when unconfigured.      │ │
│  │  draft() • summarise() • embed() • propose_pr_stack()         │ │
│  │  All outputs go through accept / edit / skip before write.   │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐ │
│  │   Intake   │  │   Spec     │  │   Sync     │  │   Build    │ │
│  │   Engine   │  │   Engine   │  │   Engine   │  │   Engine   │ │
│  │            │  │            │  │            │  │            │ │
│  │ - Triage   │  │ - Template │  │ - Section  │  │ - MCP svr  │ │
│  │ - Sources  │  │ - Stages   │  │   scoping  │  │ - Context  │ │
│  │ - Promote  │  │ - Gates    │  │ - Conflict │  │   assembly │ │
│  │ - Fast-    │  │ - Dec.Log  │  │   resolve  │  │ - Session  │ │
│  │   track    │  │ - Archive  │  │ - Role     │  │   resume   │ │
│  │            │  │            │  │   guards   │  │ - PR stack │ │
│  └────────────┘  └────────────┘  └────────────┘  └────────────┘ │
│                                                                    │
│  ┌────────────┐  ┌────────────┐  ┌──────────────────────────────┐│
│  │  Ceremony  │  │  Knowledge │  │     Adapter Registry          ││
│  │  Engine    │  │  Engine    │  │                                ││
│  │            │  │            │  │  Comms    Docs     PM          ││
│  │ - Standup  │  │ - Search   │  │  ------  ------  ------       ││
│  │ - Retro    │  │ - Context  │  │  Teams   Confl.  Jira         ││
│  │ - Metrics  │  │ - History  │  │  Slack   Notion  Linear       ││
│  │            │  │ - Embed    │  │  Discord GitHub  GH Issues    ││
│  └────────────┘  └────────────┘  │                                ││
│                                   │  Repo    Agent   Design       ││
│                                   │  ------  ------  ------       ││
│                                   │  GitHub  Claude  Figma        ││
│                                   │  GitLab  Cursor               ││
│                                   │  Bitbkt  Copilot              ││
│                                   │         Pi                    ││
│                                   │                                ││
│                                   │  Deploy   Intake   AI          ││
│                                   │  ------   ------   ------      ││
│                                   │  GH Act.  PgDuty   Anthropic   ││
│                                   │  GitLab   Slack    OpenAI      ││
│                                   │  ArgoCD   Email    Ollama      ││
│                                   │  Custom   Webhook  none        ││
│                                   └──────────────────────────────┘│
└───────────────────────────────────────────────────────────────────┘
        │              │               │
        ▼              ▼               ▼
  Specs Repo     Service Repos    ~/.spec/cache
  (canonical     (.spec/ copies    (offline fallback,
   + triage       for builds)      session state,
   + archive)                      activity log)
```

### 4.7 Configuration

The tool is configured via two files:

- **Specs repo** `spec.config.yaml` - team settings, integrations, pipeline definition. Committed to the specs repo.
- **User-level** `~/.spec/config.yaml` - personal identity and preferences. Never committed.

```yaml
# spec.config.yaml (specs repo root) - example for an Atlassian + GitHub + Teams stack

version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 7"

specs_repo:
  provider: github                   # github | gitlab | bitbucket
  owner: my-org
  repo: specs
  branch: main
  token: ${SPEC_GITHUB_TOKEN}

integrations:
  comms:
    provider: teams                    # teams | slack | discord | custom
    webhook_url: ${TEAMS_WEBHOOK_URL}
    standup_channel: "#platform-standup"

  pm:
    provider: jira                     # jira | linear | github-issues | none
    base_url: ${JIRA_BASE_URL}
    project_key: PLAT
    token: ${JIRA_API_TOKEN}

  docs:
    provider: confluence               # confluence | notion | github | local
    base_url: ${CONFLUENCE_BASE_URL}
    space_key: ENG
    token: ${CONFLUENCE_API_TOKEN}

  repo:
    provider: github                   # github | gitlab | bitbucket
    owner: my-org
    token: ${SPEC_GITHUB_TOKEN}

  agent:
    provider: claude-code              # claude-code | cursor | copilot | pi | custom

  ai:                                  # optional - spec's own LLM for drafting, summarisation, semantic search
    provider: anthropic                # anthropic | openai | ollama | none
    model: claude-sonnet-4-20250514
    token: ${AI_API_KEY}

  design:
    provider: figma                    # figma | none
    token: ${FIGMA_TOKEN}

  deploy:
    provider: github-actions           # github-actions | gitlab-ci | argocd | custom | none
    environments:
      - name: staging
        auto: true                     # auto-deploy on `done`
      - name: production
        auto: false                    # manual promotion via `spec deploy --env production`
        gate: monitoring_duration      # require monitoring period before prod

  intake:                              # optional - auto-create triage items from external sources
    sources:
      - provider: pagerduty
        auto_create: true
        filter: "severity:high"
        token: ${PAGERDUTY_TOKEN}
      - provider: slack
        channel: "#feature-requests"
        trigger: ":spec:"             # emoji reaction creates a triage item

sync:
  outbound_on_advance: true
  conflict_strategy: warn              # warn | repo-wins | remote-wins

archive:
  directory: archive

dashboard:
  stale_threshold: 48h                 # highlight specs in-stage beyond this duration
  refresh_ttl: 300                     # cache TTL in seconds for dashboard data

pipeline:
  stages:
    - name: triage
      owner_role: pm
      optional: false
      gates: []                        # no gates - triage is intentionally lightweight
    - name: draft
      owner_role: pm
    - name: tl-review
      owner_role: tl
      gates:
        - section_complete: problem_statement
    - name: design
      owner_role: designer
      gates:
        - section_complete: user_stories
    - name: qa-expectations
      owner_role: qa
      gates:
        - section_complete: design_inputs
    - name: engineering
      owner_role: engineer
      gates:
        - section_complete: acceptance_criteria
    - name: build
      owner_role: engineer
    - name: pr-review
      owner_role: engineer
      gates:
        - pr_stack_exists: true
    - name: qa-validation
      owner_role: qa
      gates:
        - prs_approved: true
    - name: done
      owner_role: tl
    - name: deploying
      owner_role: engineer
      optional: true
    - name: monitoring
      owner_role: engineer
      optional: true
      gates:
        - duration: 24h               # can't advance until 24h post-deploy
    - name: closed
      owner_role: tl
      optional: true
      auto_archive: true
```

```yaml
# ~/.spec/config.yaml (user-level) - personal identity and preferences, never committed

user:
  owner_role: engineer               # pm | tl | designer | qa | engineer | custom
  name: "Aaron Lewis"
  handle: ${COMMS_HANDLE}           # e.g. @aaron on Slack, aaron@org.com on Teams

preferences:
  editor: $EDITOR                    # default editor for `spec edit`
  dashboard_sections:                # customise which sections appear in `spec`
    - do
    - review
    - incoming
    - blocked
  standup_auto_post: false           # if true, `spec standup` posts without confirmation
  ai_drafts: true                    # if false, suppresses AI draft prompts even when ai is configured
```

### 4.8 `SPEC.md` Template Structure

The canonical template scaffolded by `spec new` contains the following sections. Sections are tagged with their responsible role via `<!-- owner: role -->` comments - these markers are required, used by `spec validate` for gate checks, and by `spec sync` for section-scoped bidirectional sync. The template is stored in the specs repo at `templates/spec.md` and versioned via git.

```markdown
---
id: SPEC-<id>
title: [Feature/Enhancement Title]
status: draft
version: 0.1.0
author: [from git config]
cycle: [from spec.config.yaml team.cycle_label]
epic_key: [from PM integration, if configured]
repos: []
revert_count: 0
source: [triage ID if promoted, or "direct"]
created: [date]
updated: [date]
---

# SPEC-<id> - [Feature/Enhancement Title]

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |

## 1. Problem Statement           <!-- owner: pm -->
## 2. Goals & Non-Goals           <!-- owner: pm -->
## 3. User Stories                <!-- owner: pm -->
## 4. Proposed Solution           <!-- owner: pm -->
  ### 4.1 Concept Overview
  ### 4.2 Architecture / Approach
## 5. Design Inputs               <!-- owner: designer -->
## 6. Acceptance Criteria         <!-- owner: qa -->
## 7. Technical Implementation    <!-- owner: engineer -->
  ### 7.1 Architecture Notes
  ### 7.2 Dependencies & Risks
  ### 7.3 PR Stack Plan
## 8. Escape Hatch Log            <!-- auto: spec eject -->
## 9. QA Validation Notes         <!-- owner: qa -->
## 10. Deployment Notes           <!-- owner: engineer -->
## 11. Retrospective              <!-- auto: spec retro -->
```

**Triage template** (`templates/triage.md`) - intentionally minimal:

```markdown
---
id: TRIAGE-<id>
title: [Brief description]
status: triage
priority: [low | medium | high | critical]
source: [support | alert | stakeholder | engineer | comms]
source_ref: [ticket #, alert ID, Slack permalink, etc.]
reported_by: [from spec whoami]
created: [date]
---

# TRIAGE-<id> - [Brief description]

## Context
[What happened, who reported it, what's the impact]

## Notes
[Any initial investigation, links, screenshots]
```

The `repos` frontmatter field lists the service repos this spec touches (e.g., `repos: [auth-service, api-gateway, frontend]`). This is populated by engineers during technical planning and used by `spec review` to aggregate PRs across repos.

### 4.9 Core Commands

#### The Daily Driver

| Command | Description |
|---|---|
| `spec` | **The dashboard.** Aggregated, prioritised view of everything awaiting your attention - specs in your queue, PR reviews requested, comms mentions, blocked items. Replaces opening Jira + Slack + GitHub. |
| `spec do [<id>]` | **Resume work.** Picks up where you left off - loads spec context, finds your branch, injects incremental context into the agent, shows acceptance criteria progress. If no ID, resumes the most recent active build. |
| `spec standup` | **Auto-generated standup.** Derives yesterday/today/blockers from git activity, spec transitions, decision log entries, and PR activity. Optionally posts to comms. |

#### Intake

| Command | Description |
|---|---|
| `spec intake "<title>" [--source <source>] [--priority <priority>]` | Create a lightweight triage item. Minimal friction - just a title is required. Source and priority can be added or inferred. |
| `spec promote <triage-id> [--title "..."]` | Promote a triage item to a full spec. Scaffolds the `SPEC.md` template with context pre-populated. If `ai` is configured, offers an AI-drafted §1 Problem Statement for review (accept / edit / skip). |

#### Spec Lifecycle

| Command | Description |
|---|---|
| `spec new [--title "..."]` | Scaffold a new `SPEC.md` in the specs repo with an auto-assigned ID, create linked PM epic, post draft notification. |
| `spec advance <id> [--to <stage>]` | Advance to the next stage (or skip to a specific stage for TL fast-track). Validates gates, transitions status, notifies next owner. |
| `spec revert <id> --to <stage> --reason "..."` | Send a spec back to a previous stage. Logs reason, notifies both current and target stage owners. |
| `spec eject <id> --reason "..."` | Log blocker, transition to `blocked`, notify TL. |
| `spec resume <id>` | Return a `blocked` spec to its pre-block stage. |
| `spec validate <id>` | Dry-run all gate checks for the current stage without advancing. |
| `spec status <id>` | Show pipeline position, section completion, sync state, reversion history, and cycle metrics. |

#### Sync & Collaboration

| Command | Description |
|---|---|
| `spec pull <id>` | Fetch spec from specs repo to `.spec/<id>.md` in the current service repo. |
| `spec sync <id> [--direction in\|out\|both]` | Bidirectional section-scoped sync with external tools. Defaults to `both`. |
| `spec link <id> --section <section> --url <url>` | Attach a resource link (Figma, doc, etc.) to a spec section. |
| `spec edit <id>` | Open spec in `$EDITOR`, or print the docs provider URL for non-dev roles. |

#### Drafting (AI-assisted)

| Command | Description |
|---|---|
| `spec draft <id> --section <slug>` | Request an AI draft of a spec section based on existing context (triage data, other sections, decision log, related specs). Presents the draft for accept / edit / skip. Requires `ai` integration; errors clearly if unconfigured. |
| `spec draft <id> --pr [--pr-number <#>]` | Generate a PR description from the diff, spec context, and PR stack position. If `--pr-number` is omitted, drafts for the current branch's open PR. Presents for accept / edit / skip. |
| `spec draft <id> --pr-stack` | Propose a PR stack plan for §7.3 based on §4 and §7.1. Presents the proposed plan for accept / edit / skip. |

#### Decisions

| Command | Description |
|---|---|
| `spec decide <id> --question "..."` | Append a new question to the decision log. |
| `spec decide <id> --resolve <#> --decision "..." --rationale "..."` | Resolve an existing decision log entry. |
| `spec decide <id> --list` | Display the current decision log. |

#### Build & Deploy

| Command | Description |
|---|---|
| `spec build <id>` | Start or resume the build phase. Starts an MCP server exposing spec context (resources + tools), spawns the configured agent, and orchestrates multi-PR sequencing. Non-MCP agents receive a consolidated context file. |
| `spec review <id>` | Post structured review request to comms with all stacked PRs across repos linked in dependency order. |
| `spec deploy <id> [--env <environment>]` | Trigger deployment via CI/CD adapter. Tracks through staging → production. |
| `spec mcp-server [--spec <id>]` | Run `spec` as a standalone MCP server (stdio transport). Used by agents that connect to MCP servers via config (e.g., `.mcp.json`). If `--spec` is omitted, serves context for the most recent active session. |

#### Knowledge Base

| Command | Description |
|---|---|
| `spec search "<query>"` | Full-text search across active and archived specs. |
| `spec context "<question>"` | Semantic search - find relevant specs, decisions, and rationale for a natural language question. Uses embeddings when `ai` is configured; falls back to keyword search otherwise. Designed for onboarding and agent context retrieval. |
| `spec history [--limit N]` | List recent completed specs with summaries and cycle metrics. |

#### Pipeline Visibility

| Command | Description |
|---|---|
| `spec watch` | Live-updating terminal dashboard showing the full pipeline. Specs grouped by stage, colour-coded, with stale indicators. Like `htop` for your team's work. |
| `spec retro [--cycle <label>]` | Auto-populate retrospective with cycle metrics: average time per stage, reversion rates, bottleneck stages, throughput. Prompts for qualitative input. |
| `spec metrics [--cycle <label>]` | Show quantitative pipeline health metrics without the retro ceremony. |

#### Identity & Config

| Command | Description |
|---|---|
| `spec whoami` | Display resolved user identity and config source. |
| `spec config init` | Interactive wizard for `spec.config.yaml` (team config). |
| `spec config init --user` | Interactive wizard for `~/.spec/config.yaml` (personal identity). |
| `spec config test` | Validate all configured integrations and surface auth issues. |
| `spec list [--role <role>] [--all]` | List specs by role queue. `--all` shows full pipeline grouped by stage. |

### 4.10 Build Engine - Deep Dive

The build phase is where engineers spend 80% of their time. `spec build` and `spec do` must be significantly better than manually prompting an agent. This is the DX that makes `spec` indispensable.

#### Session continuity

`spec` maintains build session state in `~/.spec/sessions/<spec-id>/`:

```
~/.spec/sessions/SPEC-042/
  ├── state.yaml          # current PR step, branch, last activity
  ├── context.md          # accumulated context from previous steps
  └── activity.log        # timestamped log of builds, decisions, commits
```

When an engineer runs `spec do`, the build engine:
1. Reads `state.yaml` to determine where they left off
2. Loads the current spec from `.spec/SPEC-042.md` (warns if stale vs specs repo)
3. Determines the current PR step from §7.3 PR Stack Plan
4. Assembles context: spec + previous PR diffs + failing tests + conventions
5. Injects into the configured agent provider
6. Updates `activity.log` (used by `spec standup`)

#### Multi-PR orchestration

If §7.3 defines a PR stack:

```markdown
### 7.3 PR Stack Plan
1. [auth-service] Add token bucket rate limiter
2. [auth-service] Integrate with Redis backend
3. [api-gateway] Add rate limit middleware
4. [frontend] Add rate limit error handling
```

`spec build` walks this sequence step by step. Each step:
- Checks out the correct repo and creates a branch (`spec-042/step-1-token-bucket`)
- Injects the spec + cumulative context from prior steps
- On completion, the engineer marks the step done and moves to the next
- Cross-repo steps prompt the engineer to switch repos

#### Agent context delivery - MCP + fallback

`spec` delivers context to coding agents via two mechanisms:

**Primary: MCP server (pull model).** `spec` runs an MCP (Model Context Protocol) server that the agent connects to. The agent pulls exactly the context it needs, when it needs it. This is the default for any MCP-compatible agent (Claude Code, Cursor, Copilot, Pi).

The MCP server exposes:

**Resources** (structured context the agent reads):
- `spec://current/full` - full spec markdown for the active build
- `spec://current/section/{slug}` - specific section (e.g., `technical_implementation`)
- `spec://current/decisions` - decision log
- `spec://current/acceptance-criteria` - ACs for validation during build
- `spec://current/prior-diffs` - cumulative diffs from earlier PR stack steps
- `spec://current/conventions` - project conventions from `.spec/conventions.md`
- `spec://related?q={query}` - related specs via knowledge search

**Tools** (actions the agent can take mid-session):
- `spec_decide` - record a decision to the decision log from within the agent session
- `spec_step_complete` - mark the current PR step as done, advance session state
- `spec_status` - check current pipeline status and acceptance criteria progress
- `spec_search` - query the knowledge base for context on past decisions

This means:
- **Zero file pollution.** Nothing written to the workspace for context delivery.
- **Any MCP agent works automatically.** No per-agent format knowledge needed.
- **Dynamic context.** The agent queries for what it needs, not a static dump.
- **Bidirectional.** The agent records decisions and marks steps done mid-session, which flows back into `spec`'s activity log and session state.

The one-time agent-side setup is minimal. For Claude Code, add to `.mcp.json`:

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

**Fallback: consolidated context file.** For agents that don't support MCP, `spec` assembles a single markdown file containing the spec, prior diffs, conventions, current step scope, and a system prompt. The file path is passed to the agent as a CLI argument or piped to stdin. The adapter only needs to know how to pass a file path to the agent's CLI - not how to write to proprietary config locations.

```
~/.spec/sessions/SPEC-042/context.md   ← consolidated fallback file
```

### 4.11 Dashboard Engine - Deep Dive

The `spec` (no args) dashboard aggregates signals from multiple adapters:

| Section | Data Source | Adapter |
|---|---|---|
| DO | Specs where stage `owner_role` matches user, sorted by time-in-stage | Spec Engine + PM adapter |
| REVIEW | Open PRs where user is requested reviewer | Repo adapter (GitHub/GitLab) |
| INCOMING | New triage items, specs entering user's stage, comms mentions | Intake Engine + Comms adapter |
| BLOCKED | Specs in `blocked` state related to user | Spec Engine |
| FYI | Recently completed specs user was involved in | Spec Engine |

**Performance:** The dashboard fetches live data with a cache TTL (configurable, default 5 min). Subsequent invocations within the TTL read from `~/.spec/cache/`. The cache is populated as a side effect of live queries. Offline mode reads entirely from cache.

**Passive awareness line:** Every non-dashboard `spec` command checks the cache for pending items and prints a one-line summary if anything needs attention. This check is cache-only (never blocks on network) and adds negligible latency.

### 4.12 Adoption Path

Each version is independently useful. Nobody has to buy the whole vision to start getting value.

| Version | What ships | Value without integrations |
|---|---|---|
| **v0.1** | `spec new`, `spec list`, `spec advance`, `spec decide`, `spec validate`, `spec whoami`, `spec status`, `spec edit` | Local spec lifecycle with markdown files. Works with zero config beyond `spec config init --user`. A team can manage specs in a git repo with structured pipeline stages immediately. |
| **v0.2** | `spec` (dashboard), `spec pull`, `spec build`, `spec do`, `spec intake`, `spec promote`, `spec draft` | The agent integration, personal dashboard, and AI drafting layer. This is where the DX magic starts - one command to resume work, one command to see your queue, AI-drafted content where configured. Still works local-only; AI is optional. |
| **v0.3** | `spec sync`, `spec review`, `spec link`, comms/docs/repo adapters | The integration layer. Connect to the tools the team already uses. PMs contribute from Confluence. Review requests post to Slack. AI-enriched review messages. The external tools become invisible. |
| **v0.4** | `spec standup`, `spec watch`, `spec context`, `spec retro`, `spec metrics`, `spec deploy`, deploy/intake adapters | The "never leave the terminal" features. Async standups, live pipeline view, semantic knowledge search, deployment orchestration. Full control plane. |

### 4.13 AI Service Layer - Deep Dive

#### Design principle: progressive enhancement, not dependency

Every feature in `spec` works without an LLM. If you configure one, things get better - drafts appear, summaries sharpen, search gets semantic. If you don't, or the provider is down, or you're offline, everything functions via templates, deterministic logic, and manual input. **AI is a drafting assistant, not a decision maker.**

#### Two distinct agent concepts

`spec` separates two fundamentally different uses of AI:

| | `agent` integration | `ai` integration |
|---|---|---|
| **Purpose** | External coding tool for building features | `spec`'s own LLM for content drafting, summarisation, semantic search |
| **Interaction** | `spec` writes context files and launches the tool | `spec` calls an API and receives text |
| **Examples** | Claude Code, Cursor, Copilot, Pi | Anthropic API, OpenAI API, Ollama (local) |
| **When used** | `spec build`, `spec do` | `spec draft`, `spec promote`, `spec review`, `spec context` |
| **Required?** | No - engineers can build without an agent | No - every feature has a non-AI fallback |

A team can use Cursor for builds and Ollama for drafting, or Claude Code for builds and no AI at all. The two are fully independent.

#### The `null` contract

The AI service layer exposes a small API that every calling feature uses:

```
interface AIService {
  draft(prompt: string, context: string[]): string | null
  summarise(text: string, max_length?: number): string | null
  embed(text: string): float[] | null
}
```

Every method returns `null` when:
- `ai.provider` is `none` or unconfigured
- The provider is unreachable (offline, rate-limited, errored)
- The user has set `preferences.ai_drafts: false`

**Callers always handle `null` gracefully.** This is enforced architecturally - no feature can assume AI is available. The fallback is always: template output, manual input, or the feature simply skips the enhancement.

#### The accept / edit / skip interaction model

Every AI-generated draft follows the same three-option flow:

```
$ spec promote TRIAGE-088

Drafting §1 Problem Statement from triage context...

─── DRAFT §1 - Problem Statement ───────────────────────
 Auth tokens are expiring prematurely for EU users due to
 a timezone miscalculation in the token rotation logic
 introduced in SPEC-039. Support has received 12 tickets
 in the last 48 hours. EU-region sessions are failing at
 a rate of 3.2% vs. the baseline of 0.1%.

 Source: Support ticket #8821, PagerDuty alert #PD-4421
──────────────────────────────────────────────────

 Accept draft? [y/e/s] (yes / edit in $EDITOR / skip)
```

- **Accept (y)** - writes the draft to the spec as-is
- **Edit (e)** - opens `$EDITOR` with the draft pre-populated; the user refines it, saves, and it's written to the spec
- **Skip (s)** - leaves the section blank; the user writes it from scratch later

AI never writes directly to a spec without human review. There is no "auto-accept" mode.

#### Where AI is used across the lifecycle

| Feature | Trigger | What AI does | Without AI |
|---|---|---|---|
| **Triage summarisation** | `spec intake` with `--source-ref` pointing to an alert or Slack thread | Summarises the source into a readable triage description | User writes the description manually |
| **Section drafting** | `spec draft <id> --section <slug>` | Drafts the requested section using existing spec context, triage data, decision log, and related specs | Command errors: `AI integration not configured. Write the section manually with 'spec edit'.` |
| **Promote drafting** | `spec promote <triage-id>` | Drafts §1 Problem Statement from the triage item's context | Template with blank §1; user writes from scratch |
| **PR description** | `spec draft <id> --pr` | Generates a PR description from the diff + spec context + PR stack position | User writes the PR description manually or uses a template |
| **PR stack proposal** | `spec draft <id> --pr-stack` | Reads §4 and §7.1, proposes a decomposition for §7.3 | User writes the PR stack plan manually |
| **Review request** | `spec review <id>` | Enriches the review message with a summary of what changed and what to look for | Template-based message with PR links only |
| **Semantic search** | `spec context "<question>"` | Uses embeddings for semantic matching; optionally synthesises an answer with citations | Falls back to keyword/full-text search (`spec search`) |
| **Commit messages** | `spec build` / `spec do` (if configured) | Suggests conventional commit messages from staged changes + spec context | User writes commit messages manually |

#### What AI is NOT used for

- **Gate validation** - gates are deterministic (section non-empty, PRs approved, duration elapsed). AI never decides if a gate passes.
- **Pipeline transitions** - `spec advance` and `spec revert` are human decisions. AI doesn't suggest or auto-trigger transitions.
- **Decision making** - the decision log captures human decisions. AI doesn't suggest options or recommend resolutions.
- **Acceptance criteria** - QA writes ACs. AI-generated ACs that get rubber-stamped are worse than no ACs. If a team wants to use `spec draft` for ACs, the accept/edit/skip flow ensures QA reviews, but `spec` does not auto-generate ACs as part of any workflow.
- **Standup generation** - standups are derived deterministically from activity logs, git commits, and spec transitions. AI adds style but not substance; the deterministic version is already accurate.

#### The `ollama` option

Including `ollama` (local models) as a first-class provider is essential for:
- Enterprise teams that can't send data to external APIs
- Air-gapped environments
- Privacy-sensitive specs
- Cost control

Local models are worse at drafting long-form content but perfectly adequate for embeddings (semantic search) and summarisation (triage intake). The `ai` adapter interface is the same regardless of provider - switching from `anthropic` to `ollama` is a config change.

---

## 5. Design Inputs <!-- owner: designer -->

- [ ] **`spec` (dashboard) output** - this is the most important screen in the product. Should feel like a focused, personal inbox - not a data dump. Colour-coded sections (DO = bold/urgent, REVIEW = neutral, INCOMING = highlighted, BLOCKED = muted/warning). Each item is 2 lines max: title line + context line. Stale items (beyond `stale_threshold`) get a visual indicator. Empty sections are hidden, not shown blank. The "nothing to do" state should feel rewarding: `✓ All clear. 14 specs completed this cycle.`
- [ ] **Passive awareness line** - must be visually distinct from command output but not distracting. Consider dim/muted text: `⚠ 1 pending · run 'spec' for details`. Should never exceed one line.
- [ ] **`spec do` output** - the transition into flow state. Show just enough to orient: spec title, current PR step, acceptance criteria progress, what context was loaded. Then get out of the way. No unnecessary decoration.
- [ ] **`spec status <id>` output** - render an ASCII pipeline diagram showing current position, section completion per owner, sync freshness, reversion history, and cycle time per stage.
- [ ] **`spec watch` output** - live-updating terminal dashboard. Specs grouped by stage, pipeline rendered as horizontal bars. Stale items highlighted. Should feel like `htop` or `k9s` - information-dense but scannable.
- [ ] **`spec standup` output** - clean, readable, copy-pasteable. Should look good both in the terminal and when posted to Slack/Teams. Consider: the posted version might need different formatting than the terminal preview.
- [ ] **`spec list` output** - table with colour-coded stage badges, time-in-stage, urgency indicators. `--all` view groups by stage. Muted "nothing to do" state.
- [ ] **Notification message templates** - each comms notification should be skimmable in <5 seconds. Standardise format: `[SPEC-042] Stage → tl-review | Owner: @alice | Link: ...`
- [ ] **`spec sync` output** - diff summary: what changed per section, which direction, any conflicts needing resolution.
- [ ] **`spec retro` / `spec metrics` output** - timeline visualisation showing time per stage, a clear bottleneck indicator, reversion rate trend. Should tell a story without interpretation.
- [ ] **Error states** - every error should include the next action. Not `"Config not found"` but `"Config not found. Run 'spec config init' to set up."` Every dead-end has an exit.
- [ ] **AI draft presentation** - the draft block should be visually distinct from normal output (bordered, dimmed background, or indented). The `[y/e/s]` prompt must be obvious and consistent across all AI-drafting commands. The draft should be long enough to be useful but short enough to review in-terminal without scrolling.
- [ ] **AI unavailable states** - when `ai` is not configured and a user runs `spec draft`, the error should be helpful, not punishing: `AI integration not configured. Write the section manually with 'spec edit SPEC-042'. To enable AI drafting, run 'spec config init' and configure the ai integration.` When `ai` is configured but the provider is unreachable, degrade gracefully: `AI provider unreachable. Proceeding without draft.`

---

## 6. Acceptance Criteria <!-- owner: qa -->

### US-01 - Triage intake
- [ ] `spec intake "Auth tokens expiring for EU users"` creates a `TRIAGE-<id>.md` in the specs repo `triage/` directory
- [ ] ID is auto-assigned by scanning existing triage items
- [ ] Only `title` is required; `--source`, `--priority`, and `--source-ref` are optional
- [ ] If `--priority` is omitted, defaults to `medium`
- [ ] A notification is posted to the configured comms channel with the triage item details
- [ ] `spec list --triage` shows all open triage items sorted by priority then age

### US-01a - AI-assisted triage summarisation
- [ ] When `ai` is configured and `spec intake` includes `--source-ref` pointing to an alert or Slack thread, the AI service summarises the source into a triage description
- [ ] The summary is presented via accept / edit / skip before being written to the triage item
- [ ] When `ai` is not configured, `--source-ref` is stored in the triage item but no summary is generated; the user writes the description manually
- [ ] When `ai` is configured but the provider is unreachable, `spec intake` proceeds without the summary and prints a degradation notice

### US-02 - Triage promotion
- [ ] `spec promote TRIAGE-088` scaffolds a new `SPEC-<id>.md` with context from the triage item pre-populated (title, problem context, source reference)
- [ ] When `ai` is configured, an AI draft of §1 Problem Statement is offered via accept / edit / skip
- [ ] When `ai` is not configured, §1 is left blank with the triage context available in the frontmatter `source` field for manual reference
- [ ] The triage item's `source` and `source_ref` are carried into the spec's `source` frontmatter field
- [ ] The original triage file is removed from `triage/` after promotion
- [ ] If PM integration is configured, an Epic is created and linked

### US-03 - TL fast-track
- [ ] `spec advance TRIAGE-088 --to engineering` promotes the triage item to a spec and skips directly to the `engineering` stage
- [ ] Requires `owner_role: tl`; other roles receive a permission error
- [ ] Skipped stages are logged in the decision log as a fast-track entry
- [ ] A notification is sent to the assigned engineer with the context

### US-04 - New spec scaffolding
- [ ] `spec new --title "Auth refactor"` auto-assigns the next sequential ID by scanning existing specs (active + archived)
- [ ] The new spec is created as `SPEC-<id>.md` in the specs repo root with all required sections
- [ ] YAML frontmatter is pre-populated with `status: draft`, auto-assigned ID, current date, and author from git config
- [ ] If PM integration is configured, an Epic is created and its key is written into `epic_key`
- [ ] Decision log table is present and empty
- [ ] A draft notification is sent to the configured comms channel

### US-12 - Dashboard (`spec` no args)
- [ ] `spec` with no arguments displays the personal dashboard with DO, REVIEW, INCOMING, BLOCKED sections
- [ ] DO section shows specs where the active stage's `owner_role` matches the user's configured role, sorted by time-in-stage descending
- [ ] REVIEW section shows open PRs where the user is a requested reviewer (from repo adapter)
- [ ] INCOMING section shows new triage items at the user's role level and unread comms mentions referencing specs
- [ ] BLOCKED section shows specs in `blocked` state that the user is involved in
- [ ] Empty sections are hidden from output
- [ ] If all sections are empty: `✓ All clear. N specs completed this cycle.`
- [ ] Dashboard data is cached with configurable TTL; subsequent calls within TTL read from cache
- [ ] Offline mode reads entirely from cache with a `(cached)` indicator

### US-13 - Passive awareness
- [ ] Every `spec` subcommand (not the dashboard itself) checks the cache for pending items
- [ ] If items are pending, a single muted line is printed before the command's output: `⚠ N pending · run 'spec' for details`
- [ ] This check is cache-only and adds <50ms latency
- [ ] If no items are pending, nothing is printed (no "all clear" noise on every command)

### US-14 - `spec list` role-filtered queue
- [ ] `spec list` with no flags returns only specs where the active stage `owner_role` matches the user's configured role
- [ ] Each row shows: spec ID, title, current stage, time-in-stage, and a direct link to the spec
- [ ] Results are sorted by time-in-stage descending (longest waiting first)
- [ ] Specs in `blocked` state are visually distinguished even if owned by the user's role
- [ ] `spec list --all` shows all specs across all roles and stages, grouped by stage
- [ ] `spec list --role qa` views the queue from another role's perspective
- [ ] `spec list --triage` shows open triage items
- [ ] If the user's role has no pending specs: `✓ Nothing awaiting your action. Run 'spec list --all' to see the full pipeline.`

### US-16 - Spec pull
- [ ] `spec pull SPEC-042` fetches the spec from the specs repo and writes to `.spec/SPEC-042.md`
- [ ] If a local copy exists and has uncommitted changes, user is prompted before overwrite
- [ ] If the spec does not exist, a clear error is returned

### US-17 - `spec do` session resume
- [ ] `spec do` with no arguments resumes the most recent active build session
- [ ] `spec do SPEC-042` resumes the session for a specific spec
- [ ] Session state is persisted in `~/.spec/sessions/<spec-id>/state.yaml`
- [ ] On resume, the build engine: loads the spec, determines current PR step, assembles cumulative context, injects into the agent
- [ ] If the local `.spec/` copy is older than the specs repo version, a warning is printed with `spec pull` suggestion
- [ ] If no active session exists, `spec do` is equivalent to `spec build`
- [ ] Activity (builds started, steps completed) is logged to `~/.spec/sessions/<spec-id>/activity.log`

### US-18 - Multi-PR build orchestration
- [ ] `spec build SPEC-042` reads §7.3 PR Stack Plan and presents the build sequence
- [ ] Each step creates a branch in the target repo following the pattern `spec-<id>/step-N-<slug>`
- [ ] Completing a step updates the session state and presents the next step
- [ ] Cross-repo steps prompt the engineer to switch to the target repo
- [ ] An MCP server is started before the agent is spawned, exposing spec context as resources and `spec_decide`/`spec_step_complete`/`spec_status`/`spec_search` as tools
- [ ] MCP-compatible agents (Claude Code, Cursor, etc.) connect to the MCP server automatically; non-MCP agents receive a consolidated context file path as a CLI argument
- [ ] Decisions recorded via the MCP `spec_decide` tool are written to the spec's decision log and activity log in real time
- [ ] Step completions recorded via the MCP `spec_step_complete` tool advance the session without a post-exit prompt
- [ ] `spec build SPEC-042` validates the spec is at `build` stage before proceeding
- [ ] A build-start notification is sent to the comms channel
- [ ] `spec mcp-server` can be run standalone for agents that manage their own MCP server connections via config (e.g., `.mcp.json`)

### US-19 - Escape hatch
- [ ] `spec eject SPEC-042 --reason "..."` appends an entry to the Escape Hatch Log section
- [ ] Spec status transitions to `blocked`; previous stage is recorded for `spec resume`
- [ ] TL receives a notification with the reason and a link to the spec

### US-20 - Stage reversion
- [ ] `spec revert SPEC-042 --to build --reason "..."` transitions the spec to the specified previous stage
- [ ] `--reason` is required; omitting it produces an error
- [ ] The reason is appended to the decision log as a reversion entry
- [ ] Both the current and target stage owners are notified
- [ ] Only the current stage owner can revert; other roles receive a permission error
- [ ] Reverting to a stage ahead of the current stage is rejected
- [ ] `revert_count` frontmatter is incremented
- [ ] `spec status <id>` shows reversion history

### US-21 - Review coordination
- [ ] `spec review SPEC-042` aggregates PRs from all repos listed in the `repos` frontmatter field
- [ ] PRs are listed in dependency order matching §7.3 PR Stack Plan
- [ ] A structured review request is posted to the configured comms channel

### US-22 - Deployment
- [ ] `spec deploy SPEC-042` triggers the deployment pipeline via the configured deploy adapter
- [ ] Deployment targets are determined by the `repos` frontmatter field and `deploy.environments` config
- [ ] Deploy status is printed in real-time (or polled) showing per-repo results
- [ ] On successful staging deploy, QA is notified with staging URLs
- [ ] `spec deploy SPEC-042 --env production` triggers production promotion (only if `deploy.environments[production].gate` conditions are met)
- [ ] Spec status transitions to `deploying` (if post-merge stages are configured)

### US-24 - Knowledge search
- [ ] `spec search "authentication"` performs full-text search across active and archived specs
- [ ] Results show spec ID, title, status, and matching excerpts with highlighting
- [ ] `spec context "how does auth work?"` performs semantic search and returns structured results: relevant specs with their decision logs and key sections

### US-26 - Stack flexibility
- [ ] Switching `integrations.comms.provider` from `teams` to `slack` requires only a config change
- [ ] `spec config test` reports a clear pass/fail for each configured integration
- [ ] Adapter interface is documented for third-party custom integrations

### US-27 - User role declaration
- [ ] `user.owner_role` in `~/.spec/config.yaml` is respected by all role-aware commands
- [ ] `spec whoami` outputs role, name, handle, and config file path
- [ ] Missing `owner_role` prints: `No role configured. Run 'spec config init --user' to set up your identity.`
- [ ] `--role <role>` flag temporarily overrides for that invocation
- [ ] `user` block is excluded from repo-level config by `spec config init`

### US-28 - Cross-repo specs
- [ ] A spec can declare `repos: [auth-service, api-gateway]` in frontmatter
- [ ] `spec pull SPEC-042` works from any service repo
- [ ] `spec review SPEC-042` aggregates PRs from all listed repos

### US-29 - Auto-generated standup
- [ ] `spec standup` generates a standup from the last 24h of activity
- [ ] Sources: git commits on `spec-*` branches, spec transitions, decision log entries, PR reviews
- [ ] Output is grouped into Yesterday / Today / Blockers
- [ ] "Today" is inferred from the current active build session's next step
- [ ] User is prompted to confirm or edit before posting
- [ ] If `standup_auto_post: true` in user config, posts without confirmation
- [ ] Posted message is formatted appropriately for the comms provider

### US-30 - Retrospective
- [ ] `spec retro` for the current cycle auto-populates: timeline per spec, average time per stage, reversion count and reasons, throughput (specs completed), bottleneck stage
- [ ] `spec retro --cycle "Cycle 6"` retrospects a previous cycle
- [ ] After metrics display, prompts for qualitative input (what went well, what to improve)
- [ ] Qualitative input is appended to a `retro/` document in the specs repo
- [ ] `spec metrics` shows the quantitative data without the qualitative prompts

### US-09 - Bidirectional sync
- [ ] `spec sync SPEC-042` pulls inbound changes from docs provider, scoped to matching role sections
- [ ] `spec sync SPEC-042` pushes the full spec outbound to docs provider
- [ ] From a service repo, also writes local `.spec/` changes back to specs repo, scoped to user's role
- [ ] Inbound changes cannot overwrite sections owned by a different role
- [ ] Same-section conflicts warn and require `--force` or `--skip`
- [ ] Outbound sync is optionally auto-triggered on `spec advance` (per config)

### US-10 - Design resource linking
- [ ] `spec link SPEC-042 --section design --url "https://figma.com/..."` appends a resource link
- [ ] Link includes timestamp and user identity
- [ ] Links are rendered as a list, not inline replacements

### US-11a - AI-assisted section drafting
- [ ] `spec draft SPEC-042 --section problem_statement` generates an AI draft using available context (triage data, other completed sections, decision log, related specs from `spec context`)
- [ ] The draft is presented via accept / edit / skip; accepted or edited content is written to the target section in the spec
- [ ] `spec draft` requires the `ai` integration to be configured; if unconfigured, it prints: `AI integration not configured. Write the section manually with 'spec edit SPEC-042'. To enable AI drafting, run 'spec config init' and configure the ai integration.`
- [ ] If `preferences.ai_drafts: false` is set, `spec draft` prints: `AI drafting is disabled in your preferences. Set 'preferences.ai_drafts: true' in ~/.spec/config.yaml to enable.`
- [ ] AI-generated content is never written to a spec without passing through accept / edit / skip
- [ ] The section slug must match a valid section in the spec template; invalid slugs produce a clear error listing valid options

### US-18a - AI-proposed PR stack plan
- [ ] `spec draft SPEC-042 --pr-stack` generates a proposed PR stack plan from §4 Proposed Solution and §7.1 Architecture Notes
- [ ] The proposed plan follows the format of §7.3: numbered list with `[repo] description` per step
- [ ] The plan is presented via accept / edit / skip; accepted or edited content is written to §7.3
- [ ] Requires `ai` integration; errors clearly if unconfigured
- [ ] The proposal is a starting point - engineers are expected to review and adjust

### US-21a - AI-generated PR descriptions
- [ ] `spec draft SPEC-042 --pr` generates a PR description from: the git diff of the current branch, the spec context (§7 Technical Implementation, §6 Acceptance Criteria), and the PR's position in the stack plan
- [ ] The description is presented via accept / edit / skip
- [ ] If accepted or edited, the description is set on the PR via the repo adapter (GitHub/GitLab API)
- [ ] `--pr-number <#>` targets a specific PR; without it, targets the current branch's open PR
- [ ] Requires both `ai` and `repo` integrations; errors clearly if either is unconfigured

### US-24a - AI-synthesised knowledge answers
- [ ] When `ai` is configured, `spec context "how does auth work?"` returns an AI-synthesised answer grounded in matching specs, with citations (spec IDs, section references, decision log entry numbers)
- [ ] The answer is clearly labelled as AI-generated
- [ ] When `ai` is not configured, `spec context` falls back to keyword search matching (equivalent to `spec search` but with structured output)
- [ ] The synthesised answer never fabricates information - it only references content that exists in the specs repo; if insufficient context is found, it says so

### US-11 - Decision log management
- [ ] `spec decide SPEC-042 --question "REST vs gRPC?"` appends a new row with auto-incremented number, user identity, date
- [ ] New entry has empty Options/Decision/Rationale fields
- [ ] `spec decide SPEC-042 --resolve 003 --decision "gRPC" --rationale "..."` updates the entry
- [ ] Resolving a non-existent entry produces a clear error
- [ ] `spec decide SPEC-042 --list` displays the decision log in a readable table

---

## 7. Technical Implementation <!-- owner: engineer -->

### 7.1 Language, Runtime & Distribution

- **Language:** Go
- **Minimum Go version:** 1.22+ (rangefunc, improved toolchain management)
- **Distribution:** Single statically-linked binary. No runtime dependencies.
  - GitHub Releases - prebuilt binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
  - Homebrew tap - `brew install aaronl1011/tap/spec`
  - `go install` - `go install github.com/aaronl1011/spec-cli@latest`
- **CLI framework:** [Cobra](https://github.com/spf13/cobra) for command routing, flag parsing, and shell completion generation
- **Startup target:** <100ms to first output from SQLite cache. The dashboard (`spec` no args) must feel instant.

### 7.2 Project Structure

```
spec-cli/
├── cmd/                          # Cobra command definitions (thin - parse flags, call internal/)
│   ├── root.go                   # `spec` (dashboard) + global flags + passive awareness
│   ├── new.go                    # `spec new`
│   ├── list.go                   # `spec list`
│   ├── status.go                 # `spec status`
│   ├── advance.go                # `spec advance`
│   ├── revert.go                 # `spec revert`
│   ├── eject.go                  # `spec eject`
│   ├── resume.go                 # `spec resume`
│   ├── validate.go               # `spec validate`
│   ├── decide.go                 # `spec decide`
│   ├── edit.go                   # `spec edit`
│   ├── pull.go                   # `spec pull`
│   ├── sync.go                   # `spec sync`
│   ├── link.go                   # `spec link`
│   ├── build.go                  # `spec build`
│   ├── do.go                     # `spec do`
│   ├── review.go                 # `spec review`
│   ├── deploy.go                 # `spec deploy`
│   ├── draft.go                  # `spec draft`
│   ├── intake.go                 # `spec intake`
│   ├── promote.go                # `spec promote`
│   ├── search.go                 # `spec search`
│   ├── context.go                # `spec context`
│   ├── history.go                # `spec history`
│   ├── standup.go                # `spec standup`
│   ├── retro.go                  # `spec retro`
│   ├── metrics.go                # `spec metrics`
│   ├── watch.go                  # `spec watch`
│   ├── whoami.go                 # `spec whoami`
│   ├── config.go                 # `spec config init`, `spec config test`
│   └── mcp_server.go             # `spec mcp-server` (standalone MCP server mode)
│
├── internal/
│   ├── config/                   # Config loading, resolution, env var interpolation
│   │   ├── config.go             # Team config (spec.config.yaml) types + loader
│   │   ├── user.go               # User config (~/.spec/config.yaml) types + loader
│   │   └── resolve.go            # Resolution chain: cwd → repo root → specs repo → user
│   │
│   ├── markdown/                 # Markdown parsing engine
│   │   ├── frontmatter.go        # YAML frontmatter read/write
│   │   ├── sections.go           # Section extraction by heading + <!-- owner --> markers
│   │   ├── decisionlog.go        # Decision log table parser + mutator
│   │   └── template.go           # Template scaffolding (spec + triage)
│   │
│   ├── pipeline/                 # Pipeline stage machine
│   │   ├── stages.go             # Stage definitions, transitions, validation
│   │   ├── gates.go              # Gate condition evaluation
│   │   └── transitions.go        # advance, revert, eject, resume logic
│   │
│   ├── git/                      # Git operations (shells out to `git`)
│   │   ├── git.go                # Core: clone, fetch, pull, commit, push, status, log
│   │   ├── specsrepo.go          # Specs repo management: ensure cloned, sync, read specs
│   │   └── branch.go             # Branch helpers for build engine (create, checkout, detect)
│   │
│   ├── store/                    # SQLite persistence layer
│   │   ├── db.go                 # DB init, migrations, connection management
│   │   ├── cache.go              # Dashboard cache (specs, PRs, mentions - with TTL)
│   │   ├── sessions.go           # Build session state (current step, branch, context)
│   │   ├── activity.go           # Activity log (timestamped events per spec)
│   │   └── embeddings.go         # Vector storage for semantic search
│   │
│   ├── sync/                     # Bidirectional sync engine
│   │   ├── engine.go             # Orchestrator: inbound, outbound, conflict detection
│   │   ├── sections.go           # Section-level diffing and merging
│   │   └── conflict.go           # Conflict resolution (warn, repo-wins, remote-wins)
│   │
│   ├── build/                    # Build orchestration engine
│   │   ├── engine.go             # Build/do orchestration, PR stack walking
│   │   ├── context.go            # Context assembly (spec + diffs + tests + conventions)
│   │   ├── session.go            # Session state management (resume, step tracking)
│   │   └── mcp.go                # MCP server: resources + tools, stdio transport
│   │
│   ├── dashboard/                # Dashboard aggregation
│   │   ├── dashboard.go          # Aggregate signals into sections (DO, REVIEW, etc.)
│   │   └── awareness.go          # Passive "you have mail" line for non-dashboard commands
│   │
│   ├── ceremony/                 # Async ceremonies
│   │   ├── standup.go            # Activity log → standup generation
│   │   ├── retro.go              # Cycle metrics + qualitative capture
│   │   └── metrics.go            # Quantitative pipeline health
│   │
│   ├── knowledge/                # Knowledge base engine
│   │   ├── search.go             # Full-text search via git-grep
│   │   ├── context.go            # Semantic search (embeddings) + answer synthesis
│   │   └── index.go              # Embedding index management
│   │
│   ├── ai/                       # AI service layer
│   │   ├── service.go            # AIService interface + null-safe wrapper
│   │   ├── prompt.go             # Prompt templates for each drafting use case
│   │   └── review.go             # Accept / edit / skip interaction flow
│   │
│   ├── adapter/                  # Adapter interfaces + registry
│   │   ├── registry.go           # Adapter registry: resolve provider → adapter from config
│   │   ├── comms.go              # CommsAdapter interface
│   │   ├── pm.go                 # PMAdapter interface
│   │   ├── docs.go               # DocsAdapter interface
│   │   ├── repo.go               # RepoAdapter interface
│   │   ├── agent.go              # AgentAdapter interface
│   │   ├── deploy.go             # DeployAdapter interface
│   │   ├── intake.go             # IntakeAdapter interface
│   │   └── ai.go                 # AIAdapter interface
│   │
│   └── adapter/                  # Adapter implementations (one subpackage per provider)
│       ├── github/               # GitHub: RepoAdapter + DeployAdapter (Actions)
│       │   ├── repo.go           # PRs, reviews, CI status, branch protection
│       │   └── deploy.go         # GitHub Actions workflow dispatch
│       ├── jira/                 # Jira: PMAdapter
│       │   └── pm.go             # Epics, issues, status sync
│       ├── confluence/           # Confluence: DocsAdapter
│       │   └── docs.go           # Page read/write, section mapping
│       ├── teams/                # Teams: CommsAdapter
│       │   └── comms.go          # Webhook notifications, channel posting
│       ├── claude/               # Claude Code: AgentAdapter
│       │   └── agent.go          # Subprocess spawn, MCP-native
│       ├── anthropic/            # Anthropic API: AIAdapter
│       │   └── ai.go             # Chat completions, embeddings
│       ├── openai/               # OpenAI API: AIAdapter
│       │   └── ai.go             # Chat completions, embeddings
│       └── ollama/               # Ollama: AIAdapter
│           └── ai.go             # Local model completions, embeddings
│
├── main.go                       # Entrypoint: initialise Cobra root, execute
├── go.mod
├── go.sum
├── Makefile                      # build, test, lint, release targets
└── .goreleaser.yaml              # GoReleaser config for cross-compilation + Homebrew
```

**Key structural rules:**
- `cmd/` files are thin - parse flags, resolve config, call `internal/`. No business logic in `cmd/`.
- `internal/adapter/*.go` files define interfaces only. Implementations live in `internal/adapter/<provider>/`.
- Engines (`pipeline/`, `sync/`, `build/`, `dashboard/`, `ceremony/`, `knowledge/`, `ai/`) depend on adapter interfaces, never on concrete implementations. The `registry` resolves config → concrete adapter at startup and injects into engines.
- `internal/git/` wraps all `exec.Command("git", ...)` calls. No other package shells out to git directly.
- `internal/store/` owns all SQLite access. No other package opens the database.

### 7.3 Adapter Interfaces

All adapters are Go interfaces in `internal/adapter/`. Engines depend on these interfaces, never on concrete implementations. The adapter registry reads `spec.config.yaml` and returns the concrete implementation for each configured provider.

```go
// internal/adapter/comms.go
type CommsAdapter interface {
    // Notify sends a structured message to the configured channel.
    Notify(ctx context.Context, msg Notification) error
    // PostStandup posts a formatted standup to the standup channel.
    PostStandup(ctx context.Context, standup StandupReport) error
    // FetchMentions returns recent mentions of spec IDs in configured channels.
    // Returns nil, nil if the provider doesn't support mention retrieval.
    FetchMentions(ctx context.Context, since time.Time) ([]Mention, error)
}

// internal/adapter/pm.go
type PMAdapter interface {
    // CreateEpic creates a new epic/issue linked to a spec.
    CreateEpic(ctx context.Context, spec SpecMeta) (epicKey string, err error)
    // UpdateStatus syncs the spec's pipeline status to the PM tool.
    UpdateStatus(ctx context.Context, epicKey string, status string) error
    // FetchUpdates returns status changes from the PM tool since last sync.
    FetchUpdates(ctx context.Context, epicKey string) (*PMUpdate, error)
}

// internal/adapter/docs.go
type DocsAdapter interface {
    // FetchSections retrieves the current content of the spec from the docs provider,
    // keyed by section slug.
    FetchSections(ctx context.Context, specID string) (map[string]string, error)
    // PushFull publishes the complete spec to the docs provider.
    PushFull(ctx context.Context, specID string, content string) error
    // PageURL returns the URL of the spec's page in the docs provider.
    PageURL(ctx context.Context, specID string) (string, error)
}

// internal/adapter/repo.go
type RepoAdapter interface {
    // ListPRs returns open PRs matching a spec's branch pattern across its repos.
    ListPRs(ctx context.Context, repos []string, specID string) ([]PullRequest, error)
    // PRStatus returns the review/CI status of a specific PR.
    PRStatus(ctx context.Context, repo string, prNumber int) (*PRDetail, error)
    // SetPRDescription updates a PR's description.
    SetPRDescription(ctx context.Context, repo string, prNumber int, body string) error
    // RequestedReviews returns PRs where the current user is a requested reviewer.
    RequestedReviews(ctx context.Context, user string) ([]PullRequest, error)
}

// internal/adapter/agent.go
type AgentAdapter interface {
    // Invoke spawns the agent as a subprocess. The MCP server is already
    // running (started by the build engine before Invoke). The agent connects
    // to it for context. contextFile is the fallback consolidated markdown
    // for non-MCP agents. Invoke blocks until the agent exits.
    Invoke(ctx context.Context, contextFile string, workDir string) error
    // SupportsMCP returns true if the agent natively connects to MCP servers.
    // If false, the build engine passes the context file via CLI args.
    SupportsMCP() bool
}

// internal/adapter/deploy.go
type DeployAdapter interface {
    // Trigger initiates a deployment for the given repos to the target environment.
    Trigger(ctx context.Context, repos []string, env string) (*DeployRun, error)
    // Status polls the deployment run for current state.
    Status(ctx context.Context, run *DeployRun) (*DeployStatus, error)
}

// internal/adapter/ai.go
type AIAdapter interface {
    // Complete sends a prompt and returns the completion.
    Complete(ctx context.Context, prompt string, system string) (string, error)
    // Embed returns a vector embedding for the given text.
    // Returns nil, ErrEmbeddingsNotSupported if the provider doesn't support embeddings.
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

**Null adapters:** Every adapter category has a `noop` implementation that is used when the provider is `none` or unconfigured. Noop adapters return empty results and `nil` errors - they never panic, never block on network. This is how `spec` functions with zero integrations configured.

### 7.4 Core Data Types

```go
// SpecMeta represents the YAML frontmatter of a SPEC.md file.
type SpecMeta struct {
    ID          string   `yaml:"id"`
    Title       string   `yaml:"title"`
    Status      string   `yaml:"status"`
    Version     string   `yaml:"version"`
    Author      string   `yaml:"author"`
    Cycle       string   `yaml:"cycle"`
    EpicKey     string   `yaml:"epic_key,omitempty"`
    Repos       []string `yaml:"repos"`
    RevertCount int      `yaml:"revert_count"`
    Source      string   `yaml:"source,omitempty"`
    Created     string   `yaml:"created"`
    Updated     string   `yaml:"updated"`
}

// Section represents a parsed markdown section with ownership.
type Section struct {
    Slug      string // e.g. "problem_statement"
    Heading   string // e.g. "## 1. Problem Statement"
    Level     int    // heading level (2 = ##, 3 = ###)
    Owner     string // from <!-- owner: role --> marker, or "auto"
    Content   string // raw markdown content (excluding heading line)
    StartLine int    // line number in the source file
    EndLine   int    // line number (exclusive)
}

// BuildContext is the assembled context payload passed to an agent.
type BuildContext struct {
    SpecPath      string   // path to the spec file
    SpecContent   string   // full spec markdown
    PriorDiffs    []string // cumulative diffs from earlier PR stack steps
    FailingTests  string   // output from last failing test run, if any
    Conventions   string   // contents of .spec/conventions.md, if present
    CurrentStep   PRStep   // the current PR stack step
    SystemPrompt  string   // agent instruction prompt
}

// PRStep represents one step in the PR stack plan.
type PRStep struct {
    Number      int
    Repo        string
    Description string
    Branch      string // e.g. "spec-042/step-1-token-bucket"
    Status      string // "pending", "in-progress", "complete"
}

// SessionState persists the build session for `spec do` resume.
type SessionState struct {
    SpecID       string    `yaml:"spec_id"`
    CurrentStep  int       `yaml:"current_step"`
    Branch       string    `yaml:"branch"`
    Repo         string    `yaml:"repo"`
    WorkDir      string    `yaml:"work_dir"`
    LastActivity time.Time `yaml:"last_activity"`
    Steps        []PRStep  `yaml:"steps"`
}
```

### 7.5 SQLite Schema

All local state lives in a single SQLite database at `~/.spec/spec.db`. The database is created on first run via `internal/store/db.go` with schema migrations.

```sql
-- Dashboard cache: stores aggregated signals with TTL
CREATE TABLE cache (
    key        TEXT PRIMARY KEY,           -- e.g. "dashboard:specs", "dashboard:prs"
    value      TEXT NOT NULL,              -- JSON-serialised payload
    fetched_at INTEGER NOT NULL,           -- unix timestamp
    ttl        INTEGER NOT NULL DEFAULT 300 -- seconds
);

-- Build sessions: one row per active spec build
CREATE TABLE sessions (
    spec_id      TEXT PRIMARY KEY,
    state        TEXT NOT NULL,             -- JSON-serialised SessionState
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

-- Activity log: append-only event log per spec
CREATE TABLE activity (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    spec_id    TEXT NOT NULL,
    event_type TEXT NOT NULL,               -- "advance", "build", "decide", "commit", "review", etc.
    summary    TEXT NOT NULL,               -- human-readable one-liner
    metadata   TEXT,                        -- JSON: PR number, decision #, stage names, etc.
    user_name  TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_activity_spec ON activity(spec_id, created_at);
CREATE INDEX idx_activity_time ON activity(created_at);

-- Embeddings: vector storage for semantic search
CREATE TABLE embeddings (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    spec_id   TEXT NOT NULL,
    section   TEXT NOT NULL,                -- section slug or "full" for whole-spec
    content   TEXT NOT NULL,                -- the text that was embedded
    vector    BLOB NOT NULL,                -- float32 array, serialised
    model     TEXT NOT NULL,                -- model used for embedding
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_embed_spec ON embeddings(spec_id);

-- Sync state: tracks last-synced hashes per section per spec
CREATE TABLE sync_state (
    spec_id    TEXT NOT NULL,
    section    TEXT NOT NULL,
    direction  TEXT NOT NULL,               -- "inbound" or "outbound"
    hash       TEXT NOT NULL,               -- SHA-256 of section content at last sync
    synced_at  INTEGER NOT NULL,
    PRIMARY KEY (spec_id, section, direction)
);
```

### 7.6 Git Operations Layer

`internal/git/` wraps all git interactions via `exec.Command`. No other package shells out to git.

**Specs repo management** (`specsrepo.go`):
- On first run, `spec` clones the specs repo to `~/.spec/repos/<owner>/<repo>/`. Subsequent runs do `git fetch origin && git reset --hard origin/<branch>` to ensure the local clone is current.
- All spec mutations (new, advance, decide, etc.) follow the pattern: **fetch → mutate file → commit → push**. This is wrapped in a helper:

```go
// WithSpecsRepo fetches the latest, calls the mutator function, then commits and pushes.
// The mutator receives the local clone path and returns the commit message.
// If the push fails due to a conflict (another user pushed first), it retries:
// fetch → re-apply → push, up to 3 times.
func WithSpecsRepo(cfg *config.Config, mutate func(repoPath string) (commitMsg string, err error)) error
```

- **Retry on conflict:** Since specs are separate files, conflicts are rare. But if two users advance the same spec simultaneously, the push will fail. `WithSpecsRepo` retries by re-fetching and re-applying the mutation. If the retry also fails (e.g., someone else already advanced the same spec), it returns a clear error: `Spec SPEC-042 was modified by another user. Run 'spec status SPEC-042' to see the current state.`

**Branch operations** (`branch.go`):
- `spec build` creates branches in service repos (not the specs repo) following the pattern `spec-<id>/step-<N>-<slug>`.
- Branch detection: `spec do` with no args checks the current branch name for the `spec-<id>/` prefix to auto-detect which spec and step the user is working on.

**Auth:** `spec` inherits the user's git auth configuration. SSH keys, credential helpers, personal access tokens in `~/.gitconfig` - whatever works for `git push` on the command line works for `spec`. No separate auth mechanism.

### 7.7 Markdown Engine

`internal/markdown/` is the parser that makes section-scoped sync, gate validation, and frontmatter mutation possible. It does **not** use a full markdown AST parser - it operates on line-level patterns, which is simpler and sufficient for the structured `SPEC.md` format.

**Frontmatter** (`frontmatter.go`):
- Reads/writes YAML between `---` delimiters at the top of the file.
- `ReadMeta(path) → SpecMeta` and `WriteMeta(path, SpecMeta)` - WriteMeta preserves the rest of the file content.
- Uses `gopkg.in/yaml.v3` for structured parsing.

**Section extraction** (`sections.go`):
- Scans for lines matching `^#{1,6} ` (markdown headings).
- For each heading, looks for an `<!-- owner: <role> -->` comment on the same line or the next line.
- Returns `[]Section` with slug, owner, content, and line ranges.
- Slug derivation: strip section number and punctuation, lowercase, replace spaces with underscores. `## 1. Problem Statement` → `problem_statement`. `### 7.3 PR Stack Plan` → `pr_stack_plan`.
- Sub-sections inherit the parent section's owner unless they have their own `<!-- owner -->` marker.

**Section replacement** (`sections.go`):
- `ReplaceSection(filePath, slug, newContent) error` - replaces the content of a section (between its heading and the next same-or-higher-level heading) with new content. Used by sync engine inbound, `spec draft` accept, and `spec decide`.

**Decision log** (`decisionlog.go`):
- Parses the markdown table under `## Decision Log`.
- `AppendDecision(path, question, user, date) (number int, err error)` - adds a row with the next sequential number.
- `ResolveDecision(path, number, decision, rationale, user, date) error` - updates an existing row.
- Table is parsed/written as raw text lines (pipe-delimited), not via a markdown table library. The format is fixed and predictable.

### 7.8 Sync Engine

The sync engine (`internal/sync/`) handles bidirectional, section-scoped synchronisation between the specs repo and external tools. This is the most complex subsystem.

**Sync flow for `spec sync SPEC-042`:**

```
1. Fetch latest spec from specs repo (git fetch + read file)
2. Fetch sections from docs provider (DocsAdapter.FetchSections)
3. For each section in the spec:
   a. Compute hash of local (specs repo) content
   b. Compute hash of remote (docs provider) content
   c. Look up last-synced hashes from sync_state table
   d. Determine what changed:
      - Local changed, remote unchanged → outbound push
      - Remote changed, local unchanged → inbound pull (if role matches)
      - Both changed → CONFLICT
      - Neither changed → skip
4. For inbound changes:
   a. Verify the remote change is to a section owned by the remote editor's role
   b. Apply via ReplaceSection
5. For outbound changes:
   a. Push full spec via DocsAdapter.PushFull
6. For conflicts:
   a. If conflict_strategy is "warn": print diff, require --force or --skip
   b. If "repo-wins": push outbound, overwrite remote
   c. If "remote-wins": pull inbound, overwrite local
7. Update sync_state table with new hashes
8. Commit + push specs repo changes (via WithSpecsRepo)
```

**Section-level diffing** (`sections.go`):
- Sections are compared by SHA-256 hash of their trimmed content.
- The sync_state table stores the hash of each section at last sync time, per direction. This three-way comparison (local now, remote now, last-synced) is what detects conflicts vs. clean one-directional changes.

**Role guard:**
- Inbound changes are only applied if the section's `<!-- owner: role -->` matches the inbound source's role. A PM's Confluence edits to §7 Technical Implementation are silently ignored (logged as a warning, not applied).

**Confluence adapter specifics** (`adapter/confluence/docs.go`):
- Uses the Confluence REST API v2 (`/wiki/api/v2/pages`).
- On outbound push: converts markdown to Confluence storage format (XHTML). Uses a lightweight markdown-to-confluence converter (custom, not a full library - the spec format is constrained enough that heading/paragraph/table/code-block conversion covers 95% of cases).
- On inbound fetch: converts Confluence storage format back to markdown sections. This is lossy for complex formatting but faithful for the structured content in spec sections (prose paragraphs, bullet lists, tables).
- Section mapping: Confluence headings are mapped to spec section slugs by the same slugification logic. The outbound push inserts `<!-- spec-section: slug -->` HTML comments into the Confluence page to make inbound mapping reliable even if someone renames a heading in Confluence.

### 7.9 Build Engine

`internal/build/` orchestrates the coding agent integration.

**`spec build <id>` flow:**

1. Validate the spec is at `build` stage (or `engineering` if no work has started).
2. Read the spec from `.spec/<id>.md` in the current service repo. Warn if stale.
3. Parse §7.3 PR Stack Plan into `[]PRStep`.
4. If no session exists, create one (first step, initial branch).
5. If a session exists, resume from `session.CurrentStep`.
6. For the current step:
   a. Verify the user is in the correct repo (from `PRStep.Repo`). If not, print: `This step targets <repo>. Switch to that repo and run 'spec do' again.`
   b. Create or checkout the step branch: `spec-<id>/step-<N>-<slug>`.
   c. Assemble `BuildContext` (spec content, prior diffs, failing tests, conventions).
   d. Write consolidated context file to `~/.spec/sessions/<spec-id>/context.md` (fallback for non-MCP agents).
   e. Start the MCP server (binds to the session, serves resources + tools from BuildContext).
   f. Call `AgentAdapter.Invoke()` to spawn the agent subprocess. This blocks - the agent takes over the terminal. MCP-compatible agents connect to the server automatically; non-MCP agents receive the context file path as a CLI argument.
   g. While the agent runs, the MCP server handles tool calls: `spec_decide` writes to the decision log, `spec_step_complete` marks the step done, `spec_search` queries the knowledge base.
7. After the agent exits, stop the MCP server. Collect any decisions/step-completions recorded via MCP tools.
8. Update session state and activity log.
9. If step was not marked complete via MCP, prompt: `Step N complete? [y/n]` - if yes, advance to next step.

**`spec do` flow:**

1. If an ID is given, resume that spec's session.
2. If no ID, check for an active session in the SQLite sessions table (most recently updated). If no session exists, check the current branch name for a `spec-<id>/` prefix.
3. Resume from step 6 above.

**MCP server** (`internal/build/mcp.go`):

The MCP server runs as a stdio transport - it communicates with the agent via stdin/stdout of a child process. The build engine starts it before invoking the agent and stops it after the agent exits.

```go
// MCPServer serves spec context to MCP-compatible agents.
type MCPServer struct {
    session  *SessionState
    ctx      *BuildContext
    store    *store.DB
    specPath string
}

// Resources served:
// - spec://current/full           → full spec markdown
// - spec://current/section/{slug} → specific section
// - spec://current/decisions       → decision log
// - spec://current/prior-diffs     → diffs from prior steps
// - spec://current/conventions     → conventions.md content
// - spec://related?q={query}       → knowledge search results
//
// Tools served:
// - spec_decide(question)                         → appends to decision log
// - spec_decide_resolve(number, decision, rationale) → resolves decision
// - spec_step_complete()                          → marks current step done
// - spec_status()                                 → returns pipeline status + AC progress
// - spec_search(query)                            → searches spec history
```

**Agent subprocess invocation** (`adapter/claude/agent.go` as example):

```go
func (a *ClaudeAdapter) Invoke(ctx context.Context, contextFile string, workDir string) error {
    // Claude Code connects to MCP servers configured in .mcp.json.
    // The spec MCP server is already running; Claude discovers it via config.
    // We just spawn claude in the working directory.
    cmd := exec.CommandContext(ctx, "claude", "--resume")
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Dir = workDir
    return cmd.Run()
}

func (a *ClaudeAdapter) SupportsMCP() bool { return true }
```

For a non-MCP agent, the adapter passes the context file directly:

```go
func (a *CustomAdapter) Invoke(ctx context.Context, contextFile string, workDir string) error {
    // Custom agents receive the context file path as an argument.
    cmd := exec.CommandContext(ctx, a.command, "--context", contextFile)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Dir = workDir
    return cmd.Run()
}

func (a *CustomAdapter) SupportsMCP() bool { return false }
```

The build engine doesn't know or care which agent is being used. MCP-compatible agents get dynamic, queryable context. Non-MCP agents get a static context file. Both work.

### 7.10 Dashboard Engine

`internal/dashboard/` aggregates signals from all configured adapters into the terminal view.

**Data flow:**

```
spec (no args)
  │
  ├─ Read cache from SQLite (cache table, keyed by section)
  │   │
  │   ├─ Cache hit + within TTL → use cached data
  │   └─ Cache miss or stale → fetch live:
  │       ├─ Specs repo: git fetch, read frontmatter of all active specs
  │       ├─ RepoAdapter.RequestedReviews() → REVIEW section
  │       ├─ CommsAdapter.FetchMentions() → INCOMING section
  │       └─ Write all results to cache with current timestamp
  │
  ├─ Filter by user role:
  │   ├─ DO: specs where stage.owner_role == user.role, sorted by time-in-stage desc
  │   ├─ REVIEW: PRs where user is requested reviewer
  │   ├─ INCOMING: new triage items + specs entering user's stage + mentions
  │   ├─ BLOCKED: specs in blocked state involving user
  │   └─ FYI: recently completed specs user was involved in
  │
  └─ Render to terminal (colour-coded, 2 lines per item max)
```

**Passive awareness** (`awareness.go`):
- Called at the start of every non-dashboard command.
- Reads from cache only (no network). Counts items that would appear in DO + REVIEW.
- If count > 0, prints a single dim line: `⚠ N pending · run 'spec' for details`
- Adds <10ms. If the cache table doesn't exist yet (first run), prints nothing.

### 7.11 Key Dependencies

| Dependency | Purpose | Notes |
|---|---|---|
| `github.com/spf13/cobra` | CLI framework | Command routing, flag parsing, shell completions |
| `github.com/spf13/viper` | Config loading | YAML parsing, env var interpolation, config file search |
| `modernc.org/sqlite` | SQLite driver | Pure Go, no CGo - critical for cross-compilation and static binary |
| `gopkg.in/yaml.v3` | YAML frontmatter | Structured frontmatter read/write |
| `github.com/fatih/color` | Terminal colours | Dashboard rendering, status indicators |
| `github.com/olekukonez/tablewriter` | Table rendering | `spec list`, `spec decide --list`, metrics output |
| `github.com/google/go-github/v62` | GitHub API client | RepoAdapter, DeployAdapter |
| `github.com/andygrunwald/go-jira` | Jira API client | PMAdapter |
| `golang.org/x/term` | Terminal size detection | Dashboard + watch layout |
| `github.com/mark3labs/mcp-go` | MCP server SDK | Stdio transport, resource/tool registration; the reference Go MCP implementation |

**No CGo.** The binary must be statically linked and cross-compilable. This rules out `mattn/go-sqlite3` (requires CGo) in favour of `modernc.org/sqlite` (pure Go SQLite). Slightly slower but the tradeoff is worth it for single-binary distribution.

### 7.12 Testing Strategy

- **Unit tests** for all `internal/` packages. Each engine is tested against adapter interfaces, not concrete implementations. Test doubles (mocks) implement each adapter interface.
- **Markdown engine tests** are the highest priority - section extraction, frontmatter mutation, decision log parsing, and section replacement must be rock-solid since the entire system depends on them. Use golden file tests: input markdown → expected parsed output.
- **Git operations tests** use a temporary git repo created in `t.TempDir()`. `WithSpecsRepo` is tested with simulated concurrent pushes (two goroutines racing to push).
- **Sync engine tests** use mock DocsAdapter implementations that return known section content. Test the three-way diff logic (local changed, remote changed, both changed, neither changed) exhaustively.
- **Integration tests** for each adapter against real APIs are gated behind `SPEC_INTEGRATION_TEST=1` and require tokens. These run in CI on a schedule, not on every push.
- **CLI tests** use Cobra's built-in testing support: execute commands programmatically and assert on stdout/stderr output.
- **No test database:** Each test creates a fresh SQLite in-memory database (`:memory:` DSN). No shared test state.

### 7.13 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Integration APIs change (Jira, Confluence, GitHub) | Medium | Medium | Adapter version-pinning; integration test suite against API mocks; adapters are isolated so a broken Confluence adapter doesn't affect GitHub |
| Dashboard latency exceeds 100ms target | Medium | High | Cache-first: dashboard always reads from SQLite first; live fetch is async with stale-while-revalidate pattern |
| Engineers skip `spec do` and prompt agents directly | High | High | Make `spec do` obviously more convenient - session resume, incremental context, PR stack sequencing; good DX is the only mitigation |
| Config complexity overwhelms initial setup | Medium | High | `spec config init` wizard; sensible defaults; `--provider none` escape; v0.1 works with zero integrations |
| User commits personal identity to shared repo config | Medium | Medium | `spec config init` writes user block only to `~/.spec/config.yaml`; template excludes user key |
| Section-scoped sync produces unexpected merges | Medium | High | Conservative default: `conflict_strategy: warn`; `spec sync --dry-run`; clear diff output; three-way hash comparison |
| Confluence markdown round-trip is lossy | High | Medium | Constrain the converter to the subset of markdown used in specs (headings, paragraphs, lists, tables, code blocks); insert `<!-- spec-section: slug -->` markers for reliable re-mapping; warn on unrecognised formatting |
| Specs repo push conflicts from concurrent users | Low | Medium | `WithSpecsRepo` retries up to 3 times with re-fetch; specs are separate files so conflicts are rare; same-spec conflicts produce a clear error |
| `spec pull` copies go stale in service repos | Medium | Medium | `spec do` compares local `.spec/` file hash against specs repo; warns if stale; `spec pull` shows last-updated timestamp |
| Scope creep | High | High | Strict adoption path: v0.1 ships core lifecycle + dashboard; each version adds one layer; ship and validate before expanding |
| Semantic search quality for `spec context` | Medium | Medium | Start with full-text search via git-grep; add embedding search when AI adapter ships; structured spec format makes keyword search effective |
| Activity log grows unbounded | Low | Low | Prune activity older than 90 days on `spec` startup; archive with spec on close |
| AI-generated content gets rubber-stamped | Medium | High | Accept/edit/skip forces conscious choice; no auto-accept; `spec retro` can track AI acceptance rates as quality signal |
| AI provider costs accumulate | Medium | Medium | AI only called on explicit user action; no background calls; `ollama` for zero-cost local; `preferences.ai_drafts: false` per-user opt-out |
| AI hallucination in `spec context` | Medium | High | Answers grounded in retrieved spec content only; citations required and verifiable; labelled as AI-generated |
| Pure Go SQLite (modernc.org) is slower than CGo version | Low | Low | Acceptable for the data volumes spec handles (hundreds of specs, not millions); benchmark during v0.1 |
| Teams webhook limitations (no bidirectional read) | Medium | Medium | Teams adapter supports outbound notifications via webhook; FetchMentions may require Graph API with additional auth; degrade gracefully if Graph API isn't configured |

### 7.14 PR Stack Plan

**Phase 1 - Foundation (v0.1)**

| # | Repo | PR | Description | Depends on |
|---|---|---|---|---|
| 1 | spec-cli | Project scaffold | Go module, Cobra root command, Makefile, GoReleaser config, CI (lint + test), `main.go` entrypoint | - |
| 2 | spec-cli | Config system | `internal/config/` - team config + user config types, YAML loading, env var interpolation (`${VAR}`), resolution chain (cwd → repo root → specs clone → user), `spec config init` + `spec config init --user` wizards, `spec config test` | 1 |
| 3 | spec-cli | SQLite store | `internal/store/` - db init, migrations, connection pool, cache/sessions/activity/sync_state tables, in-memory test helper | 1 |
| 4 | spec-cli | Git operations | `internal/git/` - clone, fetch, commit, push, `WithSpecsRepo` retry wrapper, specs repo management (`~/.spec/repos/`), branch helpers, auth passthrough | 2 |
| 5 | spec-cli | Markdown engine | `internal/markdown/` - frontmatter read/write, section extraction with `<!-- owner -->` markers, slug derivation, section replacement, decision log table parser/mutator. Golden file test suite | 1 |
| 6 | spec-cli | Adapter interfaces + registry | `internal/adapter/` - all interfaces (comms, pm, docs, repo, agent, deploy, ai), noop implementations, adapter registry that resolves config → concrete adapter | 2 |
| 7 | spec-cli | Pipeline engine | `internal/pipeline/` - stage definitions from config, gate evaluation (`section_complete`, `pr_stack_exists`, `prs_approved`, `duration`), forward transitions, backward transitions, eject/resume, fast-track. `spec advance`, `spec revert`, `spec eject`, `spec resume`, `spec validate` commands | 4, 5, 6 |
| 8 | spec-cli | Core spec commands | `spec new` (template scaffolding, auto-ID, epic creation via PMAdapter), `spec list` (role-filtered, `--all`, `--triage`), `spec status` (pipeline diagram, section completion, reversion history), `spec edit` ($EDITOR or docs URL), `spec whoami`, `spec decide` | 4, 5, 6, 7 |
| 9 | spec-cli | Dashboard | `internal/dashboard/` - signal aggregation (DO, REVIEW, INCOMING, BLOCKED, FYI), cache-first rendering, TTL management. `spec` (root command) renders dashboard. Passive awareness line on all other commands. | 3, 6, 8 |
| 10 | spec-cli | Intake + promote + pull | `spec intake` (triage template scaffolding), `spec promote` (triage → full spec), `spec pull` (specs repo → service repo `.spec/`), stale detection | 4, 5, 8 |

**Phase 2 - Build & Agent Integration (v0.2)**

| # | Repo | PR | Description | Depends on |
|---|---|---|---|---|
| 11 | spec-cli | Build engine core + MCP server | `internal/build/` — PR stack plan parser (from §7.3 markdown), session state management, context assembly, step sequencing, MCP server (stdio transport, resources + tools). `spec build`, `spec do`, `spec mcp-server` commands. | 3, 5, 6 |
| 12 | spec-cli | Claude Code adapter | `internal/adapter/claude/` - MCP-native agent adapter. `SupportsMCP() = true`, `Invoke` spawns `claude` subprocess (agent discovers MCP server via `.mcp.json`). | 6, 11 |
| 13 | spec-cli | GitHub adapter (repo) | `internal/adapter/github/repo.go` - `ListPRs`, `PRStatus`, `SetPRDescription`, `RequestedReviews`. Uses `go-github`. | 6 |
| 14 | spec-cli | AI service layer | `internal/ai/` - `AIService` null-safe wrapper, accept/edit/skip interaction flow, prompt templates. `spec draft` command (section, PR, PR stack). | 3, 5, 6 |
| 15 | spec-cli | Anthropic AI adapter | `internal/adapter/anthropic/` - chat completions + embeddings via Anthropic API. | 6, 14 |
| 16 | spec-cli | Ollama AI adapter | `internal/adapter/ollama/` - local model completions + embeddings. | 6, 14 |

**Phase 3 - Integrations & Sync (v0.3)**

| # | Repo | PR | Description | Depends on |
|---|---|---|---|---|
| 17 | spec-cli | Teams adapter (comms) | `internal/adapter/teams/` - webhook notifications, channel posting, standup posting. Graph API for FetchMentions (optional, degrades gracefully). | 6 |
| 18 | spec-cli | Jira adapter (PM) | `internal/adapter/jira/` - epic creation, status sync, fetch updates. Uses `go-jira`. | 6 |
| 19 | spec-cli | Confluence adapter (docs) | `internal/adapter/confluence/` - page read (XHTML → markdown sections), page write (markdown → XHTML), section slug markers. | 5, 6 |
| 20 | spec-cli | Sync engine | `internal/sync/` - three-way section hash diffing, inbound/outbound orchestration, role guard, conflict resolution (warn/repo-wins/remote-wins), `--dry-run`. `spec sync` command. | 3, 5, 19 |
| 21 | spec-cli | `spec review` | Aggregate PRs across repos (from RepoAdapter), render in dependency order, post structured review request to comms. AI-enriched summary if configured. | 13, 17, 14 |

**Phase 4 - Full Control Plane (v0.4)**

| # | Repo | PR | Description | Depends on |
|---|---|---|---|---|
| 22 | spec-cli | GitHub Actions deploy adapter | `internal/adapter/github/deploy.go` - workflow dispatch trigger, run status polling. `spec deploy` command. | 6, 13 |
| 23 | spec-cli | Ceremony engine | `internal/ceremony/` - standup generation from activity log + git + PR data, retro metrics aggregation, qualitative capture. `spec standup`, `spec retro`, `spec metrics` commands. | 3, 13, 17 |
| 24 | spec-cli | Knowledge engine | `internal/knowledge/` - full-text search via git-grep, semantic search with embeddings (lazy index build), answer synthesis. `spec search`, `spec context`, `spec history` commands. | 3, 4, 14 |
| 25 | spec-cli | `spec watch` | Live-updating terminal dashboard. Polling loop with configurable interval. Pipeline visualisation - specs grouped by stage, stale indicators. Uses `golang.org/x/term` for raw terminal mode. | 9 |

---

## 8. Escape Hatch Log <!-- auto: spec eject -->

*No escapes logged.*

---

## 9. QA Validation Notes <!-- owner: qa -->

---

## 10. Deployment Notes <!-- owner: engineer -->

---

## 11. Retrospective <!-- auto: spec retro -->

---

*Generated with `spec new` · S.P.E.C methodology · aaronlewis.blog/posts/spec*
