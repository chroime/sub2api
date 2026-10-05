import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import api from './upstream-import-templates'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
const settings = { concurrency: 30, priority: 0, quota_enabled: true, quota_daily_limit: 100, quota_weekly_limit: 500, quota_limit: 1000, upstream_billing_rate_sync_enabled: false, openai_long_context_billing_enabled: true }
describe('import template collection requests', () => {
  beforeEach(() => vi.resetAllMocks())
  it('reads the independent global collection without issuing a write', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { version: 0, templates: [] } })
    expect(await api.list()).toEqual({ version: 0, templates: [] })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/import-templates')
    expect(apiClient.put).not.toHaveBeenCalled()
  })
  it('sends only template metadata and the eight editable settings with the collection version', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ data: { version: 4, templates: [] } })
    await api.save({ version: 3, templates: [{ id: 'fixture', name: 'Fixture', is_default: true, settings: { ...settings, password: 'not-persisted', model_mapping: { a: 'a' } } as typeof settings }] })
    expect(apiClient.put).toHaveBeenCalledWith('/admin/upstream-governance/import-templates', { version: 3, templates: [{ id: 'fixture', name: 'Fixture', is_default: true, settings }] })
  })
})
