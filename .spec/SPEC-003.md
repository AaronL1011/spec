---
id: SPEC-003
title: Dogfood spec-cli MCP integration
status: build
version: 0.3.0
author: Aaron
cycle: Cycle 0
epic_key: ""
repos: [spec-cli]
revert_count: 0
source: direct
created: 2026-04-22
updated: 2026-04-22
---

# SPEC-003 - Dogfood spec-cli MCP integration

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Should the MCP server require an active build session or work at any pipeline stage? | (1) Any stage, (2) Build-only, (3) Build-only with read-only mode for other stages | **(2) Build-only** | The MCP server's tools (`spec_step_complete`, `spec_decide`, etc.) are build-phase actions; exposing them outside build creates ambiguity about who owns the spec at that stage; read-only context is already available via `spec pull` | @aaron | 2026-04-22 |
| 002 | Should we bypass GitHub for local-only dogfooding? | (1) Require full GitHub round-trip, (2) Allow local-only with direct file edits, (3) Add an offline mode | **(2) Allow local-only** | Dogfooding should have minimal friction; the spec file is already cloned locally; `spec build` already checks `.spec/` first; we can push to remote later | @aaron | 2026-04-22 |
| 003 | What should the first dogfood build implement? | (1) A new feature, (2) Fix the silent MCP server failure, (3) Both as a multi-step PR stack | **(3) Multi-step stack** | The silent exit-code-1 bug is a real UX issue discovered during dogfooding — fixing it first validates the MCP workflow, then we add a small feature to exercise the full step-advance cycle | @aaron | 2026-04-22 |
| 004 | Should mcp-server duplicate the stderr error or rely on main.go? Decision: rely on main.go — it already prints 'error: ...' to stderr for all commands. The fix adds context-rich error messages (with next-action hints) to the errors returned by runMCPServer, which main.go then surfaces. | | **Rely on main.go for stderr output** | main.go already handles all command errors uniformly; duplicating stderr writes creates noise for MCP clients | agent | 2026-04-22 |
| 005 | MCP resources serve stale context after spec_decide writes to disk — should resources re-read the file on each request? Decision: yes, at minimum the decision log and full spec resources should re-read from disk to reflect tool-side mutations. Filed as a follow-up improvement. | | | | agent | 2026-04-22 |

## 1. Problem Statement           <!-- owner: pm -->

The `spec mcp-server` command silently exits with code 1 when no build session exists, giving MCP clients (like pi) an opaque "Connection closed" error. Additionally, the MCP integration has never been exercised end-to-end by an actual developer workflow — there are no dogfooding results to validate the tool-calling contract, context assembly, or step-advance cycle.

Without dogfooding, we don't know whether:
- The MCP resource URIs return useful context for an agent
- The `spec_step_complete` / `spec_decide` tools work correctly in a real session
- The error messages are actionable when things go wrong
- The build→MCP→agent loop actually improves developer experience

## 2. Goals & Non-Goals           <!-- owner: pm -->

### Goals
- **G1**: Fix the silent failure in `spec mcp-server` so it reports actionable errors to stderr
- **G2**: Successfully complete a multi-step build using the MCP integration with pi as the agent
- **G3**: Validate that MCP resources (spec content, decisions, acceptance criteria) are correctly served
- **G4**: Validate that MCP tools (`spec_decide`, `spec_step_complete`, `spec_status`) work in a live session
- **G5**: Document any friction, bugs, or missing features discovered during dogfooding

### Non-Goals
- Implementing new MCP tools beyond what already exists
- Supporting transports other than stdio
- Automated testing of the MCP protocol (covered separately)
- Making `spec advance` work without GitHub (out of scope — we work around it for now)

## 3. User Stories                <!-- owner: pm -->

| ID | Role | Story | Acceptance |
|---|---|---|---|
| US-D1 | Engineer | When `spec mcp-server` fails, I see a clear error on stderr explaining what's wrong and what to do | Error message includes the missing prerequisite and the command to fix it |
| US-D2 | Engineer | I can start a build session and have pi connect to the MCP server to read my spec | Pi's MCP tools list shows `spec_decide`, `spec_step_complete`, `spec_status`, `spec_search` |
| US-D3 | Engineer | I can ask pi to check build status and it calls `spec_status` | Response shows current step, total steps, and step descriptions |
| US-D4 | Engineer | I can ask pi to record a decision and it calls `spec_decide` | Decision appears in the spec's Decision Log section |
| US-D5 | Engineer | When a step is done, pi calls `spec_step_complete` and the session advances | Session moves to next step; `spec_status` reflects the change |

## 4. Proposed Solution           <!-- owner: pm -->

### 4.1 Concept Overview

Two-part fix-then-validate approach:

1. **Fix**: Make `spec mcp-server` write structured errors to stderr before exiting, so MCP clients can surface them. The cobra command already has `SilenceErrors: true`, which suppresses the default error printing — we need to explicitly write to stderr in the `RunE` function.

2. **Validate**: Use SPEC-003 itself as the dogfood spec. Populate all sections, start a build session, configure `.mcp.json`, and exercise every MCP tool through pi during the build.

### 4.2 Architecture / Approach

No architectural changes. The fix is a ~5-line change in `cmd/mcp_server.go` to write errors to stderr. The validation is a manual end-to-end exercise of the existing build→MCP pipeline.

## 5. Design Inputs               <!-- owner: designer -->

N/A — CLI-only change, no visual design needed. Error messages should follow the existing `spec` convention: describe what's wrong, then tell the user what to do.

## 6. Acceptance Criteria         <!-- owner: qa -->

- [ ] `spec mcp-server` with no session prints an actionable error to stderr and exits with code 1
- [ ] `spec mcp-server` with a valid session starts and serves the MCP protocol on stdio
- [ ] `spec_status` tool returns current step and step list
- [ ] `spec_decide` tool appends a decision to the spec's Decision Log
- [ ] `spec_decide_resolve` tool resolves an existing decision
- [ ] `spec_step_complete` tool advances the session to the next step
- [ ] `spec_search` tool returns keyword matches from the spec
- [ ] pi can connect to the MCP server via `.mcp.json` configuration
- [ ] All existing tests pass after the error-handling fix

## 7. Technical Implementation    <!-- owner: engineer -->

### 7.1 Architecture Notes

The change is isolated to `cmd/mcp_server.go`. The `RunE` function returns errors that cobra suppresses due to `SilenceErrors: true`. We add an explicit `fmt.Fprintf(os.Stderr, ...)` before returning the error so MCP clients see it.

No changes to `internal/build/mcp.go` or the MCP protocol layer.

### 7.2 Dependencies & Risks

- **Risk**: The local specs repo clone may drift from remote if we edit files directly. Mitigation: push with `spec push` once `GITHUB_TOKEN` is set.
- **Dependency**: pi must support `.mcp.json` server configuration (confirmed working).

### 7.3 PR Stack Plan

1. [spec-cli] Fix silent mcp-server failure — add stderr error output when session resolution fails
2. [spec-cli] Dogfood validation — exercise all MCP tools via pi and fix discovered issues

## 8. Escape Hatch Log            <!-- auto: spec eject -->

## 9. QA Validation Notes         <!-- owner: qa -->

## 10. Deployment Notes           <!-- owner: engineer -->

Standard `go build` and `go install`. No infrastructure changes. The fix ships with the next `spec` binary build.

## 11. Retrospective              <!-- auto: spec retro -->
