import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import api from './upstream-governance'

vi.mock('@/api/client', () => ({ apiClient: { post: vi.fn(), put: vi.fn() } }))
describe('scoped pricing requests', () => {
  beforeEach(() => vi.resetAllMocks())
  it('separates read-only draft preview from scoped policy and notification writes', async () => {
    const policy = { enabled: true, mode: 'target_margin' as const, min_margin: 0.25, safety_buffer: 0.1, decrease_stability_seconds: 60, max_increase_percent: 20 }
    vi.mocked(apiClient.post).mockResolvedValue({ data: { fingerprint: 'verified' } })
    expect(await api.previewPricingPolicy(5, 12, { policy })).toEqual({ fingerprint: 'verified' })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/upstream-governance/sites/5/pricing-policies/12/preview', { policy })
    expect(apiClient.put).not.toHaveBeenCalled()
    vi.mocked(apiClient.put).mockResolvedValue({ data: { version: 9 } })
    await api.savePricingPolicy(5, 12, { policy, fingerprint: 'verified' })
    expect(apiClient.put).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/pricing-policies/12', { policy, fingerprint: 'verified' })
    const notifications = { enabled: true, recipients: ['ops@example.test'], group_changes: true, rate_changes: true, pricing_changes: false, protection_changes: true }
    await api.savePricingNotifications(5, { version: 9, notifications })
    expect(apiClient.put).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/pricing-notifications', { version: 9, notifications })
  })
})
