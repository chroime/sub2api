# Channel Monitor Availability Reset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an admin action that sets a rolling seven-day availability baseline for a channel's primary model without adding database tables or changing stored check history.

**Architecture:** Store a versioned availability-reset JSON document under the existing `extra_headers` JSONB column using an internal `sub2api:` key. The service layer reads this metadata and combines a linearly decaying virtual baseline with post-reset real checks for the seven-day summary and timeline; fifteen-/thirty-day detail statistics remain real-history-only. Admin POST/DELETE endpoints update only that internal key, while repository adapters strip it from HTTP headers and API responses.

**Tech Stack:** Go 1.27, Gin, Ent/PostgreSQL repository, Vue 3, TypeScript, Vitest, existing BaseDialog/i18n/API client patterns.

**Spec:** `docs/superpowers/specs/2026-09-04-channel-monitor-availability-reset-design.md`

## Global Constraints

- Do not add a database table, column, migration, or history row for the reset.
- Apply reset behavior only to the channel `primary_model`; extra-model status and availability remain unchanged.
- Accept availability `0.00`–`100.00` with at most two decimal places and `degraded_bars` `0`–`8`.
- Hide pre-reset primary-model red timeline points, preserve post-reset failures, and restore all real history after cancellation or expiry.
- Never expose `sub2api:availability_reset` through `extra_headers` or forward it to upstream providers.
- Copying a channel must clear the availability-reset metadata on the new channel.
- Keep all user-visible tooltip text in the existing status/latency format; do not label manual points as an artificial baseline.

---

### Task 1: Synchronize the worktree and establish test seams

**Files:**
- Modify: repository worktree from remote `origin/main` commit `0d489b72a61b1360be30d99c6fa392b20e0da5a6`
- Test: `backend/internal/service/channel_monitor_availability_reset_test.go` (create)

**Interfaces:**
- Consumes: existing `ChannelMonitor`, `ChannelMonitorHistoryEntry`, and availability aggregation types.
- Produces: table-driven pure-function tests that later service code must satisfy.

- [ ] **Step 1: Verify the remote snapshot is the active source.**

Run `git -C remote-sync rev-parse HEAD` and confirm it prints `0d489b72a61b1360be30d99c6fa392b20e0da5a6`; compare hashes for the channel monitor files in the active worktree and `remote-sync`.

- [ ] **Step 2: Add failing tests for reset math and timeline composition.**

Create tests for `availabilityResetRemaining`, `applyAvailabilityReset`, and `buildResetTimeline` using fixed UTC timestamps. Cover reset-at-now, halfway through seven days, expired reset, zero real checks, real post-reset failures, zero/eight yellow bars, and cancellation represented by nil metadata. Assert the exact percentage and status sequence.

- [ ] **Step 3: Run the focused test package.**

Run `go test ./internal/service -run AvailabilityReset -count=1` from `backend`; expect compilation failures naming the not-yet-defined helpers.

---

### Task 2: Add reset metadata and pure service calculations

**Files:**
- Modify: `backend/internal/service/channel_monitor_types.go`
- Modify: `backend/internal/service/channel_monitor_service.go`
- Modify: `backend/internal/service/channel_monitor_aggregator.go`
- Test: `backend/internal/service/channel_monitor_availability_reset_test.go`

**Interfaces:**
- Consumes: `extra_headers` maps returned by the repository, current primary model, interval seconds, real availability rows, and real timeline entries.
- Produces: `ChannelMonitorAvailabilityReset`, metadata encode/decode helpers, `ApplyAvailabilityReset`, and timeline/availability helper functions used by list/detail aggregation.

- [ ] **Step 1: Define the internal metadata contract.**

Add `ChannelMonitorAvailabilityReset` with `Version`, `Model`, `TargetPct`, `DegradedBars`, `ResetAt`, `BaselineTotal`, `BaselineOK`, and `CreatedBy`, plus a service-facing `AvailabilityResetActive bool` on `ChannelMonitor` for the admin UI. Add `ChannelMonitorAvailabilityResetMetadataKey = "sub2api:availability_reset"`. Keep the JSON document internal; only the boolean active flag may be serialized in admin responses.

- [ ] **Step 2: Implement strict metadata parsing and validation.**

Decode the JSON string only when version is `1`, model is non-empty, target is finite and within `0..100`, degraded bars are `0..8`, reset time is valid, and baseline counts are non-negative with `BaselineOK <= BaselineTotal`. Invalid, expired-format, or model-mismatched metadata returns nil and logs a structured warning from the caller.

- [ ] **Step 3: Implement reset math.**

Use `remaining = max(0, 1-elapsed/(7*24*time.Hour))`. Compute `baselineTotal = max(1, ceil(7 days / max(intervalSeconds, 1)))` at reset creation and `baselineOK = round(baselineTotal*targetPct/100)`. Combine decayed baseline counts with post-reset real counts and return 0 when the resulting denominator is zero.

- [ ] **Step 4: Implement timeline composition.**

Filter real primary-model entries to `CheckedAt >= ResetAt`, keep newest-first order, and prepend synthetic points until at most 60 points exist while `remaining > 0`. Synthetic points use `operational` except for the configured number of `degraded` bars, nil latency values, and deterministic evenly spaced timestamps before `ResetAt`. Do not attach an origin field or tooltip marker.

- [ ] **Step 5: Run the focused service tests.**

Run `go test ./internal/service -run AvailabilityReset -count=1`; expect PASS before wiring repository or handlers.

---

### Task 3: Isolate metadata in repository persistence and queries

**Files:**
- Modify: `backend/internal/repository/channel_monitor_repo.go`
- Modify: `backend/internal/service/channel_monitor_service.go`
- Test: `backend/internal/repository/channel_monitor_availability_reset_test.go` (create)
- Test: `backend/internal/service/channel_monitor_duplicate_test.go`

**Interfaces:**
- Consumes: the internal metadata key and existing `channelMonitorHeadersForPersistence`/`entToServiceMonitor` conversion functions.
- Produces: repository maps that preserve the reset document for service aggregation but never expose it as user headers or upstream request headers.

- [ ] **Step 1: Add failing repository isolation tests.**

Assert that a persisted map containing `sub2api:availability_reset` and `sub2api:duplicate_operation_id` is decoded into service metadata while `ExtraHeaders` excludes both keys, and that persistence re-adds only the internal values. Assert duplicate creation receives no availability-reset key.

- [ ] **Step 2: Extend conversion helpers for both internal keys.**

Read and remove `ChannelMonitorAvailabilityResetMetadataKey` alongside the duplicate key in `entToServiceMonitor`; add a dedicated internal metadata map or field on `ChannelMonitor` that survives service round trips without being returned by `channelMonitorToResponse`. Ensure `channelMonitorHeadersForPersistence` merges that value while filtering user-supplied attempts to override it.

- [ ] **Step 3: Make ordinary update/template paths preserve the key atomically.**

When `Update` receives `extra_headers`, merge the freshly loaded internal reset document back into the persistence map. Template application must use the same merge path. In `Duplicate`, construct the new monitor with an empty reset document even if the source has one.

- [ ] **Step 4: Run repository and duplicate tests.**

Run `go test ./internal/repository ./internal/service -run 'AvailabilityReset|Duplicate' -count=1`; expect PASS.

---

### Task 4: Wire reset-aware aggregation and admin service operations

**Files:**
- Modify: `backend/internal/service/channel_monitor_service.go`
- Modify: `backend/internal/service/channel_monitor_aggregator.go`
- Modify: `backend/internal/repository/channel_monitor_repo.go`
- Test: `backend/internal/service/channel_monitor_availability_reset_test.go`

**Interfaces:**
- Consumes: monitor metadata, real batch availability/latest/timeline repository methods, and authenticated admin ID.
- Produces: `SetAvailabilityReset(ctx, monitorID, actorID, targetPct, degradedBars)` and `ClearAvailabilityReset(ctx, monitorID)` methods plus reset-aware list/user/detail views.

- [ ] **Step 1: Add service operation tests.**

Test valid writes, invalid values, missing monitor, missing primary model, idempotent clear, and preservation of unrelated headers. Use the existing repository mock pattern and assert `CreatedBy` and UTC `ResetAt` are written.

- [ ] **Step 2: Implement atomic set/clear operations.**

Load the monitor, validate request values, calculate baseline counts from `IntervalSeconds`, decode current headers, modify only the availability-reset key, and call the existing repository `Update` once. Return a decrypted monitor object with the reset key still hidden from ordinary header output.

- [ ] **Step 3: Feed metadata into batch summaries.**

Change the batch aggregation input to include monitor metadata or monitor objects, compute primary seven-day availability through the shared reset helper, and keep extra-model aggregation unchanged. Apply the same logic to `GetUserDetail` for the seven-day primary-model row only.

- [ ] **Step 4: Feed metadata into user timelines.**

Change `batchTimeline`/`buildUserViewFromSummary` so the primary model's timeline is composed from post-reset real entries plus synthetic points. Keep latest status sourced from the latest real check; do not make synthetic points affect `PrimaryStatus` or latency.

- [ ] **Step 5: Run all channel-monitor service tests.**

Run `go test ./internal/service ./internal/repository -run 'ChannelMonitor|AvailabilityReset' -count=1`; expect PASS.

---

### Task 5: Add admin HTTP endpoints and routes

**Files:**
- Modify: `backend/internal/handler/admin/channel_monitor_handler.go`
- Modify: `backend/internal/server/routes/admin.go`
- Test: `backend/internal/handler/admin/channel_monitor_availability_reset_test.go` (create)

**Interfaces:**
- Consumes: JSON `{availability_pct, degraded_bars}`, existing admin auth/audit middleware, and service set/clear methods.
- Produces: `POST /api/v1/admin/channel-monitors/:id/availability-reset` and `DELETE /api/v1/admin/channel-monitors/:id/availability-reset`.

- [ ] **Step 1: Add failing handler tests.**

Exercise valid POST, invalid percentage precision, invalid percentage range, invalid yellow-bar range, invalid monitor ID, and DELETE idempotency. Assert the existing error envelope, status codes, and that the response excludes the internal metadata key.

- [ ] **Step 2: Add request types and handlers.**

Bind a numeric `availability_pct` and integer `degraded_bars`; use `ParseChannelMonitorID`, `middleware2.GetAuthSubjectFromContext`, and `response.ErrorFrom` consistently with neighboring handlers. POST returns the updated channel response after recomputing its list summary; DELETE returns success after clearing the key.

- [ ] **Step 3: Register routes under the existing feature guard.**

Place the two routes beside `/run` and `/history` in `registerChannelMonitorRoutes`, so admin auth, rate limiting, audit logging, compliance guard, and channel-monitor feature gating apply automatically.

- [ ] **Step 4: Run handler tests and compile the backend.**

Run `go test ./internal/handler/admin -run AvailabilityReset -count=1` and then `go test ./internal/handler/admin ./internal/server/routes -run ChannelMonitor -count=1`.

---

### Task 6: Add the admin reset dialog and API client

**Files:**
- Modify: `frontend/src/api/admin/channelMonitor.ts`
- Create: `frontend/src/components/admin/monitor/MonitorAvailabilityResetDialog.vue`
- Modify: `frontend/src/components/admin/monitor/MonitorActionsCell.vue`
- Modify: `frontend/src/views/admin/ChannelMonitorView.vue`
- Modify: `frontend/src/i18n/locales/zh/admin/channels.ts`
- Modify: `frontend/src/i18n/locales/en/admin/channels.ts`
- Test: `frontend/src/components/admin/monitor/MonitorAvailabilityResetDialog.spec.ts` (create)
- Test: `frontend/src/components/admin/monitor/MonitorActionsCell.spec.ts`
- Test: `frontend/src/views/admin/__tests__/ChannelMonitorView.availabilityReset.spec.ts` (create)

**Interfaces:**
- Consumes: `ChannelMonitor` rows and admin endpoints from Task 5.
- Produces: typed `availabilityReset` and `clearAvailabilityReset` API functions, reset/cancel events, and a dialog with client-side boundary validation.

- [ ] **Step 1: Add API types and methods.**

Define `AvailabilityResetParams { availability_pct: number; degraded_bars: number }`, add `availabilityReset(id, params)` and `clearAvailabilityReset(id)`, and expose both from `channelMonitorAPI`.

- [ ] **Step 2: Build the dialog with existing BaseDialog conventions.**

Use a number input constrained to 0–100 with step `0.01`, eight or fewer selectable yellow-bar controls, and explicit submit/cancel buttons. Validate finite numeric input, no more than two decimal places, and integer yellow-bar count before emitting `submit`.

- [ ] **Step 3: Add reset/cancel actions to the operation cell.**

Add an icon+text button beside “立即检测”; emit `availability-reset` for a fresh reset and `availability-reset-cancel` when the row is active. Disable the action while the row request is pending and retain the existing duplicate/edit/delete behavior.

- [ ] **Step 4: Connect view state and reload behavior.**

Track the selected row, dialog visibility, and pending action in `ChannelMonitorView.vue`. On success show the existing app-store toast and call `reload`; on failure show `extractApiErrorMessage` while retaining the dialog's entered values. Use the backend's safe `availability_reset_active` boolean to render “取消重置”; the reset dialog remains available for replacing an active baseline.

- [ ] **Step 5: Add bilingual strings and run focused frontend tests.**

Add labels, title, validation errors, success/failure messages, and cancel text to both locales. Run `pnpm vitest run src/components/admin/monitor/MonitorAvailabilityResetDialog.spec.ts src/components/admin/monitor/MonitorActionsCell.spec.ts src/views/admin/__tests__/ChannelMonitorView.availabilityReset.spec.ts` from `frontend`.

---

### Task 7: Verify end-to-end behavior and clean the synchronization helper

**Files:**
- Modify: any files needed to resolve test/typecheck failures from Tasks 2–6
- Delete: `remote-sync/` temporary clone after all source files have been synchronized
- Preserve: `.superpowers/` as existing untracked brainstorming state; do not add it to the feature commit

**Interfaces:**
- Consumes: completed backend and frontend implementation.
- Produces: a clean feature commit based on remote commit `0d489b72` and verified build/test output.

- [ ] **Step 1: Run backend formatting and focused tests.**

Run `gofmt -w` on changed Go files, then `go test ./internal/service ./internal/repository ./internal/handler/admin -run 'ChannelMonitor|AvailabilityReset' -count=1` from `backend`.

- [ ] **Step 2: Run frontend typecheck and tests.**

Run `pnpm typecheck` and the focused Vitest command from `frontend`; fix all type or assertion failures before proceeding.

- [ ] **Step 3: Run production builds.**

Run `go test ./...` from `backend` and `pnpm build` from `frontend` if dependency/runtime availability permits; record any environment-only limitation explicitly.

- [ ] **Step 4: Inspect the final diff.**

Use `git --git-dir=remote-sync/.git --work-tree=. diff --check` and `git --git-dir=remote-sync/.git --work-tree=. status --short` to confirm only the feature, approved design/plan documents, and expected test changes remain. Search changed files for `sub2api:availability_reset` and verify every exposure path strips it.

- [ ] **Step 5: Commit and push the feature.**

Using the writable temporary Git metadata, stage the feature files and approved docs with `git --git-dir=remote-sync/.git --work-tree=. add -A` while excluding `.superpowers/`, then commit `feat: add channel monitor availability reset`. Push the resulting `main` commit to `origin/main` only after tests pass and the user has requested remote submission.
