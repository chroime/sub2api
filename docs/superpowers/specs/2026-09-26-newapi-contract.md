# New API Ordinary-User Contract Evidence

Date: 2026-09-26. This document is a source inspection, not a live-site test.
Only unauthenticated public GitHub source was downloaded. No real upstream,
account, password, cookie, or API credential was accessed or used.

## Pinned Sources

- Repository: `QuantumNous/new-api`.
- Current `main` at inspection: `c2b7a9a9e0b548c2051a949fceabb59029adcb49`, committed 2026-09-25T04:43:11Z.
- Current source cache: `C:/Users/Cracker/AppData/Local/Temp/codex-upstream-contract/new-api-c2b7a9a9e0b548c2051a949fceabb59029adcb49`.
- Legacy authentication comparison: `0d5995eb63f8801d32eb32fbe74b75b68752bfa9`, committed 2026-07-05T05:35:10Z.
- Legacy source cache: `C:/Users/Cracker/AppData/Local/Temp/codex-upstream-contract/new-api-legacy-0d5995eb63f8801d32eb32fbe74b75b68752bfa9`.
- Authentication migration commit: [31d70fca393ff2e09bbae012af2e3ccefdd389a1](https://github.com/QuantumNous/new-api/commit/31d70fca393ff2e09bbae012af2e3ccefdd389a1), 2026-07-20, "replace dashboard sessions with stateless tokens and session control".

All JSON below uses invented values. Response examples are field excerpts unless
explicitly identified as the complete success response.

## Authentication Is Versioned

### Current Bearer Profile

[middleware/auth.go:158](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/middleware/auth.go#L158)
reads `Authorization` and classifies either a dashboard access token or a user's
management personal access token (PAT). Dashboard tokens are checked against a
live server-side login session. Ordinary inference keys from `/api/token/` are a
different credential class and must not be used as dashboard credentials.

Use `Authorization: Bearer <dashboard-access-token-or-management-PAT>` for the
ordinary-user endpoints below. `New-Api-User` is not read by this current
authentication implementation. PAT requests have no browser `SessionID` and
cannot use endpoints that explicitly require a browser session.

Browser login issues both a bearer access token in JSON and an HttpOnly
`new_api_refresh` cookie. The refresh cookie alone does not authenticate ordinary
dashboard requests. Its path is `/api/user/auth`, SameSite is Strict, and Secure
depends on the server setting. See
[service/auth_session.go:308](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/service/auth_session.go#L308).

`POST /api/user/auth/refresh` consumes that cookie and rotates it. Optional
`X-Auth-Session: <expected-sid>` detects session mismatch. Secure-cookie mode
requires a permitted `Origin` or `Referer`; do not silently retry a rejected
origin or switch credential schemes. See
[controller/auth_session.go:17](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/auth_session.go#L17)
and [middleware/auth_origin.go:17](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/middleware/auth_origin.go#L17).

### Legacy Cookie Profile

The pinned legacy
[middleware/auth.go:37](https://github.com/QuantumNous/new-api/blob/0d5995eb63f8801d32eb32fbe74b75b68752bfa9/middleware/auth.go#L37)
first reads `gin-contrib/sessions`, then falls back to `Authorization` as a
management access token if no logged-in session exists. Both paths require
`New-Api-User: <numeric-user-id>` and compare that ID with the authenticated user
at [line 96](https://github.com/QuantumNous/new-api/blob/0d5995eb63f8801d32eb32fbe74b75b68752bfa9/middleware/auth.go#L96).

Legacy password login stores a session cookie and returns `data.id`,
`data.username`, `data.role`, `data.status`, and `data.group`, with no dashboard
access token. Legacy 2FA returns `data.require_2fa: true` and stores a pending
identity in the cookie session. Its second step accepts `{"code":"123456"}`
with the same cookie jar. Evidence:
[controller/user.go:75](https://github.com/QuantumNous/new-api/blob/0d5995eb63f8801d32eb32fbe74b75b68752bfa9/controller/user.go#L75),
[controller/user.go:138](https://github.com/QuantumNous/new-api/blob/0d5995eb63f8801d32eb32fbe74b75b68752bfa9/controller/user.go#L138),
[controller/twofa.go:398](https://github.com/QuantumNous/new-api/blob/0d5995eb63f8801d32eb32fbe74b75b68752bfa9/controller/twofa.go#L398).

The `Auth-Version` response header is **not a reliable discriminator**: both
pinned profiles emit `864b7076dbcd0a3c01b5520316720ebf`. Do not use that header to
infer cookie versus bearer authentication.

## Current Login And 2FA

Routes are defined at
[router/api-router.go:77](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/router/api-router.go#L77).

1. Inspect `/api/status` authentication settings before attempting login.
2. `GET /api/user/login/encryption-key` returns `data.enabled`; when enabled it also returns `data.kid` and PEM `data.public_key`.
3. `POST /api/user/login` accepts `{"username":"example-user","password":"example-password"}` only when plaintext password mode is enabled.
4. If password encryption is enabled, send `username`, `password_encrypted`, and `encryption_key_id`, not plaintext `password`.
5. If Turnstile is enabled, its proof is the `turnstile` query parameter, not a JSON body property.

Evidence:
[controller/user.go:30](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/user.go#L30),
[web/src/features/auth/api.ts:51](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/web/src/features/auth/api.ts#L51),
[middleware/turnstile-check.go:16](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/middleware/turnstile-check.go#L16).

An implementation that does not implement the upstream encrypted-password
protocol must return an explicit unsupported-authentication error. It must not
fall back to sending plaintext, omit an enabled CAPTCHA, or repeatedly retry a
password. Prefer user-provided management PAT support for the first release.

Successful current login, excerpt:

```json
{"success":true,"message":"","data":{"access_token":"<secret>","token_type":"Bearer","access_expires_at":1790380800,"session":{"sid":"<session-id>","current":true},"user":{"id":42,"username":"example-user","group":"default"}}}
```

[controller/user.go:198](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/user.go#L198)
defines that response. `access_expires_at` and session timestamps are Unix
seconds. Refresh tokens are only in `Set-Cookie`, not the JSON response.

A successful primary-password check can instead return this challenge:

```json
{"success":true,"data":{"require_verification":true,"flow_token":"<ephemeral-secret>","expires_at":1790380200,"methods":[{"method":"2fa","available":true}]}}
```

This is not authenticated success. The challenge lifetime is five minutes.
`POST /api/user/login/2fa` delegates to the same handler as
`POST /api/user/login/verify`; body:

```json
{"flow_token":"<ephemeral-secret>","method":"2fa","code":"123456"}
```

`method` may be omitted and defaults to `2fa`. Only use an offered, available
method. Passkey-only challenges require an interactive flow and should be
reported as unsupported in a noninteractive adapter. Evidence:
[service/login_verification.go:11](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/service/login_verification.go#L11),
[controller/login_verification.go:17](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/login_verification.go#L17),
[controller/twofa.go:171](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/twofa.go#L171).

## User-Visible Groups And Models

Use authenticated `GET /api/user/self` to establish and verify the current
identity. Then use authenticated `GET /api/user/self/groups`, not the public
`GET /api/user/groups` route, to discover user-visible groups.

```json
{"success":true,"message":"","data":{"default":{"ratio":1,"desc":"Default"},"vip":{"ratio":0.8,"desc":"VIP"},"auto":{"ratio":"\u81ea\u52a8","desc":"Automatic"}}}
```

The numeric `ratio` already uses `GetUserGroupRatio(userGroup, groupName)`.
`auto.ratio` is the string shown above, not a numeric multiplier. Do not coerce
it to zero, one, NaN, or a guessed group multiplier. Group keys are strings, not
local Sub2API numeric Group IDs. Evidence:
[controller/group.go:26](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/group.go#L26).

`GET /api/user/models?group=vip` returns:

```json
{"success":true,"message":"","data":["example-model-a","example-model-b"]}
```

No `group` means the union of user-usable groups. `group=auto` uses the user's
auto groups if selectable. An unknown or forbidden group yields an empty model
list, not necessarily an error. Validate requested groups against the previous
authenticated groups snapshot. Evidence:
[controller/user.go:614](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/user.go#L614).

## Pricing

`GET /api/pricing` may be public, require authentication, or be disabled by site
navigation policy. Always send the chosen user's valid dashboard credential
when collecting a governance snapshot. Its envelope has sibling fields outside
`data`; a generic helper that keeps only `data` loses group ratios.

```json
{"success":true,"data":[{"model_name":"example-model","quota_type":0,"model_ratio":1,"model_price":0,"owner_by":"example","completion_ratio":3,"enable_groups":["default"],"supported_endpoint_types":[]}],"vendors":[],"group_ratio":{"default":1},"usable_group":{"default":"Default"},"supported_endpoint":{},"auto_groups":["default"],"pricing_version":"a42d372ccf0b5dd13ecf71203521f9d2"}
```

Current pricing is filtered by user-visible groups. Fields can additionally
include cache/image/audio ratios, `billing_mode`, `billing_expr`, and plugin
variants. They are not all representable as one Sub2API rate multiplier.
`quota_type=0` is ratio-based token pricing and `quota_type=1` is per-request
pricing in the inspected source. The upstream UI derives ordinary input USD
per million tokens as `model_ratio * 2 * group_ratio` and output as that value
times `completion_ratio`; do not apply this formula to expression/plugin modes.
Preserve raw pricing as observed metadata until a supported conversion is
explicitly selected and confirmed.

Evidence:
[controller/pricing.go:38](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/pricing.go#L38),
[model/pricing.go:28](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/model/pricing.go#L28),
[model/pricing.go:347](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/model/pricing.go#L347),
[web/src/features/pricing/lib/price.ts:63](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/web/src/features/pricing/lib/price.ts#L63),
[middleware/header_nav.go:112](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/middleware/header_nav.go#L112).

## Inference Token Management

All routes below are ordinary-user routes under `UserAuth`, not administrator
routes. Evidence:
[router/api-router.go:273](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/router/api-router.go#L273).

| Method and path | Contract |
| --- | --- |
| `GET /api/token/?p=1&page_size=100` | `data` is `{page,page_size,total,items}`. Each `items[].key` is masked. |
| `GET /api/token/search?keyword=...&p=1&page_size=100` | Same pagination shape; do not put secret tokens into query strings. |
| `GET /api/token/:id` | One masked token belonging to the authenticated user. |
| `POST /api/token/:id/key` | No required body; returns `{success:true,message:"",data:{key:"sk-..."}}`; secret read is audited. |
| `POST /api/token/` | Creates an inference token; complete success body is only `{success:true,message:""}`. No ID or key is returned. |

Current create request example:

```json
{"name":"governance-example-operation-id","expired_time":-1,"remain_quota":1000,"unlimited_quota":false,"model_limits_enabled":false,"model_limits":"","allow_ips":"","group":"vip","cross_group_retry":false,"auto_groups":[]}
```

`remain_quota` is an upstream integer quota unit, not directly USD.
`expired_time=-1` means never expires. For a finite expiry use Unix seconds.
`model_limits` is a comma-separated string. `auto_groups` is relevant only for
`group=auto`; the upstream normalizes cross-group retry off for other groups.
Do not choose unlimited quota or nonexpiring tokens without explicit UI intent.

Create operations need a unique operation name and a pre/post list comparison
to find exactly one newly created ID. On timeout or ambiguity, stop and require
reconciliation; there is no observed idempotency key or returned ID to make a
blind retry safe. Retrieve the full key only after the user confirms import,
store it server-side, and never import the list's masked value.

Evidence:
[controller/token.go:55](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/token.go#L55),
[controller/token.go:130](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/token.go#L130),
[controller/token.go:187](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/token.go#L187),
[controller/token.go:278](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/token.go#L278),
[model/token.go:14](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/model/token.go#L14),
[common/page_info.go:41](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/common/page_info.go#L41).

## Fail-Closed Implementation Rules

- Treat HTTP status and JSON `success` independently. HTTP 200 with `success:false` is failure. Login challenges are pending, not success.
- Pin adapter capabilities and fixture-test each supported profile. An unrecognized login response, unsupported encryption, CAPTCHA, or verification method stops that flow without trying weaker authentication.
- Verify `/api/user/self` identity before reading personal groups or tokens. Never infer identity only from a submitted `New-Api-User` header.
- Do not silently fall back from authenticated group/pricing reads to public defaults, admin endpoints, or a different credential type. A 404/405 means unsupported capability; a 401 means reconnect; a 403 means denied or disabled.
- Preserve a previous snapshot as stale on failure; never replace it with an empty successful snapshot. Bound pages, page size, total items, response bytes, timeouts, and retry count.
- Keep site credentials scoped to one validated HTTPS origin, disable redirects, reject unsafe destinations, and keep cookie jars separate for each connection. Never mix management PATs, dashboard sessions, and inference keys.
- Do not log raw requests/responses containing login credentials, challenges, cookies, PATs, or retrieved inference keys. Store only required secrets using the local encrypted-secret facility.
- Read-only collection must not generate/revoke a management PAT or create/update/delete inference tokens. Remote writes and local imports require an explicit action and confirmation; ambiguous writes are not retried automatically.
- The current `GET/POST /api/user/token` management-PAT generation endpoint requires a security proof. Do not call it as an automatic post-login step. See [controller/access_token.go:26](https://github.com/QuantumNous/new-api/blob/c2b7a9a9e0b548c2051a949fceabb59029adcb49/controller/access_token.go#L26).

This evidence verifies the pinned source, not every deployed New API fork or
version. Live compatibility remains unverified until a user-authorized
connection is tested against the supported adapter contract.
