import { describe, it, expect, vi, beforeEach } from 'vitest'
import { apiClient } from '@/api/client'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/client', () => ({
  apiClient: { post: vi.fn(), get: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))
describe('governance confirmation boundary', () => {
  beforeEach(() => vi.clearAllMocks())
  it('keeps browser jobs scoped to the selected site and separates completion from input', async () => {
    const job = { id: 'job/id', site_id: 2, status: 'waiting', expires_at: '2026-09-28T12:00:00Z' }
    vi.mocked(apiClient.post).mockResolvedValue({ data: job })
    vi.mocked(apiClient.get).mockResolvedValue({ data: job })
    vi.mocked(apiClient.delete).mockResolvedValue({ data: { cancelled: true } })
    const credentials = { expected_site_version: 9, username: 'fixture', password: 'fixture-password' }
    expect(await api.startBrowserAuth(2, credentials)).toEqual(job)
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/browser-auth', credentials)
    expect(await api.browserAuth(2, job.id)).toEqual(job)
    expect(apiClient.get).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/browser-auth/job%2Fid')
    await api.browserAuthAction(2, job.id, { type: 'key', key: 'Enter' })
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/browser-auth/job%2Fid/actions', { type: 'key', key: 'Enter' })
    await api.completeBrowserAuth(2, job.id)
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/browser-auth/job%2Fid/complete', {})
    await api.cancelBrowserAuth(2, job.id)
    expect(apiClient.delete).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/browser-auth/job%2Fid')
    await api.authStatus(2)
    expect(apiClient.get).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/auth-status')
  })
  it('allows long native collection and key batches to finish beyond the default client timeout', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ data: { items: [] } })
    const input = { snapshot_id: 5, selections: [{ remote_group_id: 'r', platform: 'openai' as const }] }
    await api.createKeys(2, input)
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/keys', input, { timeout: 120000 })
    await api.sync(2)
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/2/sync', undefined, { timeout: 300000 })
  })
  it('scopes read-only key audit and explicit replacement steps to the selected site and key', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ data: { stage: 'prepared' } })
    vi.mocked(apiClient.get).mockResolvedValue({ data: null })
    await api.auditKeys(5)
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/keys/audit', {}, { timeout: 30000 })
    await api.keyRepair(5, 9)
    expect(apiClient.get).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/keys/9/repairs')
    await api.prepareKeyRepair(5, 9, { site_version: 3 })
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/keys/9/repairs', { site_version: 3 }, { timeout: 30000 })
    await api.confirmKeyRepair(5, 9, 'repair/id')
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/keys/9/repairs/repair%2Fid/confirm', {}, { timeout: 120000 })
    await api.abandonKeyRepair(5, 9, 'repair/id', { acknowledge_uncertain_create: true })
    expect(apiClient.post).toHaveBeenLastCalledWith('/admin/upstream-governance/sites/5/keys/9/repairs/repair%2Fid/abandon', { acknowledge_uncertain_create: true }, { timeout: 30000 })
  })
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
      undefined,
      { timeout: 300000 },
    )
  })
  it('preserves conflict status for stale-preview feedback', async () => {
    const conflict = { status: 409, code: 'CONFLICT' }
    vi.mocked(apiClient.post).mockRejectedValue(conflict)
    await expect(api.apply(2, 'expired')).rejects.toBe(conflict)
  })
  it('reads login details through the dedicated admin endpoint and atomically saves edited credentials', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { username: 'fixture-user', password: 'fixture-password', version: 9 } })
    expect(await api.loginCredentials(2)).toEqual({ username: 'fixture-user', password: 'fixture-password', version: 9 })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/login-credentials')
    const input = { name: 'Fixture', platform: 'sub2api' as const, base_url: 'https://fixture.example', proxy_id: null, enabled: true, interval_minutes: 15, version: 9, login_credentials: { username: 'fixture-user', password: 'changed-password' } }
    vi.mocked(apiClient.put).mockResolvedValue({ data: { id: 2, version: 10 } })
    await api.update(2, input)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/upstream-governance/sites/2', input)
  })
})
