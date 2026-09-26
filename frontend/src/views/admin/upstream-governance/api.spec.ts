import { describe, it, expect, vi, beforeEach } from 'vitest'
import { apiClient } from '@/api/client'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/client', () => ({
  apiClient: { post: vi.fn(), get: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
describe('governance confirmation boundary', () => {
  beforeEach(() => vi.clearAllMocks())
  it('previews selections without applying or creating keys', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ data: { id: 'frozen' } })
    const selections = [
      {
        remote_group_id: 'r',
        platform: 'openai' as const,
        local_group_id: 4,
        account_name: 'fixture',
        cost_multiplier: 0.8,
      },
    ]
    expect(await api.preview(2, { selections })).toEqual({ id: 'frozen' })
    expect(apiClient.post).toHaveBeenCalledTimes(1)
    expect(apiClient.post).toHaveBeenCalledWith(
      '/admin/upstream-governance/sites/2/previews',
      { selections },
    )
  })
  it('applies only persisted preview identity with no new mapping body', async () => {
    const result = {
      preview_id: 'frozen',
      items: [{ remote_group_id: 'r', status: 'failed', error: 'unavailable' }],
    }
    vi.mocked(apiClient.post).mockResolvedValue({ data: result })
    expect(await api.apply(2, 'frozen')).toEqual(result)
    expect(apiClient.post).toHaveBeenCalledTimes(1)
    expect(apiClient.post).toHaveBeenCalledWith(
      '/admin/upstream-governance/sites/2/previews/frozen/apply',
    )
  })
  it('preserves conflict status for stale-preview feedback', async () => {
    const conflict = { status: 409, code: 'CONFLICT' }
    vi.mocked(apiClient.post).mockRejectedValue(conflict)
    await expect(api.apply(2, 'expired')).rejects.toBe(conflict)
  })
})
