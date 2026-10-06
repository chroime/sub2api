import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import api from './upstream-operations'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn() } }))

describe('read-only operations requests', () => {
  beforeEach(() => vi.resetAllMocks())

  it('loads all-site workbench pagination without inventing a category filter', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 0 } })
    expect(await api.workbench()).toEqual({ items: [], total: 0 })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/workbench', { params: { page: 1, page_size: 20 } })
  })

  it('scopes workbench reads to the selected site and requested page', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 21 } })
    await api.workbench({ site_id: 7, page: 2, page_size: 20 })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/workbench', { params: { site_id: 7, page: 2, page_size: 20 } })
  })

  it('uses server-side timeline kind filtering and pagination', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 4 } })
    await api.timeline(7, { kind: 'notification', page: 3 })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/upstream-governance/sites/7/timeline', { params: { kind: 'notification', page: 3, page_size: 20 } })
  })
})
