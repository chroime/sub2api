# Infinite Canvas Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a normal-user Infinite Canvas page that stores projects locally, lets users select or create an eligible image-generation API key, and generates images through Sub2API's existing gateway and billing path.

**Architecture:** Implement a native Vue canvas instead of embedding the reference React application. Keep project metadata and media in an IndexedDB-backed `CanvasRepository`, keep complete API key secrets in memory only, and reuse the existing `/keys`, `/groups/available`, `/v1/models`, `/v1/images/generations`, and `/v1/images/edits` paths. Add the missing Gemini image-modality permission check in the native Gemini handler so UI filtering cannot be bypassed.

**Tech Stack:** Vue 3, TypeScript, Vue Router, Pinia/reactive composables already used by the frontend, IndexedDB, SVG/HTML pointer events, Vitest, Go/Gin handler tests, `file-saver`, and `fflate` for project export archives.

**Spec:** `docs/superpowers/specs/2026-10-04-infinite-canvas-design.md`

## Global Constraints

- Store projects and image blobs in the current browser; do not add cloud synchronization in this implementation.
- Never persist a complete user API key in IndexedDB, localStorage, project JSON, or export archives.
- Only expose active, unexpired keys whose available group has `allow_image_generation=true`.
- Reuse existing gateway authentication, group model allowlists, image pricing, balance checks, quotas, usage logs, and settlement.
- Keep the first release to prompt, config, and image nodes; do not add audio, video, plugin, or account-sharing features.
- Preserve the existing user's uncommitted changes; stage only the intended hunks when a touched file already has unrelated modifications.
- Keep reference-project attribution requirements in mind; use its interaction ideas and data shape rather than copying React source.

---

### Task 1: Define canvas domain types and the IndexedDB repository

**Files:**
- Create: `frontend/src/features/infiniteCanvas/types.ts`
- Create: `frontend/src/features/infiniteCanvas/storage/canvasRepository.ts`
- Create: `frontend/src/features/infiniteCanvas/storage/indexedDbCanvasRepository.ts`
- Create: `frontend/src/features/infiniteCanvas/storage/__tests__/indexedDbCanvasRepository.spec.ts`

**Interfaces:**
- Produces `CanvasProject`, `CanvasNode`, `CanvasEdge`, `CanvasAsset`, `CanvasRepository`, and `createIndexedDbCanvasRepository()` for all later frontend tasks.
- `CanvasRepository` must expose `listProjects`, `loadProject`, `saveProject`, `deleteProject`, `saveAsset`, `loadAsset`, and `deleteAsset`.
- `CanvasNode.metadata` is intentionally extensible, but `type` is restricted to `prompt | config | image` in the first schema version.

- [ ] **Step 1: Write failing serialization and repository tests**

Add tests that create a version-one project containing one prompt node, one config node, one image node, and one edge; save and reload it; assert that dates, viewport, node metadata, edge kind, and `activeKeyId` survive. Add an asset test that saves a PNG Blob, returns a non-empty `storageKey`, reloads equal bytes and MIME type, then deletes it. Add a migration test that rejects an unknown schema version with a typed error instead of silently discarding the project.

```ts
it('round trips project metadata without an API key secret', async () => {
  const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
  const project = makeFixtureProject({ activeKeyId: 7 })
  await repo.saveProject(project)
  const loaded = await repo.loadProject(project.id)
  expect(loaded).toMatchObject(project)
  expect(JSON.stringify(loaded)).not.toContain('sk-secret')
})
```

- [ ] **Step 2: Run the focused tests and confirm the repository is not implemented**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/storage/__tests__/indexedDbCanvasRepository.spec.ts`

Expected: FAIL because the new repository factory and schema functions do not exist yet.

- [ ] **Step 3: Implement the domain types and schema version**

Create `CanvasProject` with `id`, `title`, `createdAt`, `updatedAt`, `viewport`, `backgroundMode`, optional `activeKeyId`, `nodes`, and `edges`. Create node metadata types for prompt text, image configuration, and image result status. Add `CanvasAsset` with optional `storageKey`, Blob, MIME type, dimensions, and `kind`. Define `CANVAS_SCHEMA_VERSION = 1` and a `CanvasSchemaError`.

- [ ] **Step 4: Implement the IndexedDB stores and migration guard**

Use one database with stores `projects` and `assets`. Store project metadata as structured records and assets as Blob records keyed by `storageKey`. `saveProject` must strip any accidental fields named `key`, `apiKey`, `secret`, or `token` from project metadata before writing. `loadProject` must validate the schema version and return a deep-cloned object. `deleteProject` must also remove asset records referenced only by that project after the project record is deleted.

- [ ] **Step 5: Run the focused tests and commit only this task**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/storage/__tests__/indexedDbCanvasRepository.spec.ts`

Expected: PASS.

Review `git diff -- frontend/src/features/infiniteCanvas` and commit only the new task files:

```bash
git add frontend/src/features/infiniteCanvas
git commit -m "feat: add local infinite canvas repository"
```

If the test environment lacks IndexedDB, add the smallest test-only IndexedDB shim required by the existing Vitest setup and record it in `frontend/package.json`; do not add a runtime storage dependency.

### Task 2: Add image gateway adapters and eligible-key filtering

**Files:**
- Create: `frontend/src/api/imageGeneration.ts`
- Create: `frontend/src/features/infiniteCanvas/keySelection.ts`
- Create: `frontend/src/api/__tests__/imageGeneration.spec.ts`
- Create: `frontend/src/features/infiniteCanvas/__tests__/keySelection.spec.ts`
- Modify: `frontend/src/api/keys.ts` only if a typed response option or active-key query helper is needed

**Interfaces:**
- Produces `listImageModels(apiKey)`, `generateImage(apiKey, request)`, `editImage(apiKey, request)`, `ImageGenerationError`, and `selectEligibleCanvasKeys(keys, groups)`.
- `generateImage` accepts `{ model, prompt, count, size, quality, background }` and returns normalized `{ blob: Blob, mimeType: string, width?: number, height?: number }` results.
- The adapter must use `buildGatewayUrl`, send `Authorization: Bearer <key>`, and never write the key to any store.

- [ ] **Step 1: Write failing key-filter and HTTP contract tests**

Cover active versus inactive and expired keys, missing groups, groups with `allow_image_generation=false`, and allowed OpenAI/Grok/Gemini groups. Assert that model-list requests use `GET /v1/models` and the bearer header, generation requests use `POST /v1/images/generations` with `model`, `prompt`, `n`, `size`, `quality`, and that non-success responses become `ImageGenerationError` with `status`, `code`, and a user-facing message.

```ts
it('filters keys to active, unexpired image-capable groups', () => {
  expect(selectEligibleCanvasKeys(fixtureKeys, fixtureGroups).map(item => item.id)).toEqual([11, 14])
})
```

- [ ] **Step 2: Run focused tests and confirm they fail**

Run: `pnpm --dir frontend vitest run src/api/__tests__/imageGeneration.spec.ts src/features/infiniteCanvas/__tests__/keySelection.spec.ts`

Expected: FAIL because the adapter and filter do not exist.

- [ ] **Step 3: Implement key selection as a pure function**

Join `ApiKey.group_id` with `Group.id`, reject non-active keys, reject `expires_at <= now`, reject groups without `allow_image_generation`, and return a display-safe option containing only ID, name, masked key, group name, platform, and the original full key in an in-memory-only field. Keep the filter independent of Vue so it can be tested without mounting a page.

- [ ] **Step 4: Implement the OpenAI-compatible image adapter**

Use `fetch(buildGatewayUrl('/v1/models'))` for model discovery. Normalize model responses from `{ data: [...] }`, retain models whose metadata or ID indicates image support, and allow the page to apply the group allowlist. For generation, send JSON to `/v1/images/generations`, parse `b64_json` and URL results, and convert URL results to Blobs through a same-origin fetch. For editing, send `FormData` to `/v1/images/edits` with prompt, model, size, quality and the selected reference Blob. Do not set `Content-Type` manually for multipart requests.

- [ ] **Step 5: Add Gemini request normalization without weakening backend checks**

Expose a Gemini request builder that emits `contents`, `generationConfig.responseModalities: ['TEXT', 'IMAGE']`, and image configuration only when the selected adapter is Gemini. The page may hide Gemini until its model list is available, but the adapter must preserve the provider-specific `x-goog-api-key` header and normalize the returned inline image data.

- [ ] **Step 6: Run tests and commit the adapter task**

Run: `pnpm --dir frontend vitest run src/api/__tests__/imageGeneration.spec.ts src/features/infiniteCanvas/__tests__/keySelection.spec.ts`

Expected: PASS.

Stage only the new adapter/filter files and commit:

```bash
git add frontend/src/api/imageGeneration.ts frontend/src/features/infiniteCanvas/keySelection.ts frontend/src/api/__tests__/imageGeneration.spec.ts frontend/src/features/infiniteCanvas/__tests__/keySelection.spec.ts
git commit -m "feat: add infinite canvas image adapters"
```

### Task 3: Enforce Gemini image permission at the gateway

**Files:**
- Create: `backend/internal/handler/gemini_image_generation_gate.go`
- Create: `backend/internal/handler/gemini_image_generation_gate_test.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go` immediately after the request body is read and validated

**Interfaces:**
- Produces `geminiRequestUsesImageModality(body []byte) bool` and `requireGeminiImageGenerationPermission(c *gin.Context, apiKey *service.APIKey, body []byte) bool`.
- Existing text-only Gemini calls and existing ungrouped-key compatibility behavior remain unchanged.

- [ ] **Step 1: Write failing handler tests**

Construct Gin contexts with API keys bound to groups where `AllowImageGeneration` is true and false. Send native Gemini bodies with `generationConfig.responseModalities` values `['TEXT']`, `['TEXT', 'IMAGE']`, and lowercase `['image']`. Assert that only image-containing requests are rejected with Google-style HTTP 403 when the group disallows image generation, while text-only requests continue.

- [ ] **Step 2: Run the focused Go tests and confirm failure**

Run from `backend`: `go test ./internal/handler -run 'TestGeminiImageGeneration' -count=1`

Expected: FAIL because the helper and handler call are absent.

- [ ] **Step 3: Implement strict modality parsing**

Decode only the `generationConfig.responseModalities` array, compare values case-insensitively after trimming, and return false for missing or malformed fields. Do not treat a text-only request as an image request.

- [ ] **Step 4: Add the permission check to the native Gemini handler**

After `ReadRequestBodyWithPrealloc` succeeds and before security audit, account selection, or billing work, call the helper. If it returns false for an image modality and `service.GroupAllowsImageGeneration(apiKey.Group)` is false, return `googleError(c, http.StatusForbidden, "Image generation is not allowed for this API key group")`. Preserve the existing platform and model-allowlist checks.

- [ ] **Step 5: Run the focused tests and commit only the backend hunks**

Run: `go test ./internal/handler -run 'TestGeminiImageGeneration' -count=1`

Expected: PASS.

Because `gemini_v1beta_handler.go` may contain unrelated working-tree changes, inspect `git diff -- backend/internal/handler/gemini_v1beta_handler.go`, stage only the permission-check hunk plus the new helper/test files, and commit:

```bash
git add backend/internal/handler/gemini_image_generation_gate.go backend/internal/handler/gemini_image_generation_gate_test.go
git add -p backend/internal/handler/gemini_v1beta_handler.go
git commit -m "fix: enforce Gemini image group permission"
```

### Task 4: Register the route, menu item, and translations

**Files:**
- Create: `frontend/src/i18n/locales/zh/infiniteCanvas.ts`
- Create: `frontend/src/i18n/locales/en/infiniteCanvas.ts`
- Modify: `frontend/src/i18n/locales/zh/index.ts`
- Modify: `frontend/src/i18n/locales/en/index.ts`
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Create: `frontend/src/router/__tests__/infiniteCanvasRoute.spec.ts`

**Interfaces:**
- Produces the `/infinite-canvas` route name `InfiniteCanvas`, the translated `nav.infiniteCanvas` label, and an icon component used by both normal users and an admin's personal section.
- The item must not be hidden by the batch-image feature flag and must not appear in admin-only navigation.

- [ ] **Step 1: Add failing route and navigation assertions**

Assert that the router has `/infinite-canvas` with `requiresAuth: true` and `requiresAdmin: false`, and that `buildSelfNavItems` source contains the translated label and route. Assert the label appears in both regular-user and admin-personal navigation paths.

- [ ] **Step 2: Run the focused frontend tests and confirm failure**

Run: `pnpm --dir frontend vitest run src/components/layout/__tests__/AppSidebar.spec.ts src/router/__tests__/infiniteCanvasRoute.spec.ts`

Expected: FAIL because the route, label, and menu item do not exist.

- [ ] **Step 3: Add translations and route metadata**

Add Chinese and English strings for the page title, description, empty state, project actions, key picker, model controls, generation statuses, errors, import/export messages, and the menu label. Import both locale modules through their existing index files so `check:i18n` sees matching keys. Add the lazy route beside the other user routes with `InfiniteCanvasView.vue` as its component.

- [ ] **Step 4: Add a consistent sidebar icon and menu item**

Create an inline SVG icon component in `AppSidebar.vue` that follows the current icon dimensions and theme classes. Add `{ path: '/infinite-canvas', label: t('nav.infiniteCanvas'), icon: InfiniteCanvasIcon, hideInSimpleMode: true }` after the existing image entry, so normal users and admin personal navigation share one declaration.

- [ ] **Step 5: Run tests and commit targeted hunks**

Run: `pnpm --dir frontend vitest run src/components/layout/__tests__/AppSidebar.spec.ts src/router/__tests__/infiniteCanvasRoute.spec.ts`; expected PASS.

Run: `pnpm --dir frontend run check:i18n`; expected PASS.

Review changes to `AppSidebar.vue`, `router/index.ts`, locale index files, and common locale files. Stage only the new route/menu hunks and commit:

```bash
git add frontend/src/i18n/locales/zh/infiniteCanvas.ts frontend/src/i18n/locales/en/infiniteCanvas.ts frontend/src/router/__tests__/infiniteCanvasRoute.spec.ts
git add -p frontend/src/i18n/locales/zh/index.ts frontend/src/i18n/locales/en/index.ts frontend/src/i18n/locales/zh/common.ts frontend/src/i18n/locales/en/common.ts frontend/src/components/layout/AppSidebar.vue frontend/src/router/index.ts frontend/src/components/layout/__tests__/AppSidebar.spec.ts
git commit -m "feat: add infinite canvas navigation"
```

### Task 5: Implement canvas state, viewport interactions, and node rendering

**Files:**
- Create: `frontend/src/features/infiniteCanvas/stores/useInfiniteCanvasStore.ts`
- Create: `frontend/src/features/infiniteCanvas/composables/useCanvasHistory.ts`
- Create: `frontend/src/features/infiniteCanvas/composables/useCanvasViewport.ts`
- Create: `frontend/src/features/infiniteCanvas/components/InfiniteCanvasSurface.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasNode.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasEdgeLayer.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasMinimap.vue`
- Create: `frontend/src/features/infiniteCanvas/__tests__/useInfiniteCanvasStore.spec.ts`
- Create: `frontend/src/features/infiniteCanvas/__tests__/useCanvasViewport.spec.ts`

**Interfaces:**
- `useInfiniteCanvasStore(repository)` exposes `projects`, `activeProject`, `selectedNodeIds`, `activeKeyId`, `createProject`, `renameProject`, `duplicateProject`, `deleteProject`, `setActiveProject`, `addNode`, `updateNode`, `removeNode`, `connectNodes`, `setActiveKey`, `undo`, and `redo`.
- `useCanvasViewport()` exposes `{ viewport, panBy, zoomAt, resetZoom, screenToWorld, worldToScreen }` with zoom clamped to `0.05..5`.
- `InfiniteCanvasSurface.vue` accepts the active project and emits node selection, node move, node delete, edge create, viewport update, and empty-canvas double-click events.

- [ ] **Step 1: Write failing store and viewport tests**

Test project creation, active-project switching, node insertion, node updates, deletion, edge insertion, undo/redo, and automatic `updatedAt` updates. Test `zoomAt` keeps the world point under the cursor fixed and clamps zoom to `0.05..5`; test coordinate conversion round trips.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/__tests__/useInfiniteCanvasStore.spec.ts src/features/infiniteCanvas/__tests__/useCanvasViewport.spec.ts`

Expected: FAIL because the store and composables are absent.

- [ ] **Step 3: Implement the store with repository-backed persistence**

Keep a reactive project list and active project in memory, hydrate from `CanvasRepository` on initialization, and debounce project saves after mutations. Use immutable snapshots for a bounded history stack of 50 states. `setActiveKey` writes only the key ID to the project. Every mutation must preserve unknown node metadata fields so future node types can be added safely.

- [ ] **Step 4: Implement viewport math and pointer behavior**

Track pointer capture for blank-canvas pan and node drag separately. Use `requestAnimationFrame` to coalesce move events. Use the wheel pointer location as the zoom anchor, clamp the zoom range, and suppress canvas zoom when an element has `data-canvas-no-zoom`. Support spacebar and Ctrl as temporary pan modifiers.

- [ ] **Step 5: Render nodes, edges, and minimap**

Render the world layer with `translate(viewport.x, viewport.y) scale(viewport.zoom)`. Render edge paths from node bounds, render node shells with `data-canvas-no-zoom`, show selected state, and use the project bounds to draw a clickable minimap viewport. Keep node type-specific content in the later generation task; this task renders safe placeholders for metadata.

- [ ] **Step 6: Run tests and commit the canvas core**

Run the two focused test files; expected PASS. Mount `InfiniteCanvasSurface.vue` once with a fixture project to verify no Vue warnings. Stage only `frontend/src/features/infiniteCanvas` files from this task and commit:

```bash
git add frontend/src/features/infiniteCanvas/stores frontend/src/features/infiniteCanvas/composables frontend/src/features/infiniteCanvas/components frontend/src/features/infiniteCanvas/__tests__
git commit -m "feat: add infinite canvas interaction core"
```

### Task 6: Compose the page, project sidebar, toolbar, and key inspector

**Files:**
- Create: `frontend/src/views/user/InfiniteCanvasView.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasProjectSidebar.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasToolbar.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasInspector.vue`
- Create: `frontend/src/features/infiniteCanvas/components/CanvasKeyPicker.vue`
- Create: `frontend/src/features/infiniteCanvas/__tests__/InfiniteCanvasView.spec.ts`

**Interfaces:**
- `InfiniteCanvasView.vue` owns the repository instance, loads API keys/groups, creates the store, and composes sidebar, toolbar, surface, inspector, and modals.
- `CanvasKeyPicker.vue` accepts `options: CanvasKeyOption[]`, `modelValue: number | null`, and `loading`; it emits `update:modelValue` and `create-key`.
- `CanvasInspector.vue` edits node metadata through the store and never accepts or persists a raw secret.

- [ ] **Step 1: Write failing page tests**

Mount the page with mocked `keysAPI.list`, `userGroupsAPI.getAvailable`, and a memory repository. Assert that only image-capable options appear, that a missing key shows a clear empty state with links to create a key, that switching projects keeps the selected tab/page, and that the page renders in dark mode classes without console errors.

- [ ] **Step 2: Run the page test and confirm failure**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/__tests__/InfiniteCanvasView.spec.ts`

Expected: FAIL because the page and components are absent.

- [ ] **Step 3: Implement page composition and responsive layout**

Use `AppLayout` and existing card/button/select/modal primitives. Keep the canvas horizontally scroll-safe on small screens; collapse the project sidebar and inspector into drawers below the `lg` breakpoint. Add empty states for no projects, no eligible keys, no image models, and no selected node.

- [ ] **Step 4: Implement project actions and toolbar state**

Wire new, rename, duplicate, delete, import, export, background mode, zoom, undo, redo, and save-status actions to the store. Confirm delete with the existing confirmation pattern. Display the active project's last saved time in the user's locale.

- [ ] **Step 5: Implement key picker and in-memory key lifecycle**

Load the full key list through the authenticated user API, pass it through `selectEligibleCanvasKeys`, and keep the selected full secret in a component-scoped `Map<number, string>`. Persist only the selected ID through the store. The quick-create modal calls `keysAPI.create(name, groupId)`, inserts the returned key into the in-memory map, refreshes the eligible list, and selects the new key. Clear the map on component unmount and when the auth user changes.

- [ ] **Step 6: Run tests and commit page composition**

Run the page test; expected PASS. Run `pnpm --dir frontend run typecheck`; expected PASS for all new Vue files. Stage only the new page/component/test files and commit:

```bash
git add frontend/src/views/user/InfiniteCanvasView.vue frontend/src/features/infiniteCanvas/components/CanvasProjectSidebar.vue frontend/src/features/infiniteCanvas/components/CanvasToolbar.vue frontend/src/features/infiniteCanvas/components/CanvasInspector.vue frontend/src/features/infiniteCanvas/components/CanvasKeyPicker.vue frontend/src/features/infiniteCanvas/__tests__/InfiniteCanvasView.spec.ts
git commit -m "feat: add infinite canvas user page"
```

### Task 7: Add prompt/config/image nodes and generation workflow

**Files:**
- Create: `frontend/src/features/infiniteCanvas/components/nodes/PromptNode.vue`
- Create: `frontend/src/features/infiniteCanvas/components/nodes/ConfigNode.vue`
- Create: `frontend/src/features/infiniteCanvas/components/nodes/ImageNode.vue`
- Create: `frontend/src/features/infiniteCanvas/components/nodes/NodeStatusBadge.vue`
- Create: `frontend/src/features/infiniteCanvas/composables/useCanvasGeneration.ts`
- Create: `frontend/src/features/infiniteCanvas/__tests__/useCanvasGeneration.spec.ts`
- Modify: `frontend/src/features/infiniteCanvas/components/CanvasInspector.vue`
- Modify: `frontend/src/features/infiniteCanvas/components/CanvasNode.vue`

**Interfaces:**
- `useCanvasGeneration(options)` exposes `generateFromNodes(promptNodeId, configNodeId)`, `retryImageNode(nodeId)`, `cancelGeneration(nodeId)`, and `isGenerating(nodeId)`.
- Generation accepts the in-memory key secret from `CanvasKeyPicker` only for the duration of the adapter call; the store receives normalized result metadata and a `storageKey`, never the secret.

- [ ] **Step 1: Write failing generation tests**

Mock `generateImage` to return one Base64 result and one HTTP 403 error. Assert that generation creates a pending image node, writes the Blob through the repository, transitions to completed with a `storageKey`, and transitions to failed with a Chinese user-facing error while retaining prompt/config metadata. Assert retry reuses the same prompt and configuration.

- [ ] **Step 2: Run the focused generation tests and confirm failure**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/__tests__/useCanvasGeneration.spec.ts`

Expected: FAIL because the generation composable and node components are absent.

- [ ] **Step 3: Implement node metadata editors**

`PromptNode` edits `metadata.prompt`; `ConfigNode` edits `metadata.model`, `size`, `quality`, `count`, and `background`; `ImageNode` renders the Blob URL, status, model, prompt summary, download action, delete action, and retry action. All interactive controls use `data-canvas-no-zoom`.

- [ ] **Step 4: Implement generation state transitions**

Resolve upstream prompt/config edges, create a pending image node near the config node, call the adapter with the current in-memory key, save each returned Blob, and update each node with result metadata. For multiple results, create one image node per result and link them to the same prompt/config nodes. Revoke object URLs when image nodes are deleted or replaced.

- [ ] **Step 5: Map gateway errors to actionable UI**

Map 401 to key re-selection, 403 to group/model permission text, 402 to balance/quota text, 429 to rate-limit text, 5xx/network errors to retryable text, and invalid image data to a parse-failure text. Do not automatically retry 402 or 403.

- [ ] **Step 6: Run tests and commit the generation task**

Run the focused test; expected PASS. Run `pnpm --dir frontend run typecheck`; expected PASS. Stage only node/generation files and commit:

```bash
git add frontend/src/features/infiniteCanvas/components/nodes frontend/src/features/infiniteCanvas/composables/useCanvasGeneration.ts frontend/src/features/infiniteCanvas/__tests__/useCanvasGeneration.spec.ts
git add -p frontend/src/features/infiniteCanvas/components/CanvasInspector.vue frontend/src/features/infiniteCanvas/components/CanvasNode.vue
git commit -m "feat: generate images from infinite canvas nodes"
```

### Task 8: Add project import/export and asset lifecycle

**Files:**
- Create: `frontend/src/features/infiniteCanvas/storage/projectTransfer.ts`
- Create: `frontend/src/features/infiniteCanvas/storage/__tests__/projectTransfer.spec.ts`
- Modify: `frontend/src/features/infiniteCanvas/components/CanvasProjectSidebar.vue`
- Modify: `frontend/package.json` and `frontend/pnpm-lock.yaml` to add `fflate` if it is not already available

**Interfaces:**
- Produces `exportProject(project, repository): Promise<Blob>`, `importProject(file, repository): Promise<CanvasProject>`, and `sanitizeExportProject(project)`.
- Export archives contain `project.json` plus `assets/<storageKey>` files. They never contain `activeKeySecret`, `apiKey`, or bearer tokens.

- [ ] **Step 1: Write failing archive tests**

Create a fixture project with an image asset and an accidental `metadata.apiKey` field. Assert that the exported archive contains valid project JSON and the asset bytes, omits all secret fields, and can be imported into a fresh repository with a new project ID while preserving node-to-asset references.

- [ ] **Step 2: Run the transfer tests and confirm failure**

Run: `pnpm --dir frontend vitest run src/features/infiniteCanvas/storage/__tests__/projectTransfer.spec.ts`

Expected: FAIL because the transfer module is absent.

- [ ] **Step 3: Add the archive dependency and implement sanitization**

Add `fflate` at version `^0.8.2` only if the lockfile does not already provide it. Recursively omit fields named `key`, `apiKey`, `api_key`, `secret`, `token`, `access_token`, and `activeKeySecret` from exported metadata. Preserve `activeKeyId` as an ID without a secret. Reject archives larger than 100 MiB before decompression.

- [ ] **Step 4: Implement export and import**

Use `fflate.zipSync`/`unzipSync` and existing `file-saver`. Export a versioned `project.json`; copy each referenced asset Blob into the archive. On import, validate schema version, node types, edge endpoints, asset MIME types, and total byte size; assign a new project ID and storage keys to avoid overwriting an existing project.

- [ ] **Step 5: Wire toolbar/sidebar actions and run tests**

Connect export to a download named after the sanitized project title and date; connect import to a file picker with a visible progress/error message. Run the transfer tests and expect PASS. Run `pnpm --dir frontend run check:i18n`; expect PASS.

- [ ] **Step 6: Commit only transfer files and dependency changes**

```bash
git add frontend/src/features/infiniteCanvas/storage/projectTransfer.ts frontend/src/features/infiniteCanvas/storage/__tests__/projectTransfer.spec.ts
git add -p frontend/src/features/infiniteCanvas/components/CanvasProjectSidebar.vue frontend/package.json frontend/pnpm-lock.yaml
git commit -m "feat: import and export infinite canvas projects"
```

### Task 9: Integrate, verify, and document the user-facing behavior

**Files:**
- Modify: `frontend/src/features/infiniteCanvas/__tests__/InfiniteCanvasView.spec.ts` with final integration cases
- Modify: `frontend/src/features/infiniteCanvas/__tests__/useCanvasGeneration.spec.ts` with key invalidation cases
- Modify: `backend/internal/handler/gemini_image_generation_gate_test.go` with final text-only regression coverage
- Create: `docs/infinite-canvas-user-guide.md`

**Interfaces:**
- The user guide documents the local-storage boundary, API Key selection, project export/import, and the fact that full secrets are not included in project files.

- [ ] **Step 1: Add integration regression tests**

Cover a key being disabled between page load and generation, a missing image-capable group, a page reload restoring projects but requiring a fresh key-list fetch, and a generated image being downloadable after the Blob URL is restored.

- [ ] **Step 2: Run the complete focused frontend suite**

Run:

```bash
pnpm --dir frontend vitest run \
  src/features/infiniteCanvas \
  src/api/__tests__/imageGeneration.spec.ts \
  src/components/layout/__tests__/AppSidebar.spec.ts \
  src/router/__tests__/infiniteCanvasRoute.spec.ts
```

Expected: PASS.

- [ ] **Step 3: Run backend permission tests and existing image regressions**

Run from `backend`:

```bash
go test ./internal/handler -run 'TestGeminiImageGeneration|TestGeminiV1Beta' -count=1
go test ./internal/handler -run 'TestOpenAI.*Image|TestGrok.*Image' -count=1
```

Expected: PASS.

- [ ] **Step 4: Run static checks and build**

Run:

```bash
pnpm --dir frontend run check:i18n
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
pnpm --dir frontend run build
```

Expected: all commands exit successfully. If lint reports pre-existing unrelated files, record only those paths and do not auto-fix them.

- [ ] **Step 5: Perform a local browser smoke test**

Start the already configured local services, open `/infinite-canvas` as a normal user, verify the menu item, create or select an eligible Key, create a prompt/config pair, generate against a mocked or known-good local image channel, reload the page, export the project, import it under a second title, and verify that the exported archive contains no bearer token. Verify the existing `/keys`, `/batch-image`, and `/usage` pages still load.

- [ ] **Step 6: Commit documentation and the final test-only changes**

Review `git status` and `git diff` carefully so unrelated pre-existing work remains unstaged. Stage the guide and targeted test hunks only:

```bash
git add docs/infinite-canvas-user-guide.md
git add -p frontend/src/features/infiniteCanvas/__tests__/InfiniteCanvasView.spec.ts frontend/src/features/infiniteCanvas/__tests__/useCanvasGeneration.spec.ts backend/internal/handler/gemini_image_generation_gate_test.go
git commit -m "docs: add infinite canvas usage guide"
```

The final report must include the focused test commands, frontend build result, backend permission test result, local browser smoke-test result, and any known limitation such as cloud sync being deferred.

## Self-review checklist

- [ ] Every design requirement has a task: menu/route, local IndexedDB projects and assets, API Key selection/creation, image generation, Gemini permission enforcement, import/export, errors, tests, and future sync boundary.
- [ ] No task writes a complete API Key to browser persistence or an export archive.
- [ ] All referenced types and functions are introduced before a later task consumes them.
- [ ] Touched files with unrelated working-tree modifications use targeted staging rather than whole-file staging.
- [ ] There are no `TBD`, `TODO`, or vague steps such as “add appropriate handling” in this plan.
