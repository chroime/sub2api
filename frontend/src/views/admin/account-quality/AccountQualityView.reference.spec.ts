import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import api, { type Binding } from '@/api/admin/account-quality'
import AccountQualityView from './AccountQualityView.vue'
import { newPolicy, newSettings } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { overview: vi.fn(), unbind: vi.fn(), run: vi.fn(), control: vi.fn(), savePolicy: vi.fn(), saveSettings: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
enableAutoUnmount(afterEach)

const primary: Binding = { id: 4, version: 7, account_id: 12, account_name: '039', account_type: 'oauth', platform: 'openai', policy_id: 2, policy_name: 'Reasoning', policy_version: 3, model: 'reasoning-model', auto_isolate: false, auto_control_eligible: false, state: 'healthy', failures: 0, recovery_passes: 0, isolated: false }
const secondary: Binding = { ...primary, id: 8, version: 11, policy_id: 3, policy_name: 'Second policy', state: 'pending_review', failures: 1 }
const Cell = defineComponent({ name: 'AccountQualityCell', props: ['bindings', 'disabled'], emits: ['details', 'run', 'evidence', 'control'], template: '<div />' })
const Detail = defineComponent({ name: 'AccountQualityDetail', props: ['accountId', 'initialRunId', 'initialBindingId', 'initialAction'], emits: ['close', 'changed', 'busy'], template: '<div />' })
const options = { global: { stubs: { AccountQualityCell: Cell, AccountQualityDetail: Detail, teleport: true } } }

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.overview).mockResolvedValue({ policies: [{ ...newPolicy(), id: 2, version: 1, enabled: true, name: 'Reasoning' }, { ...newPolicy(), id: 3, version: 1, name: 'Second policy' }], bindings: [structuredClone(primary), structuredClone(secondary)], templates: [], capabilities: [], settings: newSettings() })
  vi.mocked(api.control).mockResolvedValue(structuredClone(secondary))
  vi.mocked(api.savePolicy).mockImplementation(async value => value)
})

describe('account quality reference layout integration', () => {
  it('shows bulk unbind in the filters and requires a selection and confirmation', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    expect(wrapper.get('[data-test="quality-bulk-unbind"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="quality-monitor-binding-4"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    expect(api.unbind).not.toHaveBeenCalled()
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('Reasoning')
    expect(dialog.text()).not.toContain('Second policy')
    await wrapper.get('[data-test="quality-unbind-cancel"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(api.unbind).not.toHaveBeenCalled()
  })

  it('unbinds only the selected binding with its own version, even if the account has another policy', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-monitor-binding-8"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    vi.mocked(api.overview).mockResolvedValue({ policies: [], bindings: [primary], templates: [], capabilities: [], settings: newSettings() })
    await wrapper.get('[data-test="quality-unbind-confirm"]').trigger('click')
    await flushPromises()
    expect(api.unbind).toHaveBeenCalledOnce()
    expect(api.unbind).toHaveBeenCalledWith(8, 11)
    expect(wrapper.find('[data-test="quality-monitor-binding-8"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="quality-monitor-binding-4"]').exists()).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toContain('已解除 {count} 个监测绑定')
    expect(wrapper.get('[data-test="quality-bulk-unbind"]').attributes('disabled')).toBeDefined()
  })

  it('keeps only failed bindings selected and retries them with refreshed versions', async () => {
    vi.mocked(api.unbind).mockImplementation(async id => { if (id === 8) throw new Error('version conflict') })
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-select-visible"]').setValue(true)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    vi.mocked(api.overview).mockResolvedValue({ policies: [], bindings: [{ ...secondary, version: 12 }], templates: [], capabilities: [], settings: newSettings() })
    await wrapper.get('[data-test="quality-unbind-confirm"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.unbind).mock.calls).toEqual([[4, 7], [8, 11]])
    expect(wrapper.get('[role="status"]').text()).toContain('已解除 {success} 个监测绑定，{failed} 个失败')
    expect((wrapper.get('[data-test="quality-monitor-binding-8"] input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    vi.mocked(api.unbind).mockResolvedValue(undefined)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    await wrapper.get('[data-test="quality-unbind-confirm"]').trigger('click')
    await flushPromises()
    expect(api.unbind).toHaveBeenLastCalledWith(8, 12)
    expect(vi.mocked(api.unbind).mock.calls.filter(([id]) => id === 4)).toHaveLength(1)
  })

  it('locks selection, confirmation and navigation while a bulk unbind is pending', async () => {
    let finish!: () => void
    vi.mocked(api.unbind).mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-monitor-binding-4"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    await wrapper.get('[data-test="quality-unbind-confirm"]').trigger('click')
    expect(wrapper.get('[data-test="quality-unbind-confirm"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="quality-unbind-cancel"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="quality-monitor-binding-8"] input[type="checkbox"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="quality-tab-policies"]').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    finish(); await flushPromises()
    expect(api.unbind).toHaveBeenCalledOnce()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })

  it('removes successful bindings locally even when the refresh fails', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-monitor-binding-4"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    vi.mocked(api.overview).mockRejectedValue(new Error('refresh unavailable'))
    await wrapper.get('[data-test="quality-unbind-confirm"]').trigger('click')
    await flushPromises()
    expect(api.unbind).toHaveBeenCalledWith(4, 7)
    expect(wrapper.find('[data-test="quality-monitor-binding-4"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="quality-monitor-binding-8"]').exists()).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toBe('refresh unavailable')
    expect(wrapper.get('[data-test="quality-bulk-unbind"]').attributes('disabled')).toBeDefined()
  })

  it('selects and clears only visible bindings without clearing selections hidden by a filter', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-monitor-binding-8"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quality-state-filter"] .select-trigger').trigger('click')
    await wrapper.get('[data-test="quality-state-filter"]').findAll('[role="option"]')[1]!.trigger('click')
    expect(wrapper.find('[data-test="quality-monitor-binding-8"]').exists()).toBe(false)
    await wrapper.get('[data-test="quality-select-visible"]').setValue(true)
    expect((wrapper.get('[data-test="quality-monitor-binding-4"] input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-test="quality-select-visible"]').setValue(false)
    expect(wrapper.get('[data-test="quality-bulk-unbind"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="quality-bulk-unbind"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('Second policy')
    expect(wrapper.get('[role="dialog"]').text()).not.toContain('Reasoning')
  })

  it('toggles a policy directly from its card without opening the editor', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    await wrapper.get('[data-test="quality-tab-policies"]').trigger('click')
    await wrapper.get('[data-test="quality-policy-toggle-2"]').trigger('click')
    await flushPromises()

    expect(api.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ id: 2, version: 1, name: 'Reasoning', enabled: false }))
    expect(wrapper.find('#account-quality-policy-form').exists()).toBe(false)
  })

  it('applies automatic isolation directly for the exact binding without opening the detail form', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()
    wrapper.findAllComponents(Cell)[1]!.vm.$emit('control', secondary, 'set_auto')
    await flushPromises()

    expect(wrapper.findComponent(Detail).exists()).toBe(false)
    expect(api.control).toHaveBeenCalledWith(8, { version: 11, action: 'set_auto', auto_isolate: true, reason: '启用账号质量自动隔离' })
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

  it('uses themed listbox filters instead of browser-native option popups', async () => {
    const wrapper = mount(AccountQualityView, options)
    await flushPromises()

    expect(wrapper.findAll('select')).toHaveLength(0)
    expect(wrapper.get('[data-test="quality-state-filter"] .select-trigger').attributes('aria-haspopup')).toBe('true')
    expect(wrapper.get('[data-test="quality-policy-filter"] .select-trigger').attributes('aria-haspopup')).toBe('true')
  })
})
