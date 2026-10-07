import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import IQDetectionView from '../IQDetectionView.vue'

const { getIQDetection } = vi.hoisted(() => ({ getIQDetection: vi.fn() }))

vi.mock('@/api/user', () => ({
  default: { getIQDetection },
  getIQDetection,
}))
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))
vi.mock('@/views/admin/upstream-governance/model-preview', () => ({
  modelPreviewCSP: "default-src 'none'",
  modelPreviewDocument: (html: string) => `<!doctype html>${html}`,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))

describe('IQDetectionView', () => {
  it('renders candy results, timeline and an isolated pelican preview', async () => {
    getIQDetection.mockResolvedValueOnce({
      standard_answer: 21,
      window_hours: 24,
      generated_at: '2026-10-02T12:00:00Z',
      candy_results: [{ id: 'candy-1', site_name: 'fixture', group_name: 'GPT Lite', account_label: 'GPT Lite', model: 'gpt-6-astra', effort: 'low', answer: 21, verdict: 'numeric_correct', status: 'succeeded', duration_ms: 50, created_at: '2026-10-02T12:00:00Z' }],
      timeline: [{ at: '2026-10-02T12:00:00Z', status: 'pass' }],
      pelican_works: [{ id: 'pelican-1', site_name: 'fixture', group_name: 'GPT Lite', account_label: 'GPT Lite', model: 'gpt-6-astra', effort: 'low', html: '<html><body><svg /></body></html>', status: 'succeeded', duration_ms: 50, created_at: '2026-10-02T12:00:00Z' }],
    })
    const wrapper = mount(IQDetectionView, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.findAll('[data-test="candy-result-card"]')).toHaveLength(1)
    expect(wrapper.find('[data-test="candy-result-card"]').text()).toContain('GPT Lite')
    expect(wrapper.find('[data-test="candy-result-card"]').text()).toContain('gpt-6-astra · low')
    expect(wrapper.find('[data-test="candy-result-card"]').text()).not.toContain('推理低')
    expect(wrapper.find('[data-test="candy-result-card"] .bg-emerald-50').exists()).toBe(true)
    expect(wrapper.find('[data-test="iq-timeline"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-test="pelican-work-card"]')).toHaveLength(1)
    expect(wrapper.find('[data-test="iq-detection-page"]').classes()).toContain('max-w-screen-2xl')
    expect(wrapper.find('[data-test="pelican-work-grid"]').classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'md:grid-cols-2', '2xl:grid-cols-3']))
    expect(wrapper.find('[data-test="pelican-work-preview"]').classes()).toContain('aspect-[6/5]')
    expect(wrapper.get('[data-test="pelican-work-group"]').text()).toBe('GPT Lite')
    expect(wrapper.get('[data-test="pelican-work-time"]').text()).toBe('2026-10-02 20:00:00')
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('allow-scripts')
    expect(wrapper.get('iframe').attributes('scrolling')).toBe('no')
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('data-pelican-preview-style')
    expect(getIQDetection).toHaveBeenCalledWith({ hours: 24, limit: 8 })
  })
})
