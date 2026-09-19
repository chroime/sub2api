# Codex 292 and 332 ticket coexistence

The user approved integrating both the official v0.2.6 292 implementation and the supplied 332 implementation into B2. Keep both mechanisms identifiable and independently configurable. Preserve B2 ACK, balance precharge, branding, and upstream v0.2.7 features.

## Sources and behavior

- Official 292 source: `49a39b6dc1abed30fd227611e8af1108bc427610` (VERSION-only successor `8b69738d782ccaa7fd26511e1cca26ba8d1b58db`).
- Supplied 332 source: `E:/workspace/agent/sub2api-source-20260919-151351/sub2api`.
- Import only ticket-related changes from the official source, preserving B2 and v0.2.7 code in overlapping files. Retain the server VERSION at 0.2.7.
- Both modes default disabled. Official 292 defaults to fail closed; supplied 332 defaults to fail open.
- 292 harvest validates response status and header without consuming the SSE body. 332 harvest requires a completed SSE without failure/incomplete events, respects rate-limit windows, and retains its preference for ready tickets where applicable.
- Both modes keep account/model isolation, a one-hour local lifetime, ten-minute refresh lead, per-cycle delay, dedicated proxy transport, and HTTP/WS forwarding integration.

## Selection and persistence

One outbound request can carry only one `x-codex-turn-state`. An OpenAI OAuth/setup-token account selects `extra.codex_ticket_mode`: `292`, `332`, or `off`. Absent mode means `292`, matching official behavior when the administrator enables that mode. Unknown explicit values bypass rather than selecting a different mechanism. No automatic account-plan inference is introduced.

Include mode, account ID, and actual outbound model in in-memory and singleflight keys. Persist new material as `codex_turn_ticket:<mode>:<model>`. Read a legacy `codex_turn_ticket:<model>` only when its actual and recorded lengths match the selected mode. Never reuse another mode's ticket. Preserve and redact both namespaces during account edit/export; the raw ticket never appears in account DTOs or logs.

The account status DTO adds `mode` and reports the selected mechanism's model, readiness, remaining time, length, and blocked status. Shadow accounts retain their existing forwarding behavior.

## WebSocket connection policy

A server-injected ticket is part of the upstream WebSocket handshake. Pool compatibility therefore includes a private digest of the selected mode, account, actual model and ticket. Strip that internal marker before dialing; ordinary client-supplied turn-state keeps its existing connection reuse behavior.

Recheck policy before forwarding each generation turn, including turns without a prompt-cache key and passthrough sessions. If the active ticket, model or enabled/account mode no longer matches the live handshake, stop that turn before upstream forwarding and return a retryable reconnect close. Do not silently attach a new ticket to a connection whose handshake used the old one. Preserve existing turn admission and settlement hooks.

## Configuration and UI

Retain official configuration `gateway.openai_codex_ticket` for 292 and add `gateway.openai_codex_ticket_332` using the same Go configuration structure with mode-specific defaults. Length is fixed to each named mechanism; do not combine allowed lengths globally.

Admin JSON fields for 292: `openai_codex_ticket_enabled`, `openai_codex_ticket_fail_closed`, `openai_codex_ticket_harvest_proxy_url`, `openai_codex_ticket_harvest_proxy_configured`.

Admin JSON fields for 332: `openai_codex_ticket_332_enabled`, `openai_codex_ticket_332_fail_closed`, `openai_codex_ticket_332_harvest_proxy_url`, `openai_codex_ticket_332_harvest_proxy_configured`.

Settings persist independently and apply without restart. Preserve omitted fields on update, mask proxy passwords on read/audit, validate proxy URL syntax before persistence, and keep stored proxy credentials when the existing mask or an empty unchanged input is submitted.

Put two clearly labeled mechanism panels in the existing custom-features tab beside ACK/precharge: `Codex 292 · 官方 0.2.6` and `Codex 332 · 导入版本`. Each has its own enable toggle, fail-closed toggle and proxy input. Account edit exposes the mode selection and selected-mode status. Show accurate blocked/allowed text for both fail-closed settings. Provide matching Chinese/English locale keys.

## Verification and delivery

Test mode selection, exact length/prefix/expiry validation, cross-mode/account/model isolation, legacy hydration, harvest differences, cancellation/lifecycle, settings hot reload and partial updates, DTO/export redaction, scheduler gating, compact outbound mappings, and HTTP/WS injection. Re-run existing ACK and precharge tests plus frontend settings/account tests, typecheck/build, and backend build/lint appropriate to touched packages. Use mock upstreams only; enabling harvest or spending real account quota is outside this merge.

Commit the verified integrated result on `codex/b2-home-branding-20260914`, preserving unrelated untracked user files. Existing session authorization covers pushing B2; no force push. Do not restart or deploy the running service as part of this code merge.
