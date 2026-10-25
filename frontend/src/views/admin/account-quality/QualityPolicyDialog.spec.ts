import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api, { type Capability, type Policy } from '@/api/admin/account-quality'
import QualityPolicyDialog from './QualityPolicyDialog.vue'
import { newPolicy } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { savePolicy: vi.fn() } }))
enableAutoUnmount(afterEach)
const capabilities: Capability[] = [
  { platform: 'openai', api_mode: 'responses', account_types: ['apikey'], automatic_control: true },
  { platform: 'openai', api_mode: 'chat_completions', account_types: ['apikey'], automatic_control: true },
  { platform: 'openai', api_mode: 'responses', account_types: ['oauth'], automatic_control: false },
  { platform: 'anthropic', api_mode: 'anthropic', account_types: ['apikey'], automatic_control: true },
  { platform: 'gemini', api_mode: 'gemini', account_types: ['apikey'], automatic_control: true },
]
const limits = [
  ['max_output_tokens', 64, 32768], ['timeout_seconds', 5, 600], ['interval_seconds', 10, 604800],
  ['confirm_seconds', 10, 86400], ['cooldown_seconds', 10, 604800], ['recovery_interval_seconds', 10, 86400],
  ['failure_threshold', 1, 100], ['recovery_threshold', 1, 100], ['daily_request_limit', 1, 10000],
] as const
function render(policy: Partial<Policy> = {}) {
  return mount(QualityPolicyDialog, { props: { policy: { ...newPolicy(), id: 4, name: 'Text quality', model: 'gpt-4.1', ...policy }, templates: [{ id: 'logic', name: 'Logic', version: 'v1' }], capabilities }, global: { stubs: { teleport: true } } })
}
beforeEach(() => { vi.resetAllMocks(); vi.mocked(api.savePolicy).mockImplementation(async value => value) })

describe('quality policy model configuration', () => {
  it('uses themed controls and offers only GPT-6 models for a new OpenAI policy', async () => {
    const wrapper = mount(QualityPolicyDialog, { props: { templates: [{ id: 'logic', name: 'Logic', version: 'v1' }], capabilities }, global: { stubs: { teleport: true } } })
    expect(wrapper.findAll('select')).toHaveLength(0)
    expect(wrapper.find('[data-test=quality-policy-model] button.select-trigger').exists()).toBe(true)
    expect(wrapper.get('[data-test=quality-policy-model] .select-value').text()).toBe('gpt-6')
    await wrapper.get('[data-test=quality-policy-model] button.select-trigger').trigger('click')
    expect(wrapper.get('[data-test=quality-policy-model] [role=listbox]').findAll('[role=option]').map(option => option.text())).toEqual(['gpt-6', 'gpt-6-astra', 'gpt-6.1-sol', 'gpt-6-sol', 'gpt-6-luna'])
  })
  it('offers unspecified effort and sends an empty effort for a non-reasoning model', async () => {
    const wrapper = render()
    const effort = wrapper.get('[data-test=quality-policy-effort]')
    await effort.get('button.select-trigger').trigger('click')
    expect(effort.get('[role=option]').text()).toContain('不指定')
    await effort.findAll('[role=option]').find(option => option.text().includes('不指定'))!.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ model: 'gpt-4.1', effort: '' }))
  })

  it('offers only protocols reported for the selected platform and chooses a valid protocol on platform changes', async () => {
    const wrapper = render()
    const protocol = () => wrapper.get('[data-test=quality-policy-protocol]')
    await wrapper.get('[data-test=quality-policy-protocol] button.select-trigger').trigger('click')
    expect(protocol().findAll('[role=option]').map(option => option.text())).toEqual(['responses', 'chat_completions'])
    await wrapper.get('[data-test=quality-policy-platform] button.select-trigger').trigger('click')
    await wrapper.get('[data-test=quality-policy-platform]').findAll('[role=option]').find(option => option.text().includes('anthropic'))!.trigger('click')
    await flushPromises()
    expect(protocol().get('.select-value').text()).toBe('anthropic')
    await wrapper.get('[data-test=quality-policy-platform] button.select-trigger').trigger('click')
    await wrapper.get('[data-test=quality-policy-platform]').findAll('[role=option]').find(option => option.text().includes('gemini'))!.trigger('click')
    await flushPromises()
    expect(protocol().get('.select-value').text()).toBe('gemini')
  })

  it('preserves an invalid saved protocol only as a disabled display value and blocks its submission', async () => {
    const wrapper = render({ platform: 'anthropic', api_mode: 'responses' })
    const protocol = wrapper.get('[data-test=quality-policy-protocol]')
    await protocol.get('button.select-trigger').trigger('click')
    expect(protocol.get('[role=option]').attributes('aria-disabled')).toBe('true')
    expect(protocol.get('.select-value').text()).toContain('responses')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.savePolicy).not.toHaveBeenCalled()
    await protocol.findAll('[role=option]').find(option => option.text().includes('anthropic'))!.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ platform: 'anthropic', api_mode: 'anthropic' }))
  })

  it('matches the backend numeric bounds in seconds mode', async () => {
    const wrapper = render()
    await wrapper.get('[data-test=quality-policy-interval-unit] button.select-trigger').trigger('click')
    await wrapper.get('[data-test=quality-policy-interval-unit]').findAll('[role=option]').find(option => option.text().includes('秒'))!.trigger('click')
    for (const [name, minimum, maximum] of limits) {
      const input = wrapper.get(`input[name="${name}"]`)
      expect(input.attributes('min'), name).toBe(String(minimum))
      expect(input.attributes('max'), name).toBe(String(maximum))
    }
  })

  it.each(limits.flatMap(([field, minimum, maximum]) => [[field, minimum - 1], [field, maximum + 1]] as const))('rejects %s=%s before making an HTTP request', async (field, value) => {
    const wrapper = render({ [field]: value })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[role=alert]').text()).toBeTruthy()
  })
})
