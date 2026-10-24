import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EmailTemplateEditor from '../EmailTemplateEditor.vue'

const { getEmailTemplates, getEmailTemplate, previewEmailTemplate, currentLocale } = vi.hoisted(() => ({
  getEmailTemplates: vi.fn(), getEmailTemplate: vi.fn(), previewEmailTemplate: vi.fn(),
  currentLocale: { value: 'zh' }
}))
vi.mock('@/api', () => ({ adminAPI: { settings: { getEmailTemplates, getEmailTemplate, previewEmailTemplate } } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: currentLocale }) }))

const mountEditor = () => mount(EmailTemplateEditor, {
  global: { stubs: { Teleport: true, Transition: true } }
})

describe('EmailTemplateEditor health snapshot', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    currentLocale.value = 'zh'
    getEmailTemplates.mockResolvedValue({ events: [{ value: 'system.health_snapshot', optional: true }], locales: ['zh', 'en'], placeholders: ['health_status', 'group_summary_html'] })
    getEmailTemplate.mockResolvedValue({ event: 'system.health_snapshot', locale: 'zh', subject: 'Health snapshot', html: '<p>{{health_status}}</p>', is_custom: false, placeholders: ['health_status', 'group_summary_html'] })
    previewEmailTemplate.mockResolvedValue({ subject: 'Health snapshot', html: '<p>Operational</p>' })
  })

  it.each([
    ['zh', '分组与倍率更新', '当前用户有权限访问的主要分组', '账号可调度状态快照', '非上游实测'],
    ['en', 'Groups and Rates Update', 'main groups the current user is authorized to access', 'account scheduling snapshot', 'not a live upstream test']
  ])('explains the actual health data and access scope in %s', async (language, label, scope, snapshot, limitation) => {
    currentLocale.value = language
    const wrapper = mountEditor()
    await flushPromises()
    expect(wrapper.get('button#email-template-event').text()).toBe(label)
    expect(wrapper.text()).toContain(scope)
    expect(wrapper.text()).toContain(snapshot)
    expect(wrapper.text()).toContain(limitation)
    expect(wrapper.text()).toContain(language === 'zh' ? '平台、文档、模型广场和画布入口' : 'platform, documentation, model plaza, and canvas links')
    expect(getEmailTemplate).toHaveBeenCalledWith('system.health_snapshot', language)
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('Operational')
    wrapper.unmount()
  })

  it('offers all platform destination placeholders when the API has no placeholder list', async () => {
    getEmailTemplate.mockResolvedValue({ event: 'system.health_snapshot', locale: 'zh', subject: 'Groups and rates', html: '<p>{{platform_url}}</p>', is_custom: false })
    const wrapper = mountEditor()
    await flushPromises()
    const placeholders = wrapper.findAll('button').map(button => button.text())
    for (const name of ['platform_url', 'docs_url', 'model_plaza_url', 'canvas_url']) {
      expect(placeholders).toContain(`{{${name}}}`)
    }
    wrapper.unmount()
  })

  it('uses labeled shared dropdowns and reloads templates through event and language selection', async () => {
    getEmailTemplates.mockResolvedValue({
      events: [{ value: 'system.health_snapshot', optional: true }, { value: 'balance.low', optional: true }],
      locales: ['zh', 'en'], placeholders: []
    })
    const wrapper = mountEditor()
    await flushPromises()
    const eventTrigger = wrapper.get('button#email-template-event')
    const localeTrigger = wrapper.get('button#email-template-locale')
    expect(eventTrigger.attributes('aria-label')).toBe('admin.settings.emailTemplates.event')
    expect(localeTrigger.attributes('aria-label')).toBe('admin.settings.emailTemplates.locale')
    expect(wrapper.findAll('select')).toHaveLength(0)

    await eventTrigger.trigger('click')
    await flushPromises()
    const balanceOption = wrapper.findAll('[role="option"]').find(option => option.text() === '余额不足提醒')!
    expect(balanceOption.exists()).toBe(true)
    await balanceOption.trigger('click')
    await flushPromises()
    expect(getEmailTemplate).toHaveBeenLastCalledWith('balance.low', 'zh')
    expect(eventTrigger.text()).toBe('余额不足提醒')

    await localeTrigger.trigger('click')
    await flushPromises()
    const englishOption = wrapper.findAll('[role="option"]').find(option => option.text() === 'admin.settings.emailTemplates.localeEn')!
    expect(englishOption.exists()).toBe(true)
    await englishOption.trigger('click')
    await flushPromises()
    expect(getEmailTemplate).toHaveBeenLastCalledWith('balance.low', 'en')
    expect(localeTrigger.text()).toBe('admin.settings.emailTemplates.localeEn')
    wrapper.unmount()
  })
})
