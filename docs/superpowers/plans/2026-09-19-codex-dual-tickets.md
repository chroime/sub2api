# Codex dual tickets implementation plan

> For agentic workers: execute independent backend runtime, settings persistence, and frontend tasks in parallel after the upstream patch has been applied. The root agent owns patch conflicts, integration review and final verification.

**Goal:** Integrate both official 292 and supplied 332 Codex ticket mechanisms into B2 with separate configuration and storage.

**Architecture:** Port the official ticket patch onto current B2, retaining v0.2.7 and local changes. Add explicit account mode selection and independent 332 runtime settings, then bring the supplied 332 harvest and scheduling behavior into its selected-mode path. Keep common HTTP transport, lifecycle, redaction and persistence protection shared.

**Tech Stack:** Go, PostgreSQL account extra/settings, Vue 3, TypeScript, Vitest, pnpm.

**Spec:** `docs/superpowers/specs/2026-09-19-codex-dual-tickets-design.md`.

## Global constraints

- Modes: `292`, `332`, `off`; missing account mode selects 292, invalid explicit mode bypasses.
- Both mode enable defaults false; 292 fail_closed defaults true, 332 false.
- Preserve B2 ACK, precharge and branding; preserve v0.2.7 VERSION and plugin/Seedance changes.
- Persist/memoize per mode + account + model; never expose state or proxy passwords in DTOs/audit.
- Do not call real model upstreams during tests or activate harvest in existing settings.

## Task 1: Port and reconcile the source patch

- [x] Generate a binary Git patch from `efe9aab1e..8b69738d` excluding VERSION; apply with three-way merge.
- [x] Resolve overlaps by retaining both B2/v0.2.7 and ticket additions in settings, wiring and forwarding files.
- [x] Run imported ticket unit tests to establish the integration baseline.

## Task 2: Runtime coexistence

Files: `backend/internal/service/openai_codex_ticket*.go`, gateway/scheduler forwarding files, `backend/internal/handler/admin/account_handler.go`, `backend/internal/handler/dto/types.go`.

- [x] Add failing cases for choosing 292/332/off, switching modes on one account, cross-model/account isolation and legacy migration.
- [x] Implement `OpenAICodexTicketMode(account *Account) string`, profile resolution, mode-aware keys and status `mode`.
- [x] Keep official header-only probe for 292; consume completed SSE and skip rate-limited accounts for 332.
- [x] Test both modes' HTTP/WS injection, fail-open/closed gating and compact mapping, lifecycle cancellation, and redaction.

## Task 3: Independent settings

Files: `backend/internal/config/config.go`, `deploy/config.example.yaml`, `backend/internal/service/setting*.go`, `domain_constants.go`, admin settings handlers and DTO.

- [x] Add failing tests for independent mode values, partial updates, masking and immediate cache invalidation.
- [x] Add 332 config and the eight JSON fields specified in the design.
- [x] Expose runtime methods `GetOpenAICodexTicket332Enabled`, `GetOpenAICodexTicket332HarvestProxyURL`, `GetOpenAICodexTicketFailClosed` and `GetOpenAICodexTicket332FailClosed`; retain official 292 methods.
- [x] Test defaults, invalid proxy rejection, omission preservation and DTO/audit redaction.

## Task 4: Admin UI

Files: frontend settings API/types/locales, SettingsView, EditAccountModal, AccountUsageCell and associated tests.

- [x] Add settings tests for two independent mechanisms and account mode/status rendering.
- [x] Move ticket controls to custom-features tab; show independent enable/fail-closed/proxy controls for each mechanism.
- [x] Add account mode selector that stores `extra.codex_ticket_mode` and explain missing-mode default.
- [x] Render actual 292/332 mechanism identity and correct blocked/allowed state; verify Chinese/English key parity.

## Task 5: Integration and delivery

- [x] Review source diff for accidental B2/v0.2.7 loss, mixed profile keys, token/proxy leakage and lifecycle cleanup.
- [x] Run backend targeted unit/package tests, ACK/precharge regressions, frontend relevant tests, typecheck/build and lint; fix failures.
- [x] Verify clean diff formatting and absence of unresolved merge markers.
- Delivery: commit only task files to B2, push without force, verify local/remote commit equality and report exact checks and limitations.

## Verification record — 2026-09-20

- Baseline B2: `83ed62d23cfbd7894b556182df5e80d85e000b2a` (upstream v0.2.7 already integrated). Server VERSION remains 0.2.7.
- Backend full unit-tag package tests passed for `internal/handler/admin`, `internal/handler/dto`, `internal/repository`, `internal/config`, `internal/server`, and `cmd/server`.
- Targeted service and handler regressions passed for both ticket modes, HTTP and WS forwarding, WebSocket pools and multi-turn sessions, compact/runtime scheduling, streaming ACK, balance precharge, billing maintenance and usage recovery/outbox. This is a targeted regression run, not a claim that every backend unit test was executed successfully.
- Final WS tests passed after the review fixes: ticket tests, WS prewarm tests, parent-context cancellation, and dual-ticket forwarding. Both native and passthrough paths cover 292/332 mode, mapped model, refresh, enable/disable, expiry and unchanged-ticket continuation, plus retryable close type and settlement hooks.
- Independent reviews found and verified fixes for compact-model gating, SSE failure events overriding JSON event types, harvester stop cancellation, WS pool ticket isolation, later-turn checks, prewarm checks, and internal-header casing.
- Frontend: 196 tests passed across settings, account editing/status, dual-ticket panels and locale parity; typecheck, touched-file ESLint and production build passed.
- Backend with embedded frontend built successfully; running the artifact with `-version` printed 0.2.7. No running service was restarted.
- Final `golangci-lint run --timeout 5m --new-from-rev=HEAD ./...` passed with 0 issues after fixing three test-only errcheck findings.
- Tests use local simulated upstreams. No real model requests, ticket activation, database configuration changes or balance changes were performed.
- Go's race detector could not run in this environment because `CGO_ENABLED=0`; standard concurrent/lifecycle unit tests passed.

Local verification logs are under `output/compare-026/` and `output/dual-ticket-frontend-*.log`; generated output and unrelated local image assets are excluded from the code commit.
