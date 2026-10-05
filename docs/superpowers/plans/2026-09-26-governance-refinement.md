# Governance refinement implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development to execute the independent implementation tasks in this session and review the integrated result.

**Goal:** Make bulk upstream onboarding convenient, persist the requested account policy and notify administrators when an upstream wallet is low.

**Architecture:** Extend existing governance previews and the native account adapter. Persist model templates in the existing settings table with optimistic versioning. Add a per-site balance monitor using fresh catalog data and the current system mail sender.

**Tech Stack:** Go, PostgreSQL, Vue 3, TypeScript, Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-26-governance-refinement-design.md`

## Global constraints

- Work only in the existing governance worktree; preserve live local fixture changes.
- Native login/collection and plaintext administrator credential display remain available.
- Freeze full account policy in preview; preserve optimistic concurrency and retry guarantees.
- All governance displayed timestamps use `YYYY-MM-DD HH:mm:ss`.
- Never send real test email; use loopback SMTP or a synthetic sender.

## 1. Import defaults and account persistence

Files: `backend/internal/upstreamgovernance/{types,service}.go`, `backend/internal/service/{upstream_governance,admin_account}.go`, repository governance CAS and focused tests.

- [x] Add `Selection.account_config` and `AccountChange` policy fields; default/validate them before preview storage.
- [x] Apply concurrency 5000, key notes, model mapping, declared-rate sync, quota 10000/700000/10000000 and OpenAI long-context flag.
- [x] Include notes/concurrency/new policy in desired-state checks and CAS; preserve unrelated fields and counters.
- [x] Verify defaulted and explicit config, stale account edits and retry after native rate sync.

## 2. Shared model restriction templates

Files: new governance `model_templates.go`, `store_model_templates.go`, admin handler, routes and tests.

Contract: `GET/PUT /admin/upstream-governance/model-templates` returns `{version,templates}`. Template fields are `{id,name,platform,models,is_default}`; PUT must supply the current version.

- [x] Validate bounded IDs/names/model lists and at most one default per protocol.
- [x] Persist in settings with atomic version checking, and return conflict on stale saves.
- [x] Test round trip, invalid input, independent defaults, stale concurrent saves and empty initial state.

## 3. Balance monitor

Files: balance-specific governance/store/mailer/handler files, migration 249, service factory and tests.

Contract: Site carries `balance_monitor` and `balance_monitor_status`. `PUT /sites/:id/balance-monitor` takes site version plus the configuration and returns the updated Site.

- [x] Add durable configuration/state and hook successful fresh snapshots.
- [x] Reuse system SMTP sender, resolve recipient overrides/system defaults and escape mail content.
- [x] Verify threshold boundary, unknown/native units, cooldown/restarts, partial recipient delivery and failures.

## 4. Frontend

Files: governance views/components, API types, Chinese/English locales and tests.

- [x] Organize site list/overview/group mapping/monitoring with responsive layout and vendor icons.
- [x] Add shared import configuration with requested defaults and preview transparency.
- [x] Add model template management/default selection and per-site balance configuration/status.
- [x] Format all displayed timestamps consistently and prevent late responses from crossing site boundaries.
- [x] Run focused Vitest, typecheck and scoped ESLint.

## 5. Integrated verification and local delivery

- [x] Review requirement coverage and concrete regressions.
- [x] Run focused backend and real PostgreSQL integration tests.
- [x] Back up current local database; build frontend and embedded backend.
- [x] Restart only the verified local fixture process and check health.
- [x] Review desktop/mobile browser layout, default payloads, template persistence and monitoring configuration using local fixtures.
- [x] Record evidence and limitations; commit the reviewed local changes.
