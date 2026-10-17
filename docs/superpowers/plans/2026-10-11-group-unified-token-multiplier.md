# 分组 Token 统一倍率 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 分组增加 Token 统一倍率，把客户侧 input / output / cache_read / cache_write 乘同一个系数，按各桶单价计费，客户命中率与上游一致。

**Architecture:** 在现有 `TokenUsageAdjustment` 上增加 unified 模式，优先于分项输入/输出倍率。客户计费、用户用量日志、下游 API usage 共用同一套放大后的四桶；管理员 `upstream_*` 和账号统计继续用上游原值。认证快照携带新字段并升到 v29。未改配的分项倍率分组仍走缓存中性 split 逻辑。

**Tech Stack:** Go、Ent/PostgreSQL migrations、Gin DTO、Vue 3 GroupsView、现有 `go test -tags=unit`。

**Spec:** `docs/superpowers/specs/2026-10-11-group-unified-token-multiplier-design.md`

Go 命令在 `backend/` 下运行。

---

## File map

| File | Responsibility |
|---|---|
| `backend/internal/service/token_usage_adjustment.go` | unified / split / none 纯函数 |
| `backend/internal/service/downstream_usage_projection.go` | API 响应 usage 四桶改写 |
| `backend/ent/schema/group.go` | `billing_unified_token_multiplier` |
| `backend/migrations/270_group_unified_token_multiplier.sql` | 加列 + auth cache 失效触发器 |
| `backend/internal/service/group.go` | Group 字段 |
| `backend/internal/repository/group_repo.go` | Create/Update 写入 |
| `backend/internal/repository/api_key_repo.go` | 认证 Select + `groupEntityToService` |
| `backend/internal/service/api_key_auth_cache.go` | 快照 JSON 字段 |
| `backend/internal/service/api_key_auth_cache_impl.go` | 快照拷贝与版本 29 |
| `backend/internal/service/admin_service.go` / `admin_group.go` | 创建/更新校验，unified 时清分项 |
| `backend/internal/handler/admin/group_handler.go` | 请求字段 |
| `backend/internal/handler/dto/types.go` / `mappers.go` | 管理员 DTO |
| `backend/internal/service/gateway_usage_billing.go` | 通用网关客户桶与日志 |
| `backend/internal/service/openai_gateway_usage.go` | OpenAI 客户桶与日志 |
| `backend/internal/service/billing_inflight_reservation.go` | 预扣复用同一纯函数 |
| `frontend/src/views/admin/GroupsView.vue` | 统一倍率表单 |
| `frontend/src/types/index.ts` | AdminGroup / 请求类型 |
| `frontend/src/i18n/locales/{zh,en}/admin/overview.ts` | 文案 |

---

### Task 1: TokenUsageAdjustment 增加 unified 模式

**Files:**
- Modify: `backend/internal/service/token_usage_adjustment.go`
- Test: `backend/internal/service/token_usage_adjustment_test.go`

- [ ] **Step 1: Write the failing tests**

在 `token_usage_adjustment_test.go` 追加：

```go
func TestTokenUsageAdjustmentUnifiedScalesAllBuckets(t *testing.T) {
	adjustment := TokenUsageAdjustment{UnifiedMultiplier: 2, MinInputTokens: 100}
	raw := TokenUsageBuckets{InputTokens: 200, CacheReadTokens: 600, CacheWriteTokens: 200, OutputTokens: 50}
	got := adjustment.ApplyToUsage(raw)
	if got.Raw != raw {
		t.Fatalf("raw usage changed: %+v", got.Raw)
	}
	want := TokenUsageBuckets{InputTokens: 400, CacheReadTokens: 1200, CacheWriteTokens: 400, OutputTokens: 100}
	if got.Customer != want {
		t.Fatalf("customer usage = %+v, want %+v", got.Customer, want)
	}
	if !got.Applied || got.InputTokens != 400 || got.OutputTokens != 100 {
		t.Fatalf("aliases = %+v", got)
	}
}

func TestTokenUsageAdjustmentUnifiedPreservesCacheHitRate(t *testing.T) {
	adjustment := TokenUsageAdjustment{UnifiedMultiplier: 2, MinInputTokens: 0}
	raw := TokenUsageBuckets{InputTokens: 200, CacheReadTokens: 600, CacheWriteTokens: 200, OutputTokens: 50}
	got := adjustment.ApplyToUsage(raw)
	rawTotal := raw.InputTokens + raw.CacheReadTokens + raw.CacheWriteTokens
	gotTotal := got.Customer.InputTokens + got.Customer.CacheReadTokens + got.Customer.CacheWriteTokens
	if rawTotal == 0 || gotTotal == 0 {
		t.Fatal("empty totals")
	}
	rawRate := float64(raw.CacheReadTokens) / float64(rawTotal)
	gotRate := float64(got.Customer.CacheReadTokens) / float64(gotTotal)
	if absFloat(rawRate-gotRate) > 0.0001 {
		t.Fatalf("hit rate %v -> %v", rawRate, gotRate)
	}
}

func TestTokenUsageAdjustmentUnifiedWinsOverSplitMultipliers(t *testing.T) {
	adjustment := TokenUsageAdjustment{
		UnifiedMultiplier: 2,
		InputMultiplier:   5,
		OutputMultiplier:  7,
		MinInputTokens:    0,
	}
	got := adjustment.ApplyToUsage(TokenUsageBuckets{InputTokens: 100, CacheReadTokens: 50, OutputTokens: 10})
	if got.Customer.InputTokens != 200 || got.Customer.CacheReadTokens != 100 || got.Customer.OutputTokens != 20 {
		t.Fatalf("unified did not win: %+v", got.Customer)
	}
	if got.Customer.CacheReadTokens == 0 {
		t.Fatal("unified must retain scaled cache buckets")
	}
}

func TestTokenUsageAdjustmentSplitStillFoldsCacheWhenUnifiedIsOne(t *testing.T) {
	adjustment := TokenUsageAdjustment{UnifiedMultiplier: 1, InputMultiplier: 2, OutputMultiplier: 3, MinInputTokens: 0}
	got := adjustment.ApplyToUsage(TokenUsageBuckets{InputTokens: 200, CacheReadTokens: 600, CacheWriteTokens: 200, OutputTokens: 50})
	if got.Customer.InputTokens != 2000 || got.Customer.OutputTokens != 150 {
		t.Fatalf("split customer = %+v", got.Customer)
	}
	if got.Customer.CacheReadTokens != 0 || got.Customer.CacheWriteTokens != 0 {
		t.Fatalf("split must zero cache: %+v", got.Customer)
	}
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit ./internal/service -run 'TestTokenUsageAdjustmentUnified' -count=1`

Expected: FAIL，`UnifiedMultiplier` 未定义或四桶仍被折进 input。

- [ ] **Step 3: Implement unified mode**

更新 `TokenUsageAdjustment`：

```go
type TokenUsageAdjustment struct {
	UnifiedMultiplier float64
	InputMultiplier   float64
	OutputMultiplier  float64
	MinInputTokens    int
}
```

`normalized()` 对 `UnifiedMultiplier` 使用与 input/output 相同的规则（`<=0` / NaN / Inf → 1）。

增加：

```go
func (a TokenUsageAdjustment) unifiedEnabled() bool {
	return a.normalized().UnifiedMultiplier != 1
}
```

改 `ApplyToUsage`：在阈值判断里 `adjustmentEnabled := a.unifiedEnabled() || a.InputMultiplier != 1 || a.OutputMultiplier != 1`。过阈值后若 `unifiedEnabled()`：

```go
result.Customer = TokenUsageBuckets{
	InputTokens:      adjustedTokenCount(raw.InputTokens, a.UnifiedMultiplier),
	CacheReadTokens:  adjustedTokenCount(raw.CacheReadTokens, a.UnifiedMultiplier),
	CacheWriteTokens: adjustedTokenCount(raw.CacheWriteTokens, a.UnifiedMultiplier),
	OutputTokens:     adjustedTokenCount(raw.OutputTokens, a.UnifiedMultiplier),
}
result.Applied = true
```

否则保持现有 split：逻辑输入求和、客户缓存为 0。

`tokenUsageAdjustmentForGroup` 在本任务先不改 Group 字段；Task 3 再接入。

同一文件增加 TTL 明细辅助，供 Task 6 使用：

```go
func scaleCacheTTLBuckets(fiveMinute, oneHour int, multiplier float64, scaledWrite int) (int, int) {
	fiveMinute = adjustedTokenCount(fiveMinute, multiplier)
	oneHour = adjustedTokenCount(oneHour, multiplier)
	return fitCacheBuckets(scaledWrite, fiveMinute, oneHour)
}
```

`fitCacheBuckets` 目前在 `downstream_usage_projection.go`。把它挪到 `token_usage_adjustment.go`（同 package），两个文件共用，避免重复。

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit ./internal/service -run 'TestTokenUsageAdjustment' -count=1`

Expected: PASS。现有 split 测试继续通过。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/token_usage_adjustment.go backend/internal/service/token_usage_adjustment_test.go backend/internal/service/downstream_usage_projection.go
git commit -m "feat: add unified token usage adjustment"
```

若移动 `fitCacheBuckets` 导致 projection 文件只改引用，一并加入。

---

### Task 2: 下游 usage 按 unified 四桶改写

**Files:**
- Modify: `backend/internal/service/downstream_usage_projection.go`
- Test: `backend/internal/service/downstream_usage_projection_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestProjectDownstreamUsageJSONUnifiedScalesAnthropicCacheBuckets(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":80,"output_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{UnifiedMultiplier: 2})
	if !changed {
		t.Fatal("expected unified usage projection")
	}
	var body struct {
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 160 || body.Usage.OutputTokens != 20 || body.Usage.CacheReadTokens != 40 || body.Usage.CacheCreationTokens != 10 {
		t.Fatalf("projected = %+v, want 160/20/40/10", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONUnifiedScalesOpenAICachedTokens(t *testing.T) {
	input := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":5}}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{UnifiedMultiplier: 2})
	if !changed {
		t.Fatal("expected unified usage projection")
	}
	var body struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Usage.PromptTokens != 200 || body.Usage.CompletionTokens != 20 || body.Usage.TotalTokens != 220 {
		t.Fatalf("openai totals = %+v", body.Usage)
	}
	if body.Usage.PromptTokensDetails.CachedTokens != 40 || body.Usage.PromptTokensDetails.CacheWriteTokens != 10 {
		t.Fatalf("openai cache = %+v", body.Usage.PromptTokensDetails)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit ./internal/service -run 'TestProjectDownstreamUsageJSONUnified' -count=1`

Expected: FAIL，`downstreamUsageAdjustmentEnabled` 看不到 unified。

- [ ] **Step 3: Implement projection**

```go
func downstreamUsageAdjustmentEnabled(adjustment TokenUsageAdjustment) bool {
	adjustment = adjustment.normalized()
	return adjustment.UnifiedMultiplier != 1 || adjustment.InputMultiplier != 1 || adjustment.OutputMultiplier != 1
}
```

改 `projectUsageBuckets`：`ApplyForRequestUsage` 之后若 `adjustment.unifiedEnabled()`：

```go
projectedRead := adjusted.Customer.CacheReadTokens
projectedWrite := adjusted.Customer.CacheWriteTokens
total := adjusted.Customer.InputTokens + projectedRead + projectedWrite
if total < adjusted.Customer.InputTokens {
	total = maxIntValue()
}
projectedRead, projectedWrite = fitCacheBuckets(total, projectedRead, projectedWrite)
return total, adjusted.Customer.OutputTokens, projectedRead, projectedWrite, true
```

split 路径保持：用 `InputMultiplier` 缩放缓存再 fit 到折进后的客户 input。

`updateUsageCacheFields` 的 nested 5m/1h 在 unified 时传入 `UnifiedMultiplier`，split 时仍用 `InputMultiplier`。在调用处按 `unifiedEnabled()` 选择倍率。

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit ./internal/service -run 'TestProjectDownstreamUsageJSON' -count=1`

Expected: PASS。原有 split 测试和 `TestProjectDownstreamUsageJSONWithEligibilityProjectsAnthropicOutputFrame` 继续通过。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/downstream_usage_projection.go backend/internal/service/downstream_usage_projection_test.go
git commit -m "feat: project unified cache buckets to downstream usage"
```

---

### Task 3: 分组字段、迁移、Ent、仓库映射

**Files:**
- Modify: `backend/ent/schema/group.go`
- Create: `backend/migrations/270_group_unified_token_multiplier.sql`
- Modify: `backend/internal/service/group.go`
- Modify: `backend/internal/repository/group_repo.go`
- Modify: `backend/internal/repository/api_key_repo.go`
- Regenerate: `backend/ent/*` via `make generate`（在 `backend/`）

- [ ] **Step 1: Add the Ent field next to the existing multipliers**

在 `billing_output_token_multiplier` 后：

```go
field.Float("billing_unified_token_multiplier").
	SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
	Default(1.0).
	Comment("下游计费对 input/output/cache_read/cache_write 的统一 Token 倍率；1.0 表示沿用上游计量"),
```

- [ ] **Step 2: Add migration 270**

把 `267_group_token_adjustment_scope.sql` 里的 `enqueue_group_auth_cache_invalidation()` 整段复制过来，在 `billing_output_token_multiplier` 比较后增加：

```sql
AND OLD.billing_unified_token_multiplier IS NOT DISTINCT FROM NEW.billing_unified_token_multiplier
```

文件开头：

```sql
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS billing_unified_token_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0;

COMMENT ON COLUMN groups.billing_unified_token_multiplier IS
    '下游计费对 input/output/cache_read/cache_write 的统一 Token 倍率；1.0 表示沿用上游计量';
```

然后 `CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation() ...`。

- [ ] **Step 3: Generate Ent**

Run: `make generate`

Expected: `ent/group.go` 等出现 `BillingUnifiedTokenMultiplier`。

- [ ] **Step 4: Wire the service and repository**

`Group` 增加：

```go
BillingUnifiedTokenMultiplier float64
```

放在两个分项倍率旁边，注释：统一倍率优先于分项输入/输出倍率。

`group_repo.go` 的 Create 与 Update builder 增加 `.SetBillingUnifiedTokenMultiplier(groupIn.BillingUnifiedTokenMultiplier)`。

`api_key_repo.go`：

- `GetByKeyForAuth` 的 `WithGroup` Select 增加 `group.FieldBillingUnifiedTokenMultiplier`
- `groupEntityToService` 增加 `BillingUnifiedTokenMultiplier: g.BillingUnifiedTokenMultiplier`

`tokenUsageAdjustmentForGroup`：

```go
return TokenUsageAdjustment{
	UnifiedMultiplier: group.BillingUnifiedTokenMultiplier,
	InputMultiplier:   group.BillingInputTokenMultiplier,
	OutputMultiplier:  group.BillingOutputTokenMultiplier,
	MinInputTokens:    group.BillingTokenAdjustmentMinInputTokens,
}.normalized()
```

- [ ] **Step 5: Extend mapping tests**

修改 `backend/internal/repository/api_key_repo_messages_dispatch_unit_test.go` 的 `TestAPIKeyRepository_GetByKeyForAuth_PreservesBillingTokenAdjustment_SQLite`：创建分组时 `.SetBillingUnifiedTokenMultiplier(2.5)`，断言 `got.Group.BillingUnifiedTokenMultiplier` 为 2.5。

在 `token_usage_adjustment_test.go` 追加：

```go
func TestTokenUsageAdjustmentForGroupPrefersUnifiedMultiplier(t *testing.T) {
	group := &Group{
		BillingUnifiedTokenMultiplier:        2,
		BillingInputTokenMultiplier:          5,
		BillingOutputTokenMultiplier:         7,
		BillingTokenAdjustmentMinInputTokens: 0,
	}
	got := tokenUsageAdjustmentForGroup(group).ApplyToUsage(TokenUsageBuckets{InputTokens: 10, OutputTokens: 4})
	if got.Customer.InputTokens != 20 || got.Customer.OutputTokens != 8 {
		t.Fatalf("group unified = %+v", got.Customer)
	}
}
```

Run: `go test -tags=unit ./internal/repository -run 'TestAPIKeyRepository_GetByKeyForAuth_PreservesBillingTokenAdjustment_SQLite' -count=1`

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add backend/ent backend/migrations/270_group_unified_token_multiplier.sql backend/internal/service/group.go backend/internal/service/token_usage_adjustment.go backend/internal/service/token_usage_adjustment_test.go backend/internal/repository/group_repo.go backend/internal/repository/api_key_repo.go backend/internal/repository/api_key_repo_messages_dispatch_unit_test.go
git commit -m "feat: persist group unified token multiplier"
```

---

### Task 4: 认证快照升到 v29

**Files:**
- Modify: `backend/internal/service/api_key_auth_cache.go`
- Modify: `backend/internal/service/api_key_auth_cache_impl.go`
- Test: `backend/internal/service/api_key_auth_cache_billing_test.go`

- [ ] **Step 1: Write failing roundtrip and eviction tests**

在 `TestAPIKeyAuthSnapshotBillingTokenAdjustmentRoundtrip` 里设置 `apiKey.Group.BillingUnifiedTokenMultiplier = 2.25`，断言恢复后为 2.25。

追加：

```go
func TestAPIKeyService_RejectsV28AuthSnapshotWithoutUnifiedMultiplier(t *testing.T) {
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), profitAuthTestAPIKey())
	require.NotNil(t, snapshot)
	snapshot.Version = 28
	materialized, used, err := svc.applyAuthCacheEntry("sk-old-unified-multiplier", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, used, "v28 snapshots must be evicted after unified multiplier is added")
	require.Nil(t, materialized)
}
```

保留现有 v27 拒绝测试。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit ./internal/service -run 'TestAPIKeyAuthSnapshotBillingTokenAdjustmentRoundtrip|TestAPIKeyService_RejectsV28' -count=1`

Expected: roundtrip 恢复 0；v28 测试因当前版本仍是 28 而 `used==true` 失败。

- [ ] **Step 3: Add snapshot field and bump version**

`APIKeyAuthGroupSnapshot` 增加 `BillingUnifiedTokenMultiplier float64 \`json:"billing_unified_token_multiplier"\``。

`snapshotFromAPIKey` 与 `snapshotToAPIKey`（`applyAuthCacheEntry` 里组分组的那段）拷贝该字段。

`apiKeyAuthSnapshotVersion = 29 // v29: preserve group unified token multiplier`

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit ./internal/service -run 'TestAPIKeyAuthSnapshot|TestAPIKeyService_RejectsV' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/api_key_auth_cache.go backend/internal/service/api_key_auth_cache_impl.go backend/internal/service/api_key_auth_cache_billing_test.go
git commit -m "feat: carry unified token multiplier in auth snapshot"
```

---

### Task 5: 管理端创建/更新：unified 优先并清分项

**Files:**
- Modify: `backend/internal/service/admin_service.go`
- Modify: `backend/internal/service/admin_group.go`
- Modify: `backend/internal/handler/admin/group_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Test: `backend/internal/service/admin_service_group_test.go`

- [ ] **Step 1: Write failing admin tests**

```go
func TestAdminService_CreateGroup_UnifiedMultiplierClearsSplitMultipliers(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 1}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                            "unified",
		Platform:                        PlatformAnthropic,
		RateMultiplier:                  1,
		BillingUnifiedTokenMultiplier:   2,
		BillingInputTokenMultiplier:     5,
		BillingOutputTokenMultiplier:    7,
	})
	require.NoError(t, err)
	require.InDelta(t, 2, repo.created.BillingUnifiedTokenMultiplier, 1e-12)
	require.InDelta(t, 1, repo.created.BillingInputTokenMultiplier, 1e-12)
	require.InDelta(t, 1, repo.created.BillingOutputTokenMultiplier, 1e-12)
}

func TestAdminService_UpdateGroup_UnifiedMultiplierClearsSplitEvenWhenOmitted(t *testing.T) {
	existing := &Group{
		ID:                            9,
		Name:                          "g",
		Platform:                      PlatformAnthropic,
		RateMultiplier:                1,
		BillingInputTokenMultiplier:   2,
		BillingOutputTokenMultiplier:  3,
		BillingUnifiedTokenMultiplier: 1,
	}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}
	unified := 1.5
	_, err := svc.UpdateGroup(context.Background(), 9, &UpdateGroupInput{
		BillingUnifiedTokenMultiplier: &unified,
	})
	require.NoError(t, err)
	require.InDelta(t, 1.5, repo.updated.BillingUnifiedTokenMultiplier, 1e-12)
	require.InDelta(t, 1, repo.updated.BillingInputTokenMultiplier, 1e-12)
	require.InDelta(t, 1, repo.updated.BillingOutputTokenMultiplier, 1e-12)
}

func TestAdminService_CreateGroup_SplitMultipliersRemainWhenUnifiedIsOne(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 1}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                         "split",
		Platform:                     PlatformAnthropic,
		RateMultiplier:               1,
		BillingInputTokenMultiplier:  2,
		BillingOutputTokenMultiplier: 3,
	})
	require.NoError(t, err)
	require.InDelta(t, 1, repo.created.BillingUnifiedTokenMultiplier, 1e-12)
	require.InDelta(t, 2, repo.created.BillingInputTokenMultiplier, 1e-12)
	require.InDelta(t, 3, repo.created.BillingOutputTokenMultiplier, 1e-12)
}
```

`CreateGroup`/`UpdateGroup` 的 stub 构造方式与同文件其它测试保持一致；若 `adminServiceImpl` 还需要其它 nil repo，仿照 `TestAdminService_CreateGroup_WithImagePricing`。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit ./internal/service -run 'TestAdminService_CreateGroup_Unified|TestAdminService_UpdateGroup_Unified|TestAdminService_CreateGroup_SplitMultipliersRemain' -count=1`

Expected: 编译失败或字段仍为 5/7。

- [ ] **Step 3: Implement API plumbing**

`CreateGroupInput` / `UpdateGroupInput` 增加 `BillingUnifiedTokenMultiplier`（创建用 `float64`，更新用 `*float64`）。

`CreateGroupRequest` / `UpdateGroupRequest` 增加同名 JSON。`GroupHandler.Create` / `Update` 传入该字段。

`AdminGroup` 与 `GroupFromServiceAdmin` 增加 `BillingUnifiedTokenMultiplier`。

`CreateGroup`：`billingUnified, err := normalizeBillingTokenMultiplier(input.BillingUnifiedTokenMultiplier)`，写入 group；若 `billingUnified != 1`，把已归一化的 input/output 改成 `1`。

`UpdateGroup`：若 `input.BillingUnifiedTokenMultiplier != nil`，归一化后写入；若值 `!= 1`，无论请求有没有带分项字段，都设 `group.BillingInputTokenMultiplier = 1` 和 `group.BillingOutputTokenMultiplier = 1`。

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit ./internal/service -run 'TestAdminService_CreateGroup_Unified|TestAdminService_UpdateGroup_Unified|TestAdminService_CreateGroup_SplitMultipliersRemain|TestNormalizeBillingTokenMultiplier' -count=1`

Run: `go test ./internal/handler/dto -run 'Group' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/admin_service.go backend/internal/service/admin_group.go backend/internal/service/admin_service_group_test.go backend/internal/handler/admin/group_handler.go backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go
git commit -m "feat: accept unified token multiplier on group admin API"
```

---

### Task 6: RecordUsage 客户日志与扣费使用 unified 四桶

**Files:**
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Test: `backend/internal/service/gateway_record_usage_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`

- [ ] **Step 1: Write failing RecordUsage tests**

通用网关：

```go
func TestGatewayServiceRecordUsage_UnifiedMultiplierScalesCacheBuckets(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"claude-sonnet-4": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 0.1e-6, CacheCreationInputTokenCost: 1.25e-6},
	}}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	svc.billingService = NewBillingService(svc.cfg, prices)
	groupID := int64(931)
	group := &Group{
		ID:                            groupID,
		RateMultiplier:                1,
		BillingUnifiedTokenMultiplier: 2,
		BillingTokenAdjustmentMinInputTokens: 0,
	}
	require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_unified_cache",
			Model:     "claude-sonnet-4",
			Usage: ClaudeUsage{
				InputTokens: 200, OutputTokens: 50,
				CacheReadInputTokens: 600, CacheCreationInputTokens: 200,
			},
		},
		APIKey:  &APIKey{ID: 831, GroupID: &groupID, Group: group},
		User:    &User{ID: 631},
		Account: &Account{ID: 731},
	}))
	require.Equal(t, 400, usageRepo.lastLog.InputTokens)
	require.Equal(t, 100, usageRepo.lastLog.OutputTokens)
	require.Equal(t, 1200, usageRepo.lastLog.CacheReadTokens)
	require.Equal(t, 400, usageRepo.lastLog.CacheCreationTokens)
	require.Equal(t, 200, usageRepo.lastLog.UpstreamInputTokens)
	require.Equal(t, 50, usageRepo.lastLog.UpstreamOutputTokens)
	want := 400*1e-6 + 100*2e-6 + 1200*0.1e-6 + 400*1.25e-6
	require.InDelta(t, want, usageRepo.lastLog.ActualCost, 1e-12)
}
```

OpenAI：同样构造 `BillingUnifiedTokenMultiplier: 2`，上游 `InputTokens: 1000`（含缓存）、`CacheReadInputTokens: 600`、`CacheCreationInputTokens: 200`、`OutputTokens: 50`。客户非缓存 input = `(1000-600-200)*2 = 400`，cache_read=1200，cache_write=400，output=100。`UpstreamInputTokens` 仍为拆分后的 200。

现有 split 测试 `TestGatewayServiceRecordUsage_GroupTokenAdjustmentDoesNotChangeCacheCharge` 保持：分项倍率下命中/未命中实收仍相同。

OpenAI 的 `TestOpenAIGatewayServiceRecordUsage_GroupTokenAdjustmentKeepsUpstreamUsage` 在 split 下客户缓存桶为 0。按 spec 用户日志改用 Customer 桶后，该测试的 `CacheReadTokens`/`CacheCreationTokens` 应变为 `0`，`InputTokens` 变为逻辑输入 × 2 = 2000（200+600+200）。**先改这个断言**，再加 unified 测试，避免 split 行为被误当成 bug。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit ./internal/service -run 'TestGatewayServiceRecordUsage_Unified|TestOpenAIGatewayServiceRecordUsage_Unified|TestOpenAIGatewayServiceRecordUsage_GroupTokenAdjustmentKeepsUpstreamUsage' -count=1`

Expected: unified 日志仍是原缓存值；或 OpenAI split 测试在改断言后失败。

- [ ] **Step 3: Record customer buckets including cache**

通用网关 `gateway_usage_billing.go` 在 `ApplyForRequestUsage` 之后：

```go
billingResult.Usage.InputTokens = adjusted.Customer.InputTokens
billingResult.Usage.OutputTokens = adjusted.Customer.OutputTokens
displayResult.Usage.InputTokens = adjusted.Customer.InputTokens
displayResult.Usage.OutputTokens = adjusted.Customer.OutputTokens
if adjusted.Applied {
	if adjustment.unifiedEnabled() {
		billingResult.Usage.CacheReadInputTokens = adjusted.Customer.CacheReadTokens
		billingResult.Usage.CacheCreationInputTokens = adjusted.Customer.CacheWriteTokens
		five, one := scaleCacheTTLBuckets(
			rawResult.Usage.CacheCreation5mTokens,
			rawResult.Usage.CacheCreation1hTokens,
			adjustment.normalized().UnifiedMultiplier,
			adjusted.Customer.CacheWriteTokens,
		)
		billingResult.Usage.CacheCreation5mTokens = five
		billingResult.Usage.CacheCreation1hTokens = one
		displayResult.Usage.CacheReadInputTokens = adjusted.Customer.CacheReadTokens
		displayResult.Usage.CacheCreationInputTokens = adjusted.Customer.CacheWriteTokens
		displayResult.Usage.CacheCreation5mTokens = five
		displayResult.Usage.CacheCreation1hTokens = one
	} else {
		billingResult.Usage.CacheReadInputTokens = 0
		billingResult.Usage.CacheCreationInputTokens = 0
		billingResult.Usage.CacheCreation5mTokens = 0
		billingResult.Usage.CacheCreation1hTokens = 0
		displayResult.Usage.CacheReadInputTokens = 0
		displayResult.Usage.CacheCreationInputTokens = 0
		displayResult.Usage.CacheCreation5mTokens = 0
		displayResult.Usage.CacheCreation1hTokens = 0
	}
}
```

删除 `ApplyForRequest` 那条只放大非缓存的 display 分叉。

OpenAI `openai_gateway_usage.go`：

```go
displayInputTokens = adjusted.Customer.InputTokens
displayOutputTokens = adjusted.Customer.OutputTokens
```

`UsageTokens` 已用 `adjusted.Customer` 的读写桶，unified 下会自动带缩放后的缓存。写日志时：

```go
CacheCreationTokens: adjusted.Customer.CacheWriteTokens,
CacheReadTokens:     adjusted.Customer.CacheReadTokens,
```

不要再写 `result.Usage.CacheReadInputTokens` 原值。`UpstreamInputTokens` 仍是 `rawActualInputTokens`。

若 OpenAI 有 5m/1h 字段，同样用 `scaleCacheTTLBuckets`。

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit ./internal/service -run 'TestGatewayServiceRecordUsage_GroupToken|TestGatewayServiceRecordUsage_Unified|TestOpenAIGatewayServiceRecordUsage_GroupToken|TestOpenAIGatewayServiceRecordUsage_Unified' -count=1`

Expected: PASS。图片按次测试仍不放大 token。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/gateway_usage_billing.go backend/internal/service/openai_gateway_usage.go backend/internal/service/gateway_record_usage_test.go backend/internal/service/openai_gateway_record_usage_test.go
git commit -m "feat: bill and log unified cache token buckets"
```

---

### Task 7: 预扣估算使用 unified

**Files:**
- Modify: `backend/internal/service/billing_inflight_reservation.go`（仅当需要；现已调用 `ApplyToUsage`）
- Test: `backend/internal/service/billing_inflight_reservation_test.go`

- [ ] **Step 1: Write a failing reservation test**

```go
func TestInflightEstimate_UnifiedMultiplierScalesInputAndOutput(t *testing.T) {
	groupID := int64(22)
	group := &Group{
		ID:                            groupID,
		Platform:                      PlatformAnthropic,
		RateMultiplier:                1,
		BillingUnifiedTokenMultiplier: 2,
		BillingInputTokenMultiplier:   5,
		BillingOutputTokenMultiplier:  7,
		BillingTokenAdjustmentMinInputTokens: 100,
	}
	svc := newInflightEstimateGateway(t, nil)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: group}
	pricing, err := svc.billingService.GetModelPricing("claude-sonnet-4-5")
	require.NoError(t, err)
	estimate, priced := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{
		Model:     "claude-sonnet-4-5",
		BodyBytes: 800,
		MaxTokens: 50,
	})
	require.True(t, priced)
	require.InDelta(t, 400*pricing.InputPricePerToken+100*pricing.OutputPricePerToken, estimate, 1e-12)
}
```

BodyBytes/4 = 200 input，unified 2 → 400/100；分项 5/7 不得生效。

- [ ] **Step 2: Run the test**

Run: `go test -tags=unit ./internal/service -run 'TestInflightEstimate_UnifiedMultiplierScalesInputAndOutput' -count=1`

Expected: Task 3 已把 unified 写入 `tokenUsageAdjustmentForGroup` 时，此测试在 Task 1 完成后即应 PASS。若 FAIL，检查预扣是否仍调用 `Apply` 而不是 `ApplyToUsage`，或 Group 字段未传入。

- [ ] **Step 3: Commit**

```bash
git add backend/internal/service/billing_inflight_reservation.go backend/internal/service/billing_inflight_reservation_test.go
git commit -m "test: cover unified multiplier in inflight reservation"
```

---

### Task 8: 管理端表单改为统一倍率

**Files:**
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/overview.ts`
- Modify: `frontend/src/i18n/locales/en/admin/overview.ts`

当前标签是硬编码中文。改为 `t('admin.groups.form.unifiedTokenMultiplier')` 等，与旁边的 `rateMultiplierLabel` 一致。

- [ ] **Step 1: Add types and i18n**

`AdminGroup`：

```ts
billing_unified_token_multiplier: number
```

`CreateGroupRequest` / `UpdateGroupRequest` 增加 `billing_unified_token_multiplier?: number`。

中文：

```ts
unifiedTokenMultiplier: 'Token 统一倍率',
unifiedTokenMultiplierHint: '同时放大输入、输出、缓存读取和缓存写入。客户命中率与上游一致，费用按各桶单价乘该倍率。默认 1.00。',
unifiedTokenMultiplierSplitHint: '该分组仍在使用旧的输入/输出分项倍率。保存统一倍率（不等于 1）后改为四桶同倍率。',
tokenAdjustmentMinInput: 'Token 倍率生效的最小输入量',
tokenAdjustmentMinInputHint: '原始非缓存输入 Token 少于此值时不应用 Token 倍率；0 表示所有正常请求都应用。探针不使用自定义倍率。',
```

英文对应翻译。放在 `admin.groups.form` 里。

- [ ] **Step 2: Replace the two multiplier inputs**

创建和编辑表单中，删除「下游输入/输出 Token 计费倍率」两个框，换成一个：

```vue
<div>
  <label class="input-label">{{ t('admin.groups.form.unifiedTokenMultiplier') }}</label>
  <input
    v-model.number="createForm.billing_unified_token_multiplier"
    type="number"
    min="0.1"
    max="10"
    step="0.01"
    class="input"
  />
  <p class="input-hint">{{ t('admin.groups.form.unifiedTokenMultiplierHint') }}</p>
  <p
    v-if="createForm.billing_unified_token_multiplier === 1 && (createForm.billing_input_token_multiplier !== 1 || createForm.billing_output_token_multiplier !== 1)"
    class="input-hint"
  >
    {{ t('admin.groups.form.unifiedTokenMultiplierSplitHint') }}
  </p>
</div>
```

编辑表单同样。`billing_input_token_multiplier` / `billing_output_token_multiplier` 留在 reactive 对象里，界面不展示，避免改名保存时把旧 split 冲掉。

`createForm` / `editForm` 增加 `billing_unified_token_multiplier: 1.0`。重置和 `handleEdit`：

```ts
editForm.billing_unified_token_multiplier = group.billing_unified_token_multiplier ?? 1.0
editForm.billing_input_token_multiplier = group.billing_input_token_multiplier ?? 1.0
editForm.billing_output_token_multiplier = group.billing_output_token_multiplier ?? 1.0
```

提交前（create 与 edit 各一处，spread `createForm` 之前）：

```ts
if (createForm.billing_unified_token_multiplier !== 1) {
  createForm.billing_input_token_multiplier = 1.0
  createForm.billing_output_token_multiplier = 1.0
}
```

最小输入量 hint 改用 `tokenAdjustmentMinInputHint`。

- [ ] **Step 3: Typecheck**

Run: `pnpm exec vue-tsc --noEmit`（在 `frontend/`，若仓库惯用 `pnpm -C frontend` 脚本则用现有 script）

Expected: 无因新字段引起的错误。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/admin/GroupsView.vue frontend/src/types/index.ts frontend/src/i18n/locales/zh/admin/overview.ts frontend/src/i18n/locales/en/admin/overview.ts
git commit -m "feat: add group unified token multiplier form"
```

---

## 验证

全部任务完成后：

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestTokenUsageAdjustment|TestProjectDownstreamUsageJSON|TestAPIKeyAuthSnapshot|TestAPIKeyService_RejectsV|TestAdminService_CreateGroup_Unified|TestAdminService_UpdateGroup_Unified|TestGatewayServiceRecordUsage_GroupToken|TestGatewayServiceRecordUsage_Unified|TestOpenAIGatewayServiceRecordUsage_GroupToken|TestOpenAIGatewayServiceRecordUsage_Unified|TestInflightEstimate_BillingAdjustment|TestInflightEstimate_Unified' -count=1
go test -tags=unit ./internal/repository -run 'TestAPIKeyRepository_GetByKeyForAuth_PreservesBillingTokenAdjustment_SQLite' -count=1
```

图片/视频/探针现有测试不得被 unified 误伤：`TestGatewayServiceRecordUsage_GroupTokenAdjustmentDoesNotRewritePerRequestImageUsage` 仍 PASS。
