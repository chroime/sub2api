# Smart Operations Persistent Site Switching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the upstream selector visible in a left rail while the selected upstream's Smart Operations tabs and content remain in a right workspace.

**Architecture:** Preserve the existing route contract (`section` path parameter plus `site` query parameter) and the generation-based site loading in `UpstreamGovernanceView.vue`. Add a compact rendering mode to `GovernanceSitesOverview.vue`, render it persistently in a responsive two-column shell, and keep the right workspace driven by the existing `active` site and route tab.

**Tech Stack:** Vue 3 script setup, Vue Router 4, Tailwind utility classes, Vitest, Vue Test Utils, existing i18n keys.

**Spec:** `docs/superpowers/specs/2026-09-29-smart-operations-site-switching-design.md`

## Global Constraints

- Do not change backend APIs or the `/admin/upstream-governance/:section?` route contract.
- Preserve `route.query.site` whenever changing Smart Operations tabs.
- Preserve generation/request guards and navigation protection during mutations.
- Do not expose credentials, tokens, or session data in the new layout.
- Preserve dark mode, keyboard focus, and existing `governance-site-<id>` selectors.

---

### Task 1: Add compact upstream list rendering

**Files:**
- Modify: `frontend/src/views/admin/upstream-governance/GovernanceSitesOverview.vue`
- Create: `frontend/src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts`

**Interfaces:**
- Consumes: existing `sites`, `disabled`, and `select(site)` props/events.
- Produces: an optional `compact` prop that renders a reusable left-rail list without changing the selection event or site button IDs.

- [ ] **Step 1: Write the failing tests**

Add tests that mount the component with `compact` and assert that the site count/header, search/filter controls, selected-state class/attribute, and `governance-site-<id>` buttons are present while the three large metric cards are absent. Assert a click emits the same `select` payload and disabled state prevents the click.

- [ ] **Step 2: Run the focused test and verify it fails**

Run from `frontend`:

```powershell
pnpm exec vitest run src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts
```

Expected: FAIL because `compact` is not a declared prop and no compact markup exists.

- [ ] **Step 3: Implement the compact branch**

Declare `compact?: boolean` with a false default. Keep the existing full overview branch unchanged. For `compact`, render a bordered card with a compact heading, count, search input, filter buttons, and a vertically scrollable list. Add `aria-current="true"` when an optional `selectedSiteId` matches a row; emit the existing `select` event. Add `selectedSiteId?: number | null` so the active row is visibly distinct.

- [ ] **Step 4: Run the focused test and verify it passes**

Run the same Vitest command and expect all tests in the new file to pass.

- [ ] **Step 5: Commit the isolated component change**

```powershell
git add frontend/src/views/admin/upstream-governance/GovernanceSitesOverview.vue frontend/src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts
git commit -m "feat(governance): add compact upstream selector"
```

### Task 2: Build the persistent two-column workspace

**Files:**
- Modify: `frontend/src/views/admin/upstream-governance/UpstreamGovernanceView.vue`
- Modify: `frontend/src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts`

**Interfaces:**
- Consumes: `GovernanceSitesOverview` compact props, existing `SmartOperationsTabs`, route-driven `tab`, and `select()`.
- Produces: a responsive left selector plus right selected-site workspace with all existing panels scoped to `active.id`.

- [ ] **Step 1: Add failing navigation/layout tests**

Extend `UpstreamGovernanceView.spec.ts` with these assertions:

```ts
expect(wrapper.get('[data-test=smart-operations-site-rail]').isVisible()).toBe(true)
await wrapper.get('#governance-site-1').trigger('click')
await flushPromises()
expect(wrapper.get('[data-test=smart-operations-site-rail]').isVisible()).toBe(true)
await wrapper.get('#governance-site-2').trigger('click')
await flushPromises()
expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance/import?site=2')
expect(wrapper.getComponent(ImportPanel).props('siteId')).toBe(2)
```

Also assert that selecting a site on the models route keeps `models` selected and that no-site state renders the right-hand chooser while the left rail remains visible.

- [ ] **Step 2: Run the focused view tests and verify the new assertions fail**

```powershell
pnpm exec vitest run src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts
```

Expected: FAIL because the current full-width overview disappears after selection and no persistent rail exists.

- [ ] **Step 3: Implement the two-column shell**

Render a responsive grid around the existing body: the compact `GovernanceSitesOverview` in a `data-test="smart-operations-site-rail"` aside, and the existing right workspace beside it. Pass `selectedSiteId="active?.id ?? null"`. Move `SmartOperationsTabs` into the right workspace, remove the full-page `v-show="showOverview"` dependency from the site list, and replace the obsolete back-to-sites button with the persistent rail. Keep the right pane's empty state when `active` is null. Change model-panel visibility to depend on `active` and the selected tab rather than `showOverview`.

- [ ] **Step 4: Run focused tests and verify they pass**

```powershell
pnpm exec vitest run src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts
```

- [ ] **Step 5: Commit the workspace change**

```powershell
git add frontend/src/views/admin/upstream-governance/UpstreamGovernanceView.vue frontend/src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts
git commit -m "feat(governance): keep upstream selector beside smart operations"
```

### Task 3: Verify responsive behavior, route state, and regression suite

**Files:**
- Modify: `frontend/src/views/admin/upstream-governance/SmartOperationsTabs.spec.ts` only if a changed parent contract requires an assertion update.
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.smartOperations.spec.ts` only if route-preservation coverage needs an additional case.

**Interfaces:**
- Consumes: the two-column workspace and existing route-preservation helpers.
- Produces: verified navigation behavior with no backend changes.

- [ ] **Step 1: Run all affected frontend tests**

```powershell
pnpm exec vitest run src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts src/views/admin/upstream-governance/SmartOperationsTabs.spec.ts src/components/layout/__tests__/AppSidebar.smartOperations.spec.ts
```

- [ ] **Step 2: Run type checking and lint checks**

```powershell
pnpm exec vue-tsc --noEmit
pnpm exec eslint src/views/admin/upstream-governance/GovernanceSitesOverview.vue src/views/admin/upstream-governance/GovernanceSitesOverview.spec.ts src/views/admin/upstream-governance/UpstreamGovernanceView.vue src/views/admin/upstream-governance/UpstreamGovernanceView.spec.ts
```

- [ ] **Step 3: Run the complete frontend regression suite**

```powershell
pnpm run test:run
```

- [ ] **Step 4: Review the final diff and commit verification notes**

```powershell
git diff --check
git status --short
git log -2 --oneline
```

Confirm no backend files, credentials, generated artifacts, or unrelated changes are present.
