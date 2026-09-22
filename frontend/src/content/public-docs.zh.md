## 接入准备

从你正在使用的客户端开始，完成密钥、地址和模型三项配置。

1. [注册账号](/register)并登录，在[API 密钥](/keys)中创建密钥。
2. 选择与你要使用的模型和客户端协议匹配的分组，确认余额、额度和密钥有效期。
3. 在密钥页打开“使用密钥”，获取该分组对应的接入配置。专用分组如有独立路径，以这里的地址为准。
4. 从[模型广场](/model-plaza)确认模型 ID 和价格；如页面要求登录，请先登录。模型名称需要完整一致，不能直接填写 GPT、Claude 等系列名称。

> 平台支持 GPT、Claude、Gemini、Grok、GLM、Kimi、DeepSeek、MiniMax 系列。可调用的具体版本取决于密钥分组和已配置渠道；同一系列也可能通过不同协议接入。

## 配置方式

平台提供两种接入方式：手动配置适合需要精细调整参数的场景，一键配置适合希望快速完成客户端初始化的场景。

### 手动配置

在控制台打开 **API 密钥** 页面，点击目标密钥的“使用密钥”，选择客户端平台和操作系统。根据页面生成的内容，将配置复制到客户端配置文件或终端环境变量中，再重启客户端。

![使用 API 密钥手动配置](/docs/manual-setup.png)

手动配置时请确认 Base URL、模型 ID 和协议与密钥分组一致。配置文件中的密钥属于敏感信息，不要提交到公开仓库，也不要粘贴到浏览器前端代码中。

### 一键配置

在对应的 API 密钥行后点击“快速配置”，选择你需要的平台，下载快速配置脚本。脚本会根据当前密钥生成对应的客户端配置，并包含该密钥所需的认证信息。将脚本下载到自己的电脑后，按系统提示运行即可完成初始化配置，随后重启客户端并发送测试请求。

![API 密钥一键配置入口](/docs/quick-setup.png)

1. 在目标 API 密钥所在行点击“快速配置”。
2. 选择 Windows、macOS 或 Linux，下载对应脚本。
3. 仅在你自己的电脑上运行脚本；不要将脚本发送给他人或上传到公共仓库。
4. 脚本运行完成后，重新打开客户端并验证模型调用。

一键配置不会改变密钥权限，但脚本中包含你的密钥。使用后建议删除下载文件，并在怀疑泄露时立即禁用或重新生成密钥。

## 接入地址

| 客户端类型 | 默认接入地址 | 配置要点 |
| --- | --- | --- |
| OpenAI 兼容客户端 | `{{BASE_URL}}` | 一般需要结尾的 `/v1` |
| Codex CLI | `{{BASE_URL}}` | 使用支持 Responses 的模型分组 |
| Claude Code | `{{GATEWAY_URL}}` | 使用 Anthropic 协议，不手动添加 `/v1/messages` |
| Gemini CLI | `{{GATEWAY_URL}}` | 使用 Gemini 原生协议，不手动添加 `/v1beta` |

`YOUR_API_KEY` 表示你的平台密钥，`MODEL_ID` 表示密钥分组中实际可用的模型 ID。不要将它们原样保留。不要把完整请求路径填入 Base URL，也不要重复添加 `/v1`。

## Claude Code

### 安装与准备

按照 [Claude Code 官方安装指南](https://code.claude.com/docs/en/setup)安装客户端。运行 `claude --version` 确认安装成功，再选择支持 Claude Code 的分组密钥。

### 配置客户端

打开用户目录下的 `.claude/settings.json`。Windows 通常位于 `%USERPROFILE%\.claude\settings.json`，macOS / Linux 位于 `~/.claude/settings.json`。将以下 `env` 字段合并到现有文件，保留其他配置。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": {{GATEWAY_URL_JSON}},
    "ANTHROPIC_AUTH_TOKEN": "YOUR_API_KEY"
  }
}
```

如果终端或旧配置中还设置了其他服务的 `ANTHROPIC_API_KEY` 或接入地址，先检查是否与当前配置冲突。不要把不同服务的凭据混用。

### 验证连接

重启 Claude Code，在项目目录运行 `claude`，选择分组支持的模型并发送一条简短消息。在平台控制台查看是否产生对应的用量记录。

## Codex CLI / 桌面端

### 安装与配置

使用 [Codex 官方安装指南](https://developers.openai.com/codex/quickstart/)安装客户端。CLI 配置文件通常是 `~/.codex/config.toml`，Windows 对应 `%USERPROFILE%\.codex\config.toml`。先备份，再合并配置，避免重复添加同名字段或覆盖其他服务商。

```toml
model_provider = "sub2api"
model = "MODEL_ID"

[model_providers.sub2api]
name = "Sub2API"
base_url = {{BASE_URL_JSON}}
env_key = "SUB2API_API_KEY"
wire_api = "responses"
```

此配置需要密钥分组支持 Responses 接口。模型 ID 从平台复制，不要仅凭客户端模型列表判断分组权限。

### Windows PowerShell

```powershell
$env:SUB2API_API_KEY = "YOUR_API_KEY"
codex
```

### macOS / Linux

```bash
export SUB2API_API_KEY="YOUR_API_KEY"
codex
```

上面的环境变量只对当前终端及其启动的进程生效。桌面端或 IDE 插件需要所用版本支持自定义服务商，并能读取对应配置和环境变量；从桌面图标启动的进程未必继承终端中的变量。平台密钥页提供的客户端配置和[官方配置说明](https://developers.openai.com/codex/config-reference/)可用于核对。

## Gemini CLI

按照 [Gemini CLI 官方指南](https://geminicli.com/docs/get-started/)安装，选择支持 Gemini 原生协议的分组密钥。

### Windows PowerShell

```powershell
$env:GOOGLE_GEMINI_BASE_URL = {{GATEWAY_URL_JSON}}
$env:GEMINI_API_KEY = "YOUR_API_KEY"
$env:GEMINI_MODEL = "MODEL_ID"
gemini
```

### macOS / Linux

```bash
export GOOGLE_GEMINI_BASE_URL={{GATEWAY_URL_JSON}}
export GEMINI_API_KEY="YOUR_API_KEY"
export GEMINI_MODEL="MODEL_ID"
gemini
```

选择 API Key 认证方式。若客户端仍进入 Google 账号登录，检查当前认证方式以及启动进程是否读取了环境变量。支持 OpenAI 兼容接口的 Gemini 模型，不一定同时支持 Gemini CLI 原生接口。

## Cursor / Cline / Continue

在客户端的模型或服务商设置中添加 OpenAI 兼容服务，按下表配置。具体入口名称随版本变化；需要支持自定义 Base URL 的客户端版本。

| 设置项 | 填写内容 |
| --- | --- |
| 服务商 | OpenAI Compatible / OpenAI 兼容 |
| Base URL | `{{BASE_URL}}` |
| API Key | 你的平台密钥 |
| Model | 分组支持的完整模型 ID |

Cursor 使用自定义密钥时，需要开启对应的 Base URL 覆盖选项；Cline、Continue 在服务商设置中选择 OpenAI 兼容类型。先添加一个可用模型并发送测试消息，再配置更多模型。

部分编辑器的补全、Agent 或内置工具功能使用独立服务，不一定跟随自定义模型设置。聊天连接成功不代表所有编辑器功能都支持中转。

## Cherry Studio / Chatbox

1. 打开“设置”，进入“模型服务”或“服务商”。
2. 新增 OpenAI 兼容服务，填写平台密钥和 `{{BASE_URL}}`。
3. 获取模型列表；如果客户端无法自动获取，可手动添加密钥分组支持的完整模型 ID。
4. 选择模型，发送一条简短消息并确认回复。

GPT、Grok、GLM、Kimi、DeepSeek、MiniMax，以及提供 OpenAI 兼容入口的 Claude、Gemini 模型，均按此方式配置。具体模型能力以平台分组为准。

## 检查接入结果

### 检查模型列表

在终端中检查密钥能看到的模型。Windows PowerShell 可使用以下命令：

```powershell
$headers = @{ Authorization = "Bearer YOUR_API_KEY" }
Invoke-RestMethod -Uri "{{BASE_URL}}/models" -Headers $headers
```

macOS / Linux：

```bash
curl "{{BASE_URL}}/models" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 发送一条测试消息

对支持 Chat Completions 的分组，可在 macOS / Linux 终端运行下列连接测试。原生 Claude、Gemini 或仅支持 Responses 的分组，请使用对应客户端测试。

```bash
curl "{{BASE_URL}}/chat/completions" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

收到回复后，在控制台核对模型名称、请求时间和用量，确认请求走的是本平台。

## 常见接入问题

| 现象 | 优先检查 |
| --- | --- |
| 401 / 密钥无效 | 是否复制完整密钥、是否已过期、是否仍在使用其他平台凭据 |
| 403 / 没有模型权限 | 密钥分组、模型 ID 和客户端协议是否匹配 |
| 404 / 接口不存在 | Base URL 是否重复了 `/v1`、是否误填完整请求路径 |
| 429 / 请求受限 | 账户额度、密钥限额、并发和频率限制 |
| 5xx / 请求失败 | 当前渠道情况，稍后重试并保留请求 ID 供排查 |
| 修改配置后未生效 | 是否重启客户端，是否有环境变量或项目级配置覆盖 |
| 超时 / 无法连接 | 接入域名、DNS、代理规则、防火墙，以及客户端是否可访问该地址 |

首页[渠道状态](/home#channels)无需登录即可查看，展示的是配置覆盖，并非实时健康探测。模型和价格请前往[模型广场](/model-plaza)查看，其开放状态及访问权限以平台设置为准；实际扣费以密钥分组和平台计费配置为准。

不要将密钥写进公开仓库或发送给他人。联系支持时提供客户端版本、报错和请求 ID，并遮盖密钥。
