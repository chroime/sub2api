import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import api, { type Binding } from '@/api/admin/account-quality'
import AccountQualityView from './AccountQualityView.vue'
import { newPolicy, newSettings } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { overview: vi.fn(), run: vi.fn(), control: vi.fn(), savePolicy: vi.fn(), saveSettings: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
enableAutoUnmount(afterEach)

const primary: Binding = { id: 4, version: 7, account_id: 12, account_name: '039', account_type: 'oauth', platform: 'openai', policy_id: 2, policy_name: 'Reasoning', policy_version: 3, model: 'reasoning-model', auto_isolate: false, auto_control_eligible: false, state: 'healthy', failures: 0, recovery_passes: 0, isolated: false }
const secondary: Binding = { ...primary, id: 8, version: 11, policy_id: 3, policy_name: 'Second policy', state: 'pending_review', failures: 1 }
const Cell = defineComponent({ name: 'AccountQualityCell', props: ['bindings', 'disabled'], emits: ['details', 'run', 'evidence', 'control'], template: '<div />' })
const Detail = defineComponent({ name: 'AccountQualityDetail', props: ['accountId', 'initialRunId', 'initialBindingId', 'initialAction'], emits: ['close', 'changed', 'busy'], template: '<div />' })
const options = { global: { stubs: { AccountQualityCell: Cell, AccountQualityDetail: Detail } } }

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.overview).mockResolvedValue({ policies: [{ ...newPolicy(), id: 2, name: 'Reasoning' }, { ...newPolicy(), id: 3, name: 'Second policy' }], bindings: [structuredClone(primary), structuredClone(secondary)], templates: [], capabilities: [], settings: newSettings() })
})

describe('account quality reference layout integration', () => {
  it('opens the audited control form with the exact account, binding and requested action without writes', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    wrapper.findAllComponents(Cell)[1]!.vm.$emit('control', secondary, 'set_auto')
    await flushPromises()

    expect(wrapper.getComponent(Detail).props()).toMatchObject({ accountId: 12, initialBindingId: 8, initialAction: 'set_auto', initialRunId: '' })
    expect(api.control).not.toHaveBeenCalled()
    expect(api.run).not.toHaveBeenCalled()
    expect(api.savePolicy).not.toHaveBeenCalled()
    expect(api.saveSettings).not.toHaveBeenCalled()
  })

  it('clears an earlier action preselection when the administrator opens the regular account summary', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    wrapper.findAllComponents(Cell)[1]!.vm.$emit('control', secondary, 'confirm_false_positive')
    await flushPromises()
    wrapper.getComponent(Detail).vm.$emit('close')
    await flushPromises()
    await wrapper.get('[data-test="quality-monitor-binding-4"] td:nth-child(2) button').trigger('click')

    expect(wrapper.getComponent(Detail).props()).toMatchObject({ accountId: 12, initialBindingId: 0, initialAction: undefined, initialRunId: '' })
    expect(api.control).not.toHaveBeenCalled()
  })

  it('retains each binding version when running a check from its result area', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    wrapper.findAllComponents(Cell)[1]!.vm.$emit('run', secondary)
    await flushPromises()

    expect(api.run).toHaveBeenCalledOnce()
    expect(api.run).toHaveBeenCalledWith(8, { version: 11, request_id: expect.any(String) })
    expect(api.control).not.toHaveBeenCalled()
  })

  it('preserves distinct account and binding status counts when one account owns multiple strategies', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()

    expect(wrapper.get('[data-test="quality-counter-assigned"] dd').text()).toBe('1')
    expect(wrapper.get('[data-test="quality-counter-state_healthy"] dd').text()).toBe('1')
    expect(wrapper.get('[data-test="quality-counter-state_pending_review"] dd').text()).toBe('1')
    expect(wrapper.get('[data-test="quality-counter-state_quarantined"] dd').text()).toBe('0')
    expect(wrapper.findAllComponents(Cell)).toHaveLength(2)
    expect(api.control).not.toHaveBeenCalled()
  })
})
