---
id: SPEC-001
title: spec CLI Tool
status: draft
version: 0.1.0
author: —
cycle: TBD
created: 2026-04-17
updated: 2026-04-17
---

# SPEC-001 — `spec` CLI Tool

> *A lifecycle management tool for the S.P.E.C methodology.*

---

## Decision Log

> *Record all significant decisions, questions and changes here for asynchronous reference.*

| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Plugin vs. adapter architecture for integrations? | (1) Hardcoded integrations per tool, (2) Adapter pattern with community plugins, (3) Config-driven with provider abstractions | **(3) Config-driven with provider abstractions** | Keeps core logic decoupled from any specific tool; new providers added via adapters without modifying core; config is the single place teams declare their stack | — | 2026-04-17 |
| 002 | Local CLI vs. hosted service? | (1) Pure CLI with local config, (2) SaaS with team workspace, (3) CLI + optional cloud sync | **(1) Pure CLI with local config for v1** | Ship fast with minimal infrastructure; architecture should leave the door open for optional cloud sync in future (team dashboard, cross-repo visibility) but v1 is local-only | — | 2026-04-17 |
| 003 | Spec storage — where is the golden source of truth? | (1) Per-service repo `.spec/` directory, (2) External tool only (Confluence/Notion), (3) Canonical in external tool mirrored to repo, (4) Dedicated specs repo as canonical source | **(4) Dedicated specs repo** | Specs are a team artefact, not a repo artefact — a PM writes the problem statement before anyone knows which repos are involved; a dedicated specs repo makes cross-repo specs the default, simplifies `spec list`/`search`/`history`, and gives PMs/designers a single place to contribute without touching service repos | — | 2026-04-17 |
| 004 | How is a user's `owner_role` resolved at runtime? | (1) Declared once in `~/.spec/config.yaml` (user-level), (2) Declared in repo-level `spec.config.yaml` per team member, (3) Prompted interactively on first `spec` command if not set | **(1) + (3) User-level config with interactive fallback** | Role is personal, not repo-scoped; declared once in `~/.spec/config.yaml`; if missing, `spec` prompts the user to configure on first role-dependent command | — | 2026-04-17 |
| 005 | Should `spec list` query live from the docs/PM integration or from a local cache? | (1) Always live — query Confluence/Jira on invocation, (2) Local cache synced on `spec pull`, (3) Live with offline fallback to cache | **(3) Live with offline fallback to cache** | Best of both: fresh data when online, graceful degradation offline; cache is populated as a side effect of live queries; with specs repo as canonical, `spec list` can read frontmatter directly from git with remote enrichment (PR status, ticket status) | — | 2026-04-17 |
| 006 | Should specs span multiple repos? | (1) Spec lives in one service repo only, (2) Spec lives in a shared workspace directory, (3) Dedicated specs repo with `spec pull` into service repos | **(3) Dedicated specs repo with `spec pull`** | End-to-end features often span multiple repos; tying a spec to one repo forces an awkward "which repo owns this?" decision; a dedicated specs repo makes multi-repo the default; engineers `spec pull` into their working repo for local context and agent builds | — | 2026-04-17 |
| 007 | How do PMs and designers contribute without touching the specs repo? | (1) They edit markdown directly in the specs repo, (2) One-way outbound sync only (repo → Confluence), (3) Bidirectional section-scoped sync via external tools | **(3) Bidirectional section-scoped sync** | PMs should write in Confluence, designers should link from Figma/Teams; `spec sync` pulls their changes inward, scoped by `<!-- owner: role -->` markers so sections can't overwrite each other; outbound sync publishes the full spec for reading | — | 2026-04-17 |

---

## 1. Problem Statement <!-- owner: pm -->

Software teams adopting agentic development workflows lack a dedicated tool to manage the full lifecycle of a `SPEC.md` document. Today, spec artefacts are produced ad-hoc — scattered across Confluence pages, Notion docs, Google Docs, or repo markdown files — with no standardised structure, no enforced review pipeline, and no integration with the downstream tools that engineers, designers, QA and DevOps depend on daily.

This creates several failure modes:
- Decisions are made verbally and never captured, creating institutional knowledge gaps.
- Specs go stale mid-cycle because there is no single authoritative version.
- Cross-functional contributors (PM, Design, QA) have no clear entry point or handoff signal.
- Agentic coding tools receive ambiguous or incomplete context, producing low-quality output.
- New team members have no structured way to build system understanding from historical reasoning.

### Who is affected?

| Role | Pain Today |
|---|---|
| PM | No standard template; hard to track spec status across cycles |
| Tech Lead | Context-switches to manually stage work for engineers |
| Engineer | Unclear when a spec is "ready"; agent prompts built from scratch each time |
| Designer | No defined entry/exit point in the spec pipeline |
| QA | Acceptance criteria added late or forgotten entirely |
| New Hire | No structured knowledge base to onboard from |

---

## 2. Goals & Non-Goals <!-- owner: pm -->

### Goals

- Provide a CLI-first, integration-flexible tool to manage the full S.P.E.C lifecycle.
- Standardise the `SPEC.md` format without being prescriptive about tooling stack.
- Automate handoffs between contributors and roles via notifications and status transitions.
- Surface the right context to agentic coding tools at the right phase.
- Accumulate a searchable, persistent knowledge base of spec history and decision rationale.
- Be adoptable by teams using **any** common stack combination (Atlassian, GitHub/Linear/Slack, Notion/GitHub/Discord, etc.).
- Support specs that span multiple service repos without requiring a "home repo" decision.

### Non-Goals

- `spec` is not a project management tool — it does not replace Jira, Linear or equivalent.
- `spec` is not a code review tool — it does not replace GitHub PRs or equivalent.
- `spec` is not an AI coding agent — it orchestrates agents, it is not one itself.
- `spec` does not enforce any specific cycle length or team structure.

---

## 3. User Stories <!-- owner: pm -->

*(QA to add acceptance criteria per story)*

| # | As a... | I want to... | So that... | Acceptance Criteria |
|---|---|---|---|---|
| US-01 | PM | Create a new spec from a standard template | All stakeholders start from consistent structure | `spec new --title "Auth refactor"` auto-assigns the next sequential ID (e.g., `SPEC-043`), scaffolds a fully-structured `SPEC.md` in the specs repo with all sections, pre-populated frontmatter, and a blank decision log |
| US-02 | PM | Notify the TL that a spec draft is ready for feasibility review | The process moves forward without a meeting | Running `spec advance SPEC-042` validates draft gates, transitions to `tl-review`, and sends a notification to the TL via the configured comms integration with a link to the spec |
| US-03 | TL | Advance a spec to the next pipeline stage | Downstream contributors are unblocked | `spec advance SPEC-042` transitions the spec status and fires the correct notification to the next owner |
| US-04 | Designer | Know exactly what section of the spec I own and when it is needed | I can contribute at the right moment without attending a kickoff meeting | When a spec reaches `design` stage, the Designer receives a notification and the `SPEC.md` template clearly marks the design input section |
| US-05 | QA | Add acceptance criteria to a spec before engineering begins | Engineers and agents know what "done" looks like upfront | QA can annotate the `SPEC.md` acceptance criteria section; `spec validate SPEC-042` checks that this section is non-empty before allowing advancement to `engineering` stage |
| US-06 | Engineer | Pull a fully-staged spec into my local service repo | I can immediately begin technical planning with full context | `spec pull SPEC-042` fetches the latest spec from the specs repo and writes it into the current service repo at `.spec/SPEC-042.md` |
| US-07 | Engineer | Trigger an agentic build from my spec | I don't need to re-prompt context from scratch each time | `spec build SPEC-042` injects the spec as structured context into the configured agent harness and initiates the build |
| US-08 | Engineer | Declare an escape hatch when a blocker is found | Work is redirected cleanly without silently stalling | `spec eject SPEC-042 --reason "upstream dependency missing"` logs the blocker to the decision log, transitions spec to `blocked`, and notifies the TL |
| US-09 | Engineer | Submit my stacked PRs for team review | The review rotation can proceed asynchronously | `spec review SPEC-042` posts a structured review request to the comms integration, linking all stacked PRs in dependency order |
| US-10 | Any team member | Search historical specs and decision logs | I can understand why the system was built the way it was | `spec search "authentication flow"` queries the specs repo history and returns matching specs with their decision logs |
| US-11 | New hire | Browse archived specs in reverse-chronological order | I can build system understanding before writing code | `spec history --limit 10` lists the last 10 completed specs with summaries |
| US-12 | Team | Use `spec` regardless of whether our stack is Atlassian, Linear, or Notion | Teams are not locked into a single vendor | All integrations are configured via `spec.config.yaml`; the tool ships with first-class adapters for common stacks and a documented adapter interface for custom integrations |
| US-13 | Any team member | Declare my role once and have `spec` remember it | I don't have to specify who I am on every command | `spec` reads `owner_role` from `~/.spec/config.yaml`; `spec whoami` confirms the currently resolved identity; a missing role triggers a clear setup prompt |
| US-14 | Any team member | Run `spec list` and see only specs currently awaiting action from my role | I have a personal action queue without filtering noise from the whole pipeline | `spec list` returns specs where the current pipeline stage's `owner_role` matches the user's configured role, grouped by urgency and annotated with time-in-stage |
| US-15 | PM | Edit my spec sections in Confluence and have changes flow back to the specs repo | I don't have to learn git or markdown tooling to contribute | `spec sync SPEC-042` pulls PM-owned sections (§1–4) from the configured docs provider into the canonical spec, scoped by `<!-- owner: pm -->` markers |
| US-16 | Designer | Attach a Figma link or design notes to a spec from my usual tools | I can contribute design context without switching to a developer workflow | `spec link SPEC-042 --section design --url "https://figma.com/..."` appends the resource to §5; alternatively, a comms bot captures tagged messages and writes them to the design section |
| US-17 | Engineer | Work on a spec that spans multiple service repos | I don't need to decide which repo "owns" a cross-cutting feature | The spec lives in the dedicated specs repo; `spec pull SPEC-042` works from any service repo; `spec build SPEC-042` reads from the local `.spec/` copy regardless of which repo the engineer is in |
| US-18 | QA | Send a spec back to a previous stage when expectations aren't met | Work is redirected to the right phase without losing context | `spec revert SPEC-042 --to build --reason "3 of 5 acceptance criteria failing"` logs the reason, transitions the spec back, and notifies the build stage owner |
| US-19 | Any team member | Record a decision or question against a spec from the command line | Decisions are captured in the moment rather than forgotten or buried in chat | `spec decide SPEC-042 --question "REST vs gRPC for inter-service calls?"` appends a new row to the decision log; `spec decide SPEC-042 --resolve 003 --decision "gRPC" --rationale "Lower latency, schema enforcement"` updates an existing entry |

---

## 4. Proposed Solution <!-- owner: pm -->

### 4.1 Concept Overview

`spec` is a lightweight, config-driven CLI tool that acts as the **lifecycle controller** for `SPEC.md` documents. It does not store data itself — it is a coordinator that reads from and writes to the tools a team already uses.

Its core responsibilities:

1. **Scaffolding** — generate new `SPEC.md` files from a standard template.
2. **Status management** — transition specs through defined pipeline stages and validate gate conditions at each transition.
3. **Notifications** — fire the right message to the right person at the right time via the team's comms integration.
4. **Sync** — bidirectional, section-scoped synchronisation between the specs repo and external tools (Confluence, Notion, Figma, etc.).
5. **Agent orchestration** — provide the spec file as context to the configured agent provider with a system prompt defining how to reference it and what conventions to follow.
6. **Decision capture** — structured command-line interface for recording questions, decisions, and rationale to the spec's decision log.
7. **Archival** — the specs repo is the knowledge base; completed specs are moved to `archive/` and remain searchable via git history.

### 4.2 Storage Model

```
specs repo (canonical source of truth)
  └── SPEC-042.md              ← active specs at root
  └── SPEC-043.md
  └── archive/                 ← completed specs (moved here on `done`)
       └── SPEC-001.md
       └── ...
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
- **`spec sync`** provides bidirectional, section-scoped sync with external tools. Inbound changes are scoped by `<!-- owner: role -->` markers — a PM's Confluence edits can only update PM-owned sections. Outbound sync publishes the full spec for reading. `spec sync` also handles writing local changes from service repos back to the specs repo (scoped to the current user's role).
- **`spec pull`** copies a spec from the specs repo into a service repo's `.spec/` directory for local context and agent builds.
- Specs are **not tied to any single service repo**, making cross-repo features the default.
- **Archival** is a `git mv` from the specs repo root to `archive/` when a spec reaches `done`. `spec search` and `spec history` read from both active and archived specs.

### 4.3 Sync Model

Sync is **section-scoped** and **role-aware**. The `<!-- owner: role -->` markers in each section heading define who can write to that section from which tool.

| Direction | Source | Target | Sections | Trigger |
|---|---|---|---|---|
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: pm -->` sections (§1–4) | `spec sync <id>` |
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: designer -->` sections (§5) | `spec sync <id>` |
| Inbound | Confluence/Notion | Specs repo | `<!-- owner: qa -->` sections (§6, §9) | `spec sync <id>` |
| Inbound | Service repo `.spec/` | Specs repo | Sections matching user's role | `spec sync <id>` (from service repo) |
| Outbound | Specs repo | Confluence/Notion | All sections (full spec) | `spec sync <id>` or auto on `spec advance` |
| Outbound | Specs repo | Jira/Linear | Frontmatter (`status`, `epic_key`) | Auto on `spec advance` |
| Inbound | Figma / Comms | Specs repo | §5 Design Inputs (links, annotations) | `spec link <id>` or comms bot |

**Conflict resolution:** The specs repo always wins on conflict. If a section was modified in both the specs repo and Confluence since the last sync, `spec sync` warns and requires `--force` to accept the inbound change or `--skip` to keep the repo version.

### 4.4 Pipeline Stages

```
draft → tl-review → design → qa-expectations → engineering → build → pr-review → qa-validation → done
  ↑         ↑          ↑           ↑                ↑          ↑         ↑                        ↘
  └─────────┴──────────┴───────────┴────────────────┴──────────┴─────────┘                    blocked
                              spec revert --to <stage>                                    (escape hatch)
```

**Forward transitions** (`spec advance`) follow the linear happy path. Each transition validates configurable gate conditions before the advance is permitted.

**Backward transitions** (`spec revert --to <stage> --reason "..."`) allow the current stage owner to send a spec back to any previous stage. No gates are checked on reversion. The reason is logged to the decision log and both the current and target stage owners are notified. Reversions are tracked in frontmatter (`revert_count`) as a process health metric.

**Escape hatch** (`spec eject --reason "..."`) transitions to `blocked` from any stage. `spec resume` returns to the pre-block stage.

### 4.5 Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                       spec CLI                            │
│                                                           │
│  new | pull | sync | link | decide | advance | build |    │
│  eject | revert | review | validate | search | history    │
│                                                           │
│  ┌──────────────┐  ┌────────────┐  ┌──────────────────┐ │
│  │ Spec Engine  │  │ Sync Engine│  │ Adapter Registry  │ │
│  │              │  │            │  │                   │ │
│  │ - Templates  │  │ - Section  │  │ Comms   | Docs    │ │
│  │ - Stages     │  │   scoping  │  │ ------- | ------  │ │
│  │ - Gates      │  │ - Conflict │  │ Teams   | Confl.  │ │
│  │ - Dec. Log   │  │   resolve  │  │ Slack   | Notion  │ │
│  │ - Frontmatter│  │ - Role     │  │ Discord | GitHub  │ │
│  └──────────────┘  │   guards   │  │         |        │ │
│                     └────────────┘  │ PM      | Agent   │ │
│                                     │ ------- | ------  │ │
│                                     │ Jira    | Claude  │ │
│                                     │ Linear  | Cursor  │ │
│                                     │ GH Iss. | Copilot │ │
│                                     │         |        │ │
│                                     │ Repo    | Design  │ │
│                                     │ ------- | ------  │ │
│                                     │ GitHub  | Figma   │ │
│                                     │ GitLab  |        │ │
│                                     │ Bitbkt  |        │ │
│                                     └──────────────────┘ │
└──────────────────────────────────────────────────────────┘
        │              │
        ▼              ▼
  Specs Repo     Service Repos
  (canonical     (.spec/ copies
   + archive)     for builds)
```

### 4.6 Configuration

The tool is configured via two files:

- **Specs repo** `spec.config.yaml` — team settings, integrations, pipeline definition. Committed to the specs repo.
- **User-level** `~/.spec/config.yaml` — personal identity (`user` block). Never committed.

```yaml
# spec.config.yaml (specs repo root) — example for an Atlassian + GitHub + Teams stack
# NOTE: The `user` block belongs in ~/.spec/config.yaml, not here.

version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 7"

specs_repo:
  provider: github                   # github | gitlab | bitbucket
  owner: my-org
  repo: specs                        # dedicated spec repository
  branch: main
  token: ${GITHUB_TOKEN}

integrations:
  comms:
    provider: teams                    # teams | slack | discord | custom
    webhook_url: ${TEAMS_WEBHOOK_URL}

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
    token: ${GITHUB_TOKEN}

  agent:
    provider: claude-code              # claude-code | cursor | copilot | custom

  design:
    provider: figma                    # figma | none
    token: ${FIGMA_TOKEN}

sync:
  outbound_on_advance: true            # auto-push to docs provider on stage transition
  conflict_strategy: warn              # warn | repo-wins | remote-wins

archive:
  on: done                             # done | manual
  directory: archive                   # directory within specs repo for completed specs

pipeline:
  stages:
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
```

```yaml
# ~/.spec/config.yaml (user-level) — personal identity, never committed

user:
  owner_role: engineer               # pm | tl | designer | qa | engineer | custom
  name: "Jane Smith"                 # used in notification attribution
  handle: ${COMMS_HANDLE}           # e.g. @jane on Slack, jane@org.com on Teams
```

### 4.7 `SPEC.md` Template Structure

The canonical template scaffolded by `spec new` contains the following sections. Sections are tagged with their responsible role via `<!-- owner: role -->` comments — these markers are required, used by `spec validate` for gate checks, and by `spec sync` for section-scoped bidirectional sync. The template is stored in the specs repo and versioned via git like everything else.

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
created: [date]
updated: [date]
---

# SPEC-<id> — [Feature/Enhancement Title]

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
## 10. Post-Merge Notes           <!-- owner: engineer -->
```

The `repos` frontmatter field lists the service repos this spec touches (e.g., `repos: [auth-service, api-gateway, frontend]`). This is populated by engineers during technical planning and used by `spec review` to aggregate PRs across repos.

### 4.8 Core Commands

| Command | Description |
|---|---|
| `spec new [--title "..."]` | Scaffold a new `SPEC.md` in the specs repo with an auto-assigned ID (next sequential number), create linked PM epic, post draft notification |
| `spec list [--role <role>] [--all]` | List specs awaiting action by the current user's role; `--role` overrides configured role; `--all` shows full pipeline regardless of role |
| `spec status <id>` | Show a spec's current pipeline position, section completion, sync state, and reversion history |
| `spec whoami` | Display the currently resolved user identity (role, name, handle) and the config file it was sourced from |
| `spec edit <id>` | Open the spec in `$EDITOR` locally, or print the docs provider URL if configured (for PMs/designers to open in Confluence/Notion) |
| `spec pull <id>` | Fetch the latest spec from the specs repo and write to the current service repo at `.spec/<id>.md` |
| `spec sync <id> [--direction in\|out\|both]` | Bidirectional section-scoped sync between the specs repo and external tools (Confluence, Notion); also syncs local service repo changes back to specs repo scoped to the user's role; defaults to `both` |
| `spec link <id> --section <section> --url <url>` | Attach a resource link (Figma, document, etc.) to a spec section |
| `spec decide <id> --question "..."` | Append a new question to the spec's decision log with auto-incremented number, user identity, and date |
| `spec decide <id> --resolve <number> --decision "..." --rationale "..."` | Resolve an existing decision log entry with the decision and rationale |
| `spec advance <id>` | Advance to the next stage in the pipeline; validates gate conditions, transitions status, notifies next owner |
| `spec revert <id> --to <stage> --reason "..."` | Send a spec back to a previous stage; logs reason to decision log, notifies both current and target stage owners; only the current stage owner can revert |
| `spec build <id>` | Provide the spec file as context to the configured agent provider and begin build phase |
| `spec eject <id> --reason "..."` | Log blocker to escape hatch log, transition to `blocked`, notify TL |
| `spec resume <id>` | Transition a `blocked` spec back to its pre-block stage, notify the stage owner |
| `spec review <id>` | Post a review request to comms with all stacked PRs (across all `repos`) linked in dependency order |
| `spec validate <id>` | Dry-run all gate checks for the current stage without advancing |
| `spec search "<query>"` | Search active and archived specs in the specs repo |
| `spec history [--limit N]` | List recent completed (archived) specs with summaries |
| `spec config init` | Interactive wizard to generate `spec.config.yaml` for the specs repo |
| `spec config init --user` | Interactive wizard to generate `~/.spec/config.yaml` for personal identity |
| `spec config test` | Validate all configured integrations and surface auth issues |

---

## 5. Design Inputs <!-- owner: designer -->

- [ ] CLI output formatting — should feel like a first-class developer tool, not a script. Consider colour coding for stage transitions, success/error states, and a clear visual representation of the pipeline position.
- [ ] Notification message templates — each comms notification should be skimmable in <5 seconds. Standardise format: `[SPEC-042] Stage → tl-review | Owner: @alice | Link: ...`
- [ ] `spec status <id>` output — render an ASCII pipeline diagram showing current position, section completion per owner, sync freshness, and reversion history.
- [ ] `spec list` output design — should feel like a focused inbox, not a raw data dump. Consider a table with colour-coded stage badges, a muted "nothing to do" state, and a subtle urgency indicator for specs that have been in-stage beyond a configurable threshold (e.g. 48hrs). The `--all` view should visually group by stage to make the pipeline readable at a glance.
- [ ] `spec sync` output — show a diff summary of what changed per section, which direction, and any conflicts that need resolution.
- [ ] `spec decide` output — confirm the entry was added/updated and show the current decision log state.

---

## 6. Acceptance Criteria <!-- owner: qa -->

### US-01 — New spec scaffolding
- [ ] `spec new --title "Auth refactor"` auto-assigns the next sequential ID by scanning existing specs in the repo (active + archived)
- [ ] The new spec is created as `SPEC-<id>.md` in the specs repo root with all required sections
- [ ] YAML frontmatter is pre-populated with `status: draft`, the auto-assigned ID, current date, and author from git config
- [ ] If PM integration is configured, an Epic is created and its key is written into the `epic_key` frontmatter field
- [ ] Decision log table is present and empty
- [ ] A draft notification is sent to the configured comms channel

### US-06 — Spec pull
- [ ] `spec pull SPEC-042` fetches the spec from the specs repo and writes to `.spec/SPEC-042.md` in the current service repo
- [ ] If a local copy exists and has uncommitted changes, user is prompted before overwrite
- [ ] If the spec does not exist in the specs repo, a clear error is returned

### US-07 — Agent build
- [ ] `spec build SPEC-042` validates the spec is at `build` stage before proceeding
- [ ] The spec file is provided as context to the agent provider with a system prompt defining how to reference the spec and what conventions to follow
- [ ] A build-start notification is sent to the comms channel

### US-08 — Escape hatch
- [ ] `spec eject SPEC-042 --reason "..."` appends an entry to the Escape Hatch Log section
- [ ] Spec status transitions to `blocked`
- [ ] TL receives a notification with the reason and a link to the spec

### US-12 — Stack flexibility
- [ ] Switching `integrations.comms.provider` from `teams` to `slack` requires only a config change, no code change
- [ ] `spec config test` reports a clear pass/fail for each configured integration
- [ ] Adapter interface is documented such that a custom integration can be built by a third party

### US-13 — User role declaration
- [ ] `user.owner_role` set in `~/.spec/config.yaml` is respected by all role-aware commands without requiring a flag
- [ ] `spec whoami` outputs the resolved role, name, handle, and the config file path it was sourced from
- [ ] If `owner_role` is not set anywhere, running any role-dependent command prints a clear prompt: `No role configured. Run 'spec config init --user' to set up your identity.`
- [ ] `--role <role>` flag on any command temporarily overrides the configured role for that invocation only
- [ ] `user` block is excluded from the repo-level `spec.config.yaml` by `spec config init` to prevent personal identity being committed to source control

### US-14 — `spec list` role-filtered queue
- [ ] `spec list` with no flags returns only specs where the active stage `owner_role` matches the user's configured role
- [ ] Each row shows: spec ID, title, current stage, time-in-stage, and a direct link to the spec
- [ ] Results are sorted by time-in-stage descending (longest waiting first) to surface stale items
- [ ] Specs in `blocked` state are visually distinguished (e.g. flagged with a warning indicator) even if technically owned by the user's role
- [ ] `spec list --all` shows all specs across all roles and stages, with role ownership clearly labelled per row
- [ ] `spec list --role qa` allows a TL or PM to view the queue from another role's perspective without changing their own configured role
- [ ] If the user's role has no pending specs, output is: `✓ Nothing awaiting your action. Run 'spec list --all' to see the full pipeline.`
- [ ] `spec list` reads from the specs repo with enrichment from PM/repo integrations per D-005

### US-15 — Bidirectional sync
- [ ] `spec sync SPEC-042` pulls inbound changes from the docs provider, scoped to sections matching the remote editor's role
- [ ] `spec sync SPEC-042` pushes the full spec outbound to the docs provider
- [ ] When run from a service repo, `spec sync SPEC-042` also writes local `.spec/` changes back to the specs repo, scoped to the current user's role
- [ ] Inbound changes to a section cannot overwrite content owned by a different role (e.g., a PM's Confluence edit cannot modify §7 Technical Implementation)
- [ ] If both the specs repo and the remote have changes to the same section since last sync, `spec sync` warns and requires `--force` or `--skip`
- [ ] Outbound sync is optionally triggered automatically on `spec advance` (controlled by `sync.outbound_on_advance` config)

### US-16 — Design resource linking
- [ ] `spec link SPEC-042 --section design --url "https://figma.com/..."` appends a resource link to the Design Inputs section
- [ ] The link includes a timestamp and the user's identity from `spec whoami`
- [ ] Links are rendered as a list in the target section, not inline replacements

### US-17 — Cross-repo specs
- [ ] A spec in the specs repo can declare `repos: [auth-service, api-gateway]` in frontmatter
- [ ] `spec pull SPEC-042` works from any service repo, regardless of whether it is listed in `repos`
- [ ] `spec review SPEC-042` aggregates PRs from all repos listed in the `repos` field

### US-18 — Stage reversion
- [ ] `spec revert SPEC-042 --to build --reason "..."` transitions the spec from its current stage to the specified previous stage
- [ ] The `--reason` flag is required; omitting it produces an error
- [ ] The reason is appended to the decision log as a reversion entry
- [ ] Both the current stage owner and the target stage owner are notified via comms
- [ ] Only the current stage owner (matched via `spec whoami`) can run `spec revert`; other roles receive a permission error
- [ ] Reverting to a stage that is *ahead* of the current stage is rejected (use `spec advance` for forward transitions)
- [ ] The `revert_count` field in frontmatter is incremented on each reversion
- [ ] `spec status <id>` shows the reversion history (from → to, reason, date) alongside the current pipeline position

### US-19 — Decision log management
- [ ] `spec decide SPEC-042 --question "REST vs gRPC?"` appends a new row to the decision log with an auto-incremented number, the user's identity, and the current date
- [ ] The new entry has empty `Options Considered`, `Decision Made`, and `Rationale` fields ready to be filled
- [ ] `spec decide SPEC-042 --resolve 003 --decision "gRPC" --rationale "Lower latency, schema enforcement"` updates entry 003 with the decision, rationale, user identity, and date
- [ ] Attempting to resolve a non-existent entry number produces a clear error
- [ ] `spec decide SPEC-042 --list` displays the current decision log in a readable table format

---

## 7. Technical Implementation <!-- owner: engineer -->

### 7.1 Language & Runtime

- [ ] *To be decided — options: Node.js (wide ecosystem, easy npm distribution), Python (familiar to many engineers, strong CLI tooling with Typer/Click), Go (single binary distribution, fast startup)*
- Consider distribution mechanism early: npm package, Homebrew tap, pip package, or single binary releases on GitHub.

### 7.2 Architecture Notes

- **Adapter pattern** is the core architectural requirement. Each integration (comms, pm, docs, repo, agent, design) must be implemented behind a common interface so new providers can be added without modifying core logic.
- **Local-first** — the CLI must function fully offline for spec editing. Network calls only happen at explicit command invocations (`spec pull`, `spec sync`, `spec build`, etc.).
- **No daemon** — `spec` should not require a running background process. It is invoked explicitly.
- **Config resolution** — look for `spec.config.yaml` in the current directory, then walk up to repo root, then check the specs repo clone, then fall back to `~/.spec/config.yaml` for user-level defaults.
- **Role resolution** — `user.owner_role` should be defined in `~/.spec/config.yaml` (user-level) so it is personal to the individual and not committed to the repo. If unset, the tool must prompt the user to run `spec config init --user` before any role-dependent command (`spec list`, `spec advance`, etc.) can proceed. Roles should be a defined enum in the tool (`pm`, `tl`, `designer`, `qa`, `engineer`) with a `custom` escape for non-standard org structures.
- **Frontmatter as data model** — `spec` reads and writes YAML frontmatter in `SPEC.md` files as the authoritative source of spec metadata (`status`, `version`, `updated`, `epic_key`, `repos`, `revert_count`, etc.). All status transitions update the frontmatter directly.
- **Specs repo as canonical store** — all specs live in a dedicated git repo (per D-003/D-006). `spec` clones or fetches this repo locally and operates on it. Service repos get read-only copies via `spec pull`.
- **Section-scoped sync** — the `<!-- owner: role -->` HTML comments are not decorative; they are the mechanism by which `spec sync` determines which sections can be updated from which external source. The sync engine parses markdown headings, matches them to owner markers, and merges only the sections whose owner matches the inbound source's role.
- **Gate slugification** — gate conditions in the pipeline config reference sections by slug (e.g., `section_complete: problem_statement`). Slugs are derived by convention: lowercase the heading text, strip the section number and punctuation, replace spaces with underscores. `## 1. Problem Statement` → `problem_statement`, `## 5. Design Inputs` → `design_inputs`. This mapping is stable as long as section headings in the template don't change.
- **Agent context injection** — `spec build` provides the spec file as context to the configured agent provider. Each agent adapter is responsible for delivering the spec in the way its tool expects (e.g., Claude Code: prepend to system prompt or write to `CLAUDE.md`; Cursor: write to `.cursor/rules`; Copilot: write to workspace context file). The adapter also injects a standard system prompt instructing the agent to reference the spec and follow its conventions.

### 7.3 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Integration APIs change (Jira, Confluence, etc.) | Medium | Medium | Adapter version-pinning; integration test suite against API mocks |
| Teams webhook format differs from Slack | High | Low | Separate comms adapters with shared message schema |
| Engineers skip `spec build` and prompt agents directly | High | High | Make `spec build` obviously more convenient than manual prompting; good DX is the mitigation |
| Spec config complexity overwhelms initial setup | Medium | High | `spec config init` wizard; sensible defaults; `--provider none` escape for any integration |
| User commits personal identity (`user` block) to shared repo config | Medium | Medium | `spec config init` writes `user` block only to `~/.spec/config.yaml`; repo-level template explicitly excludes the `user` key; `.gitignore` guidance in docs |
| Section-scoped sync produces unexpected merges | Medium | High | Conservative default: `conflict_strategy: warn`; `spec sync --dry-run` for preview; clear diff output showing what will change per section |
| Specs repo becomes a bottleneck with many concurrent editors | Low | Medium | Specs are separate files — git handles concurrent edits to different files well; same-file conflicts handled by section scoping |
| `spec pull` copies go stale in service repos | Medium | Medium | `spec pull` shows last-updated timestamp; `spec build` warns if local copy is older than specs repo version |

### 7.4 PR Stack Plan

*(To be defined by engineer during build phase using `spec pull` context)*

---

## 8. Escape Hatch Log <!-- auto: spec eject -->

*No escapes logged.*

---

## 9. QA Validation Notes <!-- owner: qa -->

---

## 10. Post-Merge Notes <!-- owner: engineer -->

---

*Generated with `spec new` · S.P.E.C methodology · aaronlewis.blog/posts/spec*
