# Upstream Governance Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development with explicit file ownership and a whole-branch review. Checkboxes track delivered behavior.

**Goal:** Provide built-in administrator discovery, confirmed batch import and monitoring for upstream Sub2API/New API sites.

**Architecture:** A small independent governance package owns remote catalogs, encrypted sessions, snapshots, previews and monitoring. A local service adapter invokes existing account/group operations, while admin handlers and Vue pages expose explicit workflows.

**Tech Stack:** Go 1.27, PostgreSQL, Gin/Wire, Vue 3/TypeScript, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-26-upstream-governance-design.md`

## Global Constraints

- Discovery and alerts never apply local routing or price changes.
- Passwords are transient; session/key ciphertext never appears in API JSON or logs.
- Public HTTPS origins only, no redirects, fixed adapter paths, explicit proxy and bounded requests.
- Preview expiry 15 minutes; collection 5–1440 minutes (default 15); active checks opt-in, minimum 15 minutes (default 30).
- At most two concurrent remote operations; cross-process site locks; successful snapshots survive failed refreshes.
- Stable marker per site/group/transport, frozen previews and account fingerprints prevent duplicate or stale writes.
- No real upstream credentials or production deployment during tests.

## Task 1: Domain contracts and platform connectors

**Files:** `backend/internal/upstreamgovernance/types.go`, `connector*.go`, `connector*_test.go`.

**Interfaces:** `Connector.Login`, `Discover`, `EnsureKey`, `Probe`; session/group/catalog DTOs in `types.go`. HTTP clients are injected by `ClientFactory`.

- [x] Write fixture tests for Sub2API group override, successful/challenged login, key recovery, and New API bearer/session envelopes.
- [x] Run the tests and confirm missing connector behavior fails.
- [x] Implement fixed-path adapters and strict parsing, preserving unknown prices and explicit provenance.
- [x] Test no redirects, no raw error-body leaks, incomplete catalog rejection and malformed 200 probe failure.

```go
connector := NewConnector(factory)
catalog, err := connector.Discover(ctx, site, session)
// A per-user 0.8 rate overrides a group default 2.0; it must not become 1.6.
```

## Task 2: Durable governance store

**Files:** `backend/internal/upstreamgovernance/store*.go`, tests, `backend/migrations/247_upstream_governance.sql`.

**Interfaces:** `Store` in `types.go`, constructed by `NewSQLStore(*sql.DB)`.

- [x] Test optimistic site updates, complete-snapshot persistence, preview replay, unique binding keys and lock contention.
- [x] Implement parameterized SQL and bounded pagination/history; site locking uses a dedicated connection with reliable release/discard.
- [x] Add an accounts import-marker unique index and foreign-key cleanup semantics that never delete imported accounts.
- [x] Run SQL unit tests and integration tests where an isolated PostgreSQL is available.

```go
release, acquired, err := store.LockSite(ctx, siteID)
if err != nil || !acquired { return ErrBusy }
defer release()
```

## Task 3: Orchestration and local integration

**Files:** governance `service*.go`, `worker*.go`, their tests; `backend/internal/service/upstream_governance.go`; admin handler/routes/Wire providers.

**Interfaces:** connector/store from Tasks 1/2; `LocalAccounts` from `types.go` maps to existing AdminService operations.

- [x] Write failing state-transition, stale preview, duplicate application and partial recovery tests.
- [x] Implement encryption/durable-key gate, discovery diffs, server-held previews and confirmed import.
- [x] Implement stable local marker lookup, fingerprints, supported platform validation and normal cache invalidation.
- [x] Add bounded automatic discovery, opt-in probes, event acknowledgement and clean shutdown.
- [x] Wire admin routes and redact sensitive request fields from audit capture; generate Wire output and compile.

```go
preview, err := engine.Preview(ctx, siteID, selections)
// Apply accepts only the persisted preview ID, not a new unreviewed mapping payload.
result, err := engine.Apply(ctx, siteID, preview.ID)
```

## Task 4: Administrator workflow

**Files:** `frontend/src/api/admin/upstream-governance.ts`, `views/admin/upstream-governance/*`, navigation/router and bilingual locales, focused tests.

**Interfaces:** routes and JSON contract documented in `types.go` and the spec.

- [x] Write interaction tests for connection, catalog selection, preview confirmation, partial errors and stale preview feedback.
- [x] Build site list, connection dialog, catalog/mapping dialog, preview/result and event/health panels using existing components.
- [x] Add explicit model/cadence configuration for billable probes; no probe on page load.
- [x] Verify locale parity, typecheck, targeted tests, lint and production build.

```ts
const preview = await api.preview(siteId, { selections })
// Only an explicit confirmation button calls apply(siteId, preview.id).
```

## Task 5: Cross-component review and validation

- [x] Inspect the full diff against the spec and review auth, SSRF, persistence and import recovery boundaries independently.
- [x] Fix concrete findings, run covering tests and re-review the fixes.
- [x] Run isolated local server/browser fixture validation, document exact outcomes and any external-platform limitations.
- [x] Commit a reviewable feature branch; do not merge, push or deploy implicitly.
