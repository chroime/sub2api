import type { GroupPlatform } from '@/types'
import { buildCodexApiKeyAuthConfig } from '@/utils/codexAuthConfig'
import { CLIENT_SETUP_DEFAULT_MODELS, clientV1BaseUrl, normalizeClientBaseUrl, selectClientSetupModel } from '@/utils/clientSetupModels'

export const OPENAI_CC_SWITCH_CODEX_MODEL = CLIENT_SETUP_DEFAULT_MODELS.openai
export const GROK_CC_SWITCH_MODEL = CLIENT_SETUP_DEFAULT_MODELS.grok

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript: string
  availableModels?: readonly string[]
}

/**
 * Balance query CC Switch runs against the imported provider. CC Switch fills
 * `{{baseUrl}}` with the provider's base URL as stored — Codex and Grok imports
 * carry a trailing `/v1` (see `withV1Endpoint`), Claude ones do not, and users
 * may edit it either way afterwards — then evaluates the script, so the URL
 * strips an existing `/v1` instead of blindly appending one (`/v1/v1/usage`
 * is a 404 and CC Switch shows "query failed").
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string,
  availableModels?: readonly string[]
): CcSwitchImportConfig {
  const source = platform || 'anthropic'
  if (source === 'typesafe') throw new Error('TypeSafe requires the /v1/systemone client format')
  const root = normalizeClientBaseUrl(baseUrl)
  if (source === 'antigravity') {
    const endpoint = root.endsWith('/antigravity') ? root : `${root}/antigravity`
    return {
      app: clientType === 'gemini' ? 'gemini' : 'claude', endpoint,
      model: selectClientSetupModel(clientType === 'gemini' ? 'gemini' : 'anthropic', availableModels)
    }
  }
  const model = selectClientSetupModel(source, availableModels)
  if (source === 'openai') return { app: 'codex', endpoint: clientV1BaseUrl(root), model }
  if (source === 'grok') return { app: 'grokbuild', endpoint: clientV1BaseUrl(root), model }
  if (source === 'gemini') return { app: 'gemini', endpoint: root, model }
  return { app: 'claude', endpoint: root, model }
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl, input.availableModels)
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', btoa(input.usageScript)],
    ['usageAutoInterval', '30']
  ]

  if (config.model) {
    entries.splice(2, 0, ['model', config.model])
  }

  if (config.app === 'claude' && config.model) {
    // Claude Code's role aliases must stay inside the same verified catalog.
    const opus = input.platform === 'anthropic' || input.platform === 'antigravity' || !input.platform
      ? selectClientSetupModel('anthropic', input.availableModels, 'claude-opus-5-5')
      : config.model
    entries.push(['sonnetModel', config.model], ['haikuModel', config.model], ['opusModel', opus])
  }

  if (config.app === 'codex') {
    // CC Switch's embedded-config import preserves provider auth and headers.
    // It deliberately leaves global [features] settings to the user.
    const codexConfig = `model_provider = "sub2api"
model = ${JSON.stringify(config.model)}
web_search = "live"

[model_providers.sub2api]
name = "Sub2API"
base_url = ${JSON.stringify(config.endpoint)}
wire_api = "responses"
${buildCodexApiKeyAuthConfig(input.apiKey)}
`
    const payload = JSON.stringify({ config: codexConfig })
    const bytes = new TextEncoder().encode(payload)
    entries.push(['config', btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join(''))])
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}
