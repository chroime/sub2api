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
  it('renders only the pelican timeline and isolated preview', async () => {
    getIQDetection.mockResolvedValueOnce({
      window_hours: 24,
      generated_at: '2026-10-02T12:00:00Z',
      timeline: [{ at: '2026-10-02T12:00:00Z', status: 'pass' }],
      pelican_works: [{ id: 'pelican-1', site_name: 'fixture', group_name: 'GPT Lite', account_label: 'GPT Lite', model: 'gpt-6-astra', effort: 'low', html: '<html><body><svg /></body></html>', status: 'succeeded', duration_ms: 50, created_at: '2026-10-02T12:00:00Z' }],
    })
    const wrapper = mount(IQDetectionView, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.find('[data-test="candy-result-card"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="iq-timeline"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-test="pelican-work-card"]')).toHaveLength(1)
    expect(wrapper.find('[data-test="iq-detection-page"]').classes()).toContain('max-w-screen-2xl')
    expect(wrapper.find('[data-test="pelican-work-grid"]').classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'md:grid-cols-2', '2xl:grid-cols-3']))
    expect(wrapper.find('[data-test="pelican-work-preview"]').classes()).toContain('aspect-[6/5]')
    expect(wrapper.get('[data-test="pelican-work-format"]').text()).toBe('HTML')
    await wrapper.get('[data-test="pelican-work-view"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
    expect(wrapper.get('[data-test="pelican-work-group"]').text()).toBe('GPT Lite')
    expect(wrapper.get('[data-test="pelican-work-time"]').text()).toBe('2026-10-02 20:00:00')
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('allow-scripts')
    expect(wrapper.get('iframe').attributes('scrolling')).toBe('no')
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('data-pelican-preview-style')
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('scrollbar-width:none')
    expect(getIQDetection).toHaveBeenCalledWith({ hours: 24, limit: 8 })
  })

  it('paginates pelican works with a next-page request', async () => {
    const page = (id: string, pelican_page: number, pelican_has_more: boolean) => ({
      window_hours: 24,
      generated_at: '2026-10-02T12:00:00Z',
      timeline: [],
      pelican_page,
      pelican_page_size: 2,
      pelican_has_more,
      pelican_works: [{ id, site_name: 'fixture', group_name: 'GPT Lite', account_label: 'GPT Lite', model: 'gpt-6-astra', effort: 'low', html: '<html><body><svg /></body></html>', status: 'succeeded', duration_ms: 50, created_at: '2026-10-02T12:00:00Z' }],
    })
    getIQDetection.mockResolvedValueOnce(page('pelican-1', 1, true)).mockResolvedValueOnce(page('pelican-2', 2, false))
    const wrapper = mount(IQDetectionView, { global: { stubs: { Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-test="pelican-next"]').trigger('click')
    await flushPromises()
    expect(getIQDetection).toHaveBeenLastCalledWith({ hours: 24, limit: 8, pelican_page: 2, pelican_page_size: 8 })
    expect(wrapper.get('[data-test="pelican-work-card"]').text()).toContain('GPT Lite')
  })

  it('shows every non-failed timeline slot as green and hides the empty state copy', async () => {
    getIQDetection.mockResolvedValueOnce({
      window_hours: 24,
      generated_at: '2026-10-02T12:00:00Z',
      timeline: [
        { at: '2026-10-02T11:00:00Z', status: 'empty' },
        { at: '2026-10-02T11:30:00Z', status: 'fail' },
        { at: '2026-10-02T12:00:00Z', status: 'unknown' },
      ],
      pelican_works: [],
    })

    const wrapper = mount(IQDetectionView, { global: { stubs: { Icon: true } } })
    await flushPromises()

    const points = wrapper.findAll('[data-test="iq-timeline-point"]')
    expect(points).toHaveLength(3)
    expect(points[0].classes()).toContain('bg-emerald-500')
    expect(points[1].classes()).toContain('bg-rose-500')
    expect(points[2].classes()).toContain('bg-emerald-500')
    expect(wrapper.text()).not.toContain('iqDetection.eyebrow')
    expect(wrapper.text()).not.toContain('iqDetection.timelineHint')
    expect(wrapper.text()).not.toContain('iqDetection.timelineDisclaimer')
    expect(wrapper.get('[data-test="iq-timeline"]').text()).not.toContain('iqDetection.empty')
  })
})
