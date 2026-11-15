import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelMonitorPanel from './ModelMonitorPanel.vue'
import Select from '@/components/common/Select.vue'
import api, { type ModelRun, type ModelRunPage } from '@/api/admin/upstream-model-monitoring'
import { modelConfig, modelRun } from './__tests__/model-fixtures'

vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key }) }))
vi.mock('@/api/admin/upstream-model-monitoring', () => ({ default: { policies: vi.fn(), runs: vi.fn(), stats: vi.fn(), localTargets: vi.fn(), run: vi.fn(), review: vi.fn() } }))
enableAutoUnmount(afterEach)
let items: ModelRun[]
const setup = (localOnly = true) => mount(ModelMonitorPanel, { global: { stubs: { teleport: true, transition: false } }, props: { siteId: 99, localOnly, localTargets: [], remoteGroups: [], managedKeys: [] } })
const selectAll = async (wrapper: ReturnType<typeof setup>) => wrapper.get('[data-test=model-runs-select-page]').setValue(true)
const open = async (wrapper: ReturnType<typeof setup>) => { await selectAll(wrapper); await wrapper.get('[data-test=model-runs-bulk-review]').trigger('click') }

describe('selected model run bulk review', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    items = ['auto', 'one', 'two', 'running'].map(id => {
      const run = modelRun({ id, site_id: 99, status: id === 'running' ? 'running' : 'succeeded', request: { ...modelRun().request, config: { ...modelConfig, target_type: 'local_group' } } })
      if (id !== 'auto') run.result!.candy_verdict = 'needs_review'
      return run
    })
    vi.mocked(api.policies).mockResolvedValue([])
    vi.mocked(api.stats).mockResolvedValue({ days: 7, groups: [] })
    vi.mocked(api.runs).mockImplementation(async () => ({ items: [...items], total: items.length, page: 1, page_size: 20, counts: { succeeded: 3 } } as ModelRunPage))
    vi.mocked(api.review).mockImplementation(async (_site, id, review, note) => {
      const run = items.find(item => item.id === id)!
      Object.assign(run, { review, review_note: note }); return { ...run }
    })
  })

  it.each([true, false])('reviews selected finished runs in localOnly=%s and skips automatic passes', async localOnly => {
    if (!localOnly) items.forEach(run => { run.request.config.target_type = 'upstream' })
    const wrapper = setup(localOnly); await flushPromises(); await open(wrapper)
    expect(api.review).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test=model-runs-bulk-review-dialog]').text()).toContain('"count":2')
    expect(wrapper.get('[data-test=model-run-select-running]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=model-runs-bulk-review-note]').setValue('统一检查')
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click'); await flushPromises()
    expect(vi.mocked(api.review).mock.calls).toEqual([[99, 'one', 'pass', '统一检查'], [99, 'two', 'pass', '统一检查']])
    expect(wrapper.find('[data-test=model-runs-bulk-review-dialog]').exists()).toBe(false)
  })

  it('supports failure and pending and keeps only rejected records selected for retry', async () => {
    vi.mocked(api.review).mockImplementation(async (_site, id, review, note) => {
      if (id === 'two') throw { status: 409 }
      return { ...items.find(item => item.id === id)!, review, review_note: note }
    })
    const wrapper = setup(); await flushPromises(); await open(wrapper)
    wrapper.get('[data-test=model-runs-bulk-review-dialog]').getComponent(Select).vm.$emit('update:modelValue', 'fail')
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click'); await flushPromises()
    expect(api.review).toHaveBeenCalledWith(99, 'one', 'fail', '')
    expect(wrapper.get('[role=alert]').text()).toContain('bulkReviewPartial')
    expect((wrapper.get('[data-test=model-run-select-two]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test=model-run-select-one]').element as HTMLInputElement).checked).toBe(false)
    vi.mocked(api.review).mockResolvedValue({ ...items[2]!, review: 'pending' })
    await wrapper.get('[data-test=model-runs-bulk-review]').trigger('click')
    wrapper.get('[data-test=model-runs-bulk-review-dialog]').getComponent(Select).vm.$emit('update:modelValue', 'pending')
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click'); await flushPromises()
    expect(api.review).toHaveBeenLastCalledWith(99, 'two', 'pending', '')
    expect(api.review).toHaveBeenCalledTimes(3)
  })

  it('locks repeat submission, selection and navigation while requests are pending', async () => {
    let finish!: (run: ModelRun) => void
    vi.mocked(api.review).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup(); await flushPromises()
    await wrapper.get('[data-test=model-run-select-one]').setValue(true)
    await wrapper.get('[data-test=model-runs-bulk-review]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click')
    expect(wrapper.get('[data-test=model-runs-bulk-review-confirm]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=model-run-select-two]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click')
    expect(api.review).toHaveBeenCalledTimes(1)
    finish(items[1]!); await flushPromises()
  })

  it('cancels without writes and disables manual batch review when only automatic passes are selected', async () => {
    const wrapper = setup(); await flushPromises()
    await wrapper.get('[data-test=model-run-select-auto]').setValue(true)
    expect(wrapper.get('[data-test=model-runs-bulk-review]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=model-run-select-one]').setValue(true)
    await wrapper.get('[data-test=model-runs-bulk-review]').trigger('click')
    await wrapper.get('[data-test=model-runs-bulk-review-cancel]').trigger('click')
    expect(api.review).not.toHaveBeenCalled()
    await wrapper.get('[data-test=model-run-filters]').trigger('submit'); await flushPromises()
    expect((wrapper.get('[data-test=model-run-select-one]').element as HTMLInputElement).checked).toBe(false)
  })

  it('bounds concurrency to four and stops unsent chunks after switching sites', async () => {
    items = Array.from({ length: 10 }, (_, i) => ({ ...items[1]!, id: `pending-${i}` }))
    const completions: Array<() => void> = []
    vi.mocked(api.review).mockImplementation((_site, id) => new Promise(resolve => { completions.push(() => resolve({ ...items.find(run => run.id === id)!, review: 'pass' })) }))
    const wrapper = setup(); await flushPromises(); await open(wrapper)
    await wrapper.get('[data-test=model-runs-bulk-review-confirm]').trigger('click')
    expect(api.review).toHaveBeenCalledTimes(4)
    await wrapper.setProps({ siteId: 100 }); await flushPromises()
    completions.forEach(finish => finish()); await flushPromises()
    expect(api.review).toHaveBeenCalledTimes(4)
    expect(vi.mocked(api.review).mock.calls.every(call => call[0] === 99)).toBe(true)
    expect(wrapper.find('[data-test=model-runs-bulk-review-dialog]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('bulkReviewSuccess')
  })
})
