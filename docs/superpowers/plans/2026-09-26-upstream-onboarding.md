# Native upstream onboarding implementation plan

> For agentic workers: use superpowers:subagent-driven-development. Implement the bounded tasks below in this existing isolated worktree.

**Goal:** Native three-field onboarding, account/pricing collection, managed group keys and bulk mapping.

**Architecture:** Keep login/discovery in Go connectors and orchestration in Vue. Add profile metadata to catalog JSON, a separate encrypted key table, explicit key APIs and a platform-detection API. Reuse existing preview/apply transactions.

**Tech stack:** Go/Gin/PostgreSQL, Vue/TypeScript/Vitest, Playwright CLI.

**Spec:** docs/superpowers/specs/2026-09-26-upstream-onboarding-design.md

## Constraints and ownership

Never store dashboard passwords or expose session tokens in output. Read-only collection does not create keys/accounts. Missing prices are unknown. Changes happen in C:/Users/Cracker/.codex/worktrees/upstream-governance/sub2api. Real upstream requests must originate from the governance application.

- Root owns connector detection, account summary and model-plaza collection and their tests; types.go Catalog/RemoteAccount only.
- Backend-key agent owns encrypted key persistence, service key operations, preview/apply key reuse, new handler/routes and tests. New DTOs live in service_keys.go; coordinate shared types.go/route edits.
- Frontend agent owns governance Vue components, API DTOs and locale additions plus tests.
- Independent reviewer validates pricing semantics and reviews the assembled change.

## Task 1: Detection, account summary and model-plaza

- [x] Add fixture tests for native public-settings detection, profile balance/null semantics and plaza-only pricing filtered to bindable groups.
- [x] Implement POST /sites/detect with {base_url, proxy_id}; return {platform,name,base_url,captcha_required,captcha_site_key?}. Validate public HTTPS origins and never send credentials while detecting.
- [x] Add catalog.account with {user_id,username,email,balance,frozen_balance,used_balance,unit,source}; nullable money values. Read authenticated profile in Discover, preserve reauth failures.
- [x] Merge /api/v1/model-plaza only into IDs from /groups/available. Preserve pricing units/tiers and provenance; unsupported optional data yields a specific warning.
- [x] Run go test ./internal/upstreamgovernance with fixture assertions.

## Task 2: Managed group keys

- [x] Test idempotent per-group creation, invisible-group rejection, partial batch failure, explicit reveal, ciphertext-only storage and old-binding reuse.
- [x] Add forward migration and SQL key store. Implement GET /sites/:id/keys, POST /sites/:id/keys with {snapshot_id,selections:[{remote_group_id,platform}]}, POST /sites/:id/keys/:key_id/reveal. Only create/reveal responses contain secrets; mark them no-store.
- [x] Reuse created keys in Preview/Apply, maintain marker/site/user binding, bounded batches and site locking. Raise preview selection bound to the supported catalog bound (100).
- [x] Verify targeted Go and real PostgreSQL integration tests; no real upstream key creation by the agent.

## Task 3: Unified native UI

- [x] Test URL/user/password onboarding chains detection/create/connect/sync; retry reuses created site; challenge does not pretend success.
- [x] Add unified three-field onboarding dialog with advanced platform/proxy settings. ConnectDialog only shows challenge-specific controls when needed; password auth remains primary. Successful connection triggers immediate collection.
- [x] Show account balance/unit and collection timestamp. Add select-all and bulk target assignment with known transport inference and explicit unsupported rows.
- [x] Add create-group-keys and reveal/copy display, transient secret state and per-group outcomes; reset secrets on site switch/unmount. Keep immutable import preview/confirmation.
- [x] Run governance Vitest, locale completeness, typecheck and scoped lint.

## Task 4: Review, restart and application validation

- [x] Review all changes for double key creation, credential leaks, stale selections and pricing semantics.
- [x] Run targeted combined backend tests/vet and frontend production build.
- [x] Back up isolated runtime DB/binary, rebuild embedded app, restart only the verified fixture process.
- [x] Exercise native onboarding through the local governance UI. Collect real o10 data through its saved governance session; report the CAPTCHA limitation separately from data collection. Never present session import as password-login validation.
- [x] Record concrete validation evidence and leave app available for review.

Ruling: Continue implementation under the user's already approved native-center scope and latest concrete workflow requirements; do not restart a permission cycle.
