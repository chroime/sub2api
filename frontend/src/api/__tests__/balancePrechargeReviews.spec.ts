import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getBalancePrechargeReviews, resolveBalancePrechargeReview } from '@/api/admin/balancePrechargeReviews'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

const review = {
  id: 'hold-1', user_id: 1, user_email: 'customer@example.com', api_key_id: 3,
  group_id: 2, group_name: 'OpenAI', amount: 0.02, reason: 'upstream_usage_missing',
  request_id: 'request-1', account_id: 1, model: 'gpt-5.5', status: 'pending',
  created_at: '2026-09-19T00:00:00Z', updated_at: '2026-09-19T00:00:00Z'
}

describe('balance precharge review API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('requests the selected status and offset without mixing it with the precharge policy', async () => {
    get.mockResolvedValue({ data: { items: [review], total: 21 } })
    await expect(getBalancePrechargeReviews({ status: 'resolved', limit: 20, offset: 20 }))
      .resolves.toEqual({ items: [review], total: 21 })
    expect(get).toHaveBeenCalledWith('/admin/settings/balance-precharge/reviews', {
      params: { status: 'resolved', limit: 20, offset: 20 }
    })
  })

  it('sends only the financial resolution, normalizes its note and escapes the record id', async () => {
    post.mockResolvedValue({ data: { ...review, status: 'resolved' } })
    await resolveBalancePrechargeReview('hold/1', { action: 'charge', actual_cost: 0.00123456, note: '  Verified bill  ' })
    expect(post).toHaveBeenCalledWith('/admin/settings/balance-precharge/reviews/hold%2F1/resolve', {
      action: 'charge', actual_cost: 0.00123456, note: 'Verified bill'
    })
  })

  it.each([
    { action: 'charge', actual_cost: 0, note: 'Verified' },
    { action: 'charge', actual_cost: -1, note: 'Verified' },
    { action: 'charge', actual_cost: Infinity, note: 'Verified' },
    { action: 'charge', actual_cost: 0.000000001, note: 'Verified' },
    { action: 'charge', actual_cost: 1000001, note: 'Verified' },
    { action: 'release', actual_cost: 0.02, note: 'Verified' },
    { action: 'release', actual_cost: 0, note: '  ' },
    { action: 'release', actual_cost: 0, note: 'x'.repeat(2001) }
  ] as const)('does not submit an invalid monetary resolution %#', async (payload) => {
    await expect(resolveBalancePrechargeReview('hold-1', payload)).rejects.toThrow('Invalid balance precharge resolution')
    expect(post).not.toHaveBeenCalled()
  })

  it('accepts full release with zero cost and a required explanation', async () => {
    post.mockResolvedValue({ data: { ...review, status: 'resolved', resolution: 'release', actual_cost: 0 } })
    await resolveBalancePrechargeReview('hold-1', { action: 'release', actual_cost: 0, note: 'Upstream confirmed no charge' })
    expect(post).toHaveBeenCalledWith('/admin/settings/balance-precharge/reviews/hold-1/resolve', {
      action: 'release', actual_cost: 0, note: 'Upstream confirmed no charge'
    })
  })

  it('supports up to 2000 Unicode characters of reconciliation evidence', async () => {
    post.mockResolvedValue({ data: { ...review, status: 'resolved' } })
    await resolveBalancePrechargeReview('hold-1', { action: 'release', actual_cost: 0, note: '🧾'.repeat(2000) })
    expect(post).toHaveBeenCalledOnce()
  })
})
