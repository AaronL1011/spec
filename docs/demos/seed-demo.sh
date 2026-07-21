#!/usr/bin/env bash
# Seed an isolated HOME for docs/demos/demo.tape.
#
# The tape sets HOME to /tmp/spec-vhs-demo, so this never touches the
# recorder's real ~/.spec configuration or specs repositories.
set -euo pipefail

DEMO_HOME="/tmp/spec-vhs-demo"
DEMO_ROOT="$DEMO_HOME/.spec/repos/aaronl1011/spec-demo"
SPECS_DIR="$DEMO_ROOT/specs"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SPEC_BIN="${SPEC_BIN:-$PROJECT_ROOT/bin/spec}"

if [[ ! -x "$SPEC_BIN" ]]; then
  printf 'spec binary not found at %s — run make build first\n' "$SPEC_BIN" >&2
  exit 1
fi

rm -rf "$DEMO_HOME"
mkdir -p "$SPECS_DIR/triage"

cat >"$DEMO_HOME/.spec/config.yaml" <<'YAML'
user:
  owner_role: engineer
  name: Ada Lovelace
  handle: ada
preferences:
  editor: vi
  theme: ayu-mirage
  refresh_interval: 30s
  mouse: false
  ai_drafts: true
YAML

# Deliberately omit specs_repo. config.Resolve discovers this managed clone by
# directory, while the TUI's remote-sync hook becomes a local no-op. The demo
# is therefore repeatable without a provider token or network connection.
cat >"$DEMO_ROOT/spec.config.yaml" <<'YAML'
version: "1"

team:
  name: "Platform Team"
  cycle_label: "Cycle 14"

pipeline:
  preset: minimal
  # Keep the preset name for intent, and materialise its stages because the TUI
  # consumes pipeline.stages directly rather than expanding presets itself.
  stages:
    - name: triage
      owner: anyone
      icon: "📥"
    - name: draft
      owner: author
      icon: "📝"
    - name: build
      owner: engineer
      icon: "🏗️"
    - name: review
      owner: engineer
      icon: "👁️"
    - name: done
      owner: author
      icon: "🎉"
YAML

cat >"$SPECS_DIR/SPEC-042.md" <<'MARKDOWN'
---
id: SPEC-042
title: Rate-limit middleware for the public API
status: build
version: 0.3.0
author: ada
cycle: Cycle 14
epic_key: API-482
repos: [api-gateway]
revert_count: 0
source: customer-traffic-review
created: 2026-06-26
updated: 2026-07-11
stage_entered_at: 2026-07-05T09:00:00Z
assignees: [ada]
---

# SPEC-042 - Rate-limit middleware for the public API

## TL;DR                             <!-- owner: anyone -->

Add a tenant-aware rate limiter to the public API so healthy traffic stays fast
when one integration bursts. The first release is observable, reversible, and
defaults to a safe allow path if its backing store is unavailable.

## 1. Problem Statement           <!-- owner: pm -->

A small number of tenants can create sharp request bursts that consume shared
capacity and turn a local incident into a platform-wide latency problem. Today
we can see the burst only after it has affected other customers.

During INC-1842, one tenant's export integration peaked at 4,800 requests per
second for 11 minutes. Gateway p99 rose from 180ms to 1.9s, and 7% of unrelated
requests timed out before on-call isolated the source.

## 2. Goals & Non-Goals           <!-- owner: pm -->

**Goals**

- Keep well-behaved tenant traffic responsive during bursts.
- Give support a clear, actionable response when a request is limited.
- Make the rollout measurable and safe to reverse.

**Non-goals**

- Billing or quota enforcement.
- A global hard cap across every API surface in the first release.

## 3. User Stories                <!-- owner: pm -->

- As an API consumer, I receive a predictable retry signal instead of a timeout.
- As on-call, I can identify the tenant and endpoint driving a burst.
- As an engineer, I can tune a limit without redeploying the gateway.

## 4. Proposed Solution           <!-- owner: pm -->

Use a small middleware layer with per-tenant policies, a clear response
contract, and metrics that make every decision inspectable.

### 4.1 Concept Overview

Each request resolves its tenant from authenticated context, evaluates the
endpoint policy, and either continues normally or returns a standard 429 with a
retry hint. Limits ship in observe-only mode before enforcement is enabled.

### 4.2 Architecture / Approach

Each bucket is keyed by tenant and policy, not API key. This prevents a burst on
a high-volume export endpoint from consuming the tenant's allowance for ordinary
API calls, while API-key rotation leaves policy state intact.

Redis holds the shared bucket state across gateway instances. On a Redis
timeout, each gateway uses a best-effort local emergency bucket—seeded from the
last Redis response and validated policy—for at most five seconds. After that
window, enforcement fails open and raises a degraded-enforcement alert.

Policies are versioned configuration: one default policy plus explicit endpoint
overrides. The middleware emits a decision metric before returning so dashboards
can separate deliberate limits from downstream failures.

## 5. Design Inputs               <!-- owner: designer -->

The 429 response follows the existing API error envelope and includes a human
message, machine-readable reason, and `Retry-After` header.

## 6. Acceptance Criteria         <!-- owner: qa -->

At sustained 10k rps, limiter-added p99 latency must remain below 20ms with
Redis enabled. Results for the five-second local emergency bucket are reported
as a separate fallback scenario.

- A tenant over its configured burst receives HTTP 429 and `Retry-After`.
- A healthy tenant remains within its existing latency SLO during a neighbour's burst.
- Observe-only mode emits the same decision metrics without rejecting requests.
- Load tests exercise both the Redis-backed path and local emergency bucket.
- A Redis outage uses the emergency bucket for at most five seconds, then fails open and raises an alert.

## 7. Technical Implementation    <!-- owner: engineer -->

The middleware runs after authentication and route matching, but before the
handler. That position provides tenant and endpoint policy context while keeping
limited requests out of business logic. It records tenant, policy, decision,
and remaining tokens as structured metrics.

### 7.1 Architecture Notes

Keep policy parsing outside the request path. The request path only reads an
already-validated snapshot so config updates cannot add latency to live traffic.

### 7.2 Dependencies & Risks

Redis availability is the main operational dependency. The five-second local
emergency bucket limits brief failures but is intentionally less consistent
across gateways; longer failures fail open and page on-call.

### 7.3 PR Stack Plan

1. [api-gateway:go] Add policy parsing and decision metrics
2. [api-gateway:go] Add token-bucket middleware (after: 1)
3. [api-gateway:go] Add load and fail-open integration tests (after: 2)

## 8. Escape Hatch Log            <!-- auto: spec eject -->

No ejections recorded.

## 9. QA Validation Notes         <!-- owner: qa -->

The staging matrix covers steady state, a single noisy tenant, Redis latency,
Redis unavailability, observe-only mode, and rollback to the previous policy.

## 10. Deployment Notes           <!-- owner: engineer -->

Run observe-only for 24 hours, then enforce for two internal tenants. Expand to
10% of external tenants only after decision volume, false-positive rate, and
fallback alerts remain within the agreed thresholds.

## 11. Retrospective              <!-- auto: spec retro -->

Capture tuning decisions after the first production week.

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Redis failure behaviour | fail-open, fail-closed, bounded local fallback | local emergency bucket for 5s, then fail-open | Absorb brief outages without turning Redis into an API availability dependency | Ada | 2026-07-07 |
| 002 | Bucket identity | tenant, API key, tenant + policy | tenant + policy | Prevent one endpoint from starving unrelated traffic and survive key rotation | Sam | 2026-07-09 |
MARKDOWN

cat >"$SPECS_DIR/SPEC-042.threads.yaml" <<'YAML'
threads:
  - id: T-1a2b
    section: architecture_approach
    status: open
    author: sam
    created: 2026-07-09T14:20:00Z
    question: Why split buckets by policy instead of keeping one shared allowance per tenant? @ada
    mentions:
      - ada
    quote: Each bucket is keyed by tenant and policy, not API key. This prevents a burst on a high-volume export endpoint from consuming the tenant's allowance for ordinary API calls, while API-key rotation leaves policy state intact.
  - id: T-3c4d
    section: acceptance_criteria
    status: open
    author: qa
    created: 2026-07-10T08:05:00Z
    question: Does the 20ms budget include the Redis round trip and decision-metric emission, or only bucket evaluation? @ada
    mentions:
      - ada
    quote: At sustained 10k rps, limiter-added p99 latency must remain below 20ms with Redis enabled. Results for the five-second local emergency bucket are reported as a separate fallback scenario.
  - id: T-5e6f
    section: technical_implementation
    status: open
    author: ada
    created: 2026-07-10T11:30:00Z
    question: Is authenticated tenant context guaranteed here for service-token traffic as well as user sessions? @sam
    mentions:
      - sam
    quote: The middleware runs after authentication and route matching, but before the handler. That position provides tenant and endpoint policy context while keeping limited requests out of business logic. It records tenant, policy, decision, and remaining tokens as structured metrics.
  - id: T-7g8h
    section: problem_statement
    status: resolved
    author: sam
    created: 2026-07-08T09:15:00Z
    question: Can we quantify the current noisy-neighbour impact before we set a latency target?
    replies:
      - author: ada
        at: 2026-07-08T10:04:00Z
        body: Added INC-1842 traffic, p99, duration, and unrelated timeout figures from the edge dashboard.
    resolved_by: sam
    resolved_at: 2026-07-08T10:10:00Z
    quote: A small number of tenants can create sharp request bursts that consume shared capacity and turn a local incident into a platform-wide latency problem. Today we can see the burst only after it has affected other customers.
YAML

cat >"$SPECS_DIR/SPEC-041.md" <<'MARKDOWN'
---
id: SPEC-041
title: Audit log retention policy
status: review
version: 0.2.0
author: sam
cycle: Cycle 14
repos: [audit-service]
revert_count: 0
source: compliance-review
created: 2026-06-18
updated: 2026-07-10
stage_entered_at: 2026-07-08T13:00:00Z
assignees: [ada]
---

# SPEC-041 - Audit log retention policy

## TL;DR                             <!-- owner: anyone -->

Standardise retention for customer audit events: 90 days searchable, one year
in encrypted archive, then verified deletion with evidence available to Security.

## 1. Problem Statement           <!-- owner: pm -->

Audit events currently inherit storage defaults from each service. Three
customers received different answers to the same retention question during
renewal, and Security cannot prove when expired objects were actually removed.

## 2. Goals & Non-Goals           <!-- owner: pm -->

- Publish one default lifecycle for customer audit events.
- Produce immutable evidence for every deletion batch.
- Support contract-specific legal holds without silently extending all data.
- Do not redesign application logs or billing-event retention.

## 4. Proposed Solution           <!-- owner: pm -->

| Tier | Duration | Access | Storage |
|---|---:|---|---|
| Searchable | 0–90 days | Customer + Support | Primary audit store |
| Archive | 91–365 days | Security-approved restore | Encrypted object storage |
| Expired | After 365 days | None | Verified deletion |

A daily lifecycle job moves completed partitions between tiers. Legal holds are
explicit records with owner, reason, scope, and expiry; they pause deletion only
for matching tenant partitions.

## 6. Acceptance Criteria         <!-- owner: qa -->

- A sampled event is searchable through day 90 and absent from search on day 91.
- Archived partitions can be restored into an isolated review environment.
- Every deletion batch records partition IDs, object counts, bytes, and checksums.
- A legal hold preserves only the matching tenant and emits an expiry reminder.
- Security can export deletion evidence for a customer and date range.

## 7. Technical Implementation    <!-- owner: engineer -->

The lifecycle worker checkpoints each partition transition in the audit
service database before changing object-storage state. Deletion evidence is
written to the immutable compliance bucket under a seven-year retention lock.

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|
| 001 | Default retention | 90d, 365d, tiered | 90d searchable + 365d archive | Balances support access, enterprise expectations, and cost | Security | 2026-07-09 |
MARKDOWN

cat >"$SPECS_DIR/SPEC-043.md" <<'MARKDOWN'
---
id: SPEC-043
title: Webhook delivery retries
status: draft
version: 0.1.0
author: sam
cycle: Cycle 14
repos: [events-service]
revert_count: 0
source: support-escalation
created: 2026-07-03
updated: 2026-07-09
stage_entered_at: 2026-07-09T08:30:00Z
assignees: [sam]
---

# SPEC-043 - Webhook delivery retries

## TL;DR                             <!-- owner: anyone -->

Add bounded exponential retries and customer-visible delivery history so a
short endpoint outage does not permanently lose webhook events.

## 1. Problem Statement           <!-- owner: pm -->

Webhook delivery is currently single-attempt. During SUP-2917, a customer's
endpoint returned 503 for four minutes and 1,842 order events were never
received; Support reconstructed and replayed them manually from service logs.

## 2. Goals & Non-Goals           <!-- owner: pm -->

- Retry transient failures without duplicating successful deliveries.
- Let customers inspect attempts and replay a terminal failure.
- Bound queue growth and make poison endpoints visible to on-call.
- Do not guarantee ordering across independent webhook subscriptions.

## 4. Proposed Solution           <!-- owner: pm -->

Persist one delivery record per event and subscription. Retry `408`, `429`, and
`5xx` responses with exponential backoff plus jitter for 24 hours; treat other
`4xx` responses as terminal. Customer replay creates a new delivery generation
with the same event ID and a new attempt history.

## 6. Open Questions              <!-- owner: pm -->

- Should `Retry-After` override our calculated backoff for `429` responses?
- What per-subscription concurrency limit protects the shared worker pool?
- How long should attempt bodies remain visible in the customer dashboard?
MARKDOWN

cat >"$SPECS_DIR/SPEC-044.md" <<'MARKDOWN'
---
id: SPEC-044
title: EU data residency controls
status: blocked
version: 0.1.0
author: priya
cycle: Cycle 14
repos: [platform]
revert_count: 0
source: legal-review
created: 2026-07-02
updated: 2026-07-10
stage_entered_at: 2026-07-10T10:00:00Z
assignees: [priya]
blocked_from: draft
---

# SPEC-044 - EU data residency controls

## TL;DR                             <!-- owner: anyone -->

Keep EU customer content and derived search indexes in-region. Drafting is
blocked until Legal decides whether operational metadata may be processed in
the US control plane.

## 1. Problem Statement           <!-- owner: pm -->

The enterprise contract commits customer content to EU storage but does not
define the boundary for tenant identifiers, audit metadata, or support tooling.
Without that boundary, Engineering cannot choose between a regional data plane
and a fully isolated regional stack.

## 2. Decision Needed            <!-- owner: pm -->

Legal must classify these flows before architecture review:

| Data flow | Proposed treatment | Open issue |
|---|---|---|
| Customer payloads and attachments | EU only | None |
| Search indexes and embeddings | EU only | Confirm derived-data wording |
| Tenant ID, region, service health | Global control plane | Legal approval required |
| Break-glass support access | EU session with audit trail | Retention and approver rules |

## 3. Unblocked Work              <!-- owner: engineer -->

Inventory existing cross-region flows, add region labels to storage resources,
and measure the latency impact of EU-local search can proceed while the policy
boundary is under review.

## 8. Escape Hatch Log            <!-- auto: spec eject -->

2026-07-10 — blocked in draft pending Legal decision on operational metadata and
break-glass support access. Owner: Priya. Review scheduled for 2026-07-15.
MARKDOWN

cat >"$DEMO_HOME/.spec/demo-banner.sh" <<'BASH'
#!/usr/bin/env bash
clear
printf '\n\n'
printf '  \033[38;2;255;204;102m✦\033[0m  \033[1;38;2;115;218;202mspec\033[0m\n'
printf '     \033[38;2;204;204;204myour terminal is your office\033[0m\n\n'
printf '     \033[38;2;112;192;177mplan\033[0m  ·  \033[38;2;255;179;71mreview\033[0m  ·  \033[38;2;211;130;170mship\033[0m\n\n'
printf '  \033[38;2;112;112;112m──────────────────────────────────────────────────\033[0m\n'
BASH
chmod +x "$DEMO_HOME/.spec/demo-banner.sh"

cat >"$DEMO_HOME/.spec/demo-spec.sh" <<BASH
#!/usr/bin/env bash
cd /tmp
exec "$SPEC_BIN" "\$@"
BASH
chmod +x "$DEMO_HOME/.spec/demo-spec.sh"

# Give the local clone the shape of a real specs repository. No remote is
# configured: every action in the tape is either a local read or a local thread
# sidecar update, so the render has no network dependency.
git init --initial-branch=main "$DEMO_ROOT" >/dev/null
git -C "$DEMO_ROOT" config user.name "spec demo"
git -C "$DEMO_ROOT" config user.email "demo@example.invalid"
git -C "$DEMO_ROOT" add spec.config.yaml specs
git -C "$DEMO_ROOT" commit -m "docs: seed demo specs" >/dev/null

printf 'Seeded isolated demo HOME: %s\n' "$DEMO_HOME"
printf 'Render with: vhs docs/demos/demo.tape\n'
