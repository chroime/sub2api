# 上游秒级观察与自动调价 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为上游治理中心增加按秒配置的快速观察、可信倍率变化通知、按本地分组统一自动调价，以及调价失败时的亏损保护。

**Architecture:** 将站点的快速分组观察与完整目录采集拆成独立到期时间；快速观察只更新可比成本事实和结构修订，内容未变不创建新导入快照。自动调价按本地计费分组聚合所有可用上游来源，使用冻结的售价/成本基线和已有利润底线，按版本事务提交并通过现有调度/认证缓存失效机制传播。变更邮件使用持久队列，不占站点网络锁。

**Tech Stack:** Go、PostgreSQL migrations、Gin、Vue 3 + TypeScript、Vitest、现有 SMTP/邮件服务、现有 `upstreamgovernance` worker/store/reconciliation contracts。

**Spec:** `docs/superpowers/specs/2026-10-01-upstream-seconds-auto-pricing-design.md`

## Global Constraints

- 现有站点分钟配置必须无损迁移为 BIGINT 秒值；最大历史分钟值乘60也必须可表示。
- 快速观察失败、身份不符、429、超时、分页不完整、未知倍率不能被当成删除、免费或零成本。
- 首次成功观察只建立基线，不群发整站新增邮件；相同结构观察不使已有导入预览失效。
- 本地分组的多个上游来源统一按最高可比成本计算，不能由各站点最后完成顺序覆盖售价。
- 上涨立即跟随；下降默认稳定60秒后跟随；超过涨幅审核阈值或无法核算时先保护受影响来源。
- 只调整可验证的文本 Token 计费口径；图片、视频、实时音频、按次工具费、未知 New API 单位和固定价套餐不自动改写。
- 用户专属倍率不自动改写；已发出的请求不追溯改价；通知失败不回滚调价且必须可重试。
- 不修改已应用迁移247—260；所有升级前必须备份数据库、程序配置和加密密钥。

---

### Task 1: 秒级站点周期与快速观察数据模型

**Files:**
- Create: `backend/migrations/261_upstream_governance_seconds_observation.sql`
- Modify: `backend/internal/upstreamgovernance/types.go`
- Modify: `backend/internal/upstreamgovernance/store.go`
- Modify: `backend/internal/upstreamgovernance/store_site.go` (or the existing site store file containing `siteColumns`, `scanSite`, `UpdateSite`, `DueSites`)
- Test: `backend/internal/upstreamgovernance/store_seconds_test.go`
- Test: `backend/internal/upstreamgovernance/store_seconds_integration_test.go`

**Interfaces:**
- Produces `Site.FastIntervalSeconds`, `Site.NextFastObserveAt`, `Site.LastFastObserveAt`, `Site.FastObserveStatus`, `Site.FastObserveError`, and a `CatalogObservation`/revision record that can be read without replacing `Snapshot`.
- Produces `Store.DueFastObservations`, `Store.ReserveFastObservation`, `Store.ObserveFastResult`, and `Store.LatestCatalogRevision` contracts.
- Keeps existing `IntervalMinutes` API fields as compatibility aliases while persisting new values as BIGINT seconds.

- [ ] **Step 1: Write failing storage tests** for 1-second, 5-second, and `2147483647*60` second intervals, minutes-to-seconds compatibility, due reservation CAS, and preservation of an unchanged snapshot ID when only observation time advances.
- [ ] **Step 2: Run the focused Go tests** and confirm failures are caused by missing columns/contracts, not fixture setup.
- [ ] **Step 3: Add migration 261** with BIGINT second columns, independent fast/full due timestamps, fast observation status/error/identity/completeness, catalog structure revision, and a compact latest observation table or JSONB columns. Add indexes for enabled fast due work. Backfill seconds exactly from existing minutes and preserve next timestamps.
- [ ] **Step 4: Extend site DTO/storage scanners and update paths** with strict positive int64 validation, legacy minute conversion, and version CAS. Reject conflicting seconds/minutes instead of truncating.
- [ ] **Step 5: Implement reservation and observation persistence** so only the worker that wins a due reservation can perform remote work; successful unchanged observations update timestamps but not the catalog revision or full snapshot ID.
- [ ] **Step 6: Run the focused storage tests and isolated PostgreSQL migration tests** until green; inspect `pg_restore --list` and verify migration 261 applies after 260 without editing prior migrations.
- [ ] **Step 7: Commit** with `feat(governance): persist second-based observation schedules`.

### Task 2: Connector fast group observation and fair scheduler

**Files:**
- Create: `backend/internal/upstreamgovernance/fast_observation.go`
- Modify: `backend/internal/upstreamgovernance/connector.go` and `connector_catalog.go`
- Modify: `backend/internal/upstreamgovernance/worker.go`
- Modify: `backend/internal/upstreamgovernance/service.go`
- Modify: `backend/internal/upstreamgovernance/error.go` (or the existing connector error mapping file)
- Test: `backend/internal/upstreamgovernance/fast_observation_test.go`
- Test: `backend/internal/upstreamgovernance/worker_seconds_test.go`
- Test: `backend/internal/upstreamgovernance/connector_rate_limit_test.go`

**Interfaces:**
- Produces `Connector.ObserveGroups(ctx, site, session) (GroupObservation, error)` with `GroupsComplete`, source user identity, comparable rate metadata, and no model/price/channel expansion.
- Produces a fair due worker using the persisted fast/full deadlines, one site lock per operation, bounded remote slots, and no catch-up storm.

- [ ] **Step 1: Write failing connector tests** for Sub2API/New API minimal group observations, incomplete pages, unknown/`auto` rates, source identity mismatch, and HTTP 429 with `Retry-After`.
- [ ] **Step 2: Run them RED** and record the missing fast connector method/error behavior.
- [ ] **Step 3: Implement minimal connector fast paths** by reusing existing authenticated profile/groups parsing; require complete group enumeration and preserve peak metadata/timezone limitations. Add a typed rate-limit error with bounded `Retry-After` parsing.
- [ ] **Step 4: Write failing worker tests** proving a 1-second due site is eligible without waiting for a minute ticker, slow sites do not block an independent due site, and a reservation is not repeated after restart/cancellation.
- [ ] **Step 5: Replace the fixed one-minute-only scheduling loop** with a short wake-up/timer based on the next due timestamp and a bounded fair batch. Keep full collection, session renewal, key audit, model checks, and binding probes on their independent schedules and deadlines.
- [ ] **Step 6: On fast success**, compare the trusted complete group set to the previous observation, update only the catalog revision when structure/rates actually change, emit `rate_changed`, `group_added`, `group_removed`, `group_changed` events, and call the pricing coordinator. On failures, persist backoff and leave last-success timestamps and group presence unchanged.
- [ ] **Step 7: Run fast connector/worker/service tests and the existing upstream-governance package**; commit `feat(governance): observe upstream groups on second schedules`.

### Task 3: Persistent change notifications

**Files:**
- Create: `backend/migrations/262_upstream_governance_change_notifications.sql`
- Create: `backend/internal/upstreamgovernance/notification_queue.go`
- Modify: `backend/internal/upstreamgovernance/store.go`
- Modify: `backend/internal/upstreamgovernance/service.go` and `worker.go`
- Modify: existing SMTP/mail service adapter used by balance/key/model notifications
- Test: `backend/internal/upstreamgovernance/notification_queue_test.go`
- Test: `backend/internal/upstreamgovernance/notification_queue_integration_test.go`

**Interfaces:**
- Produces persisted notification records keyed by event batch, site, pricing revision, and recipient; exposes delivery state, next retry, attempts, and last error.
- Consumes existing system SMTP sender without putting secrets or upstream credentials in messages.

- [ ] **Step 1: Write failing queue tests** for first-baseline suppression, event deduplication, grouped change messages, failed delivery retry, restart recovery, and no queue work while a site advisory lock is held.
- [ ] **Step 2: Run RED** against the absent migration/store/worker.
- [ ] **Step 3: Add migration 262 and SQL store methods** for recipient subscriptions, event batches, delivery attempts, bounded exponential retry, and idempotent uniqueness.
- [ ] **Step 4: Implement queue enqueue/claim/send/finalize** using the configured SMTP service; make delivery asynchronous and independent from collection/price commits.
- [ ] **Step 5: Wire fast observation and auto-pricing events** with severity-aware subjects (upstream cost increase, protection active, price applied, recovery, group change). Do not enqueue initial baseline or repeated unchanged observations.
- [ ] **Step 6: Run focused queue tests and existing balance/key/model notifier tests**; commit `feat(governance): persist upstream change notifications`.

### Task 4: Local group cost aggregation and automatic pricing

**Files:**
- Create: `backend/migrations/263_upstream_governance_pricing_policies.sql`
- Create: `backend/internal/upstreamgovernance/pricing_policy.go`
- Create: `backend/internal/upstreamgovernance/pricing_coordinator.go`
- Modify: `backend/internal/upstreamgovernance/reconciliation_plan.go` and `reconciliation_service.go`
- Modify: `backend/internal/service/admin_group.go` or the existing group update path to invalidate rate resolver/scheduler caches
- Modify: `backend/internal/service/user_group_rate_resolver.go` to cache explicit override presence separately from the group default
- Test: `backend/internal/upstreamgovernance/pricing_policy_test.go`
- Test: `backend/internal/upstreamgovernance/pricing_coordinator_test.go`
- Test: `backend/internal/service/user_group_rate_resolver_test.go`

**Interfaces:**
- Produces `PricingPolicy` with enabled/mode, frozen `BaselineCost`, `BaselineSale`, ratio, minimum margin, safety buffer, decrease stability seconds, max increase percent, policy version, and manual ownership state.
- Produces `PricingCoordinator.Recalculate(ctx, localGroupIDs, observations)`, which atomically computes each local group from all eligible comparable sources and returns an auditable pricing operation.

- [ ] **Step 1: Write failing pure pricing tests** for baseline-ratio mode, target-margin mode, highest-source aggregation, four-decimal upward rounding, zero/unknown/incomparable costs, peak-rule uncertainty, manual ownership, and decline stability.
- [ ] **Step 2: Run RED** and verify failures demonstrate the missing formulas rather than incorrect expected values.
- [ ] **Step 3: Implement pure calculation**:
  `K = BaselineSale / BaselineCost`, `floor = cost/(1-margin-buffer)`, `target = ceil4(max(cost*K, floor))`; reject non-comparable units, zero baseline, NaN/Inf, and unknown New API units.
- [ ] **Step 4: Write failing coordinator tests** for one source, multiple sources, same local group updated by different sites, source removal, large increase protection, failed write rollback, and policy/manual version races.
- [ ] **Step 5: Implement transactional local-group pricing** with per-group advisory/version locking, highest eligible cost selection, persisted cost facts separate from applied sale rate, protection state, and idempotent operation receipts. Do not mutate user-specific overrides or independent media prices.
- [ ] **Step 6: Integrate reconciliation** so account cost facts update immediately even when a price increase needs review; automatic apply cannot leave a high-cost source schedulable under an old cost. Reuse governance pause ownership and record review/protection events.
- [ ] **Step 7: Fix user-group rate caching** so absence of a user override does not cache a stale default beyond the pricing revision; invalidate explicit overrides and scheduler/auth snapshots after a price commit.
- [ ] **Step 8: Run pricing, service, repository, and governance tests; commit `feat(governance): protect margins with coordinated upstream pricing`.

### Task 5: Governance API and admin interface

**Files:**
- Modify: `backend/internal/handler/admin/upstream_governance_handler.go`, `upstream_governance_automation.go`, and related handlers
- Modify: `backend/internal/server/routes/admin_upstream_governance.go`
- Modify: `frontend/src/api/admin/upstream-governance.ts`
- Modify: `frontend/src/views/admin/upstream-governance/SiteEditDialog.vue`
- Modify: `frontend/src/views/admin/upstream-governance/AutomationPolicyPanel.vue`
- Create: `frontend/src/views/admin/upstream-governance/PricingPolicyPanel.vue`
- Modify: `frontend/src/views/admin/upstream-governance/UpstreamGovernanceView.vue`, `GovernanceHistory.vue`, and locales
- Test: corresponding Go handler/routes tests and Vue specs

**Interfaces:**
- Adds explicit `fast_interval_seconds`, `full_interval_seconds`/compatibility fields, observation health, pricing policy preview/apply endpoints, and notification subscription/status endpoints.
- UI displays fast/full interval, last successful observation, actual delay/backoff, source-cost table, target sale rate, margin floor, protected sources, and notification delivery state.

- [ ] **Step 1: Write failing API/handler tests** for strict seconds validation, legacy minute compatibility, policy preview/apply authorization, and notification recipient validation.
- [ ] **Step 2: Write failing Vue tests** for second-unit inputs, unknown-cost protection state, multiple-source highest-cost preview, manual ownership conflict, and change-history detail.
- [ ] **Step 3: Implement API DTOs/routes and wire site edit/automation forms** without changing existing default intervals or existing prices on load.
- [ ] **Step 4: Implement the pricing/observation panels** with explicit preview before enabling automatic price writes; show “unknown/incomparable” instead of zero.
- [ ] **Step 5: Add Chinese/English locale keys and history rendering** for upstream changes, automatic price operations, protection, and notification retries.
- [ ] **Step 6: Run focused frontend tests, locale completeness, typecheck, changed-file lint, and production build; commit `feat(governance): expose second schedules and pricing safeguards`.

### Task 6: Deployment, migration rehearsal, and acceptance

**Files:**
- Modify: `docs/upstream-governance.md`
- Modify: `docs/upstream-governance-validation.md`
- Create: `docs/superpowers/specs/2026-10-01-upstream-seconds-auto-pricing-acceptance.md`

- [ ] **Step 1: Run isolated PostgreSQL migration 261–263 tests** with a fresh schema and verify old minute values, snapshots, bindings, groups, and notifications.
- [ ] **Step 2: Run the complete focused Go packages, frontend governance tests, typecheck, lint, and production build using the configured E: caches.
- [ ] **Step 3: Before local deployment, create a fresh protected backup** of database dump, executable, config, startup script, and encryption key; validate with `pg_restore --list`.
- [ ] **Step 4: Build with `-tags 'embed timetzdata'`, restart the local service, and verify health/UI HTTP 200.
- [ ] **Step 5: Read-only acceptance on a synthetic upstream** verifies a 1-second schedule, unchanged observation preserving preview IDs, real rate change creating one pricing preview/event, unknown/429 protection, and queued email without sending to a real recipient.
- [ ] **Step 6: Record exact test output, migration checksum, backup path, branch and commit IDs; do not confirm real upstream price changes or send real mail during acceptance.

## Review Checklist

- [ ] Every spec section has an implementation task.
- [ ] No prior migration is edited.
- [ ] All new public methods and DTOs have focused tests.
- [ ] Unknown observations never mutate prices or deletion state.
- [ ] Existing import previews remain valid across unchanged fast observations.
- [ ] Automatic pricing is off for existing groups until explicitly enabled and previewed.
