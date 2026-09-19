import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as settings from '@/api/admin/settings'

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

describe('balance precharge settings API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('reads and saves explicit disabled settings through the dedicated endpoint', async () => {
    const value = { enabled: false, threshold: 0, amount: 0 }
    get.mockResolvedValue({ data: value })
    put.mockResolvedValue({ data: value })
    await expect(settings.getBalancePrechargeSettings()).resolves.toEqual(value)
    await expect(settings.updateBalancePrechargeSettings(value)).resolves.toEqual(value)
    expect(get).toHaveBeenCalledWith('/admin/settings/balance-precharge')
    expect(put).toHaveBeenCalledWith('/admin/settings/balance-precharge', value)
  })

  it('keeps group identity and inherited effective settings together', async () => {
    const value = {
      group_id: 7,
      settings: { mode: 'inherit', threshold: 0, amount: 0 },
      global: { enabled: true, threshold: 10, amount: 1 },
      effective: { enabled: true, threshold: 10, amount: 1 },
    }
    get.mockResolvedValue({ data: value })
    await expect(settings.getGroupBalancePrechargeSettings(7)).resolves.toEqual(value)
    expect(get).toHaveBeenCalledWith('/admin/groups/7/balance-precharge')
    await expect(settings.getGroupBalancePrechargeSettings(8)).rejects.toThrow('Invalid group balance precharge settings')
  })

  it.each([
    { enabled: 'false', threshold: 0, amount: 0 },
    { enabled: true, threshold: 1, amount: 2 },
    { enabled: true, threshold: 1, amount: 0.000000001 },
    { enabled: true, threshold: Infinity, amount: 1 },
    { enabled: false },
  ])('rejects an invalid server policy %#', async (value) => {
    get.mockResolvedValue({ data: value })
    await expect(settings.getBalancePrechargeSettings()).rejects.toThrow('Invalid balance precharge settings')
  })
})
