import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelComparison from './ModelComparison.vue'
import api from '@/api/admin/upstream-model-monitoring'
import { modelRun } from './__tests__/model-fixtures'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/upstream-model-monitoring', () => ({ default: { runs: vi.fn(), run: vi.fn() } }))
enableAutoUnmount(afterEach)
describe('same-sample reasoning comparison', () => {
  beforeEach(() => { vi.resetAllMocks() })
  it('loads all pages, matches template and sample, and isolates every artwork independently', async () => {
    const low = modelRun({ id: 'low', request: { ...modelRun().request, template: 'pelican', effort: 'low', sample: 2 } })
    const high = modelRun({ id: 'high', request: { ...modelRun().request, template: 'pelican', effort: 'high', sample: 2 } })
    const other = modelRun({ id: 'wrong-sample', request: { ...modelRun().request, template: 'pelican', effort: 'medium', sample: 1 } })
    vi.mocked(api.runs).mockImplementation(async (_id, page) => ({ items: page === 1 ? [low, other] : [high], total: 3, page: page || 1, page_size: 2 }))
    vi.mocked(api.run).mockImplementation(async (_id, id) => { const run = id === 'low' ? low : high; return { ...run, result: { ...run.result!, response_text: `<html><body>${id}</body></html>` } } })
    const wrapper = mount(ModelComparison, { props: { siteId: 1, batchId: 'batch-fixture', template: 'pelican', sample: 2 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect(api.runs).toHaveBeenCalledWith(1, 2, 'batch-fixture')
    expect(api.run).toHaveBeenCalledTimes(2)
    expect(api.run).not.toHaveBeenCalledWith(1, 'wrong-sample')
    expect(wrapper.get('[data-test=model-compare-medium]').text()).toContain('governance.modelMonitoring.effortNotRun')
    await wrapper.get('[data-test=model-comparison-play]').trigger('click')
    expect(wrapper.findAll('iframe')).toHaveLength(2)
    for (const frame of wrapper.findAll('iframe')) { expect(frame.attributes('sandbox')).toBe('allow-scripts'); expect(frame.attributes('srcdoc')).toContain("connect-src 'none'") }
  })
})
