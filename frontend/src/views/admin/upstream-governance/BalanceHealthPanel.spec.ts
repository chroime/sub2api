import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import BalanceHealthPanel from './BalanceHealthPanel.vue'
import type { BalanceHealth } from '@/api/admin/upstream-governance'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { count: number }) => values ? `${key}:${values.count}` : key }) }))

const health: BalanceHealth = { collection_enabled: true, interval_minutes: 15, last_attempt_at: '2026-09-27T01:00:00Z', observed_at: '2026-09-27T00:00:00Z', next_run_at: '2026-09-27T01:15:00Z', stale: true, monitor_enabled: true, state: 'unknown', delivery_ready: true, recipient_count: 2, reason: 'balance_stale', delivery_reason: 'ready', last_notified_at: null, last_delivery_error: '' }
describe('balance observability', () => {
  it('keeps stale balance separate from mail configuration readiness and offers explicit refresh', async () => {
    const wrapper = mount(BalanceHealthPanel, { props: { health } })
    expect(wrapper.text()).toContain('governance.balanceStale')
    expect(wrapper.get('[data-test=balance-stale]').text()).toBe('governance.balanceStaleLabel')
    expect(wrapper.text()).toContain('governance.mailConfigurationReady')
    expect(wrapper.text()).toContain('governance.mailReadinessHint')
    expect(wrapper.text()).toContain('governance.recipientCount:2')
    expect(wrapper.get('a').attributes('href')).toBe('/admin/settings')
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    expect(wrapper.emitted('reload')).toHaveLength(1)
    wrapper.unmount()
  })
  it('reports missing recipients and unavailable observations without claiming healthy status', () => {
    const wrapper = mount(BalanceHealthPanel, { props: { health: { ...health, delivery_ready: false, delivery_reason: 'recipients_unavailable', recipient_count: 0, observed_at: null, next_run_at: null, reason: 'collection_failed' } } })
    expect(wrapper.text()).toContain('governance.balanceCollectionFailed')
    expect(wrapper.text()).toContain('governance.recipientsUnavailable')
    expect(wrapper.text()).not.toContain('governance.mailConfigurationReady')
    expect(wrapper.text()).not.toContain('governance.balanceHealthHealthy')
    wrapper.unmount()
  })
})
