# 智能运维第三批：导入配置模板与草稿保护

用户已确认上一轮提出的范围，并要求继续实施。本批只复用导入账户参数，不扩展跨模块托管、自动支付或上游操作权限。

## 模板与边界

- 管理员可保存多套全局导入参数模板，更新、删除并选择至多一个默认模板；沿用现有管理员鉴权和 settings 版本化存储，不增加数据库迁移。
- 模板仅包含并发数、优先级、配额启用及日/周/总额度、同步上游声明计费倍率开关、长上下文计费开关。不包含密码、Key、站点或分组标识、账户名称、成本/售价、模型白名单或策略权限。
- 原模型限制模板继续独立工作；非 OpenAI 协议的长上下文字段仍由原导入归一化关闭，不因模板绕过兼容性校验。
- 默认项只在新导入上下文且参数未被管理员改动时填入草稿。迟到的默认响应不得覆盖手工修改；读取失败明确提示，可重试或明确选择使用当前参数继续。
- 手动载入只改变草稿，覆盖已有不同参数须明确确认。不触发预览、导入、建 Key、登录、采集、探测、调价、发信或支付。
- 模板保存只写独立模板设置。保存/删除/默认变更或载入模板使本页旧预览失效；不会追溯修改其他页面已冻结的参数或现有账户。
- 并发更新采用集合级 CAS；冲突保留模板编辑内容和导入草稿，阻止盲目重试。显式重新读取模板列表后才可再次保存，不自动覆盖服务端新版本。

## 草稿生命周期

- 同站点新采集快照到达时，保留账户参数、配额选择及管理员明确编辑过的模型白名单；更新目录、清除旧预览，并要求重新确认分组映射。未手工编辑的模型选择仍重新读取现有模型模板/上游默认。
- 切换站点、地址、平台或已知上游账号身份时清除旧站草稿；不在浏览器持久化跨站密码、密钥或映射。
- 参数/配额/模型选择发生变化时，丢弃旧预览及迟到的预览响应。应用仍使用原服务端冻结预览与显式确认，保持导航写锁。

## 精确接口

`GET/PUT /api/v1/admin/upstream-governance/import-templates`

```ts
interface ImportTemplateSettings {
  concurrency: number
  priority: number
  quota_enabled: boolean
  quota_daily_limit: number
  quota_weekly_limit: number
  quota_limit: number
  upstream_billing_rate_sync_enabled: boolean
  openai_long_context_billing_enabled: boolean
}
interface ImportTemplate {
  id: string
  name: string
  is_default: boolean
  settings: ImportTemplateSettings
}
interface ImportTemplateCollection { version: number; templates: ImportTemplate[] }
```

最多50个模板；ID为1–64字符的英数字/下划线/连字符；名称trim后1–100字符且禁止控制字符；至多一个默认。并发1–2147483647、优先级0–2147483647且为整数；配额有限且非负，复用现有导入有效数值范围。拒绝未知字段和不完整参数，不保存额外元数据或敏感字段。初始GET为version=0、templates=[]，无默认时保留现有5000/1等内置值。

## 验收与发布

覆盖验证、默认唯一、首次写并发/CAS、空集合、严格字段白名单；前端覆盖晚响应不覆盖、覆盖确认、冲突草稿保留、默认失败手动继续、切站和同站快照更新、旧预览失效、模型白名单不得空集退化为无限制。

测试用隔离数据库随机schema和合成浏览器数据，不对真实上游进行验证性写操作。部署前备份数据库、程序、配置及加密密钥；发布仅更新本地58089，仍在codex/upstream-governance本地提交，不合main、不推送。
