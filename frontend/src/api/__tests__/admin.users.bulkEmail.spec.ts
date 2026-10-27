import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usersAPI } from '../admin/users'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post } }))

describe('admin user bulk email APIs', () => {
  beforeEach(() => vi.clearAllMocks())

  it('preserves per-recipient results and allows a batch to outlast the standard request timeout', async () => {
    const request = { user_ids: [7, 9], all: false, event: 'system.health_snapshot' as const, locale: 'zh' }
    post.mockResolvedValue({ data: {
      items: [
        { user_id: 7, email: 'one@example.com', status: 'sent' },
        { user_id: 9, email: 'two@example.com', status: 'failed', error: 'SMTP unavailable' }
      ], total: 2, sent: 1, failed: 1, skipped: 0
    } })

    expect(await usersAPI.bulkEmail(request)).toEqual({
      items: [
        { user_id: 7, email: 'one@example.com', status: 'sent' },
        { user_id: 9, email: 'two@example.com', status: 'failed', error: 'SMTP unavailable' }
      ], total: 2, sent: 1, failed: 1, skipped: 0
    })
    expect(post).toHaveBeenCalledWith('/admin/users/bulk-email', request, { timeout: 0 })
  })

  it('previews real content for the chosen template and representative recipient', async () => {
    post.mockResolvedValue({ data: { subject: 'System status', html: '<p>Healthy</p>' } })

    expect(await usersAPI.previewBulkEmail({ event: 'balance.low', locale: 'en', user_id: 7 }))
      .toEqual({ subject: 'System status', html: '<p>Healthy</p>' })
    expect(post).toHaveBeenCalledWith('/admin/users/bulk-email/preview', {
      event: 'balance.low', locale: 'en', user_id: 7
    })
  })
})
