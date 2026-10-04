import { describe, expect, it } from 'vitest'
import { selectEligibleCanvasKeys } from '../keySelection'

const keys: any[] = [
  { id: 11, name: 'OpenAI', key: 'sk-openai-12345678', group_id: 1, status: 'active', expires_at: null },
  { id: 12, name: 'Inactive', key: 'inactive', group_id: 1, status: 'inactive', expires_at: null },
  { id: 13, name: 'Expired', key: 'expired', group_id: 1, status: 'active', expires_at: '2020-01-01T00:00:00Z' },
  { id: 14, name: 'Grok', key: 'xai-123456789', group_id: 2, status: 'active', expires_at: null },
  { id: 15, name: 'No group', key: 'none', group_id: 99, status: 'active', expires_at: null },
  { id: 16, name: 'Disabled image', key: 'disabled', group_id: 3, status: 'active', expires_at: null },
]

const groups: any[] = [
  { id: 1, name: 'OpenAI Images', platform: 'openai', status: 'active', allow_image_generation: true },
  { id: 2, name: 'Grok Images', platform: 'grok', status: 'active', allow_image_generation: true },
  { id: 3, name: 'Gemini Text', platform: 'gemini', status: 'active', allow_image_generation: false },
]

describe('selectEligibleCanvasKeys', () => {
  it('filters keys to active, unexpired image-capable groups', () => {
    expect(selectEligibleCanvasKeys(keys, groups).map((item) => item.id)).toEqual([11, 14])
  })

  it('returns display-safe metadata and keeps the credential in memory only', () => {
    const [option] = selectEligibleCanvasKeys(keys, groups)
    expect(option).toMatchObject({ id: 11, groupName: 'OpenAI Images', platform: 'openai', key: keys[0].key })
    expect(option.maskedKey).not.toContain(keys[0].key)
  })

  it('rejects malformed non-empty expiration values', () => {
    const malformed = { ...keys[0], id: 17, name: 'Malformed expiry', expires_at: 'not-a-date' }
    expect(selectEligibleCanvasKeys([malformed], groups)).toEqual([])
  })

  it('carries the selected group model allowlist without exposing unrelated fields', () => {
    const [option] = selectEligibleCanvasKeys(keys, [{ ...groups[0], model_allowlist: { enabled: true, models: ['gpt-image-1'] } }])
    expect(option.allowedModels).toEqual(['gpt-image-1'])
    expect(option).not.toHaveProperty('model_allowlist')
  })
})
