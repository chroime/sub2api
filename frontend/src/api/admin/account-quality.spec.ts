import { beforeEach, describe, expect, it, vi } from 'vitest'
import api from './account-quality'
import { apiClient } from '../client'

vi.mock('../client', () => ({ apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() } }))

describe('account quality API boundaries', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(apiClient.post).mockResolvedValue({ data: [] }); vi.mocked(apiClient.get).mockResolvedValue({ data: {} }) })

  it('always assigns accounts with automatic isolation off', async () => {
    await api.assign({ policy_id: 4, account_ids: [7, 8], source: 'selected_accounts' })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/account-quality/bindings', { policy_id: 4, account_ids: [7, 8], source: 'selected_accounts', auto_isolate: false })
  })

  it('preserves version and idempotency identity when queueing manual checks', async () => {
    await api.run(8, { version: 3, request_id: 'diagnostic-unique-id' })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/account-quality/bindings/8/run', { version: 3, request_id: 'diagnostic-unique-id' })
  })

  it('encodes evidence IDs and keeps state control on the quality gate endpoint', async () => {
    await api.runDetail('run/with/slash')
    expect(apiClient.get).toHaveBeenCalledWith('/admin/account-quality/runs/run%2Fwith%2Fslash')
    await api.control(2, { version: 6, action: 'override', reason: 'Reviewing the evidence', duration_minutes: 15 })
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/account-quality/bindings/2/control', { version: 6, action: 'override', reason: 'Reviewing the evidence', duration_minutes: 15 })
  })
})
