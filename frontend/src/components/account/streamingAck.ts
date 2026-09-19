import type { AccountPlatform } from '@/types'

const streamingACKPlatforms: ReadonlySet<string> = new Set<AccountPlatform>([
  'openai',
  'anthropic',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go',
])

export function supportsStreamingACK(platform: string | undefined): boolean {
  return platform !== undefined && streamingACKPlatforms.has(platform)
}

export function readStreamingACKEnabled(
  platform: string | undefined,
  extra: Record<string, unknown> | null | undefined,
): boolean {
  if (!supportsStreamingACK(platform) || !extra) return false
  if (Object.prototype.hasOwnProperty.call(extra, 'streaming_ack_enabled')) {
    return extra.streaming_ack_enabled === true
  }
  return platform === 'openai' && extra.openai_synthetic_first_response_enabled === true
}

export function withStreamingACKExtra(
  platform: string,
  extra: Record<string, unknown> | undefined,
  enabled: boolean,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...extra, streaming_ack_enabled: enabled }
  if (platform === 'openai') delete next.openai_synthetic_first_response_enabled
  return next
}
