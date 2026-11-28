# Domestic official pricing audit implementation plan

**Goal:** Complete the interrupted official CNY audit and represent verified no-charge cache writes explicitly as zero.

**Architecture:** Update the shared catalog from reviewed primary-source evidence. Retain the existing explicit-zero backend and table behavior, make Qwen imports emit the same zero semantics, and prevent secondary imports from overwriting primary-source DeepSeek cards.

**Tech stack:** Python unittest, Go service tests, Vue/Vitest and Playwright CLI.

- [x] Recover the interrupted requests and original worktree; capture primary vendor pricing pages and Qwen's live ListModelSeries response in `E:/workspace/agent/sub2api/.dev/domestic-pricing-audit-20261128`.
- [x] Add failing Python regressions in `backend/scripts/test_import_qwen_pricing.py`: absent official cache-write rows become zero in flat and tiered cards; positive rows stay positive. Add a primary-source DeepSeek preservation regression.
- [x] Add an end-to-end catalog regression in `backend/internal/service/domestic_official_pricing_test.go`: explicit free writes survive parsing and official plaza lookup, charged writes remain charged, and verified context tiers carry zero writes.
- [x] Run the Python and Go regressions before implementation and record their expected failures.
- [x] In `backend/scripts/import_qwen_pricing.py`, set `fields.setdefault("cache_creation_input_token_cost", 0.0)` after parsing explicit write quotes. In `backend/scripts/import_domestic_pricing.py`, preserve cards carrying `official_api_verification_status == "verified"` before the legacy DeepSeek override.
- [x] Apply reviewed native CNY source rows to the catalog. Record current verification metadata, exact context tiers and explicit cache-write zeros; retain unsupported/historical cards with a reason. Save the complete model audit and source provenance under `docs/`.
- [x] Run Python import tests, focused Go pricing/billing/plaza tests, frontend model-plaza tests, and a fresh official-source comparison. Check the existing Vue table with explicit zero prices in flat and tiered official/site views.
- [x] Compare the public production catalog to the final local catalog without mutating production. Review the final diff, commit only this work, and report verified/unverified counts and deployment state.


## Verification

- Python: 16 import tests passed, including an observed three-test failure before the import fix.
- Go: 3,187 pricing, billing, context and plaza tests/subtests passed with the unit tag. Tests observed missing zero quotes before fixes, including flat site quotes, tiered quotes and historical official-price suppression.
- Frontend: 165 model-plaza and locale tests passed. Fresh production build passed (existing chunk-size and Browserslist notices remain).
- Browser: final local catalog over the captured anonymous production fixture displays GLM writes as ¥0.00 and preserves Kimi K3.1 Pro ¥13/¥26 paid writes. Backend parsing/quote/schedule semantics are separately verified by Go tests.
- Current catalog: 100 CNY entries, 81 verified, 13 retired and 6 unconfirmed. Verified cards contain 55 explicit free token-write quotes and 26 positive write quotes. Zero means no separate token-write charge, not a promise of cache support or free time-based storage.
- GLM's <32K / >=32K boundary switches at exactly 32,000 tokens. Existing stale DeepSeek USD assertions failed under the baseline overlay as well, then were updated to the established CNY fallback policy.
- Public production before update still has null cache writes for GLM/DeepSeek/Kimi K2/MiMo and Qwen3.8-max cache read 0.22 CNY/M instead of the official 1.5 CNY/M.
- User authorized server-15 deployment after repair. No migrations are introduced; production channel/group/config prices must remain unchanged. Prepared private run directory: E:/workspace/agent/sub2api/.dev/release15-domestic-pricing-20261128.
