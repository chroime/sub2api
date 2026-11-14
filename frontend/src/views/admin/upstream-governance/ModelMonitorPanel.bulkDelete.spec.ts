import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelMonitorPanel from './ModelMonitorPanel.vue'
import api, { type ModelRun, type ModelRunPage } from '@/api/admin/upstream-model-monitoring'
import { modelConfig, modelRun } from './__tests__/model-fixtures'

vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key }) }))
vi.mock('@/api/admin/upstream-model-monitoring', () => ({ default: { policies: vi.fn(), runs: vi.fn(), stats: vi.fn(), localTargets: vi.fn(), startBatch: vi.fn(), savePolicy: vi.fn(), deletePolicy: vi.fn(), hideRun: vi.fn(), deleteRun: vi.fn(), cancelBatch: vi.fn(), run: vi.fn(), review: vi.fn() } }))

enableAutoUnmount(afterEach)
const localRun = (id: string, template: ModelRun['request']['template'] = 'candy', status = 'succeeded') => modelRun({ id, site_id: 99, status, request: { ...modelRun().request, template, config: { ...modelConfig, target_type: 'local_group', local_group_id: 42, local_api_key_id: 77 } } })
let items: ModelRun[]
const page = (): ModelRunPage => ({ items: [...items], total: items.length, page: 1, page_size: 20, counts: { succeeded: items.filter(run => run.status === 'succeeded').length } })
const setup = (localOnly = true) => mount(ModelMonitorPanel, { global: { stubs: { teleport: true, transition: false } }, props: { siteId: 99, localOnly, localTargets: [], remoteGroups: [], managedKeys: [] } })
const select = async (wrapper: ReturnType<typeof setup>, id: string) => wrapper.get(`[data-test=model-run-select-${id}]`).setValue(true)

describe('local model run bulk deletion', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    items = [localRun('candy'), localRun('pelican', 'pelican'), localRun('running', 'probe', 'running'), localRun('queued', 'context', 'queued')]
    vi.mocked(api.policies).mockResolvedValue([])
    vi.mocked(api.runs).mockImplementation(async () => page())
    vi.mocked(api.stats).mockResolvedValue({ days: 7, groups: [] })
    vi.mocked(api.deleteRun).mockImplementation(async (_site, id) => { items = items.filter(run => run.id !== id) })
  })
  afterEach(() => vi.useRealTimers())

  it('places bulk deletion after search, selects finished rows only, and requires confirmation', async () => {
    const wrapper = setup(); await flushPromises()
    const actions = wrapper.get('[data-test=model-run-filters]').findAll('button[data-test]')
    expect(actions.map(button => button.attributes('data-test'))).toEqual(['model-run-reset', 'model-run-search', 'model-runs-bulk-delete'])
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=model-run-select-running]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=model-run-select-queued]').attributes('disabled')).toBeDefined()
    await select(wrapper, 'candy')
    expect((wrapper.get('[data-test=model-runs-select-page]').element as HTMLInputElement).indeterminate).toBe(true)
    await wrapper.get('[data-test=model-runs-select-page]').setValue(true)
    expect((wrapper.get('[data-test=model-run-select-pelican]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test=model-run-select-running]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    expect(api.deleteRun).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test=model-runs-bulk-delete-dialog]').text()).toContain('"count":2')
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click'); await flushPromises()
    expect(vi.mocked(api.deleteRun).mock.calls).toEqual([[99, 'candy'], [99, 'pelican']])
    expect(api.deletePolicy).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test=model-run-row-candy]').exists()).toBe(false)
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
  })

  it('cancels confirmation without deleting and clears selection when searching', async () => {
    const wrapper = setup(); await flushPromises()
    await select(wrapper, 'candy')
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-delete-cancel]').trigger('click')
    expect(api.deleteRun).not.toHaveBeenCalled()
    await wrapper.get('[data-test=model-run-query]').setValue('pelican')
    await wrapper.get('[data-test=model-run-filters]').trigger('submit'); await flushPromises()
    expect(api.runs).toHaveBeenLastCalledWith(99, 1, '', { q: 'pelican' })
    expect((wrapper.get('[data-test=model-run-select-candy]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
  })

  it('keeps only failed rows selected after partial failure and permits retry', async () => {
    vi.mocked(api.deleteRun).mockImplementation(async (_site, id) => {
      if (id === 'pelican') throw { reason: 'conflict', status: 409 }
      items = items.filter(run => run.id !== id)
    })
    const wrapper = setup(); await flushPromises()
    await wrapper.get('[data-test=model-runs-select-page]').setValue(true)
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test=model-run-row-candy]').exists()).toBe(false)
    expect((wrapper.get('[data-test=model-run-select-pelican]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.get('[role=alert]').text()).toContain('bulkDeletePartial')
    vi.mocked(api.deleteRun).mockResolvedValue(undefined)
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click'); await flushPromises()
    expect(api.deleteRun).toHaveBeenLastCalledWith(99, 'pelican')
    expect(api.deleteRun).toHaveBeenCalledTimes(3)
  })

  it('disables selection and repeat confirmation while deletion is pending', async () => {
    let finish!: () => void
    vi.mocked(api.deleteRun).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup(); await flushPromises(); await select(wrapper, 'candy')
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click')
    expect(wrapper.get('[data-test=model-runs-bulk-delete-confirm]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=model-run-select-pelican]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click')
    expect(api.deleteRun).toHaveBeenCalledTimes(1)
    finish(); await flushPromises()
  })

  it('reports a rejected deletion even when the server responds not found', async () => {
    vi.mocked(api.deleteRun).mockRejectedValue({ status: 404 })
    const wrapper = setup(); await flushPromises(); await select(wrapper, 'candy')
    await wrapper.get('[data-test=model-runs-bulk-delete]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-delete-confirm]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('bulkDeletePartial')
    expect((wrapper.get('[data-test=model-run-select-candy]').element as HTMLInputElement).checked).toBe(true)
  })

  it('clears selection on page, batch and workspace changes', async () => {
    vi.mocked(api.runs).mockImplementation(async () => ({ ...page(), total: 42 }))
    const wrapper = setup(); await flushPromises(); await select(wrapper, 'candy')
    await wrapper.get('[data-test=model-runs-next]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
    await select(wrapper, 'candy')
    await wrapper.get('[data-test=model-run-batch-candy]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
    await select(wrapper, 'candy')
    await wrapper.setProps({ siteId: 100 }); await flushPromises()
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
    expect(api.deleteRun).not.toHaveBeenCalled()
  })

  it('preserves visible selections during polling and prunes records removed elsewhere', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.mocked(api.runs).mockImplementation(async () => ({ ...page(), counts: { running: 1 } }))
    const wrapper = setup(); await flushPromises(); await select(wrapper, 'candy')
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect((wrapper.get('[data-test=model-run-select-candy]').element as HTMLInputElement).checked).toBe(true)
    items = items.filter(run => run.id !== 'candy')
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(wrapper.get('[data-test=model-runs-bulk-delete]').attributes('disabled')).toBeDefined()
  })

  it('does not show local bulk selection in the upstream panel', async () => {
    const wrapper = setup(false); await flushPromises()
    expect(wrapper.find('[data-test=model-runs-bulk-delete]').exists()).toBe(false)
    expect(wrapper.find('[data-test=model-runs-select-page]').exists()).toBe(false)
  })
})
