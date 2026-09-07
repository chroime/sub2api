import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { post },
}))

import { runNow } from '@/api/admin/channelMonitor'

describe('admin channel monitor run-now API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { results: [] } })
  })

  it('allows the backend probe and quota time budgets to complete', async () => {
    await expect(runNow(42)).resolves.toEqual({ results: [] })

    expect(post).toHaveBeenCalledWith('/admin/channel-monitors/42/run', undefined, {
      timeout: 120_000,
    })
  })
})
