import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api, { type Binding, type Run } from '@/api/admin/account-quality'
import AccountQualityDetail from '@/components/admin/account/AccountQualityDetail.vue'
import AccountQualityRunDetail from '@/components/admin/account/AccountQualityRunDetail.vue'
import AccountQualityCell from '@/components/admin/account/AccountQualityCell.vue'
import QualityPolicyDialog from './QualityPolicyDialog.vue'
import QualitySettings from './QualitySettings.vue'
import AccountQualityView from './AccountQualityView.vue'
import { newPolicy, newSettings } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { overview: vi.fn(), policies: vi.fn(), savePolicy: vi.fn(), deletePolicy: vi.fn(), assign: vi.fn(), run: vi.fn(), control: vi.fn(), summaries: vi.fn(), account: vi.fn(), runs: vi.fn(), runDetail: vi.fn(), cancelRun: vi.fn(), settings: vi.fn(), saveSettings: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
enableAutoUnmount(afterEach)
const binding: Binding = { id: 4, version: 7, account_id: 12, account_name: 'Diagnostic account', account_type: 'oauth', platform: 'openai', policy_id: 2, policy_name: 'Reasoning', policy_version: 3, model: 'reasoning-model', auto_isolate: false, auto_control_eligible: false, state: 'pending_review', failures: 1, recovery_passes: 0, isolated: false }
const rawRun: Run = { id: 'evidence-original', binding_id: 4, account_id: 12, policy_id: 2, policy_version: 3, binding_version: 7, model: 'reasoning-model', template: 'logic', template_version: 'v1', execution_status: 'completed', quality_verdict: 'fail', prompt: 'Original question', response_text: '<script>window.evidenceExecuted = true</script>Original wrong answer' }
const mountOptions = { global: { stubs: { teleport: true } } }
async function choose(wrapper: ReturnType<typeof mount>, selector: string, text: string) {
  const control = wrapper.get(selector)
  await control.get('button.select-trigger').trigger('click')
  const option = control.findAll('[role=option]').find(item => item.text().includes(text))
  if (!option) throw new Error(`missing option ${text}`)
  await option.trigger('click')
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.account).mockResolvedValue({ bindings: [structuredClone(binding)], runs: [structuredClone(rawRun)], actions: [] })
  vi.mocked(api.runDetail).mockResolvedValue(structuredClone(rawRun))
  vi.mocked(api.overview).mockResolvedValue({ policies: [], bindings: [], templates: [], capabilities: [], settings: newSettings() })
  vi.mocked(api.savePolicy).mockImplementation(async value => value)
  vi.mocked(api.control).mockResolvedValue({ ...binding, override_until: '2026-10-25T09:00:00Z' })
})

describe('account quality administrator safeguards', () => {
  it('uses themed Select controls in the account detail action form', async () => {
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    expect(wrapper.findAll('select')).toHaveLength(0)
    expect(wrapper.get('[data-test=quality-control-action] button.select-trigger').exists()).toBe(true)
  })

  it('opens the workspace through reads without queueing checks, enabling policies or saving notification settings', async () => {
    mount(AccountQualityView)
    await flushPromises()
    expect(api.overview).toHaveBeenCalledOnce()
    expect(api.run).not.toHaveBeenCalled()
    expect(api.control).not.toHaveBeenCalled()
    expect(api.savePolicy).not.toHaveBeenCalled()
    expect(api.saveSettings).not.toHaveBeenCalled()
  })

  it('creates a paused record-and-notify policy and disables automatic control for unverified adapters', async () => {
    const wrapper = mount(QualityPolicyDialog, { ...mountOptions, props: { templates: [{ id: 'logic', name: 'Logic', version: 'v1' }], capabilities: [{ platform: 'openai', api_mode: 'responses', account_types: ['oauth'], automatic_control: false }] } })
    expect((wrapper.get('[data-test=quality-policy-enabled]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-test=quality-policy-mode] .select-value').text()).toBe('记录并通知')
    await wrapper.get('[data-test=quality-policy-mode] button.select-trigger').trigger('click')
    const autoOption = wrapper.get('[data-test=quality-policy-mode]').findAll('[role=option]').find(item => item.text().includes('自动质量隔离'))
    expect(autoOption).toBeDefined()
    expect(autoOption?.attributes('aria-disabled')).toBe('true')
    await wrapper.get('[data-test=quality-policy-name]').setValue('Reasoning monitor')
    await choose(wrapper, '[data-test=quality-policy-model]', 'gpt-6-astra')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, control_mode: 'record_notify', isolation_scope: 'model' }))
  })

  it('edits a reactive policy draft without changing the original policy before Save', async () => {
    const original = { ...newPolicy(), id: 5, name: 'Original', templates: ['logic'] }
    const wrapper = mount(QualityPolicyDialog, { ...mountOptions, props: { policy: original, templates: [], capabilities: [] } })
    await wrapper.get('[data-test=quality-policy-name]').setValue('Changed draft')
    expect(original.name).toBe('Original')
    expect(api.savePolicy).not.toHaveBeenCalled()
  })

  it('requires a manual reason and submits the exact binding version to the quality gate', async () => {
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    expect(api.control).not.toHaveBeenCalled()
    await wrapper.get('[data-test=quality-control-reason]').setValue('Inspecting a suspected false positive')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.control).toHaveBeenCalledWith(4, { version: 7, action: 'override', duration_minutes: 15, reason: 'Inspecting a suspected false positive' })
    expect(rawRun.quality_verdict).toBe('fail')
    expect(rawRun.response_text).toContain('Original wrong answer')
  })

  it('prevents enabling automatic isolation for an ineligible OAuth account', async () => {
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    await choose(wrapper, '[data-test=quality-control-action]', '设置自动隔离')
    expect(wrapper.get('[data-test=quality-control-auto]').attributes('disabled')).toBeDefined()
    expect(api.control).not.toHaveBeenCalled()
  })

  it('keeps automatic isolation disabled while the effective policy only records and notifies', async () => {
    vi.mocked(api.account).mockResolvedValue({ bindings: [{ ...binding, account_type: 'apikey', auto_control_eligible: true, control_mode: 'record_notify' }], runs: [], actions: [] })
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    await choose(wrapper, '[data-test=quality-control-action]', '设置自动隔离')
    expect(wrapper.get('[data-test=quality-control-auto]').attributes('disabled')).toBeDefined()
    expect(api.control).not.toHaveBeenCalled()
  })

  it('opens an inline manual-review action for the exact requested binding and requires a reason before submitting', async () => {
    const requested: Binding = { ...binding, id: 19, version: 11, policy_id: 8, policy_name: 'Secondary policy' }
    vi.mocked(api.account).mockResolvedValue({ bindings: [binding, requested], runs: [structuredClone(rawRun)], actions: [] })
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12, initialBindingId: 19, initialAction: 'confirm_false_positive' } })
    await flushPromises()
    expect(wrapper.get('[data-test=quality-control-action] .select-value').text()).toContain('确认误报')
    expect(wrapper.get('form .select-value').text()).toContain('Secondary policy')
    expect(api.control).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    expect(api.control).not.toHaveBeenCalled()
    await wrapper.get('[data-test=quality-control-reason]').setValue('Reviewed the original sample; keeping the original verdict for audit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.control).toHaveBeenCalledWith(19, { version: 11, action: 'confirm_false_positive', reason: 'Reviewed the original sample; keeping the original verdict for audit' })
    expect(rawRun.quality_verdict).toBe('fail')
  })

  it('does not silently target another policy when an inline action requests a binding that is no longer available', async () => {
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12, initialBindingId: 999, initialAction: 'isolate' } })
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.get('[role=alert]').text()).toBeTruthy()
    expect(api.control).not.toHaveBeenCalled()
  })

  it('keeps a removed manual selection unavailable after refresh instead of reusing its reason on another binding', async () => {
    const second: Binding = { ...binding, id: 19, version: 11, policy_name: 'Second policy' }
    vi.mocked(api.account).mockResolvedValue({ bindings: [binding, second], runs: [], actions: [] })
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    await wrapper.get('form .select-trigger').trigger('click')
    const secondOption = wrapper.get('form').findAll('[role=option]').find(item => item.text().includes('Second policy'))
    if (!secondOption) throw new Error('missing second policy option')
    await secondOption.trigger('click')
    await wrapper.get('[data-test=quality-control-reason]').setValue('This reason is only for the second policy')
    vi.mocked(api.account).mockResolvedValue({ bindings: [binding], runs: [], actions: [] })
    await wrapper.findAll('button').find(button => button.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.get('[role=alert]').text()).toBeTruthy()
    expect(api.control).not.toHaveBeenCalled()
  })

  it('applies an eligible automatic-isolation toggle directly without asking for a reason', async () => {
    const eligible: Binding = { ...binding, auto_control_eligible: true, account_type: 'apikey', control_mode: 'auto_quarantine' }
    vi.mocked(api.account).mockResolvedValue({ bindings: [eligible], runs: [], actions: [] })
    const wrapper = mount(AccountQualityDetail, { ...mountOptions, props: { accountId: 12 } })
    await flushPromises()
    await wrapper.get('[role=switch]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=quality-control-reason]').exists()).toBe(true)
    expect(api.control).toHaveBeenCalledWith(binding.id, { version: binding.version, action: 'set_auto', auto_isolate: true, reason: '启用账号质量自动隔离' })
  })

  it('displays raw HTML output as text and keeps original automatic evidence visible', async () => {
    const wrapper = mount(AccountQualityRunDetail, { ...mountOptions, props: { runId: rawRun.id } })
    await flushPromises()
    expect(wrapper.text()).toContain('Original question')
    expect(wrapper.text()).toContain('<script>window.evidenceExecuted = true</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('明确不达标')
  })

  it('gives truncated samples an execution-error color and an accessible evidence action', async () => {
    const truncated = { ...rawRun, execution_status: 'truncated' }
    const wrapper = mount(AccountQualityCell, { props: { bindings: [{ ...binding, recent_runs: [truncated] }] } })
    const sample = wrapper.get('[data-test=quality-sample]')
    expect(sample.classes()).toContain('bg-amber-400')
    expect(sample.attributes('aria-label')).toContain('输出截断')
    await sample.trigger('click')
    expect(wrapper.emitted('evidence')?.[0]).toEqual([truncated])
  })

  it('keeps email and QQ off without changing their switches just by opening settings', async () => {
    const wrapper = mount(QualitySettings, { props: { settings: newSettings() } })
    expect((wrapper.get('[data-test=quality-email-enabled]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test=quality-qq-enabled]').element as HTMLInputElement).checked).toBe(false)
    expect(api.saveSettings).not.toHaveBeenCalled()
  })
})
