# Qwen official API price update

**Goal:** Update the built-in Qwen token prices to the official API prices before discounts.

**Approach:** Keep the existing JSON catalog and runtime schema. A small source importer validates native official amounts and units, maps exact model IDs, preserves whole-request context tiers, and records unsupported conditions instead of guessing. The user has explicitly requested applying these data changes.

- [x] Retrieve the official page and its versioned pricing component; identify `ListModelSeries`, `Price` and separate `Discount` fields.
- [x] Inventory existing Qwen entries, supported runtime fields, and direct-vendor preservation rules.
- [x] Add tests covering undiscounted values, cache kinds, context boundaries, official-source preservation and unsupported pricing conditions; observe failure.
- [x] Add the official Qwen importer and import the verified snapshot into `backend/resources/model-pricing/model_prices_and_context_window.json`.
- [x] Add focused Go coverage for official catalog values, parsed tiers and whole-request billing; run existing domestic-catalog operator-override regressions.
- [x] Run Python importer tests, focused Go pricing/catalog tests, JSON/diff checks and an independent comparison with the captured official response.
- [x] Record changed models, historical unmatched models, unsupported conditions and verification evidence.

## Verification evidence

- The new catalog regression failed against the previous built-in snapshot because official Qwen entries/prices were missing, then passed after the import.
- `python -m unittest test_import_qwen_pricing test_import_domestic_pricing -v`: 13 tests passed, including malformed/incomplete source rejection and protection against a later third-party price import.
- `go test -p 1 -tags unit ./internal/service -run 'Test(QwenOfficial|DomesticCatalog|IsDomesticPricingModelName|.*ContextPricing)' -count=1`: passed.
- Independently compared all 264 imported price fields and context boundaries against the saved official response, including undiscounted Qwen3.7-Plus values. All non-Qwen catalog entries are identical to the starting commit.
- Reimporting the same response/date produced an identical catalog. JSON parsing and `git diff --check` passed.
- No production deployment was requested for this update.
