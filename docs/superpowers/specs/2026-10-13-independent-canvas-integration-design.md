# 独立画布与主站登录态整合设计

## 背景

当前 Sub2API 主站已经有一套内置 Vue 画布，独立仓库
`chroime/infinite-canvas` 也已经可以作为完整的 React 画布应用运行。目标是把后者部署到
`canvas.oreniva.com`，由主站的 `/infinite-canvas` 页面以 iframe 承载，同时允许用户在新标签页中打开同一个独立应用。
独立画布需要使用主站当前用户已经创建并授权的 API key，主站退出后画布必须立即失去托管授权。

## 目标

- 将主站 `/infinite-canvas` 的用户界面完全替换成 `canvas.oreniva.com` iframe。
- 提供“在新标签页打开”入口，并让新标签页沿用主站登录态。
- 让独立画布只读取当前用户 active、未过期、分组允许生图且模型白名单允许的 key。
- 不把主站 JWT、API key 或其他凭据写入画布的 localStorage、IndexedDB、项目导出文件或 URL。
- 主站退出、会话失效或打开画布的主站窗口关闭后，画布清除托管配置，不能继续请求主站 API 或网关。
- 保留独立画布直接开发运行的本地手工配置能力；托管模式下托管配置覆盖手工配置且不持久化。
- 移除独立画布中剩余的 GitHub 入口和指向上游仓库的可见品牌链接。

## 非目标

- 不将画布项目或素材从浏览器本地存储迁移到 Sub2API 数据库。
- 不改变 Sub2API 的用户、API key、分组、计费或网关业务规则。
- 不让画布访问管理员接口、其他用户的 key 或分组。
- 不改动服务器上的其他工程；独立画布静态站点只使用 `canvas.oreniva.com` 的站点目录和反向代理配置。

## 方案与取舍

### 方案 A：共享父域 Cookie

把认证 Cookie 设置为 `.oreniva.com`，让 `canvas.oreniva.com` 直接携带 Cookie 调用主站 API。
这会扩大 Cookie 的作用域，并要求主站认证从当前前端 token 机制迁移到适合跨子域的 Cookie 机制；现有独立画布也无法直接访问
HttpOnly Cookie。改动面和失效边界都较大，因此不采用。

### 方案 B：父窗口 `postMessage` 托管授权（采用）

主站和画布只通过固定源的 `postMessage` 握手。画布发出 ready 消息后，主站把当前会话的短期授权信息和主站 API 基址发送给画布；画布在内存中使用该授权读取 `/keys` 和 `/groups/available`，再用筛选出的 key 请求网关。主站退出时发送 clear 消息，画布同时监听 opener/父窗口关闭。

此方案不需要改变主站现有登录存储方式，iframe 和新标签页都可以复用，且凭据不会出现在 URL 或持久化存储中。握手双方都校验 `event.origin` 和 `event.source`，API CORS 只允许主站与画布明确的来源。

### 方案 C：后端专用画布会话交换

主站先用现有 JWT 换取一次性画布会话，再把交换凭据交给画布。安全边界最强，但需要新增后端会话表、交换接口、过期和撤销逻辑，且当前 key 接口仍会把用户 API key 交给浏览器；在本次整合中收益不足以抵消额外的后端状态和部署复杂度。

## 架构

### 主站侧

新增一个轻量的 `CanvasEmbedView`，负责：

1. 使用 `VITE_CANVAS_ORIGIN`（生产默认 `https://canvas.oreniva.com`）构造 iframe URL，并设置明确的 `allow`、`referrerPolicy` 和尺寸。
2. 监听画布的 `sub2api.canvas.ready`，只接受来自配置源且 `event.source` 为当前 iframe 的消息。
3. 从 `useAuthStore` 读取当前用户的 access token；已登录时发送 `sub2api.auth.init`，包含 token、`https://oreniva.com/api/v1` API 基址和必要的用户摘要；未登录时发送 `sub2api.auth.clear`。
4. 监听 token、用户和登录状态变化，登录失效或退出时向 iframe 发送 clear，并销毁 iframe 的托管握手状态。
5. 提供新标签页打开按钮。打开操作保留 opener，让新标签页可以用同一套 ready/init 握手；目标源仍由严格的 `VITE_CANVAS_ORIGIN` 校验。
6. 在 iframe 加载失败、来源不匹配、未登录或主站 API 请求失败时显示可理解的状态和重新加载按钮。

`/infinite-canvas` 路由和用户导航继续保留，但路由组件改为 `CanvasEmbedView`。原来的 `InfiniteCanvasView.vue`
及其本地画布实现不再从用户路由访问；本次不删除源码，避免把已有本地数据模型和测试一起破坏，后续可单独清理。

### 独立画布侧

继续使用仓库中已有的 `useSub2ApiAuthBridge`，并补齐以下行为：

- 只接受来自 `VITE_SUB2API_PARENT_ORIGIN`（生产默认 `https://oreniva.com`）且来源为父窗口或 opener 的消息。
- `sub2api.auth.init` 到达后，以内存 token 请求主站 `/keys?page=1&page_size=100` 和 `/groups/available`。
- 只保留 active、未过期、具有 `allow_image_generation` 且通过分组模型白名单的 key；不把完整 key 写入任何持久化结构。
- 将托管 key 投影成现有画布配置的临时 channels，覆盖手工配置；刷新或 clear 时取消旧请求并清空临时 channels。
- `sub2api.auth.clear`、父窗口/ opener 关闭、主站请求返回 401 或握手来源失效时，立刻清空托管 channels 并停止后续 API 请求。
- 没有托管授权时显示登录主站提示；手工配置仍可在本地开发环境使用，但不能伪造主站托管状态。
- 所有用户可见品牌入口不再指向 GitHub；版本检查和插件清单使用 fork 后的地址或显式配置的自托管地址。

### 消息协议

```ts
type CanvasReady = {
  type: "sub2api.canvas.ready"
  version: 1
}

type AuthInit = {
  type: "sub2api.auth.init"
  version: 1
  token: string
  apiBase: string
  user?: { id: number; role?: string }
}

type AuthClear = {
  type: "sub2api.auth.clear"
  version: 1
  reason?: "logout" | "expired" | "parent-closed" | "unauthorized"
}
```

消息只允许在 `oreniva.com` 与 `canvas.oreniva.com` 之间传递；开发环境通过环境变量显式设置对应来源，不允许使用 `*`。

## 请求与生命周期

1. 主站路由完成认证检查后渲染 iframe。
2. 画布加载完成，向父窗口或 opener 发送 ready。
3. 主站验证 source/origin，按当前 token 状态回复 init 或 clear。
4. 画布用 init 中的 API 地址和 token 读取 key、分组和模型白名单；成功后显示托管可用模型。
5. 生成请求继续走主站现有 OpenAI 兼容网关，因此现有分组权限、余额、配额、计费和使用记录逻辑不变。
6. 主站 token 变化时重新发送 init；登出或 401 时发送 clear。
7. 画布检测父窗口或 opener 关闭后主动 clear，不保留可恢复的主站授权。

## 部署与安全头

- 外部仓库构建为静态资源，部署在独立的 `canvas.oreniva.com` 站点目录或容器中。
- 主站 API CORS 的 `allowed_origins` 增加 `https://canvas.oreniva.com`，保留现有主站来源；不使用带凭据的通配符。
- 画布站点允许被 `https://oreniva.com` iframe 嵌入，使用 CSP `frame-ancestors https://oreniva.com`；不使用会阻止嵌入的 `X-Frame-Options: DENY`。
- 画布静态站点不记录授权消息、Authorization 头、API key 或请求体；构建产物和反向代理日志不得包含这些内容。
- 生产环境使用 HTTPS；本地开发通过 `VITE_CANVAS_ORIGIN` 和 `VITE_SUB2API_PARENT_ORIGIN` 指向本地地址完成握手测试。

## 错误处理

- 来源不匹配、消息版本不支持或 source 不可信：静默丢弃消息并保持未授权。
- key/分组请求 401：清空托管 channels，显示“主站登录已失效”，等待下一次 ready/init。
- key/分组请求网络错误：保留手工配置状态，但不显示过期的托管 key；提供重试。
- 所有托管 key 都被过滤：显示“没有可用于画布的授权分组”，不发起生图请求。
- iframe 加载失败：主站显示重试和新标签页打开入口，不影响其他主站页面。

## 验证计划

### 主站

- `/infinite-canvas` 只渲染外部 iframe，iframe 地址和新标签页地址使用同一配置源。
- 接收错误 origin、错误 source、未知消息和重复 ready 时不发送凭据。
- 登录、刷新 token、退出登录、未登录访问、iframe reload 和新标签页 opener 关闭分别验证。
- 前端类型检查、路由测试和相关组件测试通过。

### 独立画布

- auth bridge 单元测试覆盖 origin/source 校验、init/clear、请求取消、401 清理和 opener 关闭。
- key 过滤测试覆盖 inactive、过期、无分组、禁止生图和模型白名单。
- `pnpm --dir web run typecheck`、`pnpm --dir web run build` 和现有测试通过。

### 联调

- 本地起主站和独立画布，验证 iframe 与新标签页都能看到当前用户的可用模型。
- 退出主站后确认画布不能继续调用 `/keys` 或网关。
- 直接访问 `canvas.oreniva.com` 时确认没有主站授权不会出现托管 key。
- 生产部署只检查画布站点和 CORS/CSP 相关配置，不重启或改动其他工程。

