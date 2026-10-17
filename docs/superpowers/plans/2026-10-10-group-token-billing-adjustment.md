# 分组 Token 计费倍率与缓存稳定性 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复分组 Token 倍率在认证缓存链路不生效的问题，分离客户计费用量与上游真实用量，并让启用倍率的请求对缓存命中/未命中保持稳定客户扣费。

**Architecture:** 认证查询和认证快照携带完整的分组倍率配置并升级快照版本。计费入口保留上游原始 usage，生成独立的客户计费用量；启用倍率的请求按逻辑输入量使用普通输入单价计算客户费用，账号统计与管理员原始用量继续使用缓存分类后的真实 usage。预扣估算复用同一纯函数。

**Tech Stack:** Go、Ent/PostgreSQL migrations、Gin DTO 映射、现有 Go 测试与 integration/unit build tags。

---

### Task 1: 固定认证投影和缓存快照缺字段的失败测试

**Files:**
- Create: `backend/internal/repository/api_key_repo_billing_projection_integration_test.go`
- Create: `backend/internal/service/api_key_auth_cache_billing_test.go`

- [ ] **Step 1: Write the failing tests**

  在认证投影集成测试中直接创建带 `BillingInputTokenMultiplier: 2`, `BillingOutputTokenMultiplier: 3`, `BillingTokenAdjustmentMinInputTokens: 100` 的分组，创建 API Key 后调用 `GetByKeyForAuth`，断言三值相同；在认证缓存测试中构造同样的 APIKey，执行 `snapshotFromAPIKey` → JSON 往返 → `snapshotToAPIKey`，断言三值保持不变。

- [ ] **Step 2: Run tests to verify the failure**

  Run: `go test -tags=unit ./internal/service -run 'TestAPIKeyAuthSnapshot.*Billing' -count=1`

  Expected: 缓存往返测试失败，恢复后的分组倍率为零；集成测试在启用 integration 数据库时失败，认证查询返回零值。

- [ ] **Step 3: Commit the red tests**

  Run: `git add backend/internal/repository/api_key_repo_billing_projection_integration_test.go backend/internal/service/api_key_auth_cache_billing_test.go; git commit -m "test: cover group billing fields in auth projections"`

---

### Task 2: 补齐认证投影、快照和版本失效

**Files:**
- Modify: `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/service/api_key_auth_cache.go`
- Modify: `backend/internal/service/api_key_auth_cache_impl.go`
- Test: `backend/internal/repository/api_key_repo_billing_projection_integration_test.go`
- Test: `backend/internal/service/api_key_auth_cache_billing_test.go`

- [ ] **Step 1: Add the three group fields to `GetByKeyForAuth`**

  Add `group.FieldBillingInputTokenMultiplier`, `group.FieldBillingOutputTokenMultiplier`, and `group.FieldBillingTokenAdjustmentMinInputTokens` to the existing explicit `WithGroup` select list.

- [ ] **Step 2: Add the fields to the auth snapshot**

  Add JSON fields to `APIKeyAuthGroupSnapshot`, copy them in `snapshotFromAPIKey`, and restore them in `snapshotToAPIKey`.

- [ ] **Step 3: Bump the snapshot version**

  Change `apiKeyAuthSnapshotVersion` from `27` to `28` and retain the existing stale-version eviction behavior.

- [ ] **Step 4: Run the focused tests**

  Run: `go test -tags=unit ./internal/service -run 'TestAPIKeyAuthSnapshot.*Billing|TestAPIKeyAuthSnapshot.*Version' -count=1`.

  Run: `go test -tags=integration ./internal/repository -run 'TestGetByKeyForAuthCarriesGroupBilling' -count=1` when the integration database is available.

- [ ] **Step 5: Commit**

  Run: `git add backend/internal/repository/api_key_repo.go backend/internal/service/api_key_auth_cache.go backend/internal/service/api_key_auth_cache_impl.go; git commit -m "fix: carry group token billing settings through auth"`

---

### Task 3: Add cache-neutral customer token billing helpers

**Files:**
- Modify: `backend/internal/service/token_usage_adjustment.go`
- Test: `backend/internal/service/token_usage_adjustment_test.go`

- [ ] **Step 1: Write failing tests**

  Add tests for logical input `200 + 600 cache read + 200 cache write` producing customer input `2000` at 2x while raw buckets remain unchanged, for a 99-token non-cache input staying unadjusted below threshold, and for zero/default multipliers preserving existing behavior.

- [ ] **Step 2: Run the tests and verify they fail**

  Run: `go test -tags=unit ./internal/service -run 'TestTokenUsageAdjustment.*Logical|TestTokenUsageAdjustment.*Cache' -count=1`.

  Expected: compile failure because the cache-neutral helper does not exist.

- [ ] **Step 3: Implement the minimal helper**

  Add a pure helper that receives non-cache input, cache-read, cache-write, output and the group adjustment. It returns raw counts plus customer billing counts, applies the minimum threshold to non-cache input, and treats logical input as the sum of the three input buckets only for an enabled non-1 adjustment. Keep negative values clamped and do not mutate the input struct.

- [ ] **Step 4: Run tests**

  Run: `go test -tags=unit ./internal/service -run 'TestTokenUsageAdjustment' -count=1`.

- [ ] **Step 5: Commit**

  Run: `git add backend/internal/service/token_usage_adjustment.go backend/internal/service/token_usage_adjustment_test.go; git commit -m "feat: make adjusted customer token billing cache neutral"`

---

### Task 4: Separate raw and customer usage in the service and database

**Files:**
- Modify: `backend/ent/schema/usage_log.go`
- Create: `backend/migrations/269_usage_log_upstream_token_counts.sql`
- Modify: `backend/internal/service/usage_log.go`
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Regenerate: `backend/ent/*` using the repository Ent generation command.

- [ ] **Step 1: Write failing service tests**

  Add generic and OpenAI RecordUsage tests that construct a group with 2x/3x, raw input 200/output 50, and cache buckets. Assert the persisted usage log exposes customer counts 400/150 to the customer fields, raw upstream counts 200/50 to admin fields, and `AccountStatsCost` uses raw buckets. Add a below-threshold test and a media request test.

- [ ] **Step 2: Run the tests and verify the failure**

  Run: `go test -tags=unit ./internal/service -run 'Test.*RecordUsage.*BillingAdjustment' -count=1`.

  Expected: tests fail because raw fields do not exist and current code mutates the shared usage object.

- [ ] **Step 3: Add raw token columns and mappings**

  Add nullable/default-zero integer fields `upstream_input_tokens` and `upstream_output_tokens` to the UsageLog schema and migration. Regenerate Ent, add service fields, repository create/read mapping, an admin-only DTO projection, and leave ordinary user mapper unchanged.

- [ ] **Step 4: Refactor generic and OpenAI RecordUsage**

  Capture raw usage before any customer adjustment. Build a separate customer usage object for token cost and quota deduction. Keep `result.Usage` unchanged for log metadata, cache classification, media paths and account statistics. Populate the raw admin fields from the captured values and customer-facing `InputTokens`/`OutputTokens` from the adjusted usage only for token billing.

- [ ] **Step 5: Run focused tests**

  Run: `go test -tags=unit ./internal/service -run 'Test.*RecordUsage.*BillingAdjustment|TestTokenUsageAdjustment' -count=1`.

  Run: `go test ./internal/handler/dto -run 'Usage' -count=1`.

- [ ] **Step 6: Commit**

  Run: `git add backend/ent backend/migrations/269_usage_log_upstream_token_counts.sql backend/internal/service/usage_log.go backend/internal/service/gateway_usage_billing.go backend/internal/service/openai_gateway_usage.go backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go; git commit -m "fix: separate customer and upstream token usage"`

---

### Task 5: Align inflight reservation with final adjusted billing

**Files:**
- Modify: `backend/internal/service/billing_inflight_reservation.go`
- Test: `backend/internal/service/billing_inflight_reservation_test.go`

- [ ] **Step 1: Write a failing reservation test**

  Add a group with 2x/3x and min input 100, estimate a normal token request with 200 input and 50 output, and assert the estimate includes adjusted customer input/output. Add an image/video case asserting the existing per-request estimate remains unchanged.

- [ ] **Step 2: Run the test and verify failure**

  Run: `go test -tags=unit ./internal/service -run 'TestInflight.*BillingAdjustment' -count=1`.

  Expected: the reservation estimate is lower than the final adjusted charge.

- [ ] **Step 3: Reuse the pure adjustment helper**

  Apply the same threshold and multiplier helper to the estimated token counts before applying text prices. Keep per-request image/video/audio branches unchanged.

- [ ] **Step 4: Run focused tests and commit**

  Run: `go test -tags=unit ./internal/service -run 'TestInflight.*BillingAdjustment|TestTokenUsageAdjustment' -count=1`.

  Commit: `git add backend/internal/service/billing_inflight_reservation.go backend/internal/service/billing_inflight_reservation_test.go; git commit -m "fix: align inflight reservation with group token billing"`

---

### Task 6: Verify cache stability and full regression coverage

**Files:**
- Test: `backend/internal/service/gateway_record_usage_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`
- Test: `backend/internal/service/token_usage_adjustment_test.go`

- [ ] **Step 1: Add cache invariants**

  Assert billing helpers do not mutate cache read/write counts, `ForceCacheBilling` keeps its existing classification, and a cache hit and cache miss with the same logical input produce the same customer charge under an enabled group adjustment.

- [ ] **Step 2: Run all focused checks**

  Run: `go test -tags=unit ./internal/service -run 'TokenUsageAdjustment|GatewayServiceRecordUsage|OpenAIGatewayServiceRecordUsage|Inflight' -count=1`.

  Run: `go test ./internal/service -count=1`.

  Run: `go test ./... -run '^$'`.

- [ ] **Step 3: Run repository and frontend checks**

  Run: `go test ./internal/repository -count=1`.

  Run: `pnpm run typecheck`.

- [ ] **Step 4: Review the diff**

  Confirm only the intended billing, auth cache, usage DTO, migration and test files changed; do not add the workspace's existing untracked screenshots/build artifacts.

- [ ] **Step 5: Commit verification metadata if needed**

  Keep generated code and migration changes in the implementation commits; do not push to a remote branch unless explicitly requested.

