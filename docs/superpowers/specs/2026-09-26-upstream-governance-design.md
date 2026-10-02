# Built-in upstream governance

The user approved this native administrator feature and, on 2026-09-26, confirmed that discovery and alerts run automatically while local configuration changes require a preview and explicit administrator application.

## Product contract

Add `/admin/upstream-governance` beside account management. The page manages upstream sites, their ordinary user sessions, user-visible groups/rates/models/pricing, import mappings and change/health events. A site is not a local Account, Group or Channel.

Connect Sub2API and New API sites using username/password, supported two-factor continuation, or an explicitly supplied dashboard session token. Passwords are transient and never persisted. Persist the resulting session separately under existing AES-GCM encryption, only when the configured encryption key is durable. Expired/revoked sessions enter `reauth_required`; collection does not silently change credentials or bypass challenges. Captcha requirements are surfaced as an interactive authorization requirement, with a session-token alternative. No whole-site administrator key is required.

Discover only resources visible to that upstream user. Sub2API user group rates replace group default rates rather than multiplying them. Keep declared base rates, overrides, resolved rates, peak rules, price units and source information separate. Missing or unsupported pricing is unknown, never zero. A failed or malformed discovery preserves the last successful catalog. Changes include group additions/removals, model additions/removals, rate/price changes and channel changes.

## Import workflow

Select remote groups and existing local groups. Initial supported inference transports are OpenAI, Anthropic and Gemini; a mixed upstream group requires an explicit transport choice. Show a frozen preview containing destination group, existing/new account, account cost multiplier, affected models, upstream-key creation and warnings. Never silently change local sale prices or channel pricing; those remain editable in their existing administrator pages. The governance apply operation changes only the selected imported account and its group binding after confirmation. Upstream cost and local sale prices are labelled independently.

Persist the preview server-side for 15 minutes. Applying verifies site version, latest successful snapshot and existing account fingerprint. Concurrent refresh/apply/login operations on a site are serialized across server processes using a PostgreSQL advisory lock. A stale preview returns conflict. A repeated application returns its recorded result or resumes incomplete items. Each `(site, remote group, transport)` owns one stable import marker; recover an account created before an interrupted result write by this marker. Do not overwrite unrelated accounts. Each confirmed group may create/reuse a clearly named upstream inference key; discover/preview never creates keys. Persist an acquired key encrypted before creating the local account. Partial success is returned per item, with no fake rollback of remote side effects.

## Monitoring

Site discovery polling defaults to 15 minutes and can be disabled, with allowed intervals 5–1440 minutes. The worker wakes once per minute and processes a bounded number of due sites with cross-process locking. No polling mutates local routing, account cost or sale prices. Events can be acknowledged. Preserve the latest catalog on errors and record connectivity/auth/contract failures separately.

Provide an explicit active stability check for imported bindings with a chosen text model. Scheduled checks are opt-in per binding, default interval 30 minutes, minimum 15 minutes. Use a minimal text request, a bounded output budget and at most two concurrent upstream requests. Health records store success, latency and sanitized error categories. HTTP 200 without valid text output is not success. These probes do not call the existing account test path, which can mutate account throttling state. No automatic pause/recovery or routing changes.

## Data and API boundaries

Use focused `internal/upstreamgovernance` domain, connector, repository and orchestration files. Add a forward-only SQL migration. Site sessions and imported remote keys are encrypted and omitted from JSON. SQL keeps sites, complete snapshots, bindings, previews, events and check records; bounded history is retained. Existing Account creation/update APIs and invalidation are used through a narrow local adapter. Add a partial unique index for the internal import marker to prevent duplicate accounts after crashes.

All endpoints live under existing authenticated/audited `/api/v1/admin/upstream-governance`. Collection uses public HTTPS origins only, no URL credentials/fragments/query, no redirects, bounded bodies/timeouts, fixed adapter paths, and the existing upstream public-host guard. A selected proxy is used explicitly with no fallback to direct. Refuse collection without a durable encryption key; do not prevent unrelated server startup.

API routes: `GET/POST /sites`, `PUT/DELETE /sites/:id`, `POST /sites/:id/connect`, `POST /sites/:id/sync`, `GET /sites/:id/catalog`, `GET /sites/:id/bindings`, `POST /sites/:id/previews`, `POST /sites/:id/previews/:preview_id/apply`, `GET /sites/:id/events`, `POST /sites/:id/events/:event_id/ack`, `GET /sites/:id/checks`, `POST /sites/:id/bindings/:binding_id/check`, `PUT /sites/:id/bindings/:binding_id/monitor`. Standard application response envelopes wrap domain DTOs. Lists of sites/bindings are bounded; events/checks paginate.

## Validation and delivery

Use invented credentials and local fixture servers to test platform envelopes, group overrides, challenges, hidden secrets, redirect/body limits, failure preservation, diffs, stale/duplicate applies, partial retries, lock conflicts and health semantics. Test SQL persistence with sqlmock and PostgreSQL integration where available. Test frontend connection, selection, preview-confirm-apply, loading/errors, stale previews and locale parity. Run targeted Go tests, frontend typecheck/lint/build and a browser flow against isolated local fixtures. No production changes or live upstream accounts are part of implementation validation.
