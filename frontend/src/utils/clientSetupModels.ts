import type { GroupPlatform, KnownAccountPlatform } from '@/types'
import { buildCodexModelCatalogUrl, buildCodexModelsManifestUrl } from '@/api/codex'

// These are supported repository defaults, not a promise that every group exposes them.
export const CLIENT_SETUP_DEFAULT_MODELS: Record<KnownAccountPlatform | 'composite', string> & Partial<Record<GroupPlatform, string>> = Object.assign(Object.create(null), {
  openai: 'gpt-6.1-sol',
  anthropic: 'claude-sonnet-5-5',
  gemini: 'gemini-3.8-flash',
  antigravity: 'claude-sonnet-5-5',
  grok: 'grok-4.7',
  kimi: 'kimi-k3',
  zhipu: 'glm-5.3',
  deepseek: 'deepseek-v4.1-flash',
  minimax: 'MiniMax-M3',
  opencode_go: 'glm-5.3',
  typesafe: 'jev-latest',
  command_code: 'deepseek/deepseek-v4-flash',
  cline: 'deepseek/deepseek-v4-flash',
  composite: 'gpt-6.1-sol'
})

export function normalizeClientBaseUrl(baseUrl: string): string {
  return baseUrl.trim().replace(/\/+$/, '').replace(/\/(?:v1|v1beta)$/i, '')
}

export function clientV1BaseUrl(baseUrl: string): string {
  return `${normalizeClientBaseUrl(baseUrl)}/v1`
}

export function isClientSetupTextModel(model: string): boolean {
  return !!model && model !== 'jev-latest' && !/(?:imagine|image|video|embedding|tts|whisper|transcri|realtime|audio|moderation)(?:[-_.:/]|$)/i.test(model)
}

export class ClientSetupModelUnavailableError extends Error {}

export function clientSetupModelIds(payload: unknown): string[] {
  if (!payload || typeof payload !== 'object') return []
  const value = payload as { models?: unknown; data?: unknown }
  const entries = Array.isArray(value.models) ? value.models : Array.isArray(value.data) ? value.data : []
  return [...new Set(entries.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return []
    const model = entry as { slug?: unknown; id?: unknown }
    const id = typeof model.slug === 'string' ? model.slug.trim() : typeof model.id === 'string' ? model.id.trim() : ''
    return isClientSetupTextModel(id) ? [id] : []
  }))]
}

/** An absent catalog permits a documented example default; a loaded empty one fails closed. */
export function selectClientSetupModel(
  platform: GroupPlatform | null | undefined,
  available?: readonly string[],
  preferred = CLIENT_SETUP_DEFAULT_MODELS[platform || 'openai']
): string {
  if (available === undefined) {
    if (preferred) return preferred
    throw new ClientSetupModelUnavailableError('No usable text model is available for this API key')
  }
  const models = available.filter(isClientSetupTextModel)
  if (preferred && models.includes(preferred)) return preferred
  if (platform === 'openai' || platform === 'grok') {
    throw new ClientSetupModelUnavailableError(`Required model is unavailable: ${preferred}`)
  }
  if (platform === 'anthropic' || platform === 'antigravity') {
    const claude55 = models.find(id => /(?:^|\/)claude-(?:sonnet|opus|haiku)-5[.-]5(?:$|-)/.test(id))
    if (claude55) return claude55
    if (platform === 'anthropic') throw new ClientSetupModelUnavailableError('Claude 5.5 is unavailable for this key')
  }
  if (platform === 'gemini' || platform === 'antigravity') {
    const gemini = models.find(id => {
      const version = id.match(/(?:^|\/)gemini-(\d+)(?:[.-](\d+))?(?=[-./]|$)/)
      return !!version && (Number(version[1]) > 3 || (Number(version[1]) === 3 && Number(version[2] || 0) >= 8))
    })
    if (gemini) return gemini
    throw new ClientSetupModelUnavailableError('Gemini 3.8 or newer is unavailable for this key')
  }
  const sameFamily = models.find((id) => {
    if (preferred?.startsWith('gemini-')) return id.startsWith('gemini-')
    if (preferred?.startsWith('claude-')) return id.startsWith('claude-')
    if (preferred?.startsWith('grok-')) return id.startsWith('grok-')
    if (preferred?.startsWith('gpt-')) return id.startsWith('gpt-')
    return false
  })
  const selected = sameFamily || models[0]
  if (!selected) throw new ClientSetupModelUnavailableError('No usable text model is available for this API key')
  return selected
}

export interface ClientSetupCatalog {
  modelIds: string[]
  manifestContent: string
  responseBytes: number
}

export async function fetchClientSetupCatalog(baseUrl: string, apiKey: string, signal?: AbortSignal, protocol: 'native' | 'responses' = 'responses'): Promise<ClientSetupCatalog> {
  const root = normalizeClientBaseUrl(baseUrl)
  const url = protocol === 'native' ? `${buildCodexModelCatalogUrl(root)}?endpoint=messages` : buildCodexModelsManifestUrl(root)
  const response = await fetch(url, {
    method: 'GET',
    headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
    cache: 'no-store',
    signal
  })
  if (!response.ok) throw new Error(`Model list request failed (${response.status})`)
  const text = await response.text()
  const payload: unknown = JSON.parse(text)
  const modelIds = clientSetupModelIds(payload)
  if (modelIds.length === 0) throw new Error('No usable text model is available for this API key')
  const isManifest = typeof payload === 'object' && payload !== null && 'models' in payload && Array.isArray(payload.models)
  return { modelIds, manifestContent: isManifest ? JSON.stringify(payload, null, 2) : '', responseBytes: new TextEncoder().encode(text).byteLength }
}
