import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelTemplatePanel from './ModelTemplatePanel.vue'
import api, { type RemoteGroup } from '@/api/admin/upstream-governance'
import { defaultModelSelections } from './import-config'
import Select from '@/components/common/Select.vue'
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
  it('preserves explicitly edited protocol drafts when remounted for a refreshed catalog', async () => {
    const models = { ...defaultModelSelections(), openai: { enabled: true, models: [] } }
    const wrapper = mount(ModelTemplatePanel, { props: { modelValue: models, groups, preservePlatforms: ['openai'], 'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) } } })
    await flushPromises()
    expect(wrapper.props('modelValue').openai).toEqual({ enabled: true, models: [] })
    expect(wrapper.props('modelValue').anthropic.models).toEqual(['upstream-claude'])
    expect(wrapper.emitted('edited')).toBeUndefined()
    await wrapper.get('[data-test=custom-model]').setValue('chosen-model')
    await wrapper.get('[data-test=add-model]').trigger('click')
    expect(wrapper.emitted('edited')?.at(-1)).toEqual(['openai'])
    wrapper.unmount()
  })
  it('rejects Unicode control characters in manually added model names', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=custom-model]').setValue(`bad${String.fromCodePoint(133)}model`)
    await wrapper.get('[data-test=add-model]').trigger('click')
    expect(wrapper.text()).toContain('governance.invalidModelName')
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt'])
    wrapper.unmount()
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
  it('hides old OpenAI GPT versions from the text model options while retaining aliases and Codex models', async () => {
    const openaiModels = [
      'gpt-5.2',
      'gpt-5.4-mini',
      'gpt-5.5',
      'gpt-5.6-sol',
      'gpt-6-astra',
      'gpt-5.3-codex-spark',
      'gpt-latest',
    ]
    const wrapper = mount(ModelTemplatePanel, {
      props: {
        modelValue: defaultModelSelections(),
        groups: [{ ...groups[0]!, models: openaiModels }, groups[1]!],
        'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) },
      },
    })
    await flushPromises()

    const labels = wrapper.findAll('label.governance-checkbox-label span.truncate').map(label => label.text().trim())
    expect(labels).not.toContain('gpt-5.2')
    expect(labels).not.toContain('gpt-5.4-mini')
    expect(labels).toEqual(expect.arrayContaining(['gpt-5.5', 'gpt-5.6-sol', 'gpt-6-astra', 'gpt-5.3-codex-spark', 'gpt-latest']))
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
  it('presents templates in the shared dropdown and applies the selected model set', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 3, templates: [
      { id: 'saved', name: 'Saved GPT', platform: 'openai', models: ['saved-gpt'], is_default: false },
    ] })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.findComponent(Select).exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('[data-test=model-template] button').trigger('click')
    const option = [...document.querySelectorAll<HTMLElement>('.select-dropdown-portal [role="option"]')].find(item => item.textContent?.includes('Saved GPT'))
    expect(option).toBeDefined()
    option!.click()
    await flushPromises()
    // Choosing only previews the template; the selection changes on apply.
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt'])
    expect(wrapper.get('[data-test=template-apply-prompt]').text()).toContain('governance.modelPicker.applyPrompt')
    await wrapper.get('[data-test=apply-model-template]').trigger('click')
    expect(wrapper.props('modelValue').openai.models).toEqual(['saved-gpt'])
    expect(wrapper.find('[data-test=template-apply-prompt]').exists()).toBe(false)
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
  it('keeps the selection when a previewed template is cancelled and marks an applied template as modified after edits', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 3, templates: [
      { id: 'base', name: 'Base', platform: 'openai', models: ['upstream-gpt'], is_default: true },
      { id: 'wide', name: 'Wide', platform: 'openai', models: ['upstream-gpt', 'gpt-5.5'], is_default: false },
    ] })
    const wrapper = setup()
    await flushPromises()
    const select = wrapper.getComponent(Select)
    expect(select.props('modelValue')).toBe('base')
    expect(wrapper.find('[data-test=template-modified]').exists()).toBe(false)

    select.vm.$emit('update:modelValue', 'wide')
    await flushPromises()
    expect(wrapper.find('[data-test=template-apply-prompt]').exists()).toBe(true)
    await wrapper.get('[data-test=cancel-model-template]').trigger('click')
    expect(select.props('modelValue')).toBe('base')
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt'])

    await wrapper.get('[data-test=custom-model]').setValue('extra-gpt')
    await wrapper.get('[data-test=add-model]').trigger('click')
    expect(wrapper.get('[data-test=template-modified]').text()).toBe('governance.modelPicker.modified')
    wrapper.unmount()
  })
  it('asks before deleting a template and starts new templates as non-default', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 4, templates: [
      { id: 'keep', name: 'Keep', platform: 'openai', models: ['upstream-gpt'], is_default: false },
    ] })
    const wrapper = setup()
    await flushPromises()
    expect((wrapper.get('[data-test=template-default]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('[data-test=delete-template]').exists()).toBe(false)

    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'keep')
    await flushPromises()
    await wrapper.get('[data-test=delete-template]').trigger('click')
    expect(api.saveModelTemplates).not.toHaveBeenCalled()
    await wrapper.get('[data-test=confirm-delete-template]').trigger('click')
    await flushPromises()
    expect(api.saveModelTemplates).toHaveBeenCalledWith({ version: 4, templates: [] })
    wrapper.unmount()
  })
  it('updates the selected template in place or saves a copy as a new template', async () => {
    vi.mocked(api.modelTemplates).mockResolvedValue({ version: 5, templates: [
      { id: 'first', name: 'First', platform: 'openai', models: ['upstream-gpt'], is_default: false },
      { id: 'second', name: 'Second', platform: 'anthropic', models: ['upstream-claude'], is_default: false },
    ] })
    const wrapper = setup()
    await flushPromises()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'first')
    await flushPromises()
    await wrapper.get('[data-test=custom-model]').setValue('gpt-extra')
    await wrapper.get('[data-test=add-model]').trigger('click')
    await wrapper.get('[data-test=update-template]').trigger('click')
    await flushPromises()
    expect(api.saveModelTemplates).toHaveBeenLastCalledWith({ version: 5, templates: [
      { id: 'first', name: 'First', platform: 'openai', models: ['upstream-gpt', 'gpt-extra'], is_default: false },
      { id: 'second', name: 'Second', platform: 'anthropic', models: ['upstream-claude'], is_default: false },
    ] })

    await wrapper.get('[data-test=template-name]').setValue('Copy')
    await wrapper.get('[data-test=save-template]').trigger('click')
    await flushPromises()
    const saved = vi.mocked(api.saveModelTemplates).mock.lastCall![0]
    expect(saved.templates).toHaveLength(3)
    expect(saved.templates[2]).toEqual({ id: expect.stringMatching(/^tpl_/), name: 'Copy', platform: 'openai', models: ['upstream-gpt', 'gpt-extra'], is_default: false })
    wrapper.unmount()
  })
  it('adds a pasted list of model IDs at once and keeps invalid entries for correction', async () => {
    const wrapper = setup()
    await flushPromises()
    const input = wrapper.get('[data-test=custom-model]')
    const paste = new Event('paste', { cancelable: true }) as ClipboardEvent
    Object.defineProperty(paste, 'clipboardData', { value: { getData: () => 'model-a\nmodel-b\r\nbad*model' } })
    input.element.dispatchEvent(paste)
    await flushPromises()
    expect(paste.defaultPrevented).toBe(true)
    expect((input.element as HTMLInputElement).value).toBe('model-a model-b bad*model')
    await input.setValue('model-a model-b, bad*model')
    await wrapper.get('[data-test=add-model]').trigger('click')
    expect(wrapper.props('modelValue').openai.models).toEqual(['upstream-gpt', 'model-a', 'model-b'])
    expect((input.element as HTMLInputElement).value).toBe('bad*model')
    expect(wrapper.text()).toContain('governance.invalidModelName')
    wrapper.unmount()
  })
  it('selects every search match at once, shows chips and marks upstream-visible models', async () => {
    const wrapper = mount(ModelTemplatePanel, {
      props: {
        modelValue: defaultModelSelections(),
        groups: [{ ...groups[0]!, models: ['gpt-5.5', 'gpt-5.6-sol'] }, groups[1]!],
        'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) },
      },
    })
    await flushPromises()
    await wrapper.get('[data-test=clear-models]').trigger('click')
    expect(wrapper.get('[data-test=model-platform-count-openai]').text()).toBe('0')
    await wrapper.get('[data-test=model-search]').setValue('gpt-5.')
    const matches = wrapper.findAll('label.governance-checkbox-label span.truncate').map(label => label.text())
    expect(matches.length).toBeGreaterThan(1)
    await wrapper.get('[data-test=select-filtered]').trigger('click')
    expect(wrapper.props('modelValue').openai.models).toEqual(matches)
    expect(wrapper.find('[data-test=select-filtered]').exists()).toBe(false)
    expect(wrapper.get('[data-test=selected-models]').text()).toContain('gpt-5.6-sol')
    const upstreamRows = wrapper.findAll('[data-test=upstream-model]').map(badge => badge.element.closest('label')!.textContent)
    expect(upstreamRows).toEqual([expect.stringContaining('gpt-5.5'), expect.stringContaining('gpt-5.6-sol')])
    await wrapper.get('[data-test=model-restriction-enabled]').setValue(false)
    expect(wrapper.get('[data-test=model-platform-count-openai]').text()).toBe('governance.modelPicker.unrestricted')
    wrapper.unmount()
  })
})
