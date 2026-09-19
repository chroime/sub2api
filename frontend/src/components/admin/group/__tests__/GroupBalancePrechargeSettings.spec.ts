import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupBalancePrechargeSettings from '../GroupBalancePrechargeSettings.vue'

const { getSettings, updateSettings } = vi.hoisted(() => ({ getSettings: vi.fn(), updateSettings: vi.fn() }))
vi.mock('@/api/admin/settings', () => ({ getGroupBalancePrechargeSettings: getSettings, updateGroupBalancePrechargeSettings: updateSettings }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => `${key} ${JSON.stringify(params ?? {})}` }) }))

const inherited = () => ({
  group_id: 7,
  settings: { mode: 'inherit', threshold: 0, amount: 0 },
  global: { enabled: true, threshold: 10, amount: 1 },
  effective: { enabled: true, threshold: 10, amount: 1 },
})

describe('GroupBalancePrechargeSettings', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue(inherited())
    updateSettings.mockReset().mockResolvedValue(inherited())
  })

  it('shows inherited effective USD values and saves only the persisted group ID', async () => {
    const wrapper = mount(GroupBalancePrechargeSettings, { props: { groupId: 7 } })
    await flushPromises()
    expect(getSettings).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-testid="group-precharge-effective"]').text()).toContain('"threshold":"10"')
    expect(wrapper.get('[data-testid="group-precharge-effective"]').text()).toContain('"amount":"1"')
    await wrapper.get('[data-testid="group-precharge-mode"]').setValue('custom')
    await wrapper.get('[data-testid="group-precharge-threshold"]').setValue('5')
    await wrapper.get('[data-testid="group-precharge-amount"]').setValue('2')
    await wrapper.get('[data-testid="group-precharge-save"]').trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith(7, { mode: 'custom', threshold: 5, amount: 2 })
    wrapper.unmount()
  })

  it('keeps the global switch authoritative and rejects excess decimals in custom mode', async () => {
    const value = inherited()
    value.global.enabled = false
    value.effective.enabled = false
    getSettings.mockResolvedValue(value)
    const wrapper = mount(GroupBalancePrechargeSettings, { props: { groupId: 7 } })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.balancePrecharge.globalDisabled')
    await wrapper.get('[data-testid="group-precharge-mode"]').setValue('custom')
    await wrapper.get('[data-testid="group-precharge-amount"]').setValue('0.000000001')
    expect(wrapper.get('[data-testid="group-precharge-save"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('reports a failed save and requires reloading confirmed settings', async () => {
    updateSettings.mockRejectedValue(new Error('offline'))
    const wrapper = mount(GroupBalancePrechargeSettings, { props: { groupId: 7 } })
    await flushPromises()
    await wrapper.get('[data-testid="group-precharge-mode"]').setValue('custom')
    await wrapper.get('[data-testid="group-precharge-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.settings.balancePrecharge.saveFailed')
    expect(wrapper.get('[data-testid="group-precharge-mode"]').element).toHaveProperty('value', 'inherit')
    expect(wrapper.get('[data-testid="group-precharge-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="group-precharge-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows global amounts immediately when returning a custom group to inheritance', async () => {
    getSettings.mockResolvedValue({
      ...inherited(),
      settings: { mode: 'custom', threshold: 5, amount: 2 },
      effective: { enabled: true, threshold: 5, amount: 2 },
    })
    const wrapper = mount(GroupBalancePrechargeSettings, { props: { groupId: 7 } })
    await flushPromises()
    await wrapper.get('[data-testid="group-precharge-mode"]').setValue('inherit')
    expect(wrapper.get('[data-testid="group-precharge-threshold"]').element).toHaveProperty('value', '10')
    expect(wrapper.get('[data-testid="group-precharge-amount"]').element).toHaveProperty('value', '1')
    await wrapper.get('[data-testid="group-precharge-save"]').trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith(7, { mode: 'inherit', threshold: 0, amount: 0 })
    wrapper.unmount()
  })

  it('ignores a late response for a previously selected group', async () => {
    let resolve!: (value: ReturnType<typeof inherited>) => void
    getSettings.mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const wrapper = mount(GroupBalancePrechargeSettings, { props: { groupId: 7 } })
    getSettings.mockResolvedValue({ ...inherited(), group_id: 8, global: { enabled: true, threshold: 20, amount: 3 }, effective: { enabled: true, threshold: 20, amount: 3 } })
    await wrapper.setProps({ groupId: 8 })
    await flushPromises()
    resolve(inherited())
    await flushPromises()
    expect(wrapper.get('[data-testid="group-precharge-effective"]').text()).toContain('"threshold":"20"')
    await wrapper.get('[data-testid="group-precharge-save"]').trigger('click')
    expect(updateSettings).toHaveBeenCalledWith(8, { mode: 'inherit', threshold: 0, amount: 0 })
    wrapper.unmount()
  })
})
