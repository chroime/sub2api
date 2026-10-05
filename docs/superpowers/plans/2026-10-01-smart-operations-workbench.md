# Smart Operations Workbench Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans. Tasks have explicit ownership and RED→GREEN verification.

**Goal:** Present current actionable conditions and factual operational history without new business side effects.

**Architecture:** Read-only backend projections over existing governance state, operations and notification metadata. Separate Vue workbench/timeline components own paginated reads; the existing workspace owns navigation and mutation guards.

**Tech Stack:** Go/Gin/PostgreSQL, Vue 3/TypeScript, Vitest and existing components.

**Spec:** `docs/superpowers/specs/2026-10-01-smart-operations-workbench.md`

## Global Constraints
- Existing isolated codex/upstream-governance worktree; baseline 9932ba9d9.
- No schema migration, new dependency, upstream login/inference/mail/payment or automatic configuration changes.
- Current state is distinct from unread history. Missing evidence is not success.
- UTC+8 timestamps and Chinese operational summaries; no secret/raw SMTP body fields.
- Task 1 owns backend only; Task 2 owns new frontend component/API/helper files and locales; root owns workspace integration/tests/docs/release.

## Task 1 — Backend read models

**Files:** new workbench/timeline types, service/store implementation and tests in `backend/internal/upstreamgovernance`; admin handler and route additions.

**Contracts:** `GET /upstream-governance/workbench?site_id=&page=&page_size=` and `GET /sites/:id/timeline?page=&page_size=&kind=all|event|pricing|notification`. Exact DTO agreed between implementers before writing components; page totals and filters must agree. Workbench shows severity totals and pagination without misleading current-page-only category filtering.

- [x] RED: healthy/recovered current state creates no historical-incident todo; disabled pricing is not active protection; missing data and bounded stale collection create explicit unknown/stale items.
- [x] RED: optional site filter validates input; timeline excludes other sites and paginates stable ties; sent notification is not verified receipt.
- [x] Implement batch SQL reads and pure projections using existing state, with no decrypt/login/probe/send/write operations.
- [x] GREEN: local tests and isolated random-schema PostgreSQL integration; include fixtures with old unread events, empty sources, protected shared group, pending/failed/sent notification metadata.

## Task 2 — Read-only components and presentation

**Files:** `frontend/src/api/admin/upstream-operations.ts` and tests; `GovernanceWorkbench.vue`, `GovernanceTimeline.vue`, `operations-feedback.ts` and tests; zh/en governance locales.

**Contracts:** Workbench props `siteId?: number, disabled?: boolean`, emit `navigate({siteId,section})`; timeline props `siteId:number,disabled?:boolean`. Allowed sections are overview/import/models/monitor/history.

- [x] RED: render current todos and empty-as-of state, emit navigation without mutations, preserve old same-scope results on read failure, drop old-scope results on site change.
- [x] RED: pagination/filter resets, interval cleanup and hidden-document refresh suppression; late responses ignored.
- [x] RED: factual timeline statuses, unknown evidence, no credential fields or forced stage completion; UTC+8 display and numeric summary.
- [x] Implement existing-style controls, localized reasons/actions and explicit data freshness.
- [x] GREEN: component/API/helper tests, locale completeness and typecheck.

## Task 3 — Integrate existing workspace

**Files:** `UpstreamGovernanceView.vue` and its test file; minimal existing history hooks only if necessary.

- [x] RED: default overview shows all-site workbench; selecting site scopes it; all-site button resets only selection and goes to overview.
- [x] RED: navigation from todo switches to requested existing site/section, but is blocked during mutation; invalid site/section rejected.
- [x] RED: timeline loads only while history is selected, old history remains accessible, no automatic acknowledge/repair/probe call occurs.
- [x] Insert components without removing site selection, tab persistence, authorization modals or existing guards; distinguish current tasks from raw history.
- [x] GREEN: run existing workspace suite and new integration tests.

## Task 4 — Integrated review and local release

- [x] Verify privacy/readonly paths, current vs historical semantics, filtering/totals and compatibility.
- [x] Run governance frontend suite, backend package regressions, typecheck/lint/build and isolated database tests.
- [x] Use synthetic browser fixtures for desktop/narrow/dark layout; no production mutation for acceptance.
- [x] Update docs, commit scoped work locally; retain branch without merge/push.
- [x] Backup and verify database archive plus binary/config/key before replacing local runtime. Verify health, process and embedded frontend.

## Preflight review
| Pair / task | Boundary | Ruling |
|---|---|---|
| 1↔2 | DTO fields, filters, page shape | Backend publishes exact contract before implementation; frontend owns API client. |
| 2↔3 | Component props and navigate event | Root validates target site and section against current workspace state. |
| 1 | Timeline correlation | Chronological evidence, not guessed causation; explicit shared-group label. |
| 2 | Refresh failure | Keep same-scope previous evidence with stale warning, never silently healthy. |
| 3 | Navigation safety | Existing mutation guard wins; new reads cannot trigger business operations. |
| 4 | Live verification | Backups and read-only checks only; keep real upstream unaffected. |

## Completion evidence

- Independent review found and verified fixes for six boundary issues: shrinking pages, upstream/local-rate labels, uncertain key health, fast-observation identity context, notification failure-count/retry semantics, and site navigation beyond the legacy 1,000-site window.
- Administrative site inventory now consumes the existing cursor API in batches; legacy bounded store reads used by worker fallbacks remain unchanged. A failed subsequent batch never returns truncated success.
- Frontend regression: 43 files, 549 tests passed. Typecheck and changed-file ESLint passed; final production build passed. The two component suites also passed again after the final dark-mode contrast change.
- Backend: governance full package, five related package regressions, and governance/admin vet passed. Both Operations and PricingAdmin PostgreSQL integration tests passed against temporary random schemas in the isolated fixture database, not application tables.
- Browser acceptance used synthetic API fixtures with real components: desktop, 390-pixel narrow, and dark themes. Both narrow layouts reported scrollWidth=390 and innerWidth=390. Navigation emits the expected site/section; timeline notification filtering displayed two matching records. The Vite fixture reports only its existing blocked development HMR websocket; no corresponding production bundle dependency exists.
- Local release backup: `C:/Users/Cracker/AppData/Local/Temp/governance-app-20260926/before-workbench-20261002-001329`. Custom database archive was verified with pg_restore --list; binary/config/start script/logs/install marker/encryption key and SHA256 manifest were preserved.
- Deployed binary SHA256: `D9247894EB8F49AC117BFD2AE8C3DA669F936775D651C05319CFFB8B6156D6C1`; PID at verification: 549420. Health, governance HTML and referenced embedded JS asset returned successfully.
- Authenticated local read-only endpoint checks passed for all-site workbench, site 5 workbench, all timeline and notification filter; no cross-site rows or sensitive DTO fields. Related record IDs remain null.
- No upstream login, key repair/creation, inference request, real email test, policy change, or payment was triggered by acceptance. Existing configured background schedulers remain as before.
- Scope stays on `codex/upstream-governance`; no merge to main and no remote push. Templates, extended onboarding, bulk auto-repair and payment providers remain outside this batch.
