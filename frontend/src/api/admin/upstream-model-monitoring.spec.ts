import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import api from './upstream-model-monitoring'
import { modelPolicy } from '@/views/admin/upstream-governance/__tests__/model-fixtures'
vi.mock('../client', () => ({ apiClient: { get: vi.fn(), post: vi.fn(), delete: vi.fn(), put: vi.fn() } }))
describe('administrator model monitoring transport', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(apiClient.post).mockResolvedValue({ data: modelPolicy }); vi.mocked(apiClient.get).mockResolvedValue({ data: [] }); vi.mocked(apiClient.put).mockResolvedValue({ data: {} }) })
  it('sends only writable policy fields with optimistic version', async () => {
    await api.savePolicy(1, { ...modelPolicy, notify_at: '2026-09-27T12:00:00Z', last_error: 'old', created_at: '2026-09-27T00:00:00Z' })
    const [url, body] = vi.mocked(apiClient.post).mock.calls[0]!
    expect(url).toBe('/admin/upstream-governance/sites/1/model-policies')
    expect(body).toMatchObject({ id: 8, version: 4, enabled: false })
    expect(body).not.toHaveProperty('site_id'); expect(body).not.toHaveProperty('notify_at'); expect(body).not.toHaveProperty('last_error'); expect(body).not.toHaveProperty('created_at')
  })
  it('scopes history, cancellation and review to the site and uses strict request bodies', async () => {
    await api.runs(2, 3, 'batch-id'); await api.cancelBatch(2, 'batch-id'); await api.review(2, 'run-id', 'pass', 'review'); await api.deleteRun(2, 'run-id'); await api.deletePolicy(2, 8, 4); await api.stats(2, 30)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-runs', { params: { page: 3, page_size: 20, batch_id: 'batch-id' } })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-batches/batch-id/cancel', {})
    expect(apiClient.put).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-runs/run-id/review', { review: 'pass', note: 'review' })
    expect(apiClient.delete).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-runs/run-id')
    expect(apiClient.delete).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-policies/8', { params: { version: 4 } })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/sites/2/model-stats', { params: { days: 30 } })
  })
  it('loads local targets from the administrator-scoped metadata endpoint', async () => {
    await api.localTargets()
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/local-model-targets')
  })
})
