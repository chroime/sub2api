import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import api, { type WorkbenchItem, type WorkbenchPage } from '@/api/admin/upstream-operations'
import zh from '@/i18n/locales/zh/governance'
import GovernanceWorkbench from './GovernanceWorkbench.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
vi.mock('@/api/admin/upstream-operations', () => ({ default: { workbench: vi.fn(), timeline: vi.fn() } }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
afterAll(() => vi.unstubAllGlobals())

const item: WorkbenchItem = { id: 'auth-4', site_id: 4, site_name: '测试上游', base_url: 'https://fixture.example', kind: 'authorization', severity: 'critical', status: 'action_required', reason: 'reauth_required', resource_id: '', resource_name: '', impact_count: 2, observed_at: '2026-10-01T01:02:03Z', next_attempt_at: '2026-10-01T01:12:03Z', shared: false, target_tab: 'connect' }
const page: WorkbenchPage = { items: [item], total: 1, page: 1, page_size: 20, evaluated_at: '2026-10-01T02:00:00Z', summary: { critical: 1, warning: 0, info: 0 } }
function render(props: { siteId?: number; disabled?: boolean } = {}) {
  return mount(GovernanceWorkbench, { props, global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] } })
}

describe('current operations workbench', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    vi.mocked(api.workbench).mockResolvedValue(structuredClone(page))
  })

  it('reads once even while navigation is initially disabled, then emits navigation without a mutation', async () => {
    const wrapper = render({ disabled: true })
    await flushPromises()
    expect(api.workbench).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('测试上游')
    expect(wrapper.text()).toContain('2026-10-01 09:02:03')
    expect(wrapper.get('[data-test=workbench-go-auth-4]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ disabled: false })
    await wrapper.get('[data-test=workbench-go-auth-4]').trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([[{ siteId: 4, section: 'overview' }]])
    expect(api.workbench).toHaveBeenCalledTimes(1)
  })

  it('puts critical items first and uses summary totals for the whole scope', async () => {
    vi.mocked(api.workbench).mockResolvedValue({ ...page, total: 25, items: [{ ...item, id: 'info', severity: 'info' }, item], summary: { critical: 8, warning: 10, info: 7 } })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('[data-test=workbench-item]')[0].attributes('data-id')).toBe('auth-4')
    expect(wrapper.get('[data-test=workbench-summary]').text()).toContain('25')
    expect(wrapper.get('[data-test=workbench-summary]').text()).toContain('10')
    await wrapper.get('[data-test=workbench-next]').trigger('click')
    await flushPromises()
    expect(api.workbench).toHaveBeenLastCalledWith({ page: 2, page_size: 20 })
  })

  it('describes an empty result as evidence at a time, not complete health', async () => {
    vi.mocked(api.workbench).mockResolvedValue({ ...page, items: [], total: 0, summary: { critical: 0, warning: 0, info: 0 } })
    const wrapper = render({ siteId: 4 })
    await flushPromises()
    expect(api.workbench).toHaveBeenCalledWith({ site_id: 4, page: 1, page_size: 20 })
    expect(wrapper.get('[data-test=workbench-empty]').text()).toContain('截至 2026-10-01 10:00:00')
    expect(wrapper.get('[data-test=workbench-empty]').text()).toContain('不等于全部上游健康')
  })

  it('labels affected resources accurately and does not repeat the site name as its resource', async () => {
    vi.mocked(api.workbench).mockResolvedValue({ ...page, items: [{ ...item, resource_name: item.site_name }] })
    const wrapper = render()
    await flushPromises()
    const card = wrapper.get('[data-test=workbench-item]')
    expect(card.get('dt').text()).toBe('涉及对象')
    expect(card.get('dd').text()).toContain('2 个关联对象')
    expect(card.text().match(/测试上游/g)).toHaveLength(1)
  })

  it.each([5, 0])('returns from a disappeared last page when total shrinks to %s', async total => {
    vi.mocked(api.workbench).mockResolvedValueOnce({ ...page, total: 25 })
    const wrapper = render()
    await flushPromises()
    vi.mocked(api.workbench).mockResolvedValueOnce({ ...page, page: 2, total: 25 })
    await wrapper.get('[data-test=workbench-next]').trigger('click')
    await flushPromises()
    vi.mocked(api.workbench)
      .mockResolvedValueOnce({ ...page, page: 2, total, items: [] })
      .mockResolvedValueOnce({ ...page, page: 1, total, items: total ? [item] : [] })
    await wrapper.get('[data-test=workbench-refresh]').trigger('click')
    await flushPromises()
    expect(api.workbench).toHaveBeenLastCalledWith({ page: 1, page_size: 20 })
    expect(wrapper.findAll('[data-test=workbench-item]')).toHaveLength(total ? 1 : 0)
    expect(wrapper.find('[data-test=workbench-empty]').exists()).toBe(total === 0)
  })

  it('retains old cards and marks them stale after a same-scope read fails', async () => {
    const wrapper = render()
    await flushPromises()
    vi.mocked(api.workbench).mockRejectedValue(new Error('raw secret error must not appear'))
    await wrapper.get('[data-test=workbench-refresh]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test=workbench-item]')).toHaveLength(1)
    expect(wrapper.get('[role=alert]').text()).toContain('可能已过期')
    expect(wrapper.text()).not.toContain('raw secret')
    expect(wrapper.find('[data-test=workbench-empty]').exists()).toBe(false)
  })

  it('clears old-site cards and ignores a late old-site response', async () => {
    let resolve!: (value: WorkbenchPage) => void
    vi.mocked(api.workbench).mockReturnValueOnce(new Promise(value => { resolve = value }))
    const wrapper = render({ siteId: 4 })
    await wrapper.setProps({ siteId: 9 })
    vi.mocked(api.workbench).mockResolvedValue({ ...page, items: [{ ...item, id: 'new', site_id: 9, site_name: '新站点' }] })
    await flushPromises()
    // The new-scope request had already started with the prior fixture; its result
    // is replaced by a fresh explicit read before the old request is released.
    await wrapper.get('[data-test=workbench-refresh]').trigger('click')
    await flushPromises()
    resolve(page)
    await flushPromises()
    expect(wrapper.text()).toContain('新站点')
    expect(wrapper.text()).not.toContain('测试上游')
    expect(api.workbench).toHaveBeenLastCalledWith({ site_id: 9, page: 1, page_size: 20 })
  })

  it('does not present a failed initial read as an empty or recovered scope', async () => {
    vi.mocked(api.workbench).mockRejectedValue(new Error('failed'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('无法确认当前状态')
    expect(wrapper.find('[data-test=workbench-empty]').exists()).toBe(false)
  })

  it('retries an unpopulated scope promptly when the navigation lock clears', async () => {
    vi.mocked(api.workbench).mockRejectedValueOnce(new Error('failed'))
    const wrapper = render({ disabled: true })
    await flushPromises()
    await wrapper.setProps({ disabled: false })
    await flushPromises()
    expect(api.workbench).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('[data-test=workbench-item]')).toHaveLength(1)
  })

  it('polls only while visible and unlocked, and removes polling on unmount', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const wrapper = render()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.workbench).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.workbench).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('visible')
    await wrapper.setProps({ disabled: true })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.workbench).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(api.workbench).toHaveBeenCalledTimes(2)
  })
})
