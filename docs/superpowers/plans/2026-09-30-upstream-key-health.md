# Upstream Key Health Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent stale managed upstream keys from being reused, and detect, report, and contain deleted keys during governance collection.

**Architecture:** Add a read-only, identity-checked key inventory interface, a separately persisted health observation per managed key, and a bounded audit after catalog collection. Fold confirmed key absence into existing identity-guarded account reconciliation, while keeping group absence and key absence distinct. Expose administrator-only status and notifications through existing governance APIs and SMTP adapter.

**Tech Stack:** Go service and PostgreSQL migration, Vue 3 and TypeScript, existing governance worker, Vitest/Go tests.

**Spec:** `docs/superpowers/specs/2026-09-30-upstream-key-health-design.md`

## Global Constraints

- The read-only audit cannot call `EnsureKey`, issue POSTs, or reveal key plaintext.
- A failed or partial remote inventory cannot confirm deletion or pause an account.
- Import, notification, and account mutation retain site/user/marker/version/identity checks.
- No real upstream calls, XenoAI mutations, or production database writes in automated tests.

---

### Task 1: Read-only key inventory and persisted observations

**Files:** `backend/internal/upstreamgovernance/connector_keys.go`, `types.go`, `service_keys.go`, `store_keys.go`, new tests, `backend/migrations/258_upstream_governance_key_health.sql`.

**Interfaces:** `KeyInventoryReader.ListKeyInventory(ctx, site, session) ([]RemoteKeyIdentity, error)` returns complete ID/group pairs. `KeyHealthStore` reads and saves observed health without replacing `KeyCipher`. `ManagedKey` includes public `Health` status separate from `HasKey`.

- [ ] Write tests for Sub2API/NewAPI complete list, non-POST guarantee, incomplete list rejection and typed ID/group conversion. Run focused tests; confirm red.
- [ ] Implement the read-only connector method using existing `identity` and `keyList`.
- [ ] Write store/migration tests proving observations survive reload and cannot overwrite a completed key or unrelated owner. Run focused tests; confirm red.
- [ ] Add the health columns/JSON state and specialized observation store operation, keeping `SaveManagedKey` CAS semantics.
- [ ] Run focused connector/store tests and migration integration tests; commit the isolated data/API boundary.

### Task 2: Audit scheduling and import preflight

**Files:** `backend/internal/upstreamgovernance/service_key_health.go`, `service.go`, `service_keys.go`, `worker.go`, `store.go`, targeted tests.

**Interfaces:** `auditManagedKeysLocked` works under the site lock, returns individual key observations without failing a successful catalog snapshot; `validateReusedKey` blocks stale key reuse before account writes. Worker has a due 60-second confirmation path independent of catalog interval.

- [ ] Write failing service tests for first missing, second complete missing after 60 seconds, unknown inventory, recovery and event deduplication.
- [ ] Implement one inventory call per site, persist observations and due follow-up scheduling with bounded contexts.
- [ ] Write failing import and CreateKeys tests showing an existing local ciphertext cannot be reused when remote ID/group is absent or unverifiable.
- [ ] Add one-inventory-per-operation preflight, owner matching and per-item error mapping; preserve existing creation behavior for genuinely new keys.
- [ ] Run all upstream governance package tests and worker integration tests; commit.

### Task 3: Containment, notifications, and administrator UX

**Files:** `backend/internal/upstreamgovernance/reconciliation_*.go`, `backend/internal/repository/account_repo_governance_reconciliation.go`, `backend/internal/service/upstream_governance_*`, `frontend/src/api/admin/upstream-governance.ts`, `frontend/src/views/admin/upstream-governance/ManagedKeysPanel.vue`, `GovernanceSitesOverview.vue`, i18n and tests.

**Interfaces:** A confirmed missing key yields a reason-specific, identity-guarded pause; only presence plus group availability restores a governance-owned pause. `KeyNotifier` shares system SMTP/recipients and deduplicates missing/recovered notices.

- [ ] Write failing tests for pause/recovery with both key and group disappearance, manual edits, disabled automation, and no duplicate SMTP sends.
- [ ] Implement distinct pause ownership/reason and notifier delivery reservation; never delete or overwrite old ciphertext.
- [ ] Add frontend contract/tests for local-saved versus remote-verified status, error text, checked time and site-level issue count.
- [ ] Implement Vue changes and i18n using the existing compact operational UI.
- [ ] Run Go, frontend, migration and synthetic-browser regression suites; inspect diff, commit.

### Task 4: Explicit key repair workflow

**Files:** `backend/internal/upstreamgovernance/service_key_repair.go`, `store_keys.go`, bindings/local adapter, administrator API routes, frontend repair dialog and tests.

**Interfaces:** A reviewed repair plan reserves a new remote key operation, verifies its result, updates the native account and binding under CAS, and promotes the new managed key only after all persisted identities match. Uncertain stages remain visible and resumable without a second create POST.

- [ ] Write failure-injection tests for new key creation, list lag, native account write, binding write, retry and ownership mismatch; confirm red.
- [ ] Implement explicit prepare/confirm/commit flow under site lock, reusing current idempotent Sub2API creation rules and refusing unsafe New API replay.
- [ ] Add administrator-only repair APIs and a confirmation dialog with per-stage status; never run repair from collection.
- [ ] Run all backend/frontend tests, build and synthetic browser QA; commit only after green verification.
