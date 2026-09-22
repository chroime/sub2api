## Before you connect

Start with your preferred client and configure three things: an API key, the endpoint, and a model.

1. [Create an account](/register), sign in, and create a key under [API keys](/keys).
2. Choose a group matching the model and client protocol. Check your balance, quota, and key expiry.
3. Open the key's setup instructions to get its connection settings. Use the group-specific address if it differs from the defaults below.
4. Check the [Model Plaza](/model-plaza) for the exact model ID and pricing. Sign in if the page requires it. Family names such as GPT or Claude are not model IDs.

> The platform supports GPT, Claude, Gemini, Grok, GLM, Kimi, DeepSeek, and MiniMax families. Available versions depend on your key's group and configured channels. Protocol support can differ within a family.

## Configuration methods

The platform provides two ways to connect: manual configuration for precise control, and one-click setup for a fast client initialization.

### Manual configuration

Open **API keys** in the console, click “Use key” on the target key, and choose a client and operating system. Copy the generated content into the client configuration file or terminal environment, then restart the client.

![Manual API key configuration](/docs/manual-setup.png)

Confirm that the Base URL, model ID, and protocol match the key's group. Treat configuration files as sensitive: do not commit them to public repositories or paste them into browser-side code.

### One-click setup

Click “Quick setup” on the target API key row, choose your platform, and download the quick setup script. The script generates the client configuration for the current key and includes the authentication information required by that key. Download it to your own computer and run it according to the system prompt to complete initialization, then restart the client and send a test request.

![One-click API key setup](/docs/quick-setup.png)

1. Click “Quick setup” on the target API key row.
2. Choose Windows, macOS, or Linux and download the matching script.
3. Run the script only on your own computer; never share it or upload it to a public repository.
4. Reopen the client and verify a model request after the script completes.

One-click setup does not change key permissions, but the script contains your key. Delete the downloaded file after use and disable or regenerate the key immediately if you suspect exposure.

## Connection addresses

| Client type | Default address | Configuration |
| --- | --- | --- |
| OpenAI-compatible clients | `{{BASE_URL}}` | Usually includes the trailing `/v1` |
| Codex CLI | `{{BASE_URL}}` | Requires a group supporting Responses |
| Claude Code | `{{GATEWAY_URL}}` | Anthropic protocol; do not append `/v1/messages` |
| Gemini CLI | `{{GATEWAY_URL}}` | Native Gemini protocol; do not append `/v1beta` |

Replace `YOUR_API_KEY` with your platform key and `MODEL_ID` with an exact model ID available to its group. Do not put a complete request path in Base URL or add `/v1` twice.

## Claude Code

### Install and prepare

Install using the [official Claude Code setup guide](https://code.claude.com/docs/en/setup). Run `claude --version`, then choose a key assigned to a Claude Code-compatible group.

### Configure the client

Open `.claude/settings.json` in your user directory: `%USERPROFILE%\.claude\settings.json` on Windows, or `~/.claude/settings.json` on macOS / Linux. Merge the following `env` fields with any existing settings.

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": {{GATEWAY_URL_JSON}},
    "ANTHROPIC_AUTH_TOKEN": "YOUR_API_KEY"
  }
}
```

Check for conflicting `ANTHROPIC_API_KEY` values or endpoint overrides from a previous provider. Do not mix credentials from different services.

### Verify the connection

Restart Claude Code, run `claude` in your project, and send a short message using a supported model. Confirm the request appears in your platform usage records.

## Codex CLI / Desktop

### Install and configure

Follow the [official Codex quickstart](https://developers.openai.com/codex/quickstart/). The CLI configuration file is usually `~/.codex/config.toml`, or `%USERPROFILE%\.codex\config.toml` on Windows. Back up and merge settings instead of overwriting other providers or duplicating fields.

```toml
model_provider = "sub2api"
model = "MODEL_ID"

[model_providers.sub2api]
name = "Sub2API"
base_url = {{BASE_URL_JSON}}
env_key = "SUB2API_API_KEY"
wire_api = "responses"
```

Your group must support the Responses protocol. Copy a model ID from the platform; the client's model picker does not establish your group's permissions.

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

These environment variables apply only to the current terminal and processes it starts. Desktop apps and IDE extensions need a version supporting custom providers and access to the same configuration and environment. Launching an app from a desktop shortcut may not inherit terminal variables. Check the platform key's setup instructions and the [official configuration reference](https://developers.openai.com/codex/config-reference/) for your client.

## Gemini CLI

Install using the [official Gemini CLI guide](https://geminicli.com/docs/get-started/), then choose a key with native Gemini protocol support.

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

Select API key authentication. If Google account sign-in still appears, check the selected authentication method and whether the process received the variables. An OpenAI-compatible Gemini model does not necessarily support native Gemini CLI access.

## Cursor / Cline / Continue

Add an OpenAI-compatible provider in your client's model settings. Menu names vary by version; your version must support a custom Base URL.

| Setting | Value |
| --- | --- |
| Provider | OpenAI Compatible |
| Base URL | `{{BASE_URL}}` |
| API key | Your platform key |
| Model | An exact model ID available to your group |

Enable the custom Base URL override when configuring Cursor. In Cline and Continue, select an OpenAI-compatible provider. Add one model and test a short message before adding more.

Some editor completion, agent, or built-in tools use separate services and may not follow custom model settings. A successful chat connection does not establish compatibility with every editor feature.

## Cherry Studio / Chatbox

1. Open Settings and find model services or providers.
2. Add an OpenAI-compatible provider with your key and `{{BASE_URL}}`.
3. Fetch the model list, or manually add exact model IDs supported by your key's group.
4. Select a model, send a short message, and confirm a reply.

Use this workflow for GPT, Grok, GLM, Kimi, DeepSeek, MiniMax, and Claude or Gemini models exposed through OpenAI-compatible endpoints. Capabilities depend on the platform group.

## Verify your setup

### Check the model list

Check which models your key can access. Windows PowerShell:

```powershell
$headers = @{ Authorization = "Bearer YOUR_API_KEY" }
Invoke-RestMethod -Uri "{{BASE_URL}}/models" -Headers $headers
```

macOS / Linux:

```bash
curl "{{BASE_URL}}/models" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### Send a test message

For a group supporting Chat Completions, run this connectivity check in a macOS / Linux terminal. Use the corresponding client to test native Claude, native Gemini, or Responses-only groups.

```bash
curl "{{BASE_URL}}/chat/completions" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

Confirm the model, request time, and usage in the console to verify the request used this platform.

## Troubleshooting

| Symptom | Check first |
| --- | --- |
| 401 / Invalid key | Complete key value, expiry, and credentials left over from another provider |
| 403 / Model access denied | Key group, model ID, and client protocol |
| 404 / Endpoint not found | Duplicate `/v1` or a full request path entered as Base URL |
| 429 / Request limited | Balance, key quota, concurrency, and rate limits |
| 5xx / Request failed | Current channels; retry later and retain the request ID |
| New settings have no effect | Restart the client and check environment or project-level overrides |
| Timeout / Connection failed | Endpoint domain, DNS, proxy rules, firewall, and client connectivity |

Homepage [channel coverage](/home#channels) is available without sign-in and describes configuration, not live health checks. See the [Model Plaza](/model-plaza) for models and pricing; availability and access requirements depend on platform settings. Actual charges follow your group and the platform billing settings.

Keep keys out of public repositories. When contacting support, include your client version, error, and request ID, with credentials redacted.
