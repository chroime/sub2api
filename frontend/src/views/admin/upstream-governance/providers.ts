import type { Binding, KeySelection, SiteInput, Transport } from '@/api/admin/upstream-governance'

export const governanceProviders: { value: Transport; label: string }[] = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok / xAI' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: 'GLM / Zhipu' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'opencode_go', label: 'OpenCode Go' },
]
export const transportPlatforms = governanceProviders.map(provider => provider.value)
export const providerLabel = (value: string) => governanceProviders.find(provider => provider.value === value)?.label || value

export function transportUnavailable(platform: Transport, sitePlatform?: SiteInput['platform'], remotePlatform?: string): string | undefined {
  if (sitePlatform === 'newapi' && platform === 'antigravity') return 'antigravityNeedsSub2API'
  const remote = remotePlatform?.toLowerCase()
  if (remote && remote !== 'unknown' && remote !== 'composite' && remote !== platform && !(remote === 'grok' && platform === 'openai')) return 'protocolMismatch'
}

export function initialTransport(remoteID: string, remotePlatform: string, bindings: Pick<Binding, 'remote_group_id' | 'platform'>[] = [], keys: KeySelection[] = [], sitePlatform?: SiteInput['platform']): Transport | '' {
  const remote = remotePlatform.toLowerCase()
  // Keep a pre-existing key/binding on its original transport. Grok previously
  // used the OpenAI-compatible path; changing it would generate another marker.
  for (const existing of [...bindings, ...keys]) {
    if (existing.remote_group_id === remoteID && transportPlatforms.includes(existing.platform as Transport) && !transportUnavailable(existing.platform as Transport, sitePlatform, remote)) return existing.platform as Transport
  }
  return transportPlatforms.includes(remote as Transport) && !transportUnavailable(remote as Transport, sitePlatform, remote) ? remote as Transport : ''
}
