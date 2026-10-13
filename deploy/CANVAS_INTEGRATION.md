# Oreniva 独立画布部署

独立画布来自 `chroime/infinite-canvas`，静态站点使用 `canvas.oreniva.com`，主站用户页面
`/infinite-canvas` 通过 iframe 加载它。画布不会共享数据库，也不会把主站 JWT 或 API key 写入自身的持久化存储。

## 主站 CORS

在主站 `config.yaml` 的 `cors` 段显式配置两个来源：

```yaml
cors:
  allowed_origins:
    - "https://oreniva.com"
    - "https://canvas.oreniva.com"
  allow_credentials: true
```

不要将 `allowed_origins` 设置为 `*`。画布请求使用主站下发的 `Authorization` bearer token，CORS 必须允许
`Authorization` 请求头；主站现有 CORS 中间件已经包含该请求头。

## 画布容器

在独立画布仓库中使用生产 Compose 配置：

```bash
docker compose up -d
```

生产环境使用以下公开策略变量：

```yaml
environment:
  SUB2API_PARENT_ORIGIN: https://oreniva.com
  CANVAS_ORIGIN: https://canvas.oreniva.com
  SUB2API_AUTH_REQUIRED: "true"
```

`SUB2API_AUTH_REQUIRED=true` 会禁止没有主站托管授权的独立画布使用本地手工渠道生成。主站 iframe 和从主站打开的新标签页会通过严格来源校验的 `postMessage` 握手获得当前会话下可用的图片 key；登出、会话失效、主站窗口关闭后，画布会清空托管渠道。

## 反向代理与 TLS

DNS 为 `canvas.oreniva.com` 指向画布服务器并配置 HTTPS。若使用 Nginx，可从
[`canvas-oreniva.example.conf`](./canvas-oreniva.example.conf) 开始；它只代理画布静态容器，
不代理主站数据库、Redis 或其他工程，并通过 `frame-ancestors https://oreniva.com` 限制 iframe 嵌入者。

主站同样必须使用 HTTPS。直接打开画布而没有主站 opener 时不会获得托管 key；主站页面关闭后，新标签页通过 opener 关闭检测清除内存中的授权。

## 验证

1. 登录 `https://oreniva.com`，打开 `/infinite-canvas`，确认 iframe 加载并出现当前用户允许的图片模型。
2. 点击“新标签页打开”，确认新标签页可以完成相同授权握手。
3. 在主站退出登录，确认画布清除托管授权，不能继续请求 `/keys` 或生图网关。
4. 直接打开 `https://canvas.oreniva.com`，确认未建立主站会话时没有托管 key。
5. 检查 API 的预检响应只允许上述明确来源，不允许 `*`。
