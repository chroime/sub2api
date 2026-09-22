import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as settings from '@/api/admin/settings'

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

describe('streaming ACK settings API', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('reads the effective master setting through the dedicated admin endpoint', async () => {
    get.mockResolvedValue({ data: { enabled: true } })
    expect(typeof settings.getStreamingACKSettings).toBe('function')
    await expect(settings.getStreamingACKSettings()).resolves.toEqual({ enabled: true })
    expect(get).toHaveBeenCalledWith('/admin/settings/streaming-ack')
  })

  it('preserves an explicit false when saving', async () => {
    put.mockResolvedValue({ data: { enabled: false } })
    expect(typeof settings.updateStreamingACKSettings).toBe('function')
    await expect(settings.updateStreamingACKSettings({ enabled: false })).resolves.toEqual({ enabled: false })
    expect(put).toHaveBeenCalledWith('/admin/settings/streaming-ack', { enabled: false })
  })

  it('rejects malformed status responses instead of treating them as disabled', async () => {
    get.mockResolvedValue({ data: { enabled: 'false' } })
    await expect(settings.getStreamingACKSettings()).rejects.toThrow('Invalid streaming ACK settings')
  })
})
