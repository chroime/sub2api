# Import Configuration Templates Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development with TDD and independent spec/quality review. Execute all scoped tasks without another design pause.

**Goal:** Reuse import parameters without automating upstream actions or losing current drafts.

**Architecture:** Independent global settings collection with CAS, a self-contained template picker/editor, and targeted integration into the existing import preview flow. Existing model-template logic remains authoritative for whitelist defaults.

**Tech Stack:** Go/Gin/PostgreSQL, Vue 3/TypeScript/Vitest; no new dependencies or migration.

**Baseline:** dde74da04, clean existing isolated codex/upstream-governance worktree. Contract: companion approved spec.

## Task 1 — Backend template collection (backend owner)
- Files: new import_templates.go, store_import_templates.go and tests under backend/internal/upstreamgovernance; admin handler and existing routes/tests.
- [ ] RED: normalize rejects extra/missing sensitive fields, nonfinite/negative quotas, bad integer ranges, duplicate IDs/defaults, invalid names and exhausted versions. Handler test rejects unknown nested fields and oversized/trailing payloads.
- [ ] Implement typed whitelisted DTO; GET defaults to empty version 0; PUT persists only `upstream_governance_import_templates` under advisory transaction lock plus expected-version check.
- [ ] GREEN: service/store/handler/router tests; random-schema PostgreSQL fixture verifies first-write CAS, update conflict, delete last template, and unrelated settings unchanged.
- Commands: `go test ./internal/upstreamgovernance ./internal/handler/admin ./internal/server/routes -run 'ImportTemplate|GovernanceRoutes' -count=1`.

## Task 2 — Picker/editor and client (new frontend owner)
- Files: new frontend/src/api/admin/upstream-import-templates.ts + spec; ImportTemplatePanel.vue + spec; import-templates.ts + spec in governance views; zh/en governance locales.
- Component props: `{ settings: ImportTemplateSettings; scopeKey: string; pristine: boolean; disabled?: boolean }`.
- Component events: `apply({settings, automatic:boolean})`, `changed()`, `ready(boolean)`, `busy(boolean)` (busy for template writes only).
- [ ] RED: explicit apply vs selector, dirty override confirmation, late defaults skipped, load failure explicit manual continuation, collection-conflict retains name/default/editor and does not auto-rebase, reset on scope change and stale-response suppression.
- [ ] Implement list/read-only loading; new/update/delete/default writes only to template endpoint. Initial success may emit automatic apply only while pristine; explicit reload never auto-applies default. Mutation success emits changed without changing import parameters.
- [ ] GREEN: tests + targeted ESLint/typecheck. Helper converts settings to/from existing ImportAccountConfig by whitelist; it preserves model_mapping when applying settings.

## Task 3 — Draft lifecycle and existing import integration (root)
- Files: ImportPanel.vue/spec, ImportSettingsPanel.vue, ModelTemplatePanel.vue/spec; new narrowly scoped helper if needed.
- [ ] RED: default fills only fresh account settings, manual/template changes invalidate preview, no implicit API preview/apply/keys call, template unavailable can explicitly continue current parameters.
- [ ] RED: new snapshot for same source retains account settings and edited model selections but clears mapping/preview; different site/source resets everything; previous async preview cannot return after parameter edits.
- [ ] Integrate template picker outside preview-only form; pass its readiness and write lock through existing guards. Track identity `[siteId,baseURL,platform,account.user_id]`; remount scoped picker only on identity change, not every snapshot.
- [ ] Relay `edited(platform)` from existing model picker through ImportSettingsPanel; seed preservation flags after same-site remount. Do not change model-template persistence/API.
- [ ] GREEN: ImportPanel, ModelTemplatePanel and View regressions. Keep old default values and real protocol/multigroup validation intact.

## Task 4 — Review, verify, release (root and independent reviewer)
- [ ] Review exact contracts, no implicit side effects, CAS failure behavior, source identity and generation guards; fix findings then re-review.
- [ ] Run governance frontend/i18n tests, typecheck/lint/build; governance backend/regressions/vet and isolated PostgreSQL fixture.
- [ ] Synthetic desktop/narrow/dark browser acceptance of picker/apply confirmation/conflict; no real upstream writes or email tests.
- [ ] Backup and verify DB archive plus binary/config/encryption key; build embedded server, replace only local58089 and verify health/assets/authenticated GET.
- [ ] Update evidence and scoped local commit; no merge/push, no payment or global strategy changes.

## Preflight self-review
- Shared DTO is declared once in spec; owners have disjoint file lists. Root does not modify locales or new client/picker while frontend owner writes them.
- Template writes are separate from account/import writes; loading/default selection never calls preview/apply.
- Account draft preservation does not preserve potentially obsolete group mappings or upstream cost values.
- A changed template collection invalidates only this page's preview; already stored frozen previews continue to represent their original explicit values and are never silently rewritten.
