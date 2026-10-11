# 分组 Token 统一倍率设计

## 目标

在分组上增加一个 Token 统一倍率，把客户可见的非缓存 input、output、cache_read、cache_write 乘同一个系数。费用按放大后的各桶、用各自单价计算，相当于原价 × 统一倍率。客户看到的缓存命中率与上游真实比例一致。管理员继续看上游原值。

现有「下游输入 / 输出 Token 计费倍率」在统一倍率生效时不再参与计算。尚未改配的分组仍走原来的分项逻辑，避免已配置分组隔夜变价。

## 问题

当前分项倍率只放大非缓存 input/output。计费还会把缓存折进普通输入并清零客户缓存桶。结果是：

- 客户实收变成「全量按普通输入价 × 输入倍率」，不是原价的 n 倍。
- 用户用量日志里的 `cache_read` / `cache_creation` 仍是上游原值，分母被放大后命中率下降。
- 扣费明细里 `input_cost` 很大、`cache_read_cost` 接近 0，和可见缓存桶对不上。

统一倍率同时放大四个桶，命中率公式的分子分母同乘，比例不变；费用按各桶单价计算，明细对齐。

## 范围

覆盖真实 API Key 认证路径上的 Token 计费、预扣估算、用户用量日志、下游 API 响应 usage 改写、分组管理表单和认证快照。

不改上游请求体、缓存键、会话粘性或 TTL。不伪造命中率。探针和按次/按量的图片、视频、音频不走该倍率。`rate_multiplier` 与高峰倍率仍按现有规则叠在价格上。

## 设计

### 配置

新增分组字段 `billing_unified_token_multiplier`：

- 类型 `decimal(10,4)`，非空，默认 `1.0`
- 校验与现有分项倍率相同：`0` 视为 `1`；合法范围 `[0.1, 10]`；NaN/Inf 在运行时归一为 `1`
- JSON：`billing_unified_token_multiplier`
- 注释：下游计费对 input / output / cache_read / cache_write 使用的统一 Token 倍率；`1.0` 表示沿用上游计量

`billing_input_token_multiplier`、`billing_output_token_multiplier`、`billing_token_adjustment_min_input_tokens` 保留。最小输入量阈值语义不变：按原始非缓存 input 判断；`0` 表示正常请求都应用。

迁移：新增一号 SQL，给 `groups` 加列，并把 `enqueue_group_auth_cache_invalidation()` 的「无变化可跳过」比较补上该列。Ent schema、`group_repo` 创建/更新、分组复制一并写入。

### 倍率选择

`tokenUsageAdjustmentForGroup` 产出：

| 条件 | 模式 | 行为 |
|---|---|---|
| `unified ≠ 1` | unified | 四桶乘 `unified`，忽略分项输入/输出倍率 |
| `unified == 1` 且分项有一个 `≠ 1` | split | 现有缓存中性逻辑，兼容已配置分组 |
| 都是 `1` | none | 原样透传 |

保存 `unified ≠ 1` 时，把两个分项倍率写成 `1`，即使请求体没带分项字段，避免滚动发布期间新旧进程叠乘。创建/更新 API 仍接受旧分项字段；只传分项、不传统一倍率时，统一倍率保持默认 `1`，走 split。

管理端表单用一个「Token 统一倍率」替换两个分项输入框。编辑时若当前是 split，提示：该分组仍在使用旧的输入/输出分项倍率，保存统一倍率后改为四桶同倍率。

### 客户用量

`TokenUsageAdjustment` 增加 `UnifiedMultiplier`。`ApplyToUsage` 在达到最小输入量后：

**unified**

```
input'        = round(input × m)
output'       = round(output × m)
cache_read'   = round(cache_read × m)
cache_write'  = round(cache_write × m)
```

`InputTokens` 仍表示非缓存 input。`Customer` 保留四个桶，不清零缓存。`5m` / `1h` 写入明细按同一 `m` 放大，再 `fit` 到 `cache_write'`。独立四舍五入导致读写之和略超总输入时，沿用现有 `fitCacheBuckets`。溢出钳到 `maxInt`。

**split**

保持现有行为：逻辑输入 = 非缓存 input + 读 + 写，客户 input = 逻辑输入 × 输入倍率，客户 output = output × 输出倍率，客户缓存桶为 0。

**none 或低于阈值**

`Customer = Raw`，`Applied = false`。

数值例（unified = 2）：上游 `input=200, cache_read=600, cache_write=200, output=50` → 客户 `400 / 1200 / 400 / 100`。命中率 `600/1000 = 60%` 与 `1200/2000 = 60%` 相同。费用 = 各桶 × 各自单价 × 2，等于原价的 2 倍（忽略四舍五入）。

### 计费与日志

`RecordUsage` 继续不改 `ForwardResult` 原值。

- 客户扣费、API Key 配额、订阅扣费、用量排行使用 `Customer` 四桶和对应单价。unified 路径**禁止**把 `CacheReadInputTokens` / `CacheCreation*` 置 0。
- 用户用量日志的 `input_tokens` / `output_tokens` / `cache_read_tokens` / `cache_creation_tokens` / `cache_creation_5m_tokens` / `cache_creation_1h_tokens` 与 `*_cost` 使用同一套客户桶。去掉「display 只放大非缓存、缓存仍写原值」的分叉。用户用量页和趋势图用这些字段计算命中率，因此会跟上游比例一致。
- `upstream_input_tokens` / `upstream_output_tokens` 和管理员账号统计成本继续用上游原值。
- 通用网关与 OpenAI 网关同一套纯函数。OpenAI 的聚合 input 先拆出读写再套用。

预扣：估算阶段没有缓存拆分，对估算 input/output 乘同一 `m`（unified）或走 split。图片/视频/音频按次估算不进入该逻辑。最终若出现缓存读取，实收通常不高于这笔预估。

### 下游 usage 改写

`ProjectDownstreamUsageJSON` / 流式 eligibility 继续只改写拷贝后的响应。`downstreamUsageAdjustmentEnabled` 在 unified 或 split 任一启用时为真。

`projectUsageBuckets`：

- unified：返回总输入 = `input' + cache_read' + cache_write'`，输出 = `output'`，缓存桶用客户缓存值。OpenAI 的 `prompt_tokens` / 含缓存的 `input_tokens` 用总输入；Anthropic 原生 `input_tokens` 仍用总输入减缓存桶，得到非缓存 `input'`。
- split：保持现有「折进逻辑输入 + 按输入倍率缩放缓存再 fit」行为。

流式：第一帧决定是否改写，后续仅 output 的帧沿用 eligibility。阈值仍看原始非缓存 input。

### 认证快照

`GetByKeyForAuth` 的分组 Select、`APIKeyAuthGroupSnapshot`、`snapshotFromAPIKey`、`snapshotToAPIKey` 增加 `BillingUnifiedTokenMultiplier`。`apiKeyAuthSnapshotVersion` 从 `28` 升到 `29`，淘汰不含该字段的旧快照。分组更新触发器已包含新列，负责失效。

### 前端

`GroupsView` 创建/编辑：一个数字框，范围 `0.1–10`，步长 `0.01`，默认 `1.0`。提示：同时放大输入、输出、缓存读取、缓存写入；客户命中率与上游一致；费用按各桶单价 × 统一倍率。i18n 中英日与现有分组文案一致。管理员 DTO 返回该字段；用户侧分组 DTO 不暴露。

## 错误与边界

- 创建/更新超出 `[0.1, 10]`（且不是 `0`）返回校验错误，不写库。
- 负 token 先钳到 0 再放大。
- 低于最小输入量：不放大，缓存桶保持上游原值。
- 探针、按次图片/视频/音频：不进入该纯函数。
- JSON 无法解析或不含 `usage`：下游改写返回原文。

## 测试

- 纯函数：unified 四桶同乘并保留缓存；split 仍折进逻辑输入并清零缓存；`unified` 优先于分项；阈值、`0`/非法倍率归一、负数钳制、溢出。
- 命中率：`cache_read / (input + cache_read + cache_write)` 在放大前后一致（允许 1 token 级四舍五入）。
- 计费：unified 下 `cache_read_cost` 随放大后的读桶和缓存单价变化；split 下客户缓存费用仍为 0。
- 认证投影与快照：新字段往返；版本 29 淘汰旧快照。
- 通用网关与 OpenAI 网关：用户日志四桶和费用用客户值；`upstream_*` 与账号统计用原值。
- 下游 JSON：OpenAI `prompt_tokens` + `cached_tokens`、Anthropic 原生缓存字段、混合兼容形状、流式 eligibility、5m/1h 明细。
- 预扣与最终结算使用同一 unified 规则。
- 管理端表单：统一倍率框存在；保存 `unified ≠ 1` 时分项倍率为 `1`。
- 图片/视频/音频和探针不套用。

## 非目标

- 不把命中率保底到 90%。上游 70% 则客户也是 70%。
- 不在 unified 路径把缓存折成普通输入。
- 不自动把已有分项倍率迁成统一倍率；由管理员保存统一倍率后切换。
