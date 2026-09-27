import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RechargePlanPanel from './RechargePlanPanel.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: { rechargePlan: vi.fn(), saveRechargePlan: vi.fn(), evaluateRechargePlan: vi.fn() } }))
enableAutoUnmount(afterEach)
const value = { version: 1, policy: { mode: 'disabled' as const, threshold: 10, unit: 'quota' as const, amount_minor: 1000, currency: 'USD' as const, daily_budget_minor: 10000, cooldown_minutes: 1440 }, capability: { available: false, reason: 'provider_unavailable' }, status: 'disabled' as const, evaluation: null }
describe('recharge planning without a payment provider', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(api.rechargePlan).mockResolvedValue(value) })
  it('only reads on mount, explains the missing provider and persists exact minor amounts', async () => {
    vi.mocked(api.saveRechargePlan).mockResolvedValue({ ...value, version: 2, status: 'blocked', policy: { ...value.policy, mode: 'plan_only', amount_minor: 1234 } })
    const wrapper = mount(RechargePlanPanel, { props: { siteId: 1 } })
    await flushPromises()
    expect(api.saveRechargePlan).not.toHaveBeenCalled()
    expect(api.evaluateRechargePlan).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.rechargeProviderUnavailable')
    await wrapper.get('[data-test=recharge-enabled]').setValue(true)
    await wrapper.get('[data-test=recharge-amount]').setValue('12.34')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.saveRechargePlan).toHaveBeenCalledWith(1, { version: 1, policy: { ...value.policy, mode: 'plan_only', amount_minor: 1234 } })
    expect(wrapper.text()).toContain('governance.rechargeBlocked')
    expect(wrapper.find('[data-test=recharge-execute]').exists()).toBe(false)
  })
  it('rejects sub-cent input and evaluates only the saved policy explicitly', async () => {
    vi.mocked(api.evaluateRechargePlan).mockResolvedValue({ ...value, status: 'blocked' })
    const wrapper = mount(RechargePlanPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=recharge-amount]').setValue('0.001')
    await wrapper.get('form').trigger('submit')
    expect(api.saveRechargePlan).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.moneyPrecisionInvalid')
    expect(wrapper.get('[data-test=recharge-evaluate]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=recharge-amount]').setValue('10.00')
    await wrapper.get('[data-test=recharge-evaluate]').trigger('click')
    await flushPromises()
    expect(api.evaluateRechargePlan).toHaveBeenCalledWith(1)
  })
  it.each([
    ['recharge-amount', '10000000000.01'],
    ['recharge-budget', '10000000000.01'],
    ['recharge-cooldown', '10081'],
  ])('rejects values outside the server limit for %s', async (field, input) => {
    const wrapper = mount(RechargePlanPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get(`[data-test=${field}]`).setValue(input)
    await wrapper.get('form').trigger('submit')
    expect(api.saveRechargePlan).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.invalid')
  })
  it('explains unmatched balance and collection failures while keeping evaluation blocked', async () => {
    vi.mocked(api.evaluateRechargePlan).mockResolvedValue({ ...value, status: 'blocked', evaluation: { id: 'plan', policy_matched: false, status: 'blocked', reasons: ['balance_above_threshold', 'collection_failed', 'provider_unavailable'], observed_at: null, evaluated_at: '2026-09-27T01:00:00Z', balance: 20, unit: 'quota', amount_minor: 1000, currency: 'USD', daily_budget_remaining_minor: 10000 } })
    const wrapper = mount(RechargePlanPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=recharge-evaluate]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.rechargeThresholdNotReached')
    expect(wrapper.text()).toContain('governance.balanceCollectionFailed')
    expect(wrapper.text()).toContain('governance.rechargeBlocked')
  })
})
