# 无限画布设计

日期：2026-10-04

## 背景与目标

在普通用户菜单中增加“无限画布”，参考 `basketikun/infinite-canvas` 的图片创作工作台交互，为 Sub2API 用户提供可持续编辑的生图画布。用户可以选择自己创建的、绑定了生图分组的 API Key，在画布上编写提示词、选择模型和参数、生成图片，并把生成结果继续作为画布素材使用。

首版采用“浏览器本地保存，同时预留账号同步边界”的方案：项目结构和图片素材保存在当前浏览器，不新增云端项目同步功能；数据模型通过仓库接口与后端存储解耦，后续可增加账号同步而不重做节点结构。

参考仓库为 MIT License。本实现只参考交互、数据结构和工程思路；若后续复制其实质代码或资源，必须在项目的第三方声明中保留原作者版权信息。

## 目标与非目标

### 目标

- 新增受登录保护的 `/infinite-canvas` 页面和普通用户侧边栏菜单。
- 支持多个本地画布项目，以及新建、重命名、复制、删除、导入和导出。
- 支持提示词节点、生成配置节点和图片节点。
- 支持画布平移、滚轮缩放、节点拖拽、框选、删除、撤销/重做、网格背景和小地图。
- 支持通过用户自己的 API Key 调用现有图片生成网关。
- 只允许选择有效且绑定到 `allow_image_generation=true` 分组的 API Key。
- 复用现有模型白名单、余额、配额、图片倍率、价格和使用记录。
- 图片 Blob 与项目元数据分开保存，避免把大段 Base64 写入项目数据。
- 为未来账号云端同步保留可替换的数据仓库接口。

### 非目标

- 首版不嵌入完整 React 子应用，也不引入第二套前端框架。
- 首版不实现账号云端同步、项目分享和跨设备恢复。
- 首版不实现音频、视频和插件节点。
- 首版不复制参考仓库的所有高级插件和多媒体工作流。
- 不创建画布专用 API Key 数据表或独立计费体系。

## 技术路线

采用当前 Vue 工程原生实现轻量画布。画布使用 HTML/SVG 绝对定位节点和指针事件完成拖拽、平移与缩放；项目元数据和图片 Blob 使用 IndexedDB 保存；生图请求复用现有网关和用户 API Key。

不采用 iframe 或独立 React 子应用，避免重复运行时、主题与会话桥接，以及两套权限和计费规则。画布层通过小型仓库抽象与持久化实现解耦，未来可以增加远程仓库实现。

## 页面与交互

路由为 `/infinite-canvas`，路由元数据使用 `requiresAuth: true`、`requiresAdmin: false`。

页面采用三栏布局：

1. 左侧项目栏：列出本地项目，提供新建、重命名、复制、删除、导入和导出。
2. 中间画布：显示网格、节点、连线、空状态提示、缩放控制和小地图。空白处拖动平移，滚轮按指针位置缩放，节点拖动不触发画布平移。
3. 右侧属性栏：显示当前节点的提示词、模型、尺寸、质量、数量、背景和生成状态；没有选中节点时显示当前项目和活动 Key 概览。

顶部工具栏提供项目名称、活动 Key 选择器、保存状态、撤销/重做、缩放、背景切换和导入导出按钮。菜单和页面使用现有主题变量、按钮、弹框和下拉组件，保持与 Sub2API 其余用户页面一致。

首版内置三种节点：

- `prompt`：保存用户输入的提示词。
- `config`：保存模型、尺寸、质量、数量、背景等生成参数。
- `image`：保存生成结果、参考图信息、生成状态、错误信息和生成元数据。

首版主流程为“提示词节点 → 生成配置节点 → 图片节点”。图片节点可在画布中移动、放大预览、下载、删除和再次生成。参考图编辑和更复杂的节点连线作为第二阶段扩展，但数据结构首版保留 `reference` 连线类型。

## 数据模型与存储

### 仓库接口

```ts
interface CanvasRepository {
  listProjects(): Promise<CanvasProject[]>
  loadProject(id: string): Promise<CanvasProject | null>
  saveProject(project: CanvasProject): Promise<void>
  deleteProject(id: string): Promise<void>
  saveAsset(asset: CanvasAsset): Promise<string>
  loadAsset(storageKey: string): Promise<Blob | null>
  deleteAsset(storageKey: string): Promise<void>
}

interface CanvasAsset {
  storageKey?: string
  blob: Blob
  mimeType: string
  width?: number
  height?: number
  kind: 'image' | 'thumbnail' | 'reference'
}
```

首版实现 `IndexedDbCanvasRepository`。项目、节点、连线和视口信息保存在 IndexedDB；原图、缩略图以及用户导入的参考图作为独立 Blob 保存。节点只引用 `storageKey`，不保存完整 Base64。

### 项目结构

```ts
interface CanvasProject {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  viewport: { x: number; y: number; zoom: number }
  backgroundMode: 'grid' | 'dots' | 'plain'
  activeKeyId?: number
  nodes: CanvasNode[]
  edges: CanvasEdge[]
}

interface CanvasNode {
  id: string
  type: 'prompt' | 'config' | 'image'
  x: number
  y: number
  width: number
  height: number
  metadata: Record<string, unknown>
}

interface CanvasEdge {
  id: string
  sourceNodeId: string
  targetNodeId: string
  kind: 'prompt' | 'config' | 'reference'
}
```

`activeKeyId` 只记录活动 Key 的 ID。完整密钥不得写入 IndexedDB、localStorage、项目 JSON 或导出包。项目导出包含版本号、节点结构和资源清单，图片文件与项目 JSON 一起打包；导入时校验版本、节点类型、文件大小和资源引用。

项目结构预留 `CanvasRepository` 边界。后续增加账号同步时，可以新增远程仓库和媒体上传实现，而不改变画布节点结构和页面交互。

## API Key 选择与创建

无限画布复用现有用户 API：

- `GET /api/v1/keys?status=active`
- `GET /api/v1/groups/available`
- `POST /api/v1/keys`

页面加载后获取当前用户的有效 Key 与可用分组，只显示：

- Key 状态有效；
- 未过期；
- 绑定分组仍可被当前用户使用；
- 分组 `allow_image_generation` 为 `true`；
- 分组模型白名单中存在图片模型或允许图片请求。

Key 选择器显示 Key 名称、脱敏值、分组名称、厂商和生图权限。用户可以跳转“我的 Key”页面创建，也可以在画布内快速创建。快速创建复用现有创建接口，要求选择允许生图的分组，默认名称为“无限画布-当前日期时间”，用户可以修改。

创建成功后，接口返回的完整 Key 只保存在当前页面内存中并立即设为活动 Key。页面刷新时重新请求 Key 列表并按 ID 恢复选择；如果 Key 被删除、禁用、过期或分组权限发生变化，则清除活动选择并要求重新选择。

## 生图请求与计费

1. 用户选择活动 Key。
2. 页面使用该 Key 请求模型列表，并按图片能力和分组白名单筛选。
3. 用户在提示词节点和配置节点中填写内容。
4. 点击生成后创建 `pending` 图片节点，保留提示词和配置。
5. OpenAI 兼容分组调用 `POST /v1/images/generations`；编辑场景调用 `POST /v1/images/edits`。
6. 支持的 Gemini 图片模式使用现有 Gemini 兼容路由，并使用对应的 API Key 认证方式。
7. 解析 `b64_json` 或图片 URL，转换为 Blob 写入 IndexedDB。
8. 将 `storageKey`、格式、宽高、模型、提示词、耗时和任务标识写入图片节点。
9. 生成成功后显示图片；失败则保留节点和原始配置，提供手动重试。

所有请求继续经过现有网关，所以复用 API Key 鉴权、分组模型白名单、生图权限、余额和配额校验、图片倍率和价格、使用记录与失败结算。前端不计算扣费，也不创建画布专用用量记录。

## 权限补充

当前 OpenAI/Grok 图片处理路径已有生图分组权限检查。Gemini 原生图片模式需要补充同等检查：当请求的响应模态包含图片时，后端检查 API Key 绑定分组的 `allow_image_generation`，并继续执行模型白名单检查。无分组 Key 的既有兼容逻辑保持不变。

后端增加覆盖以下情况的测试：允许生图、分组禁止生图、模型不在白名单、请求只包含文本模态、Key 无效。

## 错误处理

- `401`：显示 Key 已失效，清除活动选择并引导重新选择。
- `403`：显示具体的分组或模型权限原因。
- `402`：显示余额、配额或额度不足，不自动重试。
- `429`：显示限流信息，允许用户手动重试。
- `5xx` 或网络失败：保留节点和输入，允许重试。
- 响应缺少图片或格式异常：标记为结果解析失败，保留错误摘要。
- IndexedDB 空间不足：提示清理旧素材或先导出项目。
- 生成过程中 Key 失效：保留节点，下一次操作前要求重新选择 Key。

面向用户的错误信息使用中文；技术详情放入可展开的调试区域，页面主体不直接展示原始 JSON。

## 分期范围

### 第一阶段

- 菜单、路由和页面骨架；
- IndexedDB 项目和素材仓库；
- 提示词、配置和图片节点；
- 平移、缩放、拖拽、选择、删除、撤销和重做；
- API Key 选择和快速创建；
- OpenAI 兼容生图；
- 结果保存、预览、下载、导入和导出；
- 失败节点与手动重试；
- Gemini 图片权限检查的后端补充和测试。

### 第二阶段

- 图片参考图和图片编辑；
- 更完整的节点连线和批量生成；
- Gemini 图片生成适配；
- 小地图增强、历史任务和素材管理。

### 第三阶段

- 账号云端项目同步；
- 多设备恢复；
- 云端媒体存储；
- 项目分享和权限控制。

## 测试与验收

### 前端

- 普通用户菜单显示“无限画布”，路由守卫生效；
- 只有有效且允许生图的 Key 出现在选择器；
- 快速创建 Key 后自动选中；
- 完整 Key 不出现在 IndexedDB、localStorage 和项目导出文件；
- 刷新页面后项目、节点和视口可恢复；
- 导入导出后节点与图片引用完整；
- 拖拽、缩放、平移、删除、撤销和重做正常；
- 生图成功后生成图片节点；
- 失败后可以保留输入并重试。

### API 与后端

- OpenAI 图片生成请求体和认证头正确；
- 图片编辑 multipart 请求正确；
- Gemini 图片模态权限检查正确；
- 无权限、余额不足、Key 失效和限流响应能映射到清晰提示；
- 图片 Blob 保存、读取、删除和版本迁移正确；
- 生图仍由现有使用记录和计费链路记录。

### 浏览器验收

- 使用本地测试 Key 和模拟图片响应完成一次端到端生成；
- 重新加载后恢复项目；
- 切换 Key 后后续请求使用新 Key；
- 禁用 Key 后页面及时提示；
- 现有“我的 Key”和“批量生图”页面行为不变。
