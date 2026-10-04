import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import type { CanvasProject, CanvasRepository } from '../types'
import InfiniteCanvasView from '@/views/user/InfiniteCanvasView.vue'
import { keysAPI, userGroupsAPI } from '@/api'

vi.mock('@/api', () => ({
  keysAPI: { list: vi.fn(), create: vi.fn() },
  userGroupsAPI: { getAvailable: vi.fn() },
}))

function project(id: string, title: string): CanvasProject {
  const now = new Date('2026-01-01T00:00:00.000Z')
  return { id, title, createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [], edges: [] }
}

function repository(initial: CanvasProject[] = []): CanvasRepository {
  const projects = initial.map((item) => structuredClone(item))
  return {
    async listProjects() { return projects.map((item) => structuredClone(item)) },
    async loadProject(id) { return projects.find((item) => item.id === id) ?? null },
    async saveProject(item) { const index = projects.findIndex((project) => project.id === item.id); if (index >= 0) projects[index] = structuredClone(item); else projects.push(structuredClone(item)) },
    async deleteProject(id) { const index = projects.findIndex((item) => item.id === id); if (index >= 0) projects.splice(index, 1) },
    async saveAsset() { return 'asset' },
    async loadAsset() { return undefined },
    async deleteAsset() {},
  }
}

const groups = [{ id: 1, name: 'Images', platform: 'openai', status: 'active', allow_image_generation: true }]

describe('InfiniteCanvasView', () => {
  beforeEach(() => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [
      { id: 11, name: 'image-key', key: 'sk-image-key', group_id: 1, status: 'active', expires_at: null },
      { id: 12, name: 'text-key', key: 'sk-text-key', group_id: 2, status: 'active', expires_at: null },
    ] as never, total: 2, page: 1, page_size: 100, pages: 1 })
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue(groups as never)
  })

  function mountPage(projects = [project('one', 'One'), project('two', 'Two')]) {
    return mount(InfiniteCanvasView, {
      props: { repository: repository(projects) },
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div class="app-layout"><slot /></div>' },
          BaseDialog: { template: '<div v-if="show"><slot /><slot name="footer" /></div>', props: ['show'] },
          ConfirmDialog: { template: '<div v-if="show"></div>', props: ['show'] },
          Icon: true,
        },
      },
    })
  }

  it('shows only image-capable key options', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.findAll('[data-canvas-key-option]')).toHaveLength(1))
    expect(wrapper.text()).toContain('image-key')
    expect(wrapper.text()).not.toContain('text-key')
  })

  it('shows a clear empty state when no image key is available', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    expect(wrapper.text()).toMatch(/create.*key/i)
    expect(wrapper.find('[data-create-key-link]').exists()).toBe(true)
  })

  it('keeps the selected tab when switching projects', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-project="one"]').exists()).toBe(true))
    await wrapper.find('[data-canvas-tab="inspector"]').trigger('click')
    await wrapper.find('[data-canvas-project="two"]').trigger('click')
    expect(wrapper.find('[data-canvas-tab="inspector"]').classes()).toContain('is-active')
  })

  it('renders dark mode classes without console errors', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('.infinite-canvas-view').exists()).toBe(true))
    expect(wrapper.find('.infinite-canvas-view').classes()).toContain('dark:bg-dark-950')
    expect(error).not.toHaveBeenCalled()
    error.mockRestore()
  })

  it('offers mutually exclusive mobile project and inspector drawers', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-drawer="sidebar"]').exists()).toBe(true))
    await wrapper.find('[data-canvas-drawer="sidebar"]').trigger('click')
    expect(wrapper.find('.canvas-project-sidebar').classes()).toContain('fixed')
    await wrapper.find('[data-canvas-drawer="inspector"]').trigger('click')
    expect(wrapper.find('.canvas-project-sidebar').classes()).toContain('hidden')
    expect(wrapper.find('.canvas-inspector').classes()).toContain('fixed')
  })

  it('selects a newly created key before the background refresh completes', async () => {
    vi.mocked(keysAPI.create).mockResolvedValue({ id: 99, name: 'new-image-key', key: 'sk-new', group_id: 1, status: 'active', expires_at: null } as never)
    vi.mocked(keysAPI.list).mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never).mockImplementation(() => new Promise(() => undefined))
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(false))
    await wrapper.get('.canvas-key-picker button').trigger('click')
    const createButton = wrapper.findAll('button').find((button) => button.text() === 'Create')
    expect(createButton).toBeDefined()
    await createButton!.trigger('click')
    await vi.waitFor(() => expect((wrapper.find('.canvas-key-picker select').element as HTMLSelectElement).value).toBe('99'))
  })
})
