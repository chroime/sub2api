# Channel Status Recent-24 Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make V1 channel status availability reflect the latest 24 displayed probes, cache rate average only cache-bearing requests within the latest 24 successful streams, and remove the date-window selector.

**Architecture:** Reuse the monitor list timeline as the availability sample, preserve the existing analytics response aliases while filtering cache-rate aggregation in the repository query, and remove only V1 date-window state and fetches. Keep the monitor detail dialog's historical metrics intact.

**Tech Stack:** Go, PostgreSQL, Vue 3, TypeScript, Vitest, vue-tsc, Vite.

---

### Task 1: Verify recent-24 availability calculation

**Files:**
- Create: `frontend/src/utils/recentMonitorAvailability.ts`
- Modify: `frontend/src/components/user/monitor/MonitorCardGrid.vue`
- Test: `frontend/src/components/user/__tests__/MonitorCardGrid.spec.ts`

- [x] Calculate from the newest 24 probe points, counting operational and degraded as available and failed/error as unavailable.
- [x] Exclude empty placeholders and return `null` when there are no measured points.
- [x] Run the focused availability tests.

### Task 2: Filter cache-rate aggregation within the existing stream sample

**Files:**
- Modify: `backend/internal/repository/channel_monitor_metrics.go`
- Test: `backend/internal/repository/channel_monitor_metrics_postgres_test.go`

- [x] Keep latest-24 stream selection shared by cache rate and first-token time.
- [x] Average cache percentages only where `cache_read_tokens > 0`.
- [x] Run the PostgreSQL unit-tagged repository tests.

### Task 3: Remove V1 date-window controls and unused fetch state

**Files:**
- Modify: `frontend/src/components/user/monitor/MonitorHero.vue`
- Modify: `frontend/src/components/user/monitor/MonitorCard.vue`
- Modify: `frontend/src/views/user/ChannelStatusV1View.vue`
- Modify: `frontend/src/i18n/locales/en/dashboard.ts`
- Modify: `frontend/src/i18n/locales/zh/dashboard.ts`
- Test: `frontend/src/components/user/monitor/__tests__/MonitorHero.spec.ts`
- Test: `frontend/src/views/user/__tests__/ChannelStatusV1View.details.spec.ts`

- [x] Remove the 7/15/30-day selector and only the V1 state/fetches used by it.
- [x] Preserve refresh controls and the monitor detail dialog.
- [x] Describe the recent probe and cache-bearing stream sample in metric labels.
- [x] Run focused component tests and locale consistency checks.

### Task 4: Run final verification

- [x] Run the frontend production build and investigate any reproducible failure before changing configuration.
- [x] Run relevant backend SQL and frontend tests.
- [x] Review the complete diff and confirm no unrelated changes are included.

## Verification evidence

- `pnpm --dir frontend build` passed on November 26, including locale completeness, vue-tsc, and Vite. Production changes since that build are limited to a SQL comment.
- The final focused Vitest run passed all 34 tests in 6 files on November 27, using a single worker and `--no-cache` to avoid the full E: drive.
- `.tmp/verify-recent24-postgres.py` executed the exact repository SQL with fixtures extracted from the Go tests against local PostgreSQL temporary tables. Seven cases passed; all transactions rolled back. Removing the new cache filter reproduced the incorrect zero for a cache-free sample.
- Independent read-only review found no blocking issues.
- After local resources recovered on November 27, the native `go test -p=2 -tags=unit ./internal/repository -run TestMonitorUsagePostgres -count=1 -v` run passed all seven tests with the local PostgreSQL DSN configured. No SQL tests were skipped.
- The current backend built successfully with embedded frontend assets as `local-recent24-20261127`. The local backend at port 18081 and Vite frontend at port 3000 both returned HTTP 200; the frontend's public-settings API proxy returned code 0. Port 18080 belongs to another project and was left running.
