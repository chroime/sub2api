import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createPinia } from 'pinia'
import type { CanvasProject, CanvasRepository } from '../types'
import InfiniteCanvasView from '@/views/user/InfiniteCanvasView.vue'
import { keysAPI, userGroupsAPI } from '@/api'
import { listImageModels } from '@/api/imageGeneration'

vi.mock('@/api', () => ({
  keysAPI: { list: vi.fn(), create: vi.fn() },
  userGroupsAPI: { getAvailable: vi.fn() },
}))

vi.mock('@/api/imageGeneration', async () => {
  const actual = await vi.importActual<typeof import('@/api/imageGeneration')>('@/api/imageGeneration')
  return { ...actual, listImageModels: vi.fn() }
})

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

const groups = [{ id: 1, name: 'Images', platform: 'openai', status: 'active', allow_image_generation: true, model_allowlist: { enabled: true, models: ['gpt-image-*'] } }]

describe('InfiniteCanvasView', () => {
  beforeEach(() => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [
      { id: 11, name: 'image-key', key: 'sk-image-key', group_id: 1, status: 'active', expires_at: null },
      { id: 12, name: 'text-key', key: 'sk-text-key', group_id: 2, status: 'active', expires_at: null },
    ] as never, total: 2, page: 1, page_size: 100, pages: 1 })
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue(groups as never)
    vi.mocked(listImageModels).mockResolvedValue([])
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

  it('renders the canvas controls with Chinese labels by default', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-tab="canvas"]').exists()).toBe(true))
    expect(wrapper.find('[data-canvas-tab="canvas"]').text()).toBe('画布')
    expect(wrapper.find('[data-canvas-drawer="sidebar"]').text()).toBe('项目')
    expect(wrapper.find('.canvas-toolbar').text()).toContain('背景')
    expect(wrapper.find('.canvas-toolbar').text()).toContain('保存')
    expect(wrapper.find('.canvas-key-picker').text()).toContain('图片 API 密钥')
  })

  it('creates connected prompt and config nodes from a new empty project', async () => {
    const wrapper = mountPage([])
    await wrapper.find('[data-canvas-empty="projects"] button').trigger('click')
    await vi.waitFor(() => expect(wrapper.findAll('.canvas-node')).toHaveLength(2))
    expect(wrapper.find('.prompt-node').exists()).toBe(true)
    expect(wrapper.find('.config-node').exists()).toBe(true)
  })

  it('loads allowlisted image models into the config select', async () => {
    vi.mocked(listImageModels).mockResolvedValue([{ id: 'gpt-image-1' }, { id: 'other-image' }])
    const configProject = project('models', 'Models')
    configProject.nodes = [{ id: 'config', type: 'config', position: { x: 0, y: 0 }, metadata: {} }]
    const wrapper = mountPage([configProject])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(true))
    await wrapper.find('.canvas-key-picker select').setValue('11')
    await vi.waitFor(() => expect(wrapper.find('.config-node select').findAll('option').map((option) => option.text())).toContain('gpt-image-1'))
    expect(wrapper.find('.config-node select').findAll('option').map((option) => option.text())).not.toContain('other-image')
  })

  it('does not let a stale model response overwrite a switched key', async () => {
    let releaseFirst!: (models: { id: string }[]) => void
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [
      { id: 11, name: 'first-image-key', key: 'sk-first', group_id: 1, status: 'active', expires_at: null },
      { id: 14, name: 'second-image-key', key: 'sk-second', group_id: 2, status: 'active', expires_at: null },
    ] as never, total: 2, page: 1, page_size: 100, pages: 1 })
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue([
      ...groups,
      { id: 2, name: 'Second images', platform: 'openai', status: 'active', allow_image_generation: true, model_allowlist: { enabled: true, models: ['second-*'] } },
    ] as never)
    vi.mocked(listImageModels).mockImplementation((key) => key === 'sk-first' ? new Promise((resolve) => { releaseFirst = resolve }) : Promise.resolve([{ id: 'second-image-1' }]))
    const configProject = project('switch-models', 'Switch models')
    configProject.nodes = [{ id: 'config', type: 'config', position: { x: 0, y: 0 }, metadata: {} }]
    const wrapper = mountPage([configProject])
    await vi.waitFor(() => expect(wrapper.findAll('[data-canvas-key-option]')).toHaveLength(2))
    await wrapper.find('.canvas-key-picker select').setValue('11')
    await vi.waitFor(() => expect(listImageModels).toHaveBeenCalledWith('sk-first'))
    await wrapper.find('.canvas-key-picker select').setValue('14')
    await vi.waitFor(() => expect(wrapper.find('.config-node select').findAll('option').map((option) => option.text())).toContain('second-image-1'))
    releaseFirst([{ id: 'first-image-1' }])
    await nextTick()
    expect(wrapper.find('.config-node select').findAll('option').map((option) => option.text())).not.toContain('first-image-1')
  })

  it('shows a clear empty state when no image key is available', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    expect(wrapper.text()).toContain('创建密钥')
    expect(wrapper.find('[data-create-key-link]').exists()).toBe(true)
  })

  it('does not expose a key whose group is missing from the image-capable groups', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [
      { id: 13, name: 'orphan-key', key: 'sk-orphan', group_id: 999, status: 'active', expires_at: null },
    ] as never, total: 1, page: 1, page_size: 100, pages: 1 })
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(false)
    expect(wrapper.text()).toContain('创建密钥')
  })

  it('restores persisted projects on reload while fetching a fresh key list', async () => {
    const persisted = repository([project('persisted', 'Persisted')])
    const keyListCallsBefore = vi.mocked(keysAPI.list).mock.calls.length
    const groupCallsBefore = vi.mocked(userGroupsAPI.getAvailable).mock.calls.length
    const first = mount(InfiniteCanvasView, {
      props: { repository: persisted },
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
    await vi.waitFor(() => expect(first.find('[data-canvas-project="persisted"]').exists()).toBe(true))
    first.unmount()

    const second = mount(InfiniteCanvasView, {
      props: { repository: persisted },
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
    await vi.waitFor(() => expect(second.find('[data-canvas-project="persisted"]').exists()).toBe(true))
    expect(keysAPI.list).toHaveBeenCalledTimes(keyListCallsBefore + 2)
    expect(userGroupsAPI.getAvailable).toHaveBeenCalledTimes(groupCallsBefore + 2)
    second.unmount()
  })

  it('keeps a hydrated image downloadable after restoring its Blob URL', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue([] as never)
    const imageProject: CanvasProject = {
      ...project('downloadable', 'Downloadable'),
      nodes: [{ id: 'image-1', type: 'image', position: { x: 10, y: 10 }, metadata: { status: 'completed', storageKey: 'asset-1', prompt: 'Restored image' } }],
    }
    const imageRepository = {
      ...repository([imageProject]),
      async loadAsset(key: string) { return key === 'asset-1' ? { blob: new Blob(['image'], { type: 'image/png' }), mimeType: 'image/png', kind: 'image', storageKey: key } : undefined },
    } satisfies CanvasRepository
    const createUrl = vi.fn(() => 'blob:restored-image')
    const revokeUrl = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createUrl })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeUrl })
    let clickedHref = ''
    let clickedName = ''
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
      clickedHref = this.href
      clickedName = this.download
    })
    const wrapper = mount(InfiniteCanvasView, {
      props: { repository: imageRepository },
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
    await vi.waitFor(() => expect(wrapper.find('img').attributes('src')).toBe('blob:restored-image'))
    const download = wrapper.find('[data-canvas-image-download]')
    expect(download.exists()).toBe(true)
    await download.trigger('click')
    expect(clickedHref).toBe('blob:restored-image')
    expect(clickedName).toBe('image-1.png')
    click.mockRestore()
    wrapper.unmount()
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

  it('keeps canvas and inspector panels visible through their mobile tabs', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-tab="canvas"]').exists()).toBe(true))
    await wrapper.find('[data-canvas-tab="inspector"]').trigger('click')
    expect(wrapper.find('.canvas-inspector').classes()).toContain('fixed')
    expect(wrapper.find('.canvas-surface').element.parentElement?.classList.contains('hidden')).toBe(true)
    await wrapper.find('[data-canvas-tab="canvas"]').trigger('click')
    expect(wrapper.find('.canvas-surface').element.parentElement?.classList.contains('hidden')).toBe(false)
    expect(wrapper.find('.canvas-inspector').classes()).toContain('hidden')
  })

  it('restores the canvas when the inspector drawer closes via backdrop', async () => {
    const wrapper = mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-tab="inspector"]').exists()).toBe(true))
    await wrapper.find('[data-canvas-tab="inspector"]').trigger('click')
    await wrapper.find('[data-canvas-drawer-close]').trigger('click')
    expect(wrapper.find('[data-canvas-tab="canvas"]').classes()).toContain('is-active')
    expect(wrapper.find('.canvas-surface').element.parentElement?.classList.contains('hidden')).toBe(false)
  })

  it('ignores a stale key-request rejection after a newer generation succeeds', async () => {
    let rejectOld: ((error: Error) => void) | undefined
    vi.mocked(keysAPI.list)
      .mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
      .mockResolvedValue({ items: [{ id: 11, name: 'fresh-image-key', key: 'sk-fresh', group_id: 1, status: 'active', expires_at: null }] as never, total: 1, page: 1, page_size: 100, pages: 1 })
    const wrapper = mountPage([project('one', 'One')])
    const auth = (wrapper.vm as unknown as { authStore: { user: unknown } }).authStore
    auth.user = { id: 2 }
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(true))
    rejectOld?.(new Error('stale request failed'))
    await nextTick()
    expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(true)
  })

  it('selects a newly created key before the background refresh completes', async () => {
    vi.mocked(keysAPI.create).mockResolvedValue({ id: 99, name: 'new-image-key', key: 'sk-new', group_id: 1, status: 'active', expires_at: null } as never)
    vi.mocked(keysAPI.list).mockImplementationOnce(() => Promise.resolve({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)).mockImplementation(() => new Promise(() => undefined))
    vi.mocked(userGroupsAPI.getAvailable).mockImplementation(() => Promise.resolve(groups as never))
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-key-option]').exists()).toBe(false))
    await wrapper.get('.canvas-key-picker button').trigger('click')
    const createButton = wrapper.find('[data-create-key-submit]')
    expect(createButton.exists()).toBe(true)
    await vi.waitFor(() => expect((wrapper.vm as any).canCreateKey).toBe(true))
    await vi.waitFor(() => expect(createButton.attributes('disabled')).toBeUndefined())
    await createButton.trigger('click')
    await vi.waitFor(() => expect((wrapper.find('.canvas-key-picker select').element as HTMLSelectElement).value).toBe('99'))
  })

  it('does not select a key from a missing or non-image group', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    vi.mocked(keysAPI.create).mockResolvedValue({ id: 98, name: 'text-key', key: 'sk-text', group_id: 2, status: 'active', expires_at: null } as never)
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    await wrapper.get('.canvas-key-picker button').trigger('click')
    const createButton = wrapper.find('[data-create-key-submit]')
    await createButton.trigger('click')
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').text()).toContain('不支持图片生成'))
    expect((wrapper.find('.canvas-key-picker select').element as HTMLSelectElement).value).toBe('')
  })

  it.each([
    ['inactive', { ...groups[0], status: 'inactive' }],
    ['unsupported', { ...groups[0], platform: 'anthropic' }],
  ])('rejects a newly created key from an %s group', async (_label, group) => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    vi.mocked(keysAPI.create).mockResolvedValue({ id: 97, name: 'ineligible-key', key: 'sk-ineligible', group_id: 1, group, status: 'active', expires_at: null } as never)
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    await wrapper.get('.canvas-key-picker button').trigger('click')
    const createButton = wrapper.find('[data-create-key-submit]')
    await createButton.trigger('click')
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').text()).toContain('不支持图片生成'))
    expect((wrapper.find('.canvas-key-picker select').element as HTMLSelectElement).value).toBe('')
  })

  it('clears a quick-created key if the group becomes ineligible during refresh', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 } as never)
    vi.mocked(userGroupsAPI.getAvailable).mockResolvedValueOnce(groups as never).mockResolvedValueOnce([{ ...groups[0], status: 'inactive' }] as never)
    vi.mocked(keysAPI.create).mockResolvedValue({ id: 96, name: 'stale-key', key: 'sk-stale', group_id: 1, status: 'active', expires_at: null } as never)
    const wrapper = mountPage([project('one', 'One')])
    await vi.waitFor(() => expect(wrapper.find('[data-canvas-empty="keys"]').exists()).toBe(true))
    await wrapper.get('.canvas-key-picker button').trigger('click')
    const createButton = wrapper.find('[data-create-key-submit]')
    await createButton.trigger('click')
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').text()).toContain('已不再支持图片生成'))
    expect((wrapper.find('.canvas-key-picker select').element as HTMLSelectElement).value).toBe('')
  })
})
