import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import InfiniteCanvasView from '@/views/user/InfiniteCanvasView.vue'
import { keysAPI, userGroupsAPI } from '@/api'
import type { CanvasProject, CanvasRepository } from '../types'

vi.mock('@/api', () => ({ keysAPI: { list: vi.fn(), create: vi.fn() }, userGroupsAPI: { getAvailable: vi.fn() } }))

function project(): CanvasProject {
  const now = new Date('2026-01-01T00:00:00.000Z')
  return { id: 'asset-project', title: 'Assets', createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [{ id: 'image-1', type: 'image', position: { x: 10, y: 10 }, metadata: { status: 'completed', storageKey: 'asset-1', prompt: 'Reloaded' } }], edges: [] }
}

describe('InfiniteCanvasView image asset resolver', () => {
  afterEach(() => vi.restoreAllMocks())

  it('loads persisted blobs and creates a fresh preview URL without persisting it', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue([] as never)
    const createUrl = vi.fn(() => 'blob:fresh-preview')
    const revokeUrl = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createUrl })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeUrl })
    const asset = { blob: new Blob(['image'], { type: 'image/png' }), mimeType: 'image/png', kind: 'image', storageKey: 'asset-1' }
    const repository: CanvasRepository = {
      async listProjects() { return [structuredClone(project())] }, async loadProject() { return project() }, async saveProject() {}, async deleteProject() {},
      async saveAsset() { return 'asset-2' }, async loadAsset(key) { return key === 'asset-1' ? asset : undefined }, async deleteAsset() {},
    }
    const wrapper = mount(InfiniteCanvasView, { props: { repository }, global: { plugins: [createPinia()], stubs: { AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { template: '<div />', props: ['show'] }, ConfirmDialog: { template: '<div />', props: ['show'] } } } })
    await vi.waitFor(() => expect(wrapper.find('img').attributes('src')).toBe('blob:fresh-preview'))
    expect(createUrl).toHaveBeenCalledWith(asset.blob)
    expect(JSON.stringify(project())).not.toContain('blob:fresh-preview')
    wrapper.unmount()
    expect(revokeUrl).toHaveBeenCalledWith('blob:fresh-preview')
  })
})
