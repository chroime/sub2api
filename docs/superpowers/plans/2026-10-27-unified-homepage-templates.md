# Unified Homepage Templates Implementation Plan

> **For agentic workers:** Execute inline using superpowers:executing-plans. User approved implementation of the preceding design; continue through verification and integration without repeating approval questions.

**Goal:** Consolidate b2 into main while allowing administrators to select either existing homepage or the compact/custom modes at runtime.

**Architecture:** The existing settings store publishes a validated `homepage_template`. One renderer supplies shared public site/auth data to lazy-loaded templates. Admin settings offer a selection, conditional custom editor, and a separate authenticated preview of a browser-local draft.

**Tech Stack:** Go, PostgreSQL settings store, Vue 3, TypeScript, Vitest, Tailwind, Docker Compose, Bash.

**Spec:** `docs/superpowers/specs/2026-10-27-unified-homepage-templates-design.md`

## Global constraints

- Use only `classic`, `brand`, `compact`, `custom` for `homepage_template`.
- Retain existing service data directories and all user functionality.
- Preserve pending source changes in reviewed commits; exclude temporary logs, binaries, credentials and fixture data.
- Use the existing main worktree for integration and restore the primary workspace to main for local testing at completion.
- Backup precedes production settings or deployment changes. No database merge or branch deletion.

## Task 1: Preserve and integrate existing work

- [x] Create a filesystem snapshot of modified/untracked source and record both branch tips.
- [x] Run focused balance-notification and ranking checks, commit the two existing deliverables separately, then commit this design and plan.
- [x] Create a b2 archive tag, merge b2 into the clean existing main worktree, and preserve both homepage designs; the merge was conflict-free and the following tasks make both designs selectable.

## Task 2: Persist and publish homepage selection

**Files:** `backend/internal/service/domain_constants.go`, `settings_view.go`, `setting_parse.go`, `setting_public.go`, `setting_update.go`, new `homepage_template.go` and tests; settings request/response/audit mappings under `backend/internal/handler`.

**Interfaces:** `ResolveHomepageTemplate(template, content string, compact bool) string`; constants `HomepageTemplateClassic`, `HomepageTemplateBrand`, `HomepageTemplateCompact`, `HomepageTemplateCustom`; field `HomepageTemplate string` in service/DTO/request structures; key `SettingKeyHomepageTemplate`.

- [x] Test explicit selection overriding legacy fields, legacy custom/compact fallbacks, default classic, API/HTML parity, invalid enums, and omitted-field preservation.
- [x] Add the enum resolver and settings mappings. Validate nonempty enum values at the service boundary; reject an explicitly empty request field in the handler while allowing omitted fields.
- [x] Include the key in public reads/injection and audit changed-field lists. Preserve stored selection on partial and old-client updates.
- [x] Run focused Go settings tests and compile all modified packages.

## Task 3: Select and render templates

**Files:** `frontend/src/views/HomeView.vue`; new `components/home/HomepageRenderer.vue`, `BrandHome.vue`, `CompactHome.vue`, `CustomHome.vue`; existing `PublicHomeExperience.vue`; new `utils/homepageTemplate.ts`; settings types/store.

**Interfaces:** `HomepageTemplate = 'classic' | 'brand' | 'compact' | 'custom'`; `resolveHomepageTemplate(settings): HomepageTemplate`; `HomepageRenderer` accepts a partial public-settings snapshot and reads shared auth/visibility rules.

- [x] Test explicit selection precedence and fallback from existing legacy settings.
- [x] Extract the existing b2 markup into BrandHome, retain PublicHomeExperience for classic, extract compact/custom views, and lazy-load only the selected presentation.
- [x] Centralize site branding, destinations and model-plaza visibility. Keep template styles prefixed/scoped and provide compact fallback for load errors.
- [x] Update HomeView tests for configured templates and verify no unnecessary public pricing requests in compact/custom/classic modes.

## Task 4: Admin selection and preview

**Files:** new `components/admin/settings/HomepageSettings.vue`, `utils/homepagePreview.ts`, `views/admin/HomepagePreviewView.vue` and tests; `SettingsView.vue`, settings types, `router/index.ts`, locale files.

**Interfaces:** HomepageSettings models the selection and custom content and receives the public draft fields. `createHomepagePreview` stores one validated draft with ID/30-minute expiry; `readHomepagePreview` returns only the matching nonexpired draft.

- [x] Test selection, custom editor visibility, no saving during preview, draft expiry/ID validation, and exclusion of secret fields.
- [x] Replace the old custom/compact controls with the new component and include the selection in settings load/save.
- [x] Add the authenticated administrator preview route, public-field draft helper, and translated error/help text.
- [x] Run focused Vue tests, locale completeness and type checking.

## Task 5: Deployment compatibility

**Files:** `deploy/update-sub2api.sh`, deployment tests and a homepage rollout guide.

- [x] Change the default update branch to main and initialize missing homepage selection after verified backup but before switching branch.
- [x] Derive legacy mode from custom content, compact flag, then the old branch; retain existing explicit values with an idempotent insert.
- [x] Handle changing log files narrowly during the data archive, and keep all backup verification/failure checks.
- [x] Test branch default, precedence/idempotence and backup-before-write ordering with shell/SQL fixtures.

## Task 6: Verify and finish integration

- [x] Run the relevant Go packages, frontend suite/typecheck/build, and production-style browser checks for all four modes, preview and both themes.
- [x] Commit the completed feature on main, switch the primary workspace to main without disturbing ignored data, and restart local services with the existing configuration/data directory.
- [x] Confirm settings persistence and unchanged installation state using local HTTP APIs.
- [x] Publish main and the archive tag, then use a verified backup and explicit previous homepage selection for any authorized server rollout. Keep b2 as a frozen fallback branch.
- [x] Report final commits, tests, deployment state and remaining concrete limitations.

## Verification recorded on 2026-10-28

- Frontend: all 481 files / 4356 tests passed; production build (including locale completeness and TypeScript) passed.
- Go: service, admin/public handler, DTO, route and balance-notification packages passed with the unit build tag. Two pre-existing test assumptions were isolated/aligned without changing application behavior.
- Deployment script syntax and safety helpers passed. PostgreSQL exercised nine legacy/explicit cases and repeated initialization in a temporary table.
- Real-browser checks used the embedded production build: four templates at 1440/390 px in light/dark themes, unsaved preview, admin guard, public HTML/API agreement, partial-update preservation and invalid selection rejection.
- Local database backup: `.tmp/backups/before-unified-main-20261027T155850Z`. Original `backend/data/config.yaml` and `.installed` hashes unchanged; selected template restored to `brand` after persistence tests.

## Rollout outcome

- Feature commits: `70ec0c628` (runtime template selection/preview) and `c32043ee0` (preserve b2 header/button sizing and validate runtime permissions).
- Both archive tags and main were pushed. The primary workspace now uses main; b2 remains a frozen history reference.
- Local dev: frontend `127.0.0.1:5173`, backend `127.0.0.1:18080`, original `E:/workspace/agent/sub2api/backend/data`. Template remains `brand`; config and installation-marker hashes match the pre-update snapshot.
- Server 40: updated through `/home/sub2api/deploy/update-sub2api.sh`; runtime source `c32043ee0`; branch main, template brand, healthy service. Original environment values, ports and data mounts compare equal before/after.
- Verified deployment backup: `/home/sub2api/backups/pre-update-20261027T163344Z`. Previous application image retained as `sub2api-local:before-main-unification-20261028-r2`.
- First rollout detected an entrypoint read-permission failure and restored the previous image without restoring/replacing the database. Dockerfiles now give the entrypoint explicit 0755 permissions; the root-owned browser helper is readable by the service user. The updater tests access and binary startup as that user before recreating the application container. The subsequent rollout passed these checks.
- External HTTP verification: xenoai.eu.cc and us.xenoai.eu.cc homepage/public settings/health return 200, template is brand, and installation is complete.
- Final brand presentation checks confirm the original story header and 42px primary-button minimum height. The relevant 17 UI tests and production build passed after this restoration; deployment shell/runtime checks also pass.
