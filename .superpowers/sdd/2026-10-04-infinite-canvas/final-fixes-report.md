# Infinite Canvas Final Fixes

## Files changed

- Canonicalized imported projects with `schemaVersion: 1` and retained fail-closed asset/project rollback.
- Added a switchable, authenticated-user-scoped IndexedDB repository factory and store reload boundary.
- Added prompt/config starter-node creation for new projects, the empty-canvas double-click path, and an explicit toolbar action. The nodes are connected for the generation workflow.
- Wired active-key image model discovery into the page, including group allowlist filtering, key-switch request invalidation, model selection, loading/empty/error/retry states, and in-memory-only credentials.
- Added focused regression coverage for import activation/reload, repository namespaces, allowlists, and empty-project creation.

## Verification

- `pnpm exec vitest run src/features/infiniteCanvas src/api/__tests__/imageGeneration.spec.ts` - passed (70 tests).
- `pnpm run lint:check` - passed.
- `pnpm run typecheck` - passed.
- `pnpm run build` - passed. Vite emitted existing chunk-size/dynamic-import warnings only.

## Concern

The authenticated browser smoke test and backend Go tests remain environment-dependent; this fix wave only changes the frontend canvas path and does not alter the pre-existing Codex-ticket working-tree changes.

## Repair Wave 2

- Exposed the non-sensitive `model_allowlist` field through the ordinary user group DTO and mapper.
- Matched canvas models using backend-compatible case-insensitive exact entries and trailing-`*` prefixes.
- Added generation guards to repository hydration and deferred closing retired IndexedDB repositories until the winning switch completes.
- Added regressions for wildcard filtering, page-level model loading/filtering, stale model responses after key switching, and stale repository hydration.

Additional verification:

- `pnpm exec vitest run src/features/infiniteCanvas src/api/__tests__/imageGeneration.spec.ts` - passed (77 tests).
- `pnpm run lint:check` - passed.
- `pnpm run typecheck` - passed.
- `pnpm run build` - passed (existing Vite chunk-size/dynamic-import warnings only).
- `go test ./internal/handler/dto -run 'TestGroupFromService(IncludesModelAllowlist|OmitsProfitControl|AdminIncludesProfitControl)' -count=1` - blocked by the pre-existing missing `ValidateOpenAICodexTicketHarvestProxyURL` symbol in `internal/service/setting_update.go`.
