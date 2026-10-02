# 智能运维接入完成清单实施计划

> 本批按已确认的只读方案实施：展示接入状态与导航入口，不执行跨模块写操作。

**目标**：在选中上游的运维总览中明确显示授权、可信采集目录、本地分组绑定、托管 Key、自动管理、余额监控、调价保护、通知配置八项状态，并把读取失败与未配置严格区分。

**范围**：Go/Gin 管理员只读聚合接口、Vue 总览清单组件与中英文文案、针对性单元/组件测试。复用现有服务与路由，不新增数据库迁移，不自动授权、采集、导入、探活、调价、发信、支付或充值。

## 任务 1：后端只读就绪汇总

- 新增 `ReadinessOverview`、`ReadinessCheck` DTO，状态仅允许 `configured`、`not_configured`、`not_enabled`、`pending`、`read_failed`。
- 新增 `Service.Readiness(ctx, siteID)`：读取站点、授权、目录、绑定、托管 Key、自动管理、余额健康、观察策略、调价/通知策略；每项独立容错，单项失败返回 `read_failed` 与稳定错误码，不能把整站误判为完成。
- 新增 `GET /admin/upstream-governance/sites/:id/readiness` 及路由/handler 测试。
- 不返回凭证、密钥明文、邮件正文或原始 JSON；仅返回状态码、时间和安全的数量摘要。

## 任务 2：前端清单与数据接入

- 扩展 API 类型与 `api.readiness`。
- 在 `SiteOverview.vue` 下方增加只读清单组件，按八项展示状态、辅助说明、更新时间和导航按钮。
- `UpstreamGovernanceView.vue` 在选中站点时并发读取就绪汇总；切站/刷新时清空旧值，迟到响应不得覆盖新站点。
- 导航按钮只切换已有 `overview/import/monitor/history` tab 或打开现有授权/Key入口，不自动触发任何业务写操作。
- 清单在窄屏、深色模式和读取失败时保持可读，状态文案中不展示英文技术字段。

## 任务 3：测试先行与回归

- 先写后端服务/handler RED：完整状态映射、单项读取失败、无凭证/无快照、空绑定、无 Key、策略未启用。
- 先写前端 RED：八项状态渲染、读取失败不显示已完成、导航事件、切站防迟到响应、只读不调用写 API。
- 实现后运行治理相关前后端测试、类型检查、改动文件 lint、生产构建与 `go vet`。

## 任务 4：本地发布

- 运行 `git diff --check`、完整治理回归。
- 按既有流程备份数据库归档、程序、配置、启动脚本、日志与加密密钥。
- 构建并部署本地 `58089`，验证健康、匿名 401、认证 GET readiness 与既有 workbench/模板接口。
- 仅提交 `codex/upstream-governance` 本地分支，不合并 `main`、不推送远程。

## 完成证据

- 后端新增只读 GET /api/v1/admin/upstream-governance/sites/:id/readiness，逐项返回授权、可信目录、绑定、托管密钥、自动管理、余额监控、调价保护、变化通知状态；单项读取失败返回 read_failed，不暴露凭证、密钥、收件人或原始 payload。
- 前端在选中上游的总览页增加“接入完成清单”，支持状态、数量、UTC+8 检查时间与只读导航入口；切换站点时清空旧清单，迟到响应不会覆盖新站点。
- 定向前端治理测试 550 项通过；i18n key completeness、TypeScript 类型检查、改动文件 ESLint、生产构建通过。后端治理/handler/routes 测试及 go vet 通过。
- 未自动执行授权、采集、导入、探活、修复 Key、调价、发信、支付或充值；未新增数据库迁移。
- 仍在 codex/upstream-governance 本地分支，不合并 main、不推送远程。
