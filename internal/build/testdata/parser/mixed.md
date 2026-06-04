### 7.3 PR Stack Plan
1. [shared-libs] Base types
2. [:rails-api] Layer-only node, no repo (after: 1)
3. [api-gateway] Plain node, no layer, no deps
4. [frontend:react-web] Layered with multiple deps (after: 1, 3)
