import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GlobalSettingsView from './GlobalSettingsView.vue'
import Select from '@/components/common/Select.vue'
import api, { type GlobalSettings } from '@/api/admin/upstream-governance'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { globalSettings: vi.fn(), saveGlobalSettings: vi.fn(), astrbotGroups: vi.fn() } }))

enableAutoUnmount(afterEach)

const settings: GlobalSettings = {
  version: 4,
  notifications: { enabled: true, recipients: ['ops@example.test'], events: { group_online: true, group_offline: false, group_name_change: true, group_description_change: false, group_rate_change: true } },
  local_monitor: { enabled: true, interval_seconds: 30 },
  defaults: { fast_interval_seconds: 15, full_interval_seconds: 600 },
  astrbot: { enabled: true, base_url: 'https://astrbot.example', endpoint_path: '/api/send_msg', api_key: '', api_key_configured: true, clear_api_key: false, events: { group_online: false, group_offline: true, group_name_change: false, group_description_change: true, group_rate_change: false }, targets: [{ umo: 'group:123', label: 'Ops', enabled: true }], timeout_seconds: 8 },
}

const groups = [
  { umo: 'group:123', group_id: '123', group_name: 'Ops', bot_id: 'qq-main' },
  { umo: 'qq-backup:GroupMessage:456', group_id: '456', group_name: 'Release', bot_id: 'qq-backup' },
]

function mountView(locale: 'zh' | 'en' = 'zh') {
  const wrapper = mount(GlobalSettingsView, { global: { stubs: { teleport: true } } })
  wrapper.vm.$i18n.locale = locale
  return wrapper
}

function mockLoadedSettings(value: unknown) {
  vi.mocked(api.globalSettings).mockResolvedValue(value as GlobalSettings)
}

describe('global smart operations settings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockLoadedSettings(structuredClone(settings))
    vi.mocked(api.saveGlobalSettings).mockImplementation(async value => value)
    vi.mocked(api.astrbotGroups).mockResolvedValue({ groups: structuredClone(groups), complete: true })
  })

  it('loads settings without revealing a configured secret', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-test=global-settings]').exists()).toBe(true)
    expect((wrapper.get('[data-test=global-fast-interval]').element as HTMLInputElement).value).toBe('15')
    expect((wrapper.get('input').element as HTMLInputElement).type).toBe('checkbox')
    expect(wrapper.get('[data-test=astrbot-api-key]').attributes('type')).toBe('password')
    expect((wrapper.get('[data-test=astrbot-api-key]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-test=astrbot-api-key]').attributes('placeholder')).toContain('留空保留')
    expect(api.astrbotGroups).toHaveBeenCalledWith({ base_url: 'https://astrbot.example', timeout_seconds: 8 })
    expect(wrapper.find('[data-test=astra-api-key]').exists()).toBe(false)
  })

  it('discards plaintext keys from legacy settings responses instead of resubmitting them', async () => {
    mockLoadedSettings({ ...settings, astrbot: { ...settings.astrbot, api_key: 'legacy-secret', api_key_configured: undefined } })
    const wrapper = mountView()
    await flushPromises()
    expect((wrapper.get('[data-test=astrbot-api-key]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.html()).not.toContain('legacy-secret')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ api_key: '', api_key_configured: true }) }))
    expect(JSON.stringify(vi.mocked(api.astrbotGroups).mock.calls)).not.toContain('legacy-secret')
  })

  it('places each section enable control beside its heading and preserves independent values', async () => {
    const wrapper = mountView()
    await flushPromises()
    const headers = wrapper.findAll('form section > header')
    expect(headers).toHaveLength(3)
    const checkbox = (index: number) => headers[index].get('input[type="checkbox"]')
    expect(headers[0].get('h4').text()).toBe('全局通知邮件')
    expect(headers[1].get('h4').text()).toBe('本地分组变动监听')
    expect(headers[2].get('h4').text()).toBe('AstrBot 通知接口')
    for (const header of headers) {
      expect(header.classes()).toContain('justify-between')
      expect(header.get('label').classes()).toContain('shrink-0')
    }
    await checkbox(0).setValue(false)
    await checkbox(1).setValue(false)
    await checkbox(2).setValue(false)
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({
      notifications: expect.objectContaining({ enabled: false }),
      local_monitor: expect.objectContaining({ enabled: false }),
      astrbot: expect.objectContaining({ enabled: false, targets: settings.astrbot.targets }),
    }))
  })

  it('saves seconds, notification events, and QQ targets while preserving a blank secret input', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-fast-interval]').setValue('10')
    await wrapper.get('[data-test=global-full-interval]').setValue('300')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({ defaults: { fast_interval_seconds: 10, full_interval_seconds: 300 }, astrbot: expect.objectContaining({ api_key: '' }) }))
    expect(wrapper.get('[role=status]').text()).toContain('全局设置已保存')
  })

  it('requires an email recipient even when the independent QQ channel is configured', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('textarea').setValue('')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role=alert]').text()).toContain('请至少填写一个有效收件邮箱。')
  })

  it('allows a QQ-only policy when email notifications are disabled', async () => {
    vi.mocked(api.globalSettings).mockResolvedValue({ ...settings, notifications: { ...settings.notifications, enabled: false, recipients: [] } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalled()
    expect(wrapper.get('[role=status]').text()).toContain('全局设置已保存')
  })

  it.each([
    { locale: 'zh' as const, labels: ['分组上线', '分组下线', '分组名称调整', '分组描述调整', '分组倍率调整'] },
    { locale: 'en' as const, labels: ['Group online', 'Group offline', 'Group name changes', 'Group description changes', 'Group rate changes'] },
  ])('renders exactly five group-event options for each channel in $locale', async ({ locale, labels }) => {
    const wrapper = mountView(locale)
    await flushPromises()
    const [emailEvents, astrbotEvents] = wrapper.findAll('section > fieldset')
    for (const fieldset of [emailEvents, astrbotEvents]) {
      expect(fieldset.findAll('input[type="checkbox"]')).toHaveLength(5)
      expect(fieldset.findAll('label').map(label => label.text())).toEqual(labels)
    }
  })

  it('saves all ten switches independently without changing either channel destination or cadence', async () => {
    const wrapper = mountView()
    await flushPromises()
    const [emailEvents, astrbotEvents] = wrapper.findAll('section > fieldset')
    const emailInputs = emailEvents.findAll('input[type="checkbox"]')
    const astrbotInputs = astrbotEvents.findAll('input[type="checkbox"]')
    expect(emailInputs).toHaveLength(5)
    expect(astrbotInputs).toHaveLength(5)
    expect(emailInputs.map(input => (input.element as HTMLInputElement).checked)).toEqual([true, false, true, false, true])
    expect(astrbotInputs.map(input => (input.element as HTMLInputElement).checked)).toEqual([false, true, false, true, false])
    for (let index = 0; index < 5; index++) {
      await emailInputs[index].setValue(index % 2 === 1)
      await astrbotInputs[index].setValue(index % 2 === 0)
    }
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith({
      ...settings,
      notifications: { ...settings.notifications, events: { group_online: false, group_offline: true, group_name_change: false, group_description_change: true, group_rate_change: false } },
      astrbot: { ...settings.astrbot, events: { group_online: true, group_offline: false, group_name_change: true, group_description_change: false, group_rate_change: true } },
    })
    expect(emailInputs.map(input => (input.element as HTMLInputElement).checked)).toEqual([false, true, false, true, false])
    expect(astrbotInputs.map(input => (input.element as HTMLInputElement).checked)).toEqual([true, false, true, false, true])
  })

  it.each([
    { group: false, rate: false },
    { group: false, rate: true },
    { group: true, rate: false },
    { group: true, rate: true },
  ])('inherits missing mail switches from legacy group=$group and rate=$rate', async ({ group, rate }) => {
    mockLoadedSettings({ ...settings, notifications: { ...settings.notifications, events: { upstream_group_change: group, upstream_rate_change: rate, local_group_change: !group, pricing_change: !group, protection_change: !group, health_change: !group } } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({
      version: 4,
      notifications: expect.objectContaining({ recipients: ['ops@example.test'], events: expect.objectContaining({ group_online: group, group_offline: group, group_name_change: group, group_description_change: group, group_rate_change: rate }) }),
      astrbot: settings.astrbot,
    }))
  })

  it.each([false, true])('inherits every missing AstrBot switch from legacy local_group_change=%s', async local => {
    mockLoadedSettings({ ...settings, astrbot: { ...settings.astrbot, events: { local_group_change: local, upstream_group_change: !local, upstream_rate_change: !local, pricing_change: !local, protection_change: !local, health_change: !local } } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({
      notifications: settings.notifications,
      astrbot: expect.objectContaining({ base_url: 'https://astrbot.example', endpoint_path: '/api/send_msg', api_key: '', targets: [{ umo: 'group:123', label: 'Ops', enabled: true }], timeout_seconds: 8, events: expect.objectContaining({ group_online: local, group_offline: local, group_name_change: local, group_description_change: local, group_rate_change: local }) }),
    }))
  })

  it('uses present new switches over legacy values while inheriting only the missing switches', async () => {
    mockLoadedSettings({
      ...settings,
      notifications: { ...settings.notifications, events: { upstream_group_change: true, upstream_rate_change: false, group_online: false, group_name_change: false, group_rate_change: true } },
      astrbot: { ...settings.astrbot, events: { local_group_change: true, group_offline: false, group_description_change: false } },
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({
      notifications: expect.objectContaining({ events: expect.objectContaining({ group_online: false, group_offline: true, group_name_change: false, group_description_change: true, group_rate_change: true }) }),
      astrbot: expect.objectContaining({ events: expect.objectContaining({ group_online: true, group_offline: false, group_name_change: true, group_description_change: false, group_rate_change: true }) }),
    }))
  })

  it('defaults both five-event sets to enabled when a legacy document has no event policy', async () => {
    mockLoadedSettings({ version: 12 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith({
      version: 12,
      notifications: { enabled: false, recipients: [], events: { group_online: true, group_offline: true, group_name_change: true, group_description_change: true, group_rate_change: true } },
      local_monitor: { enabled: false, interval_seconds: 60 },
      defaults: { fast_interval_seconds: 60, full_interval_seconds: 900 },
      astrbot: { enabled: false, base_url: '', endpoint_path: '/api/v1/im/message', api_key: '', api_key_configured: false, clear_api_key: false, events: { group_online: true, group_offline: true, group_name_change: true, group_description_change: true, group_rate_change: true }, targets: [], timeout_seconds: 10 },
    })
  })

  it('searches current joined groups and fills the selected group name automatically', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-add-target]').trigger('click')
    const select = wrapper.findAllComponents(Select)[1]
    expect(select.props('searchable')).toBe(true)
    await select.get('button').trigger('click')
    await flushPromises()
    await select.get('.select-search-input').setValue('456')
    const option = select.get('[role=option]')
    expect(option.text()).toContain('Release')
    expect(option.text()).toContain('456')
    expect(option.text()).toContain('qq-backup')
    await option.trigger('click')
    const row = wrapper.findAll('[data-test=astrbot-target]')[1]
    expect((row.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Release')
    expect(row.get('[data-test=astrbot-target-label]').attributes()).toHaveProperty('readonly')
    expect(row.text()).toContain('qq-backup:GroupMessage:456')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ targets: [...settings.astrbot.targets, { umo: 'qq-backup:GroupMessage:456', label: 'Release', enabled: true }] }) }))
  })

  it('uses draft credentials to refresh without saving or clearing existing target edits', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-base-url]').setValue('https://draft.example')
    await wrapper.get('[data-test=astrbot-api-key]').setValue('new-key')
    await wrapper.get('[data-test=astrbot-timeout]').setValue('15')
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await flushPromises()
    expect(api.astrbotGroups).toHaveBeenLastCalledWith({ base_url: 'https://draft.example', api_key: 'new-key', timeout_seconds: 15 })
    expect(api.saveGlobalSettings).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-test=astrbot-target]')).toHaveLength(1)
  })

  it('uses the same exact replacement key for discovery and saving', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-api-key]').setValue(' replacement-key ')
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await flushPromises()
    expect(api.astrbotGroups).toHaveBeenLastCalledWith(expect.objectContaining({ api_key: ' replacement-key ' }))
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenLastCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ api_key: ' replacement-key ' }) }))
  })

  it('freezes the draft during saving and ignores a group refresh that began before the save', async () => {
    let finishGroups!: (value: { groups: typeof groups; complete: true }) => void
    let finishSave!: (value: GlobalSettings) => void
    const wrapper = mountView()
    await flushPromises()
    vi.mocked(api.astrbotGroups).mockImplementationOnce(() => new Promise(resolve => { finishGroups = resolve }))
    vi.mocked(api.saveGlobalSettings).mockImplementationOnce(() => new Promise(resolve => { finishSave = resolve }))
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    expect(wrapper.get('[data-test=global-settings-fields]').attributes()).toHaveProperty('disabled')
    expect(wrapper.getComponent(Select).props('disabled')).toBe(true)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', groups[1].umo)
    finishGroups({ groups: [{ ...groups[0], group_name: 'Name from pending refresh' }], complete: true })
    await flushPromises()
    expect((wrapper.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Ops')
    finishSave(structuredClone(settings))
    await flushPromises()
    expect(wrapper.get('[data-test=global-settings-fields]').attributes('disabled')).toBeUndefined()
    expect((wrapper.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Ops')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('group:123')
  })

  it('requires a replacement key after changing the address instead of reusing the saved key', async () => {
    const wrapper = mountView()
    await flushPromises()
    vi.mocked(api.astrbotGroups).mockClear()
    await wrapper.get('[data-test=astrbot-base-url]').setValue('https://different.example')
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await flushPromises()
    expect(api.astrbotGroups).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test=astrbot-groups-error]').text()).toContain('接口地址已更改')
  })

  it('marks explicit key removal for the next save and allows a new key to replace that choice', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-clear-api-key]').trigger('click')
    expect(api.saveGlobalSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('保存后清除')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ api_key: '', clear_api_key: true }) }))
    await wrapper.get('[data-test=astrbot-api-key]').setValue('replacement-secret')
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenLastCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ api_key: 'replacement-secret', clear_api_key: false }) }))
    expect((wrapper.get('[data-test=astrbot-api-key]').element as HTMLInputElement).value).toBe('')
  })

  it('retains the target draft and last complete list when group refresh fails', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-target] input[type=checkbox]').setValue(false)
    vi.mocked(api.astrbotGroups).mockRejectedValueOnce({ reason: 'astrbot_unavailable' })
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=astrbot-groups-error]').text()).toContain('暂时无法连接')
    expect(wrapper.getComponent(Select).props('options')).toEqual(expect.arrayContaining([expect.objectContaining({ value: 'group:123' }), expect.objectContaining({ value: 'qq-backup:GroupMessage:456' })]))
    expect((wrapper.get('[data-test=astrbot-target] input[type=checkbox]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Ops')
  })

  it('keeps unavailable saved targets and their labels after a complete refresh', async () => {
    vi.mocked(api.astrbotGroups).mockResolvedValue({ groups: [groups[1]], complete: true })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-test=astrbot-target-unavailable]').text()).toContain('当前群列表中不可用')
    expect((wrapper.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Ops')
    expect(wrapper.getComponent(Select).props('options')).toEqual(expect.arrayContaining([expect.objectContaining({ value: 'group:123', disabled: true })]))
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    await flushPromises()
    expect(api.saveGlobalSettings).toHaveBeenCalledWith(expect.objectContaining({ astrbot: expect.objectContaining({ targets: settings.astrbot.targets }) }))
  })

  it('disables groups selected in another row and rejects empty or duplicate targets on save', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test=astrbot-add-target]').trigger('click')
    const selects = wrapper.findAllComponents(Select)
    expect(selects[1].props('options')).toEqual(expect.arrayContaining([expect.objectContaining({ value: 'group:123', disabled: true })]))
    await wrapper.get('[data-test=global-settings-save]').trigger('click')
    expect(api.saveGlobalSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role=alert]').text()).toContain('选择一个群')
    selects[1].vm.$emit('update:modelValue', 'group:123')
    await flushPromises()
    expect(selects[1].props('modelValue')).toBe('')

    mockLoadedSettings({ ...settings, astrbot: { ...settings.astrbot, targets: [...settings.astrbot.targets, { ...settings.astrbot.targets[0] }] } })
    const duplicateWrapper = mountView()
    await flushPromises()
    await duplicateWrapper.get('[data-test=global-settings-save]').trigger('click')
    expect(api.saveGlobalSettings).not.toHaveBeenCalled()
    expect(duplicateWrapper.get('[role=alert]').text()).toContain('不能重复')
  })

  it.each(['astrbot-base-url', 'astrbot-api-key', 'astrbot-timeout'])('discards an in-flight list when %s changes', async field => {
    let finish!: (value: { groups: typeof groups; complete: true }) => void
    vi.mocked(api.astrbotGroups).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get(`[data-test=${field}]`).setValue(field === 'astrbot-timeout' ? '20' : field === 'astrbot-base-url' ? 'https://changed.example' : 'changed-secret')
    finish({ groups: [{ ...groups[0], group_name: 'Stale group name' }], complete: true })
    await flushPromises()
    expect((wrapper.get('[data-test=astrbot-target-label]').element as HTMLInputElement).value).toBe('Ops')
    expect(wrapper.html()).not.toContain('Stale group name')
    expect(wrapper.get('[data-test=astrbot-refresh-groups]').attributes('disabled')).toBeUndefined()
  })

  it('treats incomplete discovery as a failure without replacing the complete list', async () => {
    const wrapper = mountView()
    await flushPromises()
    vi.mocked(api.astrbotGroups).mockResolvedValueOnce({ groups: [], complete: false } as unknown as Awaited<ReturnType<typeof api.astrbotGroups>>)
    await wrapper.get('[data-test=astrbot-refresh-groups]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=astrbot-groups-error]').text()).toContain('群列表不完整')
    expect(wrapper.getComponent(Select).props('options')).toHaveLength(2)
    expect(wrapper.find('[data-test=astrbot-target-unavailable]').exists()).toBe(false)
  })

  it.each([
    ['astrbot_auth_failed', '密钥无效'],
    ['astrbot_scope_required', '权限'],
    ['astrbot_groups_unsupported', '群列表'],
    ['astrbot_invalid_response', '格式'],
    ['astrbot_not_configured', '接口地址和密钥'],
  ])('shows an actionable group discovery error for %s', async (reason, message) => {
    vi.mocked(api.astrbotGroups).mockRejectedValueOnce({ reason })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-test=astrbot-groups-error]').text()).toContain(message)
  })
})
