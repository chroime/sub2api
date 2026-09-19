# Codex Ticket Enhancements Implementation Plan

> For agentic workers: use subagent-driven-development with scoped ownership and a final independent review. The approved comparison recommendations define the scope; proceed without another approval round.

**Goal:** Add credential-bound tickets, optional replay verification, controlled proxy-pool harvesting and native monitoring to B2's two independent mechanisms.

**Architecture:** Extend native Go services and Vue panels. Keep business forwarding independent of background acquisition; use conditional repository updates and bounded monitoring state.

**Tech Stack:** Go, PostgreSQL account extra/settings, Vue 3, TypeScript, Vitest, pnpm.

**Spec:** docs/superpowers/specs/2026-09-20-codex-ticket-enhancements-design.md

## Global constraints

- Preserve 292/332/off, independent settings, existing defaults, ACK/precharge/group concurrency and native WS.
- Verification defaults false; per-mode harvest concurrency defaults 3 and validates 1..16; at most 32 positive unique proxy IDs.
- Use the exact shared interfaces and JSON fields in the spec. No real upstream probes/live setting activation in tests; no external plugin install, group reassignment or hibernation.
- Work on codex/b2-home-branding-20260914; preserve unrelated untracked assets. Runtime/source files have one owner at a time.

## Task 1: Ticket runtime, acquisition and monitoring (runtime agent)

Files: service/openai_codex_ticket*.go, service/openai_gateway_service.go and their ticket-focused tests. Do not edit gateway response call sites, repository, settings or frontend files.

- [ ] Write red regressions using real-format synthetic test tickets with controllable issued times. Test token replacement, stale/malformed formats and legacy missing binding before changing validity logic.
- [ ] Implement strict decoding, OpenAICodexTicketCredentialHash, credential-bound lookup/storage and corresponding test fixture updates.
- [ ] Add replay/model mismatch red tests; implement optional verification on the same proxy, preserving 292 header-only when disabled and strict 332 SSE parsing.
- [ ] Add rotation/expired-proxy/concurrency/429/auth retry red tests; implement per-mode bounded workers, route selection, persistent cooldown and context cancellation.
- [ ] Implement bounded monitor snapshot/events and the private request-use snapshot/observer. The observer calls the production optional InvalidateCodexTicket capability; root supplies actual HTTP/WS hooks.
- [ ] Verify runtime tests and report exact file ownership, types, failure codes and any integration gaps.

## Task 2: Backend settings and monitoring API (settings agent)

Files: config/config.go, service/setting*.go, domain_constants.go, admin settings handlers/DTO, routes/admin.go, handler/wire.go, cmd/server/wire_gen.go, deploy/config.example.yaml and associated tests.

- [ ] Write red tests for independent options, partial updates and invalid concurrency/proxy ID lists.
- [ ] Implement the six settings fields, GetOpenAICodexTicketHarvestOptions and mode config defaults. Use read-through caching/invalidation consistent with current runtime settings; do not mutate the shared base config.
- [ ] Add GetCodexTicketMonitor handler using the runtime snapshot. Bind it at GET /settings/codex-tickets/monitor under existing admin middleware.
- [ ] Wire gateway/proxy repository through existing handler providers/setters and regenerate Wire once dependencies are complete.
- [ ] Verify masking, omitted fields, API contracts and authorization. Report field/method changes to the other workers.

## Task 3: Native controls and monitoring panel (frontend agent)

Files: api/admin/settings.ts, types, CodexTicketSettings.vue, new CodexTicketMonitor.vue, SettingsView.vue, zh/en locales and tests.

- [ ] Write red tests for the new per-mode fields and saving only the selected mode.
- [ ] Add replay toggle, 1..16 harvest concurrency input, and proxy multi-selection using existing admin/proxies API. Keep the existing standalone proxy URL usable and all visible labels free of credentials.
- [ ] Add typed monitor API and status/event UI with current-instance label, mode/account filters, manual refresh and visibility-aware 5-second polling. Test no overlap, unmount cleanup and failure recovery.
- [ ] Verify frontend focused tests, locale parity, typecheck, touched-file ESLint and build.

## Task 4: Repository and forwarding integration (root)

Files: repository/account_repo*.go, scheduler_cache.go/tests, openai_gateway_forward.go/messages.go/passthrough.go, openai_ws_*.go and targeted service integration tests.

- [ ] Add red conditional-removal and scheduler projection tests. Implement InvalidateCodexTicket so state/hash comparison occurs in SQL, and project only a server-derived credential hash.
- [ ] Before each real HTTP send, snapshot the injected use from the actual request headers/model. Observe status/header results using that snapshot; old failures cannot revoke newly captured state.
- [ ] Cover WS handshake and error-frame observations without weakening existing per-turn ticket checks. Preserve lease cleanup and ACK/precharge start/finish boundaries.
- [ ] Verify direct request contexts, effective token identity, private metadata stripping and stale cache behavior.

## Task 5: Integration, review and delivery (root + independent reviewer)

- [ ] Run the completed tree's ticket/WS/compact/ACK/precharge and settings/repository/API regressions; fix actual failures.
- [ ] Run frontend and backend builds/lint; inspect final diff, secrets, conflict markers and unrelated files.
- [ ] Independently review the completed runtime and integration, fix validated issues, then commit/push B2.
- [ ] Gracefully replace the current local process with the new embedded build using existing runtime configuration; verify health/public API and served asset hashes. Keep the computer awake.

## Decisions and progress

Ruling: retain 292 header-only without the new verification option; 332 adds actual-model checking and both modes can opt into full replay verification. This preserves the two source behaviors while offering the requested validation.

Ruling: use existing IP-management proxy IDs and per-mode concurrency; dynamic-provider URLs can remain behind the existing single proxy service. This delivers native pool rotation without adding an external credentialed fetcher or platform-specific SSH/Clash services.

Ruling: legacy unbound tickets must be reacquired rather than silently assigned to current credentials. This can briefly leave a previously enabled fail-closed account waiting for its next successful harvest, but prevents reusing unknown identity material.
