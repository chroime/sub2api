import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import api, { type GlobalSettings } from './upstream-governance'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn(), post: vi.fn() } }))

const settings: GlobalSettings = {
  version: 3,
  notifications: { enabled: true, recipients: ['ops@example.test'], events: { group_online: true, group_offline: false, group_name_change: true, group_description_change: false, group_rate_change: true } },
  local_monitor: { enabled: true, interval_seconds: 30 },
  defaults: { fast_interval_seconds: 15, full_interval_seconds: 600 },
  astrbot: { enabled: true, base_url: 'https://astrbot.example', endpoint_path: '/api/send_msg', api_key: 'plain-astrbot-key', events: { group_online: false, group_offline: true, group_name_change: false, group_description_change: true, group_rate_change: false }, targets: [{ umo: 'group:123', label: 'Ops', enabled: true }], timeout_seconds: 8 },
}

describe('global smart operations settings requests', () => {
  beforeEach(() => vi.resetAllMocks())

  it('loads the singleton settings document', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: settings })
    await expect(api.globalSettings()).resolves.toEqual(settings)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/global-settings')
  })

  it('writes the complete singleton settings document', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ data: settings })
    await expect(api.saveGlobalSettings(settings)).resolves.toEqual(settings)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/upstream-governance/global-settings', settings)
  })

  it('lists groups using draft connection details without saving settings', async () => {
    const input = { base_url: 'https://draft-astrbot.example', api_key: 'replacement-key', timeout_seconds: 12 }
    const result = { groups: [{ umo: 'qq:GroupMessage:123', group_id: '123', group_name: 'Ops', bot_id: 'qq' }], complete: true }
    vi.mocked(apiClient.post).mockResolvedValue({ data: result })
    await expect(api.astrbotGroups(input)).resolves.toEqual(result)
    expect(apiClient.post).toHaveBeenCalledWith('/admin/upstream-governance/global-settings/astrbot-groups', input)
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it('omits a new key when listing groups with the saved connection', async () => {
    const input = { base_url: 'https://astrbot.example', timeout_seconds: 8 }
    vi.mocked(apiClient.post).mockResolvedValue({ data: { groups: [], complete: true } })
    await api.astrbotGroups(input)
    expect(apiClient.post).toHaveBeenCalledWith('/admin/upstream-governance/global-settings/astrbot-groups', input)
  })
})
