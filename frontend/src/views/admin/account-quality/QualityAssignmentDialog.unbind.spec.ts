import { defineComponent, onMounted } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api, { type Binding } from '@/api/admin/account-quality'
import groupsAPI from '@/api/admin/groups'
import QualityAssignmentDialog from './QualityAssignmentDialog.vue'
import { newPolicy } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { assign: vi.fn(), account: vi.fn(), unbind: vi.fn() } }))
vi.mock('@/api/admin/groups', () => ({ default: { getAll: vi.fn() } }))
enableAutoUnmount(afterEach)

const bindings = [
  { id: 11, version: 3, account_id: 39, policy_id: 1, policy_name: 'Reasoning policy', model: 'test-model' },
  { id: 12, version: 7, account_id: 39, policy_id: 2, policy_name: 'Precision policy', model: 'other-model' },
] as Binding[]
const Picker = defineComponent({
  props: ['modelValue', 'platform', 'disabled'], emits: ['validation'],
  setup(props, { emit }) {
    onMounted(() => emit('validation', { valid: true, pending: false, platform: props.platform, accountIds: props.modelValue }))
    return () => null
  },
})
const policies = [{ ...newPolicy(), id: 1, name: 'Reasoning policy', platform: 'openai', model: 'test-model' }]
const render = () => mount(QualityAssignmentDialog, {
  props: { policies, accountIds: [39] },
  global: { stubs: { teleport: true, QualityAccountSelector: Picker } },
})
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(groupsAPI.getAll).mockResolvedValue([])
  vi.mocked(api.account).mockResolvedValue({ bindings: [...bindings], runs: [], actions: [] })
  vi.mocked(api.unbind).mockResolvedValue(undefined)
})

describe('quality policy quick unbinding', () => {
  it('removes exactly the clicked assignment and refreshes the parent without closing', async () => {
    const wrapper = render()
    await flushPromises()
    expect(api.account).toHaveBeenCalledWith(39)
    expect(wrapper.get('[data-test=quality-bound-policies]').text()).toContain('Precision policy')
    await wrapper.get('[data-test=quality-unbind-11]').trigger('click')
    await flushPromises()
    expect(api.unbind).toHaveBeenCalledWith(11, 3)
    expect(wrapper.find('[data-test=quality-unbind-11]').exists()).toBe(false)
    expect(wrapper.find('[data-test=quality-unbind-12]').exists()).toBe(true)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(api.assign).not.toHaveBeenCalled()
  })

  it('blocks duplicate unbinding and assignment while the request is in flight', async () => {
    let resolve!: () => void
    vi.mocked(api.unbind).mockReturnValue(new Promise<void>(done => { resolve = done }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-test=quality-unbind-11]').trigger('click')
    expect(wrapper.get('[data-test=quality-unbind-12]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=quality-assignment-save]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=quality-unbind-11]').trigger('click')
    expect(api.unbind).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('busy')).toEqual([[true]])
    resolve()
    await flushPromises()
    expect(wrapper.emitted('busy')).toEqual([[true], [false]])
  })

  it('preserves the assignment and reports a failed removal', async () => {
    vi.mocked(api.unbind).mockRejectedValue({ response: { data: { message: 'Version changed' } } })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-test=quality-unbind-11]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=quality-unbind-11]').exists()).toBe(true)
    expect(wrapper.find('[role=alert]').exists()).toBe(true)
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.get('[data-test=quality-unbind-11]').attributes('disabled')).toBeUndefined()
  })

  it('never displays a late response for a previously selected account', async () => {
    let first!: (value: Awaited<ReturnType<typeof api.account>>) => void
    vi.mocked(api.account).mockReturnValueOnce(new Promise(done => { first = done }))
    vi.mocked(api.account).mockResolvedValueOnce({ bindings: [{ ...bindings[1]!, id: 99, account_id: 40 }], runs: [], actions: [] })
    const wrapper = render()
    await wrapper.setProps({ accountIds: [40] })
    await flushPromises()
    first({ bindings, runs: [], actions: [] })
    await flushPromises()
    expect(wrapper.find('[data-test=quality-unbind-99]').exists()).toBe(true)
    expect(wrapper.find('[data-test=quality-unbind-11]').exists()).toBe(false)
  })
})
