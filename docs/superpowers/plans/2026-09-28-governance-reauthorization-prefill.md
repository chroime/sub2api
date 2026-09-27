# Reauthorization Credential Prefill Implementation Plan

**Goal:** Reuse saved administrator-visible upstream login details in the reauthorization dialog without automatically logging in.

**Architecture:** Reuse the existing login-credentials endpoint and its site version. Bind submitted credentials to that version under the backend site lock, preserving callers that omit it. Guard asynchronous form updates against closing or changing sites.

**Tech Stack:** Vue 3, TypeScript, Vitest, Go, existing local embedded server.

**Spec:** User approved automatic editable credential display; missing or changed credentials require manual entry; upstream verification remains explicit.

## Constraints

- Keep username/password plaintext in the administrator form, as in the existing editor.
- Never automatically submit credentials or test a real upstream login during acceptance.
- Preserve CAPTCHA/TOTP continuation and password whitespace.
- Back up the local database, executable, launcher and encryption key before runtime replacement.

## Tasks

- [x] Add failing component tests for prefill, loading/failure, editable values, stale asynchronous responses and challenge compatibility.
- [x] Add optional expected_site_version; reject stale or invalid versions before remote login, with focused backend tests.
- [x] Implement guarded loading, manual fallback, explicit retry and stale-target reload; update status messages.
- [x] Run component, related onboarding/editor/view tests, typecheck, scoped lint and backend verification.
- [x] Build and replace local runtime after verified backup; compare fingerprints and health.
- [x] Check real browser with synthetic credentials/login fixtures and document results.
- [x] Commit the completed change locally.

## Evidence

- First component run reproduced eight failures for the missing behavior. After implementation, six related test files / 72 tests passed, including existing onboarding and editor flows.
- Governance domain suite passed with isolated PostgreSQL, as did related handlers/routes and governance go vet. New backend tests failed before the version guard and passed afterwards.
- Scoped ESLint, TypeScript production check, frontend build and embedded Go build passed.
- Browser fixtures verified prefill without auto-login, editable whitespace-preserving passwords, explicit login and read retries, CAPTCHA/TOTP continuation, stale-target clearing/reload and 390px mobile layout.
- Backup and release details are recorded in docs/upstream-governance-validation.md. No real upstream test login was sent.
