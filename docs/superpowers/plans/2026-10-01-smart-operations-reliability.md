# Smart Operations Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Track task verification below.

**Goal:** Make pricing previews truthful, writes scoped, and protection messages actionable.

**Architecture:** Add read-only authoritative per-group previews and transactionally checked per-group saves, with a separate notification write. Keep the existing automatic pricing coordinator responsible for production price changes. Vue retains independent drafts and binds explicit saves to fresh previews.

**Tech Stack:** Go, PostgreSQL, Gin, Vue 3, TypeScript, Vitest, existing project UI components.

**Spec:** `docs/superpowers/specs/2026-10-01-smart-operations-reliability.md`

## Global Constraints

- Work in the existing isolated `codex/upstream-governance` worktree.
- No new dependencies, automatic payments, upstream calls or real test emails.
- Preview must not mutate; ignore client-supplied authoritative cost/baseline state.
- Save strategy only; no immediate sale change and no automatic ownership changes.
- Keep UTC+8 formatting and bilingual locale key completeness.
- Back up local runtime before replacement; keep PostgreSQL and Redis running.

## Task 1: Backend preview and isolated writes

**Files:** add `backend/internal/upstreamgovernance/pricing_admin.go`, associated unit/store tests; modify pricing store, admin handlers and routes.

**Interfaces:**
- `POST sites/:id/pricing-policies/:group_id/preview` accepts `{policy: PricingPolicyDraft}`; returns current sale, trusted cost, proposed sale, reason, shared source/binding impact and `fingerprint`.
- `PUT sites/:id/pricing-policies/:group_id` accepts `{policy: PricingPolicyDraft, fingerprint: string}`; returns full current pricing configuration.
- `PUT sites/:id/pricing-notifications` accepts `{version, notifications}`; returns full configuration.
- Draft contains only enabled, mode, min_margin, safety_buffer, decrease_stability_seconds, max_increase_percent; manual ownership remains server-owned.

- [x] RED: draft target-margin 25% + buffer 10% at trusted cost 0.22 gives literal 0.3385; changing draft changes result; no persistence call from preview.
- [x] RED: scoped save rejects unbound group, stale fingerprint and invalid finite/range inputs; preserves unrelated policy/notification/runtime state.
- [x] Implement shared authoritative preparation and transactional comparison; semantic fingerprints ignore fresh timestamps when facts are otherwise identical.
- [x] GREEN: `go test ./internal/upstreamgovernance ./internal/handler/admin ./internal/server/routes -run 'Pricing|Observation|Governance' -count=1` with isolated fixtures; review affected runtime paths.

## Task 2: Frontend real previews and draft isolation

**Files:** `frontend/src/api/admin/upstream-governance.ts`, new API tests, `ObservationPricingPanel.vue`, `ObservationPricingPanel.spec.ts`.

**Interfaces:** consumes Task 1 JSON contracts; uses reason helpers and locale strings from Task 3.

- [x] RED: edit margin then preview sends only editable fields and displays server 0.3385; does not save.
- [x] RED: saving row 12 sends only row 12; editing row 13 and notifications survives successful row 12 save.
- [x] RED: saving notifications sends no policy; saving intervals keeps policy/notification drafts.
- [x] RED: changing draft invalidates preview; failed preview has no confirmation action; stale response after unmount/site switch is ignored.
- [x] Replace Set-only previews with returned per-row previews, explicit isolated saves, scoped merge of server responses and appropriate disabled states.
- [x] Remove duplicate observation thresholds; add real per-group decrease-stability input and preserve existing percentage input semantics.
- [x] GREEN: targeted Vitest, typecheck, locale completeness and lint for changed files.

## Task 3: Actionable feedback and localized protection

**Files:** new `pricing-feedback.ts`/test; `frontend/src/i18n/locales/{zh,en}/governance.ts`; root integrates helper into panel.

**Interface:** pure helpers return locale keys for known reason/status plus a next-step key; unknown reasons never render raw technical messages in the main card.

- [x] RED: disabled is not shown as managed; manual owner and protected reasons have specific next steps; unknown reason falls back without inventing recovery.
- [x] Add translations for preview freshness, scope, preserved drafts, save-not-apply behavior, editable-field labels and safe cost limitations.
- [x] GREEN: helper tests plus locale completeness; test panel renders readable feedback and folds technical details.

## Task 4: Review, regression and release

- [x] Review the integrated diff for preview/write parity, transactions, ownership, stale-data handling and scope.
- [x] Run governance frontend suite, backend tests, typecheck/lint, frontend and embedded backend builds.
- [x] Use synthetic fixtures for browser QA where live actions would change configuration; no production form submissions.
- [x] Update user docs and record verification; commit only task files.
- [x] Back up and verify database archive + current binary/config/key; replace only the verified app executable and restart.
- [x] Verify PID, binary hash, health and UI route. Report this batch's delivery and deferred phases accurately.

## Pre-flight review

| Tasks | Shared interface / constraint | Decision |
|---|---|---|
| 1 ↔ 2 | Request/response contracts | Backend sends exact contract before frontend integration; endpoints names fixed above. |
| 2 ↔ 3 | Locale and feedback imports | Task 3 owns locales/helper; root alone owns panel/API. |
| 1 | Preview vs save | Share one preparer; reject changed facts inside transaction; no direct sale writes. |
| 2 | Saves vs drafts | Merge only saved scope; invalidate dependent previews without dropping other input. |
| 3 | Helpful vs unsafe recovery | Explain and navigate, never auto-dismiss protection or alter ownership. |
| 4 | Verification vs live effects | Isolated fixtures; no real upstream/email/payment tests. |

Ruling: deliver the first recommended batch now, not the entire future roadmap; the user's approval follows the explicit first-batch recommendation.

## Completed verification

- Frontend: 430 tests across 32 files, typecheck, changed-file ESLint and production build passed.
- Backend: full upstreamgovernance package plus six-package targeted regression and vet passed; isolated PostgreSQL integration passed.
- Browser: desktop, 390px mobile and dark synthetic-data UI verified; no horizontal overflow. No real upstream inference, login or test emails sent.
- Deployment: backed up database, executable, configuration and encryption key; verified archive list. Local 58089 service PID 502736 is healthy and serves the new frontend asset.
- Existing branch retained; no main merge or remote push.
