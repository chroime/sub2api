# 渠道监控手动重置 7 天可用率设计

## 目标

在“渠道管理 -> 渠道监控”的操作列增加“重置 7 天”能力。管理员可以为渠道主模型设置一个 0.00%–100.00% 的 7 天目标可用率，并选择 0–8 根黄色柱作为当前时间线的人工展示基线。重置不删除、不修改检测历史；重置后的真实检测继续参与统计，新的失败/错误立即显示为红柱。整个实现不新增数据库表或字段。

## 已确认的产品行为

- 操作入口位于现有操作列，与“立即检测、复制、编辑、删除”并列。
- 只作用于当前渠道的 `primary_model`，附加模型的状态、统计和时间线不受影响。
- 目标可用率允许 `0.00` 到 `100.00`，最多两位小数。
- 黄色柱数量允许 `0` 到 `8`；其余人工柱为绿色。
- 鼠标悬停人工柱时使用普通状态和延迟文案，不出现“人工基线”字样。
- 重置后仅隐藏重置时刻之前的主模型红柱；重置之后产生的 `failed`/`error` 必须立即显示。
- 支持“取消重置”。取消后恢复原始 7 天聚合、原始近 60 条真实历史和此前被隐藏的红柱。
- 不增加“调整原因”字段。
- 管理员普通编辑、复制、模板应用、checker 请求都不能把内部元数据泄露为 HTTP Header 或 API 响应字段。

## 数据存储

复用 `channel_monitors.extra_headers` JSONB 字段中的内部保留键：

```text
sub2api:availability_reset
```

值是 JSON 字符串，结构如下：

```json
{
  "version": 1,
  "model": "gpt-5.5",
  "target_pct": 98.5,
  "degraded_bars": 2,
  "reset_at": "2026-09-04T00:00:00Z",
  "baseline_total": 10080,
  "baseline_ok": 9929,
  "created_by": 1
}
```

字段含义：

- `model` 固定为写入时渠道的主模型；读取时若与当前 `primary_model` 不一致，则忽略该重置并按真实数据统计。
- `target_pct` 是管理员设置的目标百分比。
- `degraded_bars` 是时间线左侧人工柱中的黄色柱数量，受 0–8 限制。
- `reset_at` 使用 UTC RFC3339。
- `baseline_total` 是重置时按渠道检测间隔估算的 7 天预期检测次数，最小为 1。
- `baseline_ok` 为 `round(baseline_total * target_pct / 100)`，并限制在 `[0, baseline_total]`。
- `created_by` 记录管理员 ID，用于审计上下文，不对普通用户暴露。

现有 repository 的内部键剥离逻辑扩展为同时处理该键。`extra_headers` 对外响应不包含该键，用户配置的普通 Header 也不能覆盖它。

## 统计模型

### 7 天可用率

新增一个 service 层纯函数，根据真实聚合值和重置元数据计算展示值：

1. 查询窗口仍为当前时间往前 7 天，保持现有 `operational`/`degraded` 计为可用、`failed`/`error` 计为不可用的规则。
2. 仅取 `reset_at` 之后的主模型真实检测参与重置后的展示统计；重置前真实记录不再计入重置后的 7 天展示值。
3. 人工基线的剩余权重按 `reset_at` 到当前时间的线性时间比例衰减：
   `remaining = max(0, 1 - elapsed / 7 days)`。
4. 人工贡献为 `baseline_total * remaining` 个样本，其中可用样本为 `baseline_ok * remaining`。展示可用率为：
   `(baseline_ok*remaining + real_ok) / (baseline_total*remaining + real_total) * 100`。
5. `remaining == 0` 时完全回退到真实 7 天统计；若真实数据为空且人工基线也已退出，返回现有无历史语义（0）。
6. 15 天、30 天详情统计不使用人工基线，始终读取真实历史。

批量 admin 列表、用户列表和用户详情都通过同一 service 规则计算，避免三处数值不一致。重置只改变展示聚合，不写入 `channel_monitor_histories`。

### 近 60 根柱

后端在构造主模型 timeline 时应用同一重置边界：

1. 查询 `reset_at` 之后主模型最近 60 条真实历史，按现有接口顺序返回（最新在前）。
2. 以人工柱补足左侧缺失数量，最多补到 60 根；人工柱只在人工基线仍有效时生成。
3. 人工柱状态由 `degraded_bars` 决定：黄色柱为 `degraded`，其余为 `operational`。人工柱的 `checked_at` 使用等间隔的合成时间戳，仅用于稳定排序和前端高度计算，不作为历史记录持久化。
4. 真实记录从右侧进入展示；重置后的 `failed`/`error` 原样保留并显示红色。
5. 人工柱返回普通 `status`、`latency_ms`、`ping_latency_ms`、`checked_at` 字段，不增加会进入 tooltip 的“人工基线”标签。
6. 重置取消或过期后，timeline 直接恢复 repository 返回的真实最近 60 条记录。

## API

### 设置重置

`POST /api/v1/admin/channel-monitors/:id/availability-reset`

请求体：

```json
{
  "availability_pct": 98.5,
  "degraded_bars": 2
}
```

服务端要求：

- 使用现有 admin 鉴权、审计和错误响应格式。
- 校验百分比为有限数值、范围 0–100、最多两位小数。
- 校验 `degraded_bars` 为整数 0–8。
- 读取渠道当前主模型并写入 metadata；主模型为空或渠道不存在时返回现有业务错误。
- 写入必须是单次原子更新，重复点击以最后一次成功请求为准，不产生多条状态。
- 返回更新后的渠道响应，并包含新的 `availability_7d` 展示值；不返回内部 metadata。
- 管理端渠道响应可包含安全的 `availability_reset_active` 布尔值，用于显示“取消重置”操作；不得返回 metadata JSON、管理员 ID 或内部 Header 键。

### 取消重置

`DELETE /api/v1/admin/channel-monitors/:id/availability-reset`

- 删除 `sub2api:availability_reset` 内部键，保留其他 `extra_headers`。
- 幂等：不存在活动重置时仍返回成功。
- 返回更新后的渠道响应或现有空成功响应，具体沿用 admin 删除/更新接口约定。

## 后端改动边界

- `backend/internal/service/channel_monitor_types.go`：增加内部重置结构和必要的 summary 字段，不把 metadata 作为普通 Header 暴露。
- `backend/internal/service/channel_monitor_service.go`：增加 metadata 编解码、设置/取消方法、校验和聚合辅助函数。
- `backend/internal/service/channel_monitor_aggregator.go`：批量列表、用户列表、用户详情和 timeline 接入重置规则。
- `backend/internal/repository/channel_monitor_repo.go`：扩展内部键的读写剥离；提供按 `reset_at` 和主模型读取真实历史的查询能力。
- `backend/internal/handler/admin/channel_monitor_handler.go`、`backend/internal/server/routes/admin.go`：增加两个 admin 路由和请求校验。
- 复制、模板应用和普通更新路径：保留既有内部键隔离规则；复制新渠道时清除 availability reset，避免新渠道继承旧渠道的展示状态。

## 前端改动边界

- `frontend/src/api/admin/channelMonitor.ts`：增加 reset/cancel API、请求类型和响应类型。
- `channelMonitorResponse` 仅新增 `availability_reset_active` 状态字段，不暴露重置 JSON 内容。
- `frontend/src/components/admin/monitor/MonitorActionsCell.vue`：新增“重置 7 天”按钮及活动状态下的“取消重置”入口/事件。
- `frontend/src/views/admin/ChannelMonitorView.vue`：管理弹窗状态、提交校验、加载和成功/失败提示，成功后刷新列表。
- 新增小型弹窗组件或复用现有 Dialog：百分比数字输入、0–8 黄色柱选择、提交/取消；输入控件在移动端不溢出。
- `frontend/src/components/user/monitor/MonitorTimeline.vue` 无需识别新状态，继续按 `operational`/`degraded`/`failed`/`error` 映射颜色，确保人工柱 tooltip 不显示额外标签。
- 中英文 i18n 同步增加按钮、弹窗、校验和错误文案。

## 错误处理与并发

- metadata JSON 损坏、版本未知、模型不匹配或时间非法时按“无活动重置”处理，并记录结构化日志；不得阻断监控列表。
- 设置/取消与普通编辑并发时，service 在同一更新事务中重新读取 `extra_headers` 并只修改内部键，保留用户 Header 更新。
- 设置/取消与复制并发时，复制操作读取到的源渠道重置状态不得写入新渠道。
- checker 运行前必须剥离所有 `sub2api:` 内部键。

## 测试与验收

后端单元测试覆盖：

- 百分比边界、两位小数和黄色柱 0–8 校验。
- metadata 编解码、损坏/过期/模型不匹配处理。
- 重置刚创建、衰减中、7 天后过期三种可用率计算。
- 重置前红柱隐藏、重置后红柱保留、取消后真实历史恢复。
- 附加模型和 15/30 天统计不受影响。
- 内部键不会出现在 API 响应或上游请求 Header；复制不会继承重置。

前端测试覆盖：

- 操作列按钮触发 reset/cancel 事件。
- 弹窗提交 0–100、两位小数、0–8 黄色柱的边界值。
- API 成功刷新列表，API 失败显示错误且不丢失当前表单输入。

验收标准：管理员无需数据库迁移即可设置、取消重置；列表和用户监控页面的 7 天数值一致；重置前红柱不在当前时间线显示，重置后新红柱立即显示；取消后所有统计恢复原始真实数据。
