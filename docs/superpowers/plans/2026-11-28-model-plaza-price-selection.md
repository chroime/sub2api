# Model plaza price selection implementation plan

**Goal:** Express official/site price viewing, access-group selection and model-vendor filtering as separate user decisions.

**Architecture:** Keep the existing pricing table and API contract. Add an explicit official/site state to `ModelPlazaContent.vue`; remember a numeric access-group ID independently. Derive the existing catalog/group scope and table group from the active view. Keep the vendor filter's stable counts and add precise localized feedback.

**Tech stack:** Vue 3, TypeScript, vue-i18n, Tailwind, Vitest, Playwright CLI.

- [x] Add failing interaction tests in `frontend/src/components/modelPlaza/__tests__/ModelPlazaContent.priceView.spec.ts` for explicit views, group-only options, remembered groups, vendor mismatch recovery, search/empty states and data refresh.
- [x] Implement `priceView`, remembered group, derived scope, picker closure and localized empty feedback in `ModelPlazaContent.vue`. Keep existing pricing and monitoring behavior; preserve search/vendor selections.
- [x] Update `PlazaVendorFilter.vue` to label its universal choice All vendors and its counts as model counts. Add Chinese/English messages in the existing dashboard locale modules and use the public page's existing theme variables for selected/focus states.
- [x] Adapt `ModelPlazaContent.spec.ts` to enter the site view before opening its picker, remove official mode from group options, and retain the existing rate/identity/monitor/model-scope coverage.
- [x] Run `node node_modules/vitest/vitest.mjs run src/components/modelPlaza/__tests__ src/i18n/__tests__/localeKeyCompleteness.spec.ts --maxWorkers=1 --minWorkers=1 --no-cache` from the frontend, then the frontend production build.
- [x] Preview the actual page locally; check official/site transitions, desktop/mobile layout, and Qwen-zero recovery against a production-derived public fixture. Save browser evidence and verify the final diff before committing.

## Verification

- Seven new interaction tests failed on the previous component, then passed after the state/selector change. A further regression verifies that leaving the site view aborts an authenticated monitor request.
- Final focused suite: 165 tests in 8 files passed, including model-plaza pricing, group/monitor interactions and locale completeness.
- Final `pnpm run build` passed, including locale checks, vue-tsc and Vite. Existing bundle-size notices remain informational.
- Real browser: official catalog 64 models; site group `国模全套` 9 actual models. The Qwen-zero case gives a specific group/vendor message and restores all 9 models without changing the group. Search `kimi` finds 4 catalog models versus 3 actual group models; view toggles remember the group and search.
- Desktop 1440px and mobile 375px screenshots were reviewed in light/dark modes. Mobile search remains a full row and the document has no horizontal overflow. Browser interactions produced no JavaScript errors.
- Preview is served at `http://127.0.0.1:3001/model-plaza?embedded=1` using captured anonymous public responses; no server deployment is part of this change.
