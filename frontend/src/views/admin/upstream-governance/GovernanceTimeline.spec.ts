import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import api, { type OperationsPage, type TimelineItem } from '@/api/admin/upstream-operations'
import Select from '@/components/common/Select.vue'
import zh from '@/i18n/locales/zh/governance'
import GovernanceTimeline from './GovernanceTimeline.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
vi.mock('@/api/admin/upstream-operations', () => ({ default: { timeline: vi.fn(), workbench: vi.fn() } }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
afterAll(() => vi.unstubAllGlobals())

const item: TimelineItem = { id: 'event:1', record_id: '1', kind: 'event', site_id: 4, site_name: '测试上游', base_url: 'https://fixture.example', resource_id: 'grp-1', resource_name: 'Claude', severity: 'info', status: 'acknowledged', reason: 'rate_changed', created_at: '2026-10-01T01:02:03Z', shared: false, acknowledged: true, before_rate: 0.030000000000000027, after_rate: .04, before_cost: null, after_cost: null, attempts: null, next_attempt_at: null, sent_at: null, related_record_id: null }
const page: OperationsPage<TimelineItem> = { items: [item], total: 1, page: 1, page_size: 20, evaluated_at: '2026-10-01T02:00:00Z' }
function render(props: { siteId: number; disabled?: boolean } = { siteId: 4 }) {
  return mount(GovernanceTimeline, { props, global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] } })
}

describe('factual operations timeline', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    vi.mocked(api.timeline).mockResolvedValue(structuredClone(page))
  })

  it('opens on operations events and compacts each into time, event type, group and change', async () => {
    const wrapper = render({ siteId: 4, disabled: true })
    await flushPromises()
    expect(api.timeline).toHaveBeenCalledWith(4, { kind: 'event', page: 1, page_size: 20 })
    const record = wrapper.get('[data-test=timeline-record]')
    expect(record.text()).toContain('2026-10-01 09:02:03')
    expect(record.text()).toContain('上游倍率变更')
    expect(record.text()).toContain('【Claude】')
    expect(record.get('[data-test=timeline-change]').text()).toBe('倍率 0.03 → 0.04')
    expect(record.text()).not.toContain('0.030000000')
    // Internal record ids, status codes and the read marker are not shown.
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('记录编号')
    expect(wrapper.text()).not.toContain('已读')
    expect(wrapper.find('[data-test=timeline-record-status]').exists()).toBe(false)
  })

  it('names an event group from the collected catalog when the record only has its id', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, reason: 'price_changed', resource_id: '74', resource_name: '', before_rate: null, after_rate: null }] })
    const wrapper = mount(GovernanceTimeline, { props: { siteId: 4, remoteGroups: [{ id: '74', name: 'Claude Max' }] }, global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] } })
    await flushPromises()
    expect(wrapper.text()).toContain('【Claude Max】')
    expect(wrapper.text()).not.toContain('资源 #74')
  })

  it('falls back to the reason when an event carries no measured change', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, reason: 'sync_failed', resource_id: '', resource_name: '', before_rate: null, after_rate: null }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('目录采集失败')
    expect(wrapper.text()).toContain('站点范围')
    // The heading already names it, so no second line repeats it.
    expect(wrapper.find('[data-test=timeline-change]').exists()).toBe(false)
  })

  it('labels applied pricing values as local sale multipliers rather than upstream rates', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, kind: 'pricing', status: 'applied', reason: 'increase', acknowledged: null }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-test=timeline-change]').text()).toBe('本地售价倍率 0.03 → 0.04')
    expect(wrapper.get('[data-test=timeline-record-status]').text()).toBe('已应用')
  })

  it('labels accepted mail as sender acceptance rather than inbox delivery', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, id: 'notification:1', kind: 'notification', status: 'sent', reason: 'notification_accepted', acknowledged: null, attempts: 2, sent_at: '2026-10-01T01:03:00Z' }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-test=timeline-record-status]').text()).toBe('发件服务已接受')
    expect(wrapper.text()).toContain('2 次发送失败')
  })

  it('treats a sent record with zero failures as accepted, not as zero sending attempts', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, kind: 'notification', status: 'sent', reason: 'notification_accepted', acknowledged: null, attempts: 0, sent_at: '2026-10-01T01:03:00Z' }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-test=timeline-record-status]').text()).toBe('发件服务已接受')
    expect(wrapper.text()).toContain('0 次发送失败')
    expect(wrapper.text()).not.toContain('0 次发送尝试')
  })

  it('shows shared pricing as independent evidence rather than a causal stage chain', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [{ ...item, kind: 'pricing', status: 'protected', reason: 'unknown_cost', shared: true, acknowledged: null }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('不能据此认定由本站点触发')
    expect(wrapper.text()).toContain('受保护')
  })

  it('uses server-side filters and resets the page when the category changes', async () => {
    vi.mocked(api.timeline).mockResolvedValue({ ...page, total: 30 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-test=timeline-next]').trigger('click')
    await flushPromises()
    expect(api.timeline).toHaveBeenLastCalledWith(4, { kind: 'event', page: 2, page_size: 20 })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'notification')
    await flushPromises()
    expect(api.timeline).toHaveBeenLastCalledWith(4, { kind: 'notification', page: 1, page_size: 20 })
  })

  it.each([5, 0])('returns from a disappeared timeline last page when total shrinks to %s', async total => {
    vi.mocked(api.timeline).mockResolvedValueOnce({ ...page, total: 25 })
    const wrapper = render()
    await flushPromises()
    vi.mocked(api.timeline).mockResolvedValueOnce({ ...page, page: 2, total: 25 })
    await wrapper.get('[data-test=timeline-next]').trigger('click')
    await flushPromises()
    vi.mocked(api.timeline)
      .mockResolvedValueOnce({ ...page, page: 2, total, items: [] })
      .mockResolvedValueOnce({ ...page, page: 1, total, items: total ? [item] : [] })
    await wrapper.get('[data-test=timeline-refresh]').trigger('click')
    await flushPromises()
    expect(api.timeline).toHaveBeenLastCalledWith(4, { kind: 'event', page: 1, page_size: 20 })
    expect(wrapper.findAll('[data-test=timeline-record]')).toHaveLength(total ? 1 : 0)
    expect(wrapper.find('[data-test=timeline-empty]').exists()).toBe(total === 0)
  })

  it('keeps same-scope records with a stale warning when refresh fails', async () => {
    const wrapper = render()
    await flushPromises()
    vi.mocked(api.timeline).mockRejectedValue(new Error('raw smtp secret'))
    await wrapper.get('[data-test=timeline-refresh]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test=timeline-record]')).toHaveLength(1)
    expect(wrapper.get('[role=alert]').text()).toContain('可能已过期')
    expect(wrapper.text()).not.toContain('raw smtp')
  })

  it('drops old scope and ignores responses after site switch and unmount', async () => {
    let resolve!: (value: OperationsPage<TimelineItem>) => void
    vi.mocked(api.timeline).mockReturnValueOnce(new Promise(value => { resolve = value }))
    const wrapper = render()
    vi.mocked(api.timeline).mockResolvedValue({ ...page, items: [], total: 0 })
    await wrapper.setProps({ siteId: 9 })
    await flushPromises()
    resolve(page)
    await flushPromises()
    expect(wrapper.findAll('[data-test=timeline-record]')).toHaveLength(0)
    expect(api.timeline).toHaveBeenLastCalledWith(9, { kind: 'event', page: 1, page_size: 20 })
    expect(wrapper.get('[data-test=timeline-empty]').text()).toContain('当前类别下暂无记录')
    wrapper.unmount()
  })

  it('skips hidden refresh and clears its timer on unmount', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const wrapper = render()
    await flushPromises()
    visibility.mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.timeline).toHaveBeenCalledTimes(1)
    visibility.mockReturnValue('visible')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.timeline).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.timeline).toHaveBeenCalledTimes(2)
  })
})
