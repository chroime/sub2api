# Native Codex ticket enhancements

The user accepted the recommendation to absorb the useful state-reuse enhancements into B2. Implement them in the existing native 292/332 mechanisms. The comparison baseline is external commit 19500291c4f3a76544dd1058ed54ae67f392e3c6; B2 starts at 57c701e32feb87397c7ed98dbc524e2c99cefa1e.

## Scope and compatibility

- Keep explicit 292/332/off account selection, independent configurations/storage and existing fail-open/closed defaults. Both global mechanisms remain disabled unless already enabled by the administrator.
- Preserve account/group/business concurrency, ACK, precharge, normal billing and native WebSocket behavior. Do not add automatic group reassignment or install the external plugin/collector.
- Add independent optional replay verification, proxy pool selection from IP management, and a per-mode harvest concurrency limit (default 3, range 1..16).
- Existing standalone harvest URL remains usable. Selected proxy IDs augment its route list; disabled/expired/missing proxies are skipped. At most 32 unique positive IDs. Choose one route per attempt, retain successful route preference and rotate after a failed attempt. Capture and verify use the same route.
- Verification defaults false for compatibility. 292 without verification keeps the official header-only probe. Enabling verification requires both initial and replay responses to finish successfully and report the exact requested model. 332 always validates full SSE and actual model, retaining its strict malformed-frame/error/status and 1 MiB protections.
- Business requests never trigger synchronous harvest. No real model requests or live feature activation during implementation tests.

## Ticket validity and credentials

Strictly decode URL-safe Base64, require exact 292/217 or 332/249 encoded/decoded lengths for the selected mode, version byte 0x80 and a plausible embedded issue time. Allow at most 30 seconds future skew. Expiry is the earlier of issue time + configured TTL and issue time + 3570 seconds. Never extend upstream age by locally recapturing old material.

Tickets include credential_hash (SHA256 of access token + colon + ChatGPT account ID), issued_at, captured_at, expires_at, and verification status. Account edits and token rotation cannot reuse material bound to a different credential. Legacy records missing binding are retained as data but require recapture before use. Scheduler metadata carries only a server-derived hash (_codex_ticket_credential_hash), never raw OAuth tokens; full request forwarding rechecks the effective outbound identity.

## Failure and response handling

- Keep cooldown/auth state per mode + account + model + credential. Persist the bounded latest runtime record in account extra under codex_ticket_runtime:<mode>:<model>; redact/protect it like ticket material.
- 429 keeps a usable ticket, stops this attempt and delays further harvest for at least 300 seconds, respecting longer Retry-After/reset hints. Network/probe/verification failures have a bounded retry delay, allowing a different proxy on the next attempt.
- 401/403 suspend harvest for the current credential. Credential changes establish a fresh state. Business auth failures revoke only the exact ticket used by that request, never a newer replacement. An in-memory tombstone prevents stale account snapshots resurrecting that ticket; the repository removes the matching persistent record with a conditional update.
- Response hooks record the ticket actually used before sending and observe HTTP/WS handshake/error responses. Observation failures must not corrupt business response/billing and must be bounded. Ordinary client-supplied turn-state is not treated as a server-issued ticket.
- Preserve injected-use identity across an intervening refresh, including HTTP compatibility bridges and WebSocket prewarm errors. Response persistence uses a bounded, coalescing retry queue with at most eight workers; saturation does not discard a pending observation, and shutdown cancels/drains workers within a bounded interval.
- Persist runtime under an account-row lock: recheck the current credential, OR same-credential authentication blocks and retain the longer cooldown. This protects state across instances as well as within one process. Hydrate validated persisted runtime before subsequent response observations can overwrite it.

## Monitoring

Add GET /api/v1/admin/settings/codex-tickets/monitor behind the existing administrator middleware. Return current-instance snapshots and at most 300 recent events, plus at most 2048 latest state records; bound/prune maps and avoid unbounded per-request logs or new log files. Include capture/verify/persist/cooldown/auth/revoke outcomes and aggregate usage counts, without raw state, credential hashes, authorization or proxy URLs/passwords.

Snapshot shape: updated_at, states, events. State fields: mode, account_id, account_name, model, status, phase, proxy_id (optional), proxy_name, length, expires_at (optional), next_attempt_at (optional), last_error, updated_at, uses, last_used_at (optional). Event fields: id, time, mode, account_id, account_name, model, phase, status, proxy_id (optional), proxy_name, http_status, length, error_code, next_attempt_at (optional). Fixed error codes are translated in the UI; never return raw transport errors.

The native panel below the two mode cards shows status, route, last failure, expiry, next attempt and recent events, with mode/account filtering, manual refresh and 5-second polling. Pause polling when hidden/unmounted, prevent overlapping requests and preserve the last successful display on a transient failure. Label the view as current instance. No manual retry that bypasses authentication/429 cooldown.

## Shared interfaces

Settings add the following three suffixes to each existing mode prefix: verify_enabled, harvest_proxy_ids, harvest_concurrency. All partial saves preserve omitted values; proxy secrets remain masked.

```go
type CodexTicketHarvestOptions struct {
    VerifyEnabled bool
    ProxyIDs []int64
    Concurrency int
}
func (s *SettingService) GetOpenAICodexTicketHarvestOptions(ctx context.Context, mode string, fallback CodexTicketHarvestOptions) CodexTicketHarvestOptions
func OpenAICodexTicketCredentialHash(account *Account) string
func (s *OpenAIGatewayService) SetCodexTicketProxyRepository(repo ProxyRepository)
func (s *OpenAIGatewayService) GetOpenAICodexTicketMonitor() OpenAICodexTicketMonitorSnapshot

// Service-private request snapshot and response observer, used by gateway hooks.
func (s *OpenAIGatewayService) snapshotOpenAICodexTicketUse(ctx context.Context, account *Account, model string, headers http.Header) *openAICodexTicketUse
func (s *OpenAIGatewayService) observeOpenAICodexTicketUse(ctx context.Context, use *openAICodexTicketUse, status int, headers http.Header)

// Optional repository capability implemented by the production account repository.
func (r *accountRepository) InvalidateCodexTicket(ctx context.Context, accountID int64, mode, model, state, credentialHash string) (bool, error)
func (r *accountRepository) UpdateCodexTicketRuntime(ctx context.Context, accountID int64, mode, model string, runtime map[string]any) error
```

Config OpenAICodexTicketConfig adds VerifyEnabled bool, HarvestProxyIDs []int64 and HarvestConcurrency int with mapstructure suffixes above. The runtime agent owns the snapshot/use types and serialized monitoring structs. The settings agent owns settings/config, admin endpoint and wiring. Root owns repository conditional removal, metadata projection and actual response hook call sites.

## Validation and delivery

Use meaningful failing regressions for credential changes, embedded timestamps, rejected replay/model mismatch, late auth failures after refresh, 429 retention/Retry-After, bounded harvest concurrency, route rotation, expired proxies, persistent cooldowns, monitoring redaction/bounds and UI partial saves/poll cleanup. Run existing ticket/WS/compact/ACK/precharge regressions, settings and repository packages, frontend tests/typecheck/build, backend lint/build, then inspect the full diff. Keep unrelated local assets and the comparison checkout out of the commit. Deliver on the existing B2 branch under the session's commit/push authorization. Switch the local service to the verified new build at the end; do not hibernate (the user cancelled that request).
