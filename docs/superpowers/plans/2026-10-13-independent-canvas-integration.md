# Independent Canvas Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Sub2API's `/infinite-canvas` user page with the forked `canvas.oreniva.com` application while preserving the main site's authorization boundary for iframe and new-tab use.

**Architecture:** The Vue host owns the session and answers `sub2api.canvas.ready` messages from the configured canvas origin. The React canvas keeps the received access token and managed API-key channels in memory only, queries the existing user APIs, and clears them on logout, 401, or a closed parent/opener. The existing native Vue canvas source remains unreferenced for this migration, while the external fork becomes the only user-facing canvas.

**Tech Stack:** Vue 3, TypeScript, Pinia, Vue Router, Vitest, React 19, Zustand, Bun tests, Vite, nginx static hosting, existing Gin CORS middleware.

**Spec:** `docs/superpowers/specs/2026-10-13-independent-canvas-integration-design.md`

## Global Constraints

- The `/infinite-canvas` user interface is completely replaced by the `https://canvas.oreniva.com` iframe.
- The production canvas origin is `https://canvas.oreniva.com`; local development must set explicit origins and must never use `*` for postMessage trust.
- Main-site JWTs, API keys, and other credentials are never written by the canvas to localStorage, IndexedDB, project exports, or URLs.
- Only active, unexpired keys from active image-capable groups and their model allowlists may be projected into managed channels.
- Logout, an invalid session, a 401 response, or a closed parent/opener clears managed channels and aborts pending key/group requests.
- Existing Sub2API user APIs, gateway billing, quotas, and usage records remain unchanged.
- The standalone canvas must still support local development configuration, but managed host credentials always stay non-persistent.
- The external site may be embedded only by `https://oreniva.com` in production; no wildcard frame or CORS policy is allowed.
- Existing uncommitted changes in `E:/workspace/agent/infinite-canvas-chroime` must be preserved and extended, never reset.
- Do not stage the repository's existing untracked test reports, screenshots, bundles, or `.tmp` files.

---

### Task 1: Define the cross-window protocol and main-site host bridge

**Files:**
- Create: `frontend/src/features/canvasHost/protocol.ts`
- Create: `frontend/src/features/canvasHost/__tests__/protocol.spec.ts`
- Create: `frontend/src/composables/useCanvasAuthHostBridge.ts`
- Create: `frontend/src/composables/__tests__/useCanvasAuthHostBridge.spec.ts`
- Modify: `frontend/src/App.vue`

**Interfaces:**
- `CANVAS_AUTH_PROTOCOL_VERSION: 1` — the protocol version accepted by both applications.
- `DEFAULT_CANVAS_ORIGIN = 'https://canvas.oreniva.com'` — production origin.
- `CanvasReadyMessage`, `CanvasAuthInitMessage`, and `CanvasAuthClearMessage` — discriminated message types.
- `isCanvasReadyMessage(value: unknown): value is CanvasReadyMessage` — accepts only version-one ready messages.
- `normalizeCanvasOrigin(value: unknown): string` — accepts an absolute `http:` or `https:` origin and strips a trailing slash; returns the production default for an empty value and throws for invalid values.
- `buildCanvasUrl(origin: string): string` — returns `${origin}/canvas` without embedding any credentials.
- `useCanvasAuthHostBridge()` — registers one global message listener, tracks trusted canvas windows, and posts init/clear messages from the current `useAuthStore` state.

- [ ] **Step 1: Write protocol tests before implementation**

```ts
it('accepts only version-one ready messages', () => {
  expect(isCanvasReadyMessage({ type: 'sub2api.canvas.ready', version: 1 })).toBe(true)
  expect(isCanvasReadyMessage({ type: 'sub2api.canvas.ready', version: 2 })).toBe(false)
  expect(isCanvasReadyMessage({ type: 'sub2api.auth.init', version: 1 })).toBe(false)
})

it('normalizes an explicit origin and rejects paths or unsupported schemes', () => {
  expect(normalizeCanvasOrigin('https://canvas.example/')).toBe('https://canvas.example')
  expect(() => normalizeCanvasOrigin('https://canvas.example/path')).toThrow()
  expect(() => normalizeCanvasOrigin('javascript:alert(1)')).toThrow()
})

it('never places credentials in the canvas URL', () => {
  expect(buildCanvasUrl('https://canvas.example')).toBe('https://canvas.example/canvas')
})
```

- [ ] **Step 2: Run the focused protocol tests and confirm they fail**

Run: `pnpm --dir frontend vitest run src/features/canvasHost/__tests__/protocol.spec.ts`

Expected: FAIL because the protocol module does not exist.

- [ ] **Step 3: Implement the protocol module**

Use `new URL(value)` and require `url.pathname === '/'`, `url.search === ''`, and `url.hash === ''`. The message guards must verify the object, exact type, and version `1`. `buildCanvasUrl` must use `new URL('/canvas', origin).toString()` and remove the trailing slash only from the origin, never concatenate arbitrary user input.

- [ ] **Step 4: Add the global host bridge**

The composable should:

```ts
const auth = useAuthStore()
const trusted = new Set<Window>()
const origin = normalizeCanvasOrigin(import.meta.env.VITE_CANVAS_ORIGIN || DEFAULT_CANVAS_ORIGIN)

function send(target: Window) {
  if (target.closed) return
  if (auth.isAuthenticated && auth.token.value) {
    target.postMessage({
      type: 'sub2api.auth.init',
      version: 1,
      token: auth.token.value,
      apiBase: new URL('/api/v1', window.location.origin).toString().replace(/\/$/, ''),
      user: auth.user.value ? { id: auth.user.value.id, role: auth.user.value.role } : undefined,
    }, origin)
  } else {
    target.postMessage({ type: 'sub2api.auth.clear', version: 1, reason: 'logout' }, origin)
  }
}
```

Register `message` only once per mounted host app. Accept a ready message only when `event.origin === origin`, `event.source` is a Window, and the message guard passes; add that source to `trusted`, then call `send`. Watch `auth.token` and `auth.isAuthenticated` and send the current state to every non-closed trusted window. On cleanup remove the listener and clear the set. Call the composable once from `App.vue`, so navigating away from `/infinite-canvas` does not leave a new-tab canvas with a stale bridge.

- [ ] **Step 5: Run the protocol and bridge tests**

Run: `pnpm --dir frontend vitest run src/features/canvasHost/__tests__/protocol.spec.ts src/composables/__tests__/useCanvasAuthHostBridge.spec.ts`

Expected: PASS, including tests for an untrusted origin, an untrusted source, init when authenticated, clear after token removal, and repeat ready messages.

- [ ] **Step 6: Commit the host protocol unit**

```bash
git add frontend/src/features/canvasHost frontend/src/composables/useCanvasAuthHostBridge.ts frontend/src/composables/__tests__/useCanvasAuthHostBridge.spec.ts frontend/src/App.vue
git commit -m "feat: add canvas host authorization bridge"
```

### Task 2: Replace the main-site route with the external canvas embed

**Files:**
- Create: `frontend/src/views/user/CanvasEmbedView.vue`
- Create: `frontend/src/views/user/__tests__/CanvasEmbedView.spec.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/router/__tests__/infiniteCanvasRoute.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/infiniteCanvas.ts`
- Modify: `frontend/src/i18n/locales/en/infiniteCanvas.ts`

**Interfaces:**
- `CanvasEmbedView.vue` renders the external iframe, a retry button, and a new-tab link; it does not contain a second API-key implementation.
- The route remains named `InfiniteCanvas`, keeps `requiresAuth: true` and `requiresAdmin: false`, and loads `CanvasEmbedView.vue`.
- Add `infiniteCanvas.embed.*` translations for loading, open-new-tab, reload, load-failed, and host-auth-required states.

- [ ] **Step 1: Add failing route and view tests**

Update the route source assertion from `InfiniteCanvasView.vue` to `CanvasEmbedView.vue`, then add a component test that mounts the view with `VITE_CANVAS_ORIGIN` mocked to `https://canvas.example`, asserts the iframe `src` is `https://canvas.example/canvas`, asserts no query parameter contains `token`, and asserts the new-tab control uses the same URL. Add a test that increments a local iframe key after clicking retry.

- [ ] **Step 2: Run the focused tests and confirm the route assertion fails**

Run: `pnpm --dir frontend vitest run src/router/__tests__/infiniteCanvasRoute.spec.ts src/views/user/__tests__/CanvasEmbedView.spec.ts`

Expected: FAIL because the route still points to the native view and the new component does not exist.

- [ ] **Step 3: Implement the embed view**

Use `normalizeCanvasOrigin` and `buildCanvasUrl` from Task 1. Render a full-height responsive panel with:

```vue
<iframe
  :key="frameKey"
  :src="canvasUrl"
  title="Infinite Canvas"
  class="h-full min-h-[calc(100vh-8rem)] w-full border-0"
  referrerpolicy="strict-origin-when-cross-origin"
  allow="clipboard-read; clipboard-write"
  @load="loadState = 'ready'"
  @error="loadState = 'error'"
/>
```

Use a real button for reload and a same-origin-safe `window.open(canvasUrl, '_blank')` action without adding a `noopener` relationship that would prevent the opener handshake. Do not add credentials to the iframe URL. The parent route is already authenticated, so show the main-site login state only while the iframe is loading or when the host session has just expired.

- [ ] **Step 4: Change the route and add translations**

Replace only the lazy component import in the route. Keep the route name and auth metadata unchanged. Add matching Chinese and English keys under `infiniteCanvas.embed`, then run the locale completeness check.

- [ ] **Step 5: Run the route, view, and i18n checks**

Run: `pnpm --dir frontend vitest run src/router/__tests__/infiniteCanvasRoute.spec.ts src/views/user/__tests__/CanvasEmbedView.spec.ts`

Run: `pnpm --dir frontend run check:i18n`

Expected: all focused tests and the locale check pass.

- [ ] **Step 6: Commit the route replacement**

```bash
git add frontend/src/views/user/CanvasEmbedView.vue frontend/src/views/user/__tests__/CanvasEmbedView.spec.ts frontend/src/router/index.ts frontend/src/router/__tests__/infiniteCanvasRoute.spec.ts frontend/src/i18n/locales/zh/infiniteCanvas.ts frontend/src/i18n/locales/en/infiniteCanvas.ts
git commit -m "feat: embed standalone canvas in user route"
```

### Task 3: Harden the React canvas managed-auth bridge

**Files:**
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/services/sub2api-auth.ts`
- Create: `E:/workspace/agent/infinite-canvas-chroime/web/tests/sub2api-auth.test.ts`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/stores/use-config-store.ts`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/components/layout/client-root-init.tsx`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/vite-env.d.ts`

**Interfaces:**
- Export `selectManagedChannels(keys, groups, configuredNames, apiBase): ModelChannel[]` as a pure function for filtering tests.
- `loadSub2ApiManagedChannels(message, signal)` returns only filtered channels and never persists the message token.
- Add `managedAuthStatus: 'idle' | 'loading' | 'ready' | 'error'` and `setManagedAuthStatus` to the non-persisted config store state.

- [ ] **Step 1: Write Bun tests for filtering and bridge requests**

Use `bun:test` and a minimal `localStorage` shim like the existing `web/tests/model-selection.test.ts`. Cover:

```ts
test('filters inactive, expired, image-disabled, and masked keys', () => {
  const result = selectManagedChannels(keys, groups, ['gpt-image-2', 'gpt-5.5'], 'https://oreniva.com/api/v1')
  expect(result.map((channel) => channel.apiKey)).toEqual(['active-secret'])
})

test('requests keys and groups with the host bearer token and omits credentials cookies', async () => {
  const fetchMock = mockFetch({ keys: { items: fixtureKeys }, groups: fixtureGroups })
  await loadSub2ApiManagedChannels({ type: 'sub2api.auth.init', version: 1, token: 'host-token', apiBase: 'https://oreniva.com/api/v1' }, new AbortController().signal)
  expect(fetchMock).toHaveBeenCalledTimes(2)
  expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'omit', headers: { Authorization: 'Bearer host-token' } })
})

test('invalid non-empty expiry is rejected', () => {
  expect(selectManagedChannels([{ ...activeKey, expires_at: 'not-a-date' }], groups, names, apiBase)).toEqual([])
})
```

- [ ] **Step 2: Run the tests to confirm the current bridge fails the new contracts**

Run from `E:/workspace/agent/infinite-canvas-chroime`: `bun test web/tests/sub2api-auth.test.ts`

Expected: FAIL because the current bridge does not export the pure selector, accepts invalid expiry values, and sends `credentials: 'include'`.

- [ ] **Step 3: Implement strict managed-channel selection**

Move the current filtering logic into `selectManagedChannels`. Require exact `version === 1`, a non-empty absolute HTTP(S) `apiBase`, active key status, a valid future expiry (or no expiry), non-masked key, active image-capable group, and the existing model allowlist. Compare exact allowlist rules case-insensitively and preserve wildcard rules. Return channels whose `apiKey` exists only in the returned in-memory object.

- [ ] **Step 4: Implement lifecycle and request cleanup**

Change `readJson` to send `credentials: 'omit'` because the bearer token is the explicit authorization mechanism. On init set status to `loading`, abort the previous controller, and set `ready` only after a non-empty filtered result; set `error` on a non-abort failure and clear channels on 401 or clear messages. Validate `version` and source/origin before processing. Keep the existing opener-closed interval, and abort/clear on component cleanup. The token must remain a closure value and never be written through the persisted Zustand `partialize` path.

- [ ] **Step 5: Add a visible managed-auth state without exposing credentials**

Expose a small `Sub2ApiAuthStatus` banner from `ClientRootInit` or `UserLayout` that only renders state text (`loading`, `ready`, `error`, `no host session`) and a retry instruction. Do not render the bearer token, full API key, or request error body. Keep local manual configuration available when `VITE_SUB2API_AUTH_REQUIRED` is false; production deployment sets it true so a direct canvas tab without a host session cannot generate through a missing managed channel.

- [ ] **Step 6: Update type declarations and run external tests**

Add `VITE_SUB2API_PARENT_ORIGIN`, `VITE_SUB2API_AUTH_REQUIRED`, and `VITE_CANVAS_ORIGIN` to `web/src/vite-env.d.ts`. Run: `bun test web/tests/sub2api-auth.test.ts web/tests/model-selection.test.ts`. Expected: PASS.

- [ ] **Step 7: Commit only external bridge changes**

From `E:/workspace/agent/infinite-canvas-chroime`:

```bash
git add web/src/services/sub2api-auth.ts web/tests/sub2api-auth.test.ts web/src/stores/use-config-store.ts web/src/components/layout/client-root-init.tsx web/src/vite-env.d.ts
git commit -m "feat: connect canvas to Sub2API managed auth"
```

### Task 4: Make managed mode enforceable and remove remaining visible upstream branding

**Files:**
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/constant/runtime-config.ts`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/vite-env.d.ts`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/docker-entrypoint.sh`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/docker-compose.yml`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/docker-compose.local.yml`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/nginx.conf`
- Modify: `E:/workspace/agent/infinite-canvas-chroime/web/src/constant/env.ts` only if a remaining visible repository link is found
- Test: `E:/workspace/agent/infinite-canvas-chroime/web/tests/branding-and-runtime-config.test.ts`

**Interfaces:**
- Runtime values `SUB2API_PARENT_ORIGIN`, `SUB2API_AUTH_REQUIRED`, and `CANVAS_ORIGIN` are read from `window.__RUNTIME_CONFIG__` first, then `VITE_` build values, then safe defaults.
- `nginx.conf` emits `Content-Security-Policy: frame-ancestors https://oreniva.com` and does not emit `X-Frame-Options: DENY`.

- [ ] **Step 1: Add failing runtime and branding assertions**

Assert that the runtime reader prefers `window.__RUNTIME_CONFIG__`, that production defaults are `https://oreniva.com`, `true`, and `https://canvas.oreniva.com`, and that `web/src/components/layout` contains no rendered GitHub link component or upstream `basketikun/infinite-canvas` URL. Leave prompt-source provider URLs untouched because they are source attribution data rather than the app's own branding.

- [ ] **Step 2: Run the focused external tests and confirm missing runtime values fail**

Run: `bun test web/tests/branding-and-runtime-config.test.ts`

Expected: FAIL until runtime configuration and CSP are added.

- [ ] **Step 3: Implement runtime configuration injection**

Extend `RuntimeConfig` and `read` in `runtime-config.ts`. Update `docker-entrypoint.sh` to safely serialize the two origins and the boolean auth flag without accepting quotes or newlines. Add the same environment variables to both compose files and their comments. Keep `credentials` and token data out of runtime config; these variables are public policy values only.

- [ ] **Step 4: Add the iframe security header**

Add an nginx `add_header Content-Security-Policy "frame-ancestors https://oreniva.com" always;` directive at server scope. Do not allow a wildcard and do not add `X-Frame-Options: DENY`. Keep `/config.js` no-store behavior.

- [ ] **Step 5: Run branding, config, and build checks**

Run: `bun test web/tests/branding-and-runtime-config.test.ts`

Run from `E:/workspace/agent/infinite-canvas-chroime/web`: `bun run typecheck` and `bun run build`

Expected: all pass and the generated bundle contains no visible GitHub header entry.

- [ ] **Step 6: Commit deployment/config changes**

```bash
git add web/src/constant/runtime-config.ts web/src/vite-env.d.ts web/docker-entrypoint.sh docker-compose.yml docker-compose.local.yml nginx.conf web/tests/branding-and-runtime-config.test.ts
git commit -m "chore: configure canvas host deployment"
```

### Task 5: Configure main API CORS and document independent deployment

**Files:**
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`
- Modify: `docs/content/docs/overview/docker.zh-CN.mdx`
- Modify: `docs/content/docs/overview/docker.mdx`
- Create: `deploy/canvas-oreniva.example.conf`
- Test: `deploy/tests/canvas-integration-config-test.sh`

**Interfaces:**
- The example deployment explicitly lists `https://oreniva.com` and `https://canvas.oreniva.com` in CORS allowed origins without changing a running server's database or other projects.
- The example reverse proxy serves the external static container at `canvas.oreniva.com`, proxies `/api/v1` only if explicitly configured, and sets the CSP frame-ancestors policy.

- [ ] **Step 1: Write configuration tests first**

The shell test should parse the example YAML/text and fail if the canvas origin is absent, if `allowed_origins` becomes `*`, or if the example nginx config lacks `frame-ancestors https://oreniva.com`.

- [ ] **Step 2: Run the config test to establish the failure**

Run: `bash deploy/tests/canvas-integration-config-test.sh`

Expected: FAIL because the examples do not yet mention the canvas origin.

- [ ] **Step 3: Add explicit deployment documentation**

Document that the admin must set API CORS allowed origins to the two exact HTTPS origins, deploy the external static image separately, and set `SUB2API_AUTH_REQUIRED=true` in production. Do not alter existing defaults to a wildcard or make the external site share database volumes.

- [ ] **Step 4: Add the example reverse proxy and test**

The example must serve `canvas.oreniva.com` over HTTPS, proxy to the canvas container only, set `Content-Security-Policy: frame-ancestors https://oreniva.com`, and omit credentials from access logs. The shell test should check the literal host, upstream port, CSP, and absence of `proxy_set_header Authorization` logging directives.

- [ ] **Step 5: Run config and documentation checks and commit**

Run: `bash deploy/tests/canvas-integration-config-test.sh`

Run: `git diff --check`

```bash
git add deploy/config.example.yaml deploy/.env.example deploy/canvas-oreniva.example.conf deploy/tests/canvas-integration-config-test.sh docs/content/docs/overview/docker.zh-CN.mdx docs/content/docs/overview/docker.mdx
git commit -m "docs: document standalone canvas deployment"
```

### Task 6: Run end-to-end local verification and review both repositories

**Files:**
- Modify only if verification finds a defect in the files above.
- Create: `.tmp-canvas/host-bridge-verification.json` locally only; do not commit it.

**Interfaces:**
- Main route and external app use the same origin/API contract in local and production builds.
- No test artifact, bundle, screenshot, or `.tmp` report is included in either commit.

- [ ] **Step 1: Run main focused tests and type checks**

Run: `pnpm --dir frontend vitest run src/features/canvasHost src/composables/__tests__/useCanvasAuthHostBridge.spec.ts src/views/user/__tests__/CanvasEmbedView.spec.ts src/router/__tests__/infiniteCanvasRoute.spec.ts`

Run: `pnpm --dir frontend run check:i18n`

Run: `pnpm --dir frontend vue-tsc --noEmit`

- [ ] **Step 2: Run external focused tests, typecheck, and build**

Run from `E:/workspace/agent/infinite-canvas-chroime`: `bun test web/tests/sub2api-auth.test.ts web/tests/branding-and-runtime-config.test.ts web/tests/model-selection.test.ts`

Run from `E:/workspace/agent/infinite-canvas-chroime/web`: `bun run typecheck` and `bun run build`

- [ ] **Step 3: Verify the local handshake**

Start the main host with `VITE_CANVAS_ORIGIN=http://127.0.0.1:3000` and the external app with `VITE_SUB2API_PARENT_ORIGIN=http://127.0.0.1:5173 VITE_SUB2API_AUTH_REQUIRED=true`. Verify iframe ready/init, key/group requests, model projection, clear after logout, and clear when the opener closes. Capture only status and URLs, never token or key values.

- [ ] **Step 4: Review repository status and diffs**

Run `git status --short --branch` and `git diff --check` in both repositories. Confirm the main branch contains only the integration commits and the external branch contains the pre-existing auth changes plus the new integration commits. Confirm reports, screenshots, generated bundles, and `.tmp` files remain untracked.

- [ ] **Step 5: Final report**

Report the two repository commit IDs, route behavior, local verification results, and the exact production configuration still required: DNS/TLS for `canvas.oreniva.com`, API CORS allowed origins, static container, CSP, and `SUB2API_AUTH_REQUIRED=true`. Do not claim the remote server was changed unless an explicit deployment command is run and verified.

