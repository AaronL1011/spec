---
id: <% id %>
title: <% title %>
status: draft
version: 0.1.0
author: <% author %>
cycle: <% cycle %>
epic_key: ""
repos: []
revert_count: 0
source: <% source %>
created: <% date %>
updated: <% date %>
---

# <% id %> - <% title %>

## TL;DR                             <!-- owner: anyone -->

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
<!--
Parsed into a DAG and executed by 'spec build'. One line = one node:
    N. [repo:layer] Description (after: A, B)
  - [repo] must be listed in 'repos:' above and mapped in ~/.spec/config.yaml
    under workspaces: (validated before the build starts).
  - :layer is optional and routes skills (e.g. rails-api, go-grpc, react-web).
  - (after: ...) are dependency edges to earlier node numbers; nodes with no
    unmet dependency run in the same wave (in parallel). Omit for a root node.
Draft-PR URLs are appended automatically by the finisher (do not author them);
the pr-review gate passes only once every leaf node has one. Example:
    1. [auth-service:rails-api] Add token-bucket limiter
    2. [api-gateway:go-grpc] Add rate-limit middleware (after: 1)
-->

## 8. Escape Hatch Log            <!-- auto: spec eject -->

## 9. QA Validation Notes         <!-- owner: qa -->

## 10. Deployment Notes           <!-- owner: engineer -->

## 11. Retrospective              <!-- auto: spec retro -->

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
