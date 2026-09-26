import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import api, { type Site } from '@/api/admin/upstream-governance'
import BalanceMonitorPanel from './BalanceMonitorPanel.vue'
vi.mock('@/api/admin/upstream-governance', () => ({ default: { balanceMonitor: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const site: Site = { id: 1, name: 'Fixture', platform: 'newapi', base_url: 'https://fixture.example', proxy_id: null, enabled: true, interval_minutes: 15, version: 4, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
describe('balance notification configuration', () => {
  beforeEach(() => vi.resetAllMocks())
  it('uses platform-native units, keeps notifications off until saved, and deduplicates recipients', async () => {
    vi.mocked(api.balanceMonitor).mockResolvedValue({ ...site, version: 5 })
    const wrapper = mount(BalanceMonitorPanel, { props: { site } })
    expect(api.balanceMonitor).not.toHaveBeenCalled()
    await wrapper.get('#governance-balance-settings').trigger('click')
    expect(wrapper.get('input[readonly]').attributes('value')).toBe('QUOTA')
    expect(wrapper.get('[data-test=balance-cooldown]').attributes('min')).toBe('15')
    await wrapper.get('[data-test=balance-enabled]').setValue(true)
    await wrapper.get('[data-test=balance-recipients]').setValue('one@example.test, two@example.test\none@example.test')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.balanceMonitor).toHaveBeenCalledWith(1, { version: 4, enabled: true, threshold: 10, unit: 'quota', cooldown_minutes: 1440, recipients: ['one@example.test', 'two@example.test'] })
    expect(wrapper.emitted('saved')?.[0]?.[0]).toEqual({ ...site, version: 5 })
    wrapper.unmount()
  })
  it('distinguishes missing balances from failed mail delivery', () => {
    const wrapper = mount(BalanceMonitorPanel, { props: { site: { ...site, balance_monitor_status: { state: 'unknown', last_attempt_at: null, last_notified_at: null, last_error: 'balance_unavailable' } } } })
    expect(wrapper.text()).toContain('governance.balanceUnavailable')
    expect(wrapper.text()).not.toContain('governance.emailDeliveryFailed')
    wrapper.unmount()
  })
  it('does not apply the previous site’s save response after switching sites', async () => {
    let finish!: (value: Site) => void
    vi.mocked(api.balanceMonitor).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(BalanceMonitorPanel, { props: { site } })
    await wrapper.get('#governance-balance-settings').trigger('click')
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ site: { ...site, id: 2, platform: 'sub2api' } })
    finish({ ...site, version: 5 })
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('input[readonly]').attributes('value')).toBe('USD')
    wrapper.unmount()
  })
})
