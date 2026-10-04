import type { ApiKey, Group, GroupPlatform } from '@/types'

export interface CanvasKeyOption {
  id: number
  name: string
  maskedKey: string
  groupName: string
  groupId: number
  platform: GroupPlatform
  allowedModels?: string[]
  /** The credential is retained only by the caller in memory and is never persisted. */
  key: string
}

const IMAGE_PLATFORMS = new Set<GroupPlatform>(['openai', 'grok', 'gemini'])

export function isCanvasImagePlatform(platform: GroupPlatform): boolean {
  return IMAGE_PLATFORMS.has(platform)
}

function maskKey(key: string): string {
  if (key.length <= 8) return '*'.repeat(key.length)
  return `${key.slice(0, 4)}${'*'.repeat(Math.max(4, key.length - 8))}${key.slice(-4)}`
}

export function selectEligibleCanvasKeys(
  keys: Pick<ApiKey, 'id' | 'name' | 'key' | 'group_id' | 'status' | 'expires_at'>[],
  groups: Pick<Group, 'id' | 'name' | 'platform' | 'allow_image_generation' | 'status'>[],
  now: Date | number = new Date()
): CanvasKeyOption[] {
  const timestamp = now instanceof Date ? now.getTime() : now
  const groupById = new Map(groups.map((group) => [group.id, group]))
  return keys.flatMap((item) => {
    if (item.status !== 'active') return []
    if (item.expires_at !== null && item.expires_at !== undefined && item.expires_at !== '') {
      const expiresAt = new Date(item.expires_at).getTime()
      if (!Number.isFinite(expiresAt) || expiresAt <= timestamp) return []
    }
    const group = item.group_id == null ? undefined : groupById.get(item.group_id)
    if (!group || group.status !== 'active' || !group.allow_image_generation || !IMAGE_PLATFORMS.has(group.platform)) return []
    const allowlist = (group as Pick<Group, 'id'> & { model_allowlist?: { enabled?: boolean; models?: string[] } }).model_allowlist
    return [{ id: item.id, name: item.name, maskedKey: maskKey(item.key), groupName: group.name, groupId: group.id, platform: group.platform, ...(allowlist?.enabled && Array.isArray(allowlist.models) ? { allowedModels: allowlist.models.filter((model): model is string => typeof model === 'string') } : {}), key: item.key }]
  })
}

export { maskKey }
