---
id: SPEC-<id>
title: [Feature/Enhancement Title]
status: draft
version: 0.1.0
author: —
cycle: TBD
epic_key:
repos: []   # repositories touched by §7.3 nodes; each must be mapped in ~/.spec/config.yaml workspaces:
revert_count: 0
created: YYYY-MM-DD
updated: YYYY-MM-DD
---

# SPEC-<id> — [Feature/Enhancement Title]

> *One-line summary of what this spec covers.*

---

## Overview                          <!-- owner: pm -->

### What

*What is being built or changed?*

### Why

*Why is this needed? What problem does it solve?*

### How

*At a high level, how will this work?*

---

## 1. Problem Statement

*What problem are we solving? Who is affected and how?*

---

## 2. Goals & Non-Goals

### Goals

-

### Non-Goals

-

---

## 3. User Stories

| # | As a... | I want to... | So that... | Acceptance Criteria |
|---|---|---|---|---|

---

## 4. Proposed Solution

### 4.1 Concept Overview

### 4.2 Architecture / Approach

---

## 5. Design Inputs

---

## 6. Acceptance Criteria

---

## 7. Technical Implementation

### 7.1 Architecture Notes

### 7.2 Dependencies & Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|

### 7.3 PR Stack Plan

<!--
The build engine parses this section into a DAG and hands the whole graph to the
agent in one `spec build`. One line = one node:

    N. [repo:layer] Description (after: A, B)

  • N            unique, sequential node number (its id becomes nN)
  • [repo]       target repository — MUST appear in the `repos:` frontmatter and
                 be mapped to a local checkout in ~/.spec/config.yaml under
                 `workspaces:` (validated before the build starts)
  • [repo:layer] optional layer tag; routes skills (matches a skill's
                 applies_to.layers, e.g. rails-api, go-grpc, react-web, proto).
                 `[:layer]` with no repo is allowed.
  • (after: …)   dependency edges referencing earlier node numbers. Nodes with
                 no unmet dependency run in the same wave (in parallel, up to
                 build.max_parallel). Omit for a root node. Cycles / unknown
                 refs are rejected with an actionable error.

Draft-PR URLs are appended automatically as `<!-- pr: … -->` when the finisher
opens PRs — do not hand-author them. The pr-review gate passes only once every
LEAF node (one nothing else depends on) carries a recorded draft-PR URL.

Example (n1 is the root; n2 and n3 fan out from it; n4 merges both):
-->

1. [auth-service:rails-api] Add token-bucket rate limiter
2. [auth-service:rails-api] Integrate Redis backend (after: 1)
3. [api-gateway:go-grpc] Add rate-limit middleware (after: 1)
4. [frontend:react-web] Add rate-limit error handling (after: 2, 3)

---

## 8. Escape Hatch Log

*No escapes logged.*

---

## Decision Log

> *Record all significant decisions, questions and changes here for asynchronous reference.*

| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|