# Optional Upstream Browser Helper

This helper is an internal JSON-line subprocess for `backend/internal/upstreambrowser`.
It is not an HTTP service and must not be exposed directly to clients. The backend
coordinator owns administrator authorization, actor/site/version binding, the
two-session admission limit, ten-minute expiry, and explicit Complete/import.

## Runtime Installation

Supported service hosts are Windows and Linux. Install Node.js 22 or newer and an
administrator-managed Chromium/Chrome executable. Copy these files together into
a deployment directory:

- `main.mjs`
- `runtime.mjs`
- `contract.mjs`
- `package.json`
- `package-lock.json`

Run `npm ci --omit=dev --ignore-scripts` there. The pinned dependency is
`playwright-core@1.63.0`; no browser is downloaded or upgraded automatically.
The runtime directory must be read-only to untrusted users. The selected browser
must be kept patched by the deployment operator and support the pinned Playwright
version.

Configure the backend, not the helper command line:

| Variable | Purpose |
| --- | --- |
| `GOVERNANCE_BROWSER_NODE` | Node executable, default `node` |
| `GOVERNANCE_BROWSER_SCRIPT` | Absolute path to this directory's `main.mjs` |
| `GOVERNANCE_BROWSER_EXECUTABLE` | Absolute path to installed Chromium/Chrome |
| `GOVERNANCE_BROWSER_TEMP_DIR` | Existing writable directory for per-job profiles |
| `GOVERNANCE_BROWSER_HEADLESS` | Set exactly `true` to opt in to headless mode; otherwise a headed browser is used |

Headed mode requires a desktop/display accessible to the account running the
backend: an interactive desktop on Windows, or a configured display on Linux.
Service deployments without a usable display must provide one or explicitly set
`GOVERNANCE_BROWSER_HEADLESS=true` in the backend process environment. This option
changes only the browser's display mode; neither mode guarantees that an upstream
CAPTCHA or browser challenge will accept the session.

`Available()` checks prerequisite paths, not an actual browser launch or display.
Missing dependencies, incompatible browsers, an unavailable display, or
unavailable sandbox support still fail at Start. Linux must allow Chromium's
normal sandbox as an unprivileged service
user; the implementation never substitutes `--no-sandbox`. Linux process cleanup
also requires the service's own descendant process information under `/proc`.

Jobs live in one backend process. Deploy one instance or maintain sticky routing
for the entire job; restarting that process cancels the browser session. Count a
closing job against the concurrency limit until its driver Close finishes.

## Interaction And Capture

The helper opens exactly `https://<selected-origin>/login` with a fixed 1024x720
viewport. It fills an unambiguous, visible username/email and password form when
saved credentials are supplied. **It does not submit the form automatically.**
The administrator clicks the ordinary Login button in the screenshot, supplies
OTP input, and performs any interactive CAPTCHA. There is no CAPTCHA solver,
challenge clicking, fingerprint spoofing, or anti-detection integration. Ordinary
upstream page verification may finish using the upstream page's own code.

Only `pointer_down`, `pointer_move`, `pointer_up`, `wheel`, `text`, and `key` are
accepted. Coordinates are within the fixed viewport, wheel deltas are at most
2000, text is at most 2048 UTF-8 bytes without control characters, and keys come
from a small editing/navigation allowlist. No arbitrary JavaScript, URL navigation,
file selection, or shell commands are exposed. Screenshots and input are limited
to the selected origin's exact `/login` and `/login/` paths. Other login routes
and cross-origin SSO are not supported by this version. Leaving those paths
without a captured or in-flight login response ends the session with
`browser_unsupported_route`, rather than continuing with an empty waiting frame.
An already observed login response may finish capture after a same-origin
redirect; no dashboard pixels or input are exposed while capture is pending.
Transient screenshot failures report `browser_render_failed` and can recover on
the next snapshot while the page remains on a supported login route.

Capture is restricted to main-frame POST responses from the exact selected
origin and these known endpoints:

- Sub2API: `/api/v1/auth/login` and `/api/v1/auth/login/2fa`, with `code: 0` JSON.
- New API: `/api/user/login` and `/api/user/login/2fa`, with `success: true` JSON.

Two-factor completion must match the observed first-stage challenge. Tokens and
expiry are accepted only as bounded, correctly typed fields. An access-only
response is a nonrefreshable candidate, but its declared expiry is retained.
Refresh support requires both a valid refresh token and positive integer expiry;
expiry is anchored when the response
arrives, not when the administrator later completes import. New API legacy
cookie login needs a successful response, positive user ID, and a session cookie
observed on the login flow. Cookies are limited to exact-host `session` and
`cf_clearance`; other domains/cookies and localStorage are never scraped.

Verified login credentials come from the actual matching plaintext login POST,
not merely the autofill input. Known encrypted-password requests do not certify
the supplied plaintext password. The unmodified browser User-Agent is captured
before navigating to the upstream and returned for native identity verification
and subsequent requests.

Capture only produces an in-memory candidate. The caller must verify the current
upstream identity, enforce its ownership rules, and persist only after explicit
Complete. Tokens never belong in client responses. Once a candidate is captured,
the page closes and ready snapshots contain no image. The Go boundary validates
and re-encodes JPEG pixels, stripping metadata and trailing payloads.

## Network And Lifetime Controls

Every job has a random-authenticated forward proxy bound to `127.0.0.1`. The
browser's implicit loopback bypass is explicitly disabled; QUIC, nonproxied
WebRTC, direct-socket APIs, WebTransport, and WebSockets are disabled. Service
workers, downloads, extra windows, foreign-origin top-level navigation, and
non-HTTP(S) requests are blocked. TLS verification remains enabled.

The proxy rejects all nonpublic and special-use destination addresses, including
mapped IPv4, mixed public/private DNS answers, and cloud metadata ranges. It
dials a previously validated public IP rather than resolving the hostname again.
Configured HTTP, HTTPS, SOCKS5, or SOCKS5h proxies are mandatory, never silently
bypassed; they receive a pinned public destination IP. Browser SOCKS5h uses local
public-DNS validation instead of proxy-side DNS resolution. An HTTPS proxy retains certificate
verification against its original hostname.

Limits: destination ports 80/443, TLS-only CONNECT on 443, 32 active operations,
96 tracked sockets, 16 KiB headers, 1 MiB plaintext HTTP request bodies, 8 MiB
plaintext HTTP responses, and 8 MiB per TLS tunnel direction. The maximum lifetime
is ten minutes. Large upstream bundles can therefore fail closed instead of
receiving an unrestricted tunnel.

Close first shuts the proxy, allows up to two seconds for graceful browser exit,
then kills the owned process tree and removes the unique temporary profile.
Windows uses a kill-on-close Job Object. Linux independently verifies Chromium's
process ancestry/start time and cleans its detached process group as well as
Node descendants. Profile removal errors are reported as `browser_cleanup_failed`,
never with paths or secrets. The Node helper and proxy suppress raw diagnostic
logs; RPC errors contain bounded public error codes only.

## Tests

`npm test` runs parser, configuration, route/capture-race, screenshot recovery,
input, cookie, and subprocess protocol tests without contacting a real upstream.
A real-browser synthetic-fixture test is enabled with these environment variables:

```powershell
$env:GOVERNANCE_BROWSER_TEST_EXECUTABLE = 'C:/Program Files/Google/Chrome/Application/chrome.exe'
$env:GOVERNANCE_BROWSER_TEST_TEMP_DIR = 'E:/workspace/agent/.cache/sub2api-governance/browser-test'
$env:GOVERNANCE_BROWSER_TEST_ARTIFACT_DIR = 'E:/workspace/agent/.cache/sub2api-governance/browser-smoke'
# Optional for these synthetic tests; omit to test headed mode with a usable display.
$env:GOVERNANCE_BROWSER_HEADLESS = 'true'
node --test --test-reporter=tap *.test.mjs
```

Only synthetic credentials and intercepted fixture responses are used. The
fixture verifies autofill, administrator-style pointer submission, recovery from
a transient screenshot failure, and delayed capture after a same-origin redirect.
Optional artifacts are a masked login-page JPEG and a token-free verification report.

From `backend`, run `go test ./internal/upstreambrowser`. With the same explicit
test browser variables it also exercises Go -> Node -> Chromium launch, blocked
loopback navigation, shutdown, and profile removal. Linux-only tests verify
detached-child cleanup after parent exit and refuse unrelated process IDs.
Actual CAPTCHA behavior and compatibility with any particular upstream deployment
still require administrator acceptance testing.
