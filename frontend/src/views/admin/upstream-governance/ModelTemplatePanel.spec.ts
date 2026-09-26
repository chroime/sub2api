import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelTemplatePanel from './ModelTemplatePanel.vue'
import api, { type RemoteGroup } from '@/api/admin/upstream-governance'
import { defaultModelSelections } from './import-config'
vi.mock('@/api/admin/upstream-governance', () => ({ default: { modelTemplates: vi.fn(), saveModelTemplates: vi.fn() } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const groups: RemoteGroup[] = [
  { id: '1', name: 'GPT', platform: 'openai', models: ['upstream-gpt'], prices: [], rate_multiplier: 1, resolved_rate_multiplier: 1, user_rate_multiplier: null, peak_rate_enabled: false, source: 'fixture' },
  { id: '2', name: 'Claude', platform: 'anthropic', models: ['upstream-claude'], prices: [], rate_multiplier: 1, resolved_rate_multiplier: 1, user_rate_multiplier: null, peak_rate_enabled: false, source: 'fixture' },
]
function setup() {
  const models = defaultModelSelections()
  const wrapper = mount(ModelTemplatePanel, { props: { modelValue: models, groups, 'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) } } })
  return wrapper
}
describe('server-backed model templates', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 3, templates: [] })
    vi.mocked(api.saveModelTemplates).mockImplementation(async value => ({ ...value, version: value.version + 1 }))
  })
  it('uses each protocol default and otherwise the collected model list without inventing support', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 3, templates: [{ id: 'default', name: 'Saved GPT', platform: 'openai', models: ['saved-gpt'], is_default: true }] })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.props('modelValue')).toEqual({ ...defaultModelSelections(), openai: { enabled: true, models: ['saved-gpt'] }, anthropic: { enabled: true, models: ['upstream-claude'] } })
    await wrapper.get('[data-test=model-platform-anthropic]').trigger('click')
    await wrapper.get('[data-test=clear-models]').trigger('click')
    expect(wrapper.props('modelValue').anthropic).toEqual({ enabled: true, models: [] })
    expect(wrapper.props('modelValue').openai.models).toEqual(['saved-gpt'])
    expect(wrapper.text()).toContain('governance.emptyWhitelist')
    wrapper.unmount()
  })
  it('saves selected models with the current server version and at most one default per protocol', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 7, templates: [{ id: 'old', name: 'Old', platform: 'openai', models: ['saved-gpt'], is_default: true }] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=custom-model]').setValue('custom-gpt')
    await wrapper.get('[data-test=add-model]').trigger('click')
    await wrapper.get('[data-test=template-name]').setValue('New default')
    await wrapper.get('[data-test=save-template]').trigger('click')
    await flushPromises()
    expect(api.saveModelTemplates).toHaveBeenCalledWith({ version: 7, templates: [
      { id: 'old', name: 'Old', platform: 'openai', models: ['saved-gpt'], is_default: false },
      { id: expect.stringMatching(/^tpl_/), name: 'New default', platform: 'openai', models: ['saved-gpt', 'custom-gpt'], is_default: true },
    ] })
    wrapper.unmount()
  })
  it('keeps Grok and DeepSeek templates independent from OpenAI', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 3, templates: [
      { id: 'grok', name: 'Grok default', platform: 'grok', models: ['grok-4'], is_default: true },
      { id: 'deepseek', name: 'DeepSeek default', platform: 'deepseek', models: ['deepseek-chat'], is_default: true },
    ] })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.props('modelValue').grok.models).toEqual(['grok-4'])
    expect(wrapper.props('modelValue').deepseek.models).toEqual(['deepseek-chat'])
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt'])
    await wrapper.get('[data-test=model-platform-deepseek]').trigger('click')
    await wrapper.get('[data-test=clear-models]').trigger('click')
    expect(wrapper.props('modelValue').deepseek).toEqual({ enabled: true, models: [] })
    expect(wrapper.props('modelValue').grok.models).toEqual(['grok-4'])
    wrapper.unmount()
  })
  it('rejects wildcard model names and handles a failed reload after a concurrent save', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=custom-model]').setValue('*')
    await wrapper.get('[data-test=add-model]').trigger('click')
    expect(wrapper.props('modelValue').openai.models).not.toContain('*')
    expect(wrapper.text()).toContain('governance.invalidModelName')
    vi.mocked(api.saveModelTemplates).mockRejectedValue({ status: 409 })
    vi.mocked(api.modelTemplates).mockRejectedValue({ status: 503 })
    await wrapper.get('[data-test=template-name]').setValue('Conflict')
    await wrapper.get('[data-test=save-template]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.templateRefreshFailed')
    expect(wrapper.get('[data-test=save-template]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('updates defaults after protocol resolution while preserving hand-edited selections', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=custom-model]').setValue('hand-picked')
    await wrapper.get('[data-test=add-model]').trigger('click')
    await wrapper.setProps({ groups: [{ ...groups[0]!, models: ['new-snapshot-gpt'] }, { ...groups[1]!, models: ['new-claude'] }] })
    await flushPromises()
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt', 'hand-picked'])
    expect(wrapper.props('modelValue').anthropic.models).toEqual(['new-claude'])
    expect(api.modelTemplates).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('can save a new template on a non-secure HTTP deployment without randomUUID', async () => {
    vi.stubGlobal('crypto', { randomUUID: undefined })
    try {
      const wrapper = setup()
      await flushPromises()
      await wrapper.get('[data-test=template-name]').setValue('HTTP template')
      await wrapper.get('[data-test=save-template]').trigger('click')
      await flushPromises()
      expect(api.saveModelTemplates).toHaveBeenCalledWith(expect.objectContaining({ templates: [expect.objectContaining({ id: expect.stringMatching(/^tpl_[a-zA-Z0-9_-]{1,60}$/) })] }))
      wrapper.unmount()
    } finally { vi.unstubAllGlobals() }
  })
})
