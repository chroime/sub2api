import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BalancePrechargeSettings from '../BalancePrechargeSettings.vue'

const { getSettings, updateSettings } = vi.hoisted(() => ({ getSettings: vi.fn(), updateSettings: vi.fn() }))
vi.mock('@/api/admin/settings', () => ({ getBalancePrechargeSettings: getSettings, updateBalancePrechargeSettings: updateSettings }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('../BalancePrechargeReviews.vue', () => ({ default: { template: '<div />' } }))

describe('BalancePrechargeSettings', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue({ enabled: false, threshold: 0, amount: 0 })
    updateSettings.mockReset().mockImplementation(async (value) => value)
  })

  it('requires valid positive amounts before enabling and saves an eight-decimal fixed amount', async () => {
    const wrapper = mount(BalancePrechargeSettings)
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[data-testid="precharge-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="precharge-threshold"]').setValue('10')
    await wrapper.get('[data-testid="precharge-amount"]').setValue('1.12345678')
    await wrapper.get('[data-testid="precharge-save"]').trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith({ enabled: true, threshold: 10, amount: 1.12345678 })
    expect(wrapper.text()).toContain('admin.settings.balancePrecharge.saved')
    wrapper.unmount()
  })

  it.each(['0.000000001', '11', '-1', 'NaN', '1e-2', '1000001'])('rejects invalid USD input %s', async (amount) => {
    getSettings.mockResolvedValue({ enabled: true, threshold: 10, amount: 1 })
    const wrapper = mount(BalancePrechargeSettings)
    await flushPromises()
    await wrapper.get('[data-testid="precharge-amount"]').setValue(amount)
    expect(wrapper.get('[data-testid="precharge-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="precharge-validation"]').text()).toContain('admin.settings.balancePrecharge.invalidAmounts')
    expect(updateSettings).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not display an unconfirmed save as enabled and reloads after failure', async () => {
    updateSettings.mockRejectedValue(new Error('offline'))
    const wrapper = mount(BalancePrechargeSettings)
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('[data-testid="precharge-threshold"]').setValue('10')
    await wrapper.get('[data-testid="precharge-amount"]').setValue('1')
    await wrapper.get('[data-testid="precharge-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.settings.balancePrecharge.saveFailed')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    getSettings.mockResolvedValue({ enabled: true, threshold: 10, amount: 1 })
    await wrapper.get('[data-testid="precharge-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('keeps controls disabled after a failed initial load', async () => {
    getSettings.mockRejectedValue(new Error('offline'))
    const wrapper = mount(BalancePrechargeSettings)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.settings.balancePrecharge.loadFailed')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
