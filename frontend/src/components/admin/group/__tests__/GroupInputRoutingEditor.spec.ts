import { defineComponent, h, ref } from 'vue'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GroupInputRoutingEditor from '../GroupInputRoutingEditor.vue'
import Select from '@/components/common/Select.vue'
import { adminAPI } from '@/api/admin'
import zhInputRouting from '@/i18n/locales/zh/admin/groupInputRouting'
import { cloneGroupInputRouting } from '../groupInputRouting'
import type { AdminGroup, GroupInputRoutingConfig, GroupInputRoutingRule, GroupPlatform } from '@/types'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: { getAll: vi.fn() },
  accounts: { list: vi.fn(), getById: vi.fn() },
} }))

const rule = (overrides: Partial<GroupInputRoutingRule> = {}): GroupInputRoutingRule => ({
  id: 'small', name: 'Small request', enabled: true, request_type: 'all', input_tokens_mode: 'range', min_input_tokens: 0, max_input_tokens: 2000,
  account_ids: [7], group_ids: [], fallback_to_original: false, ...overrides,
})
const targetGroup = (id: number, name: string, platform: GroupPlatform, status = 'active') => ({
  id, name, platform, status,
}) as AdminGroup
const targetPlatforms: GroupPlatform[] = [
  'openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
  'deepseek', 'minimax', 'opencode_go', 'typesafe', 'command_code', 'cline', 'registered-provider',
]

function mountEditor(initial: GroupInputRoutingConfig, platform: GroupPlatform = 'openai') {
  const config = ref(initial)
  const sourcePlatform = ref(platform)
  const wrapper = mount(defineComponent({ setup: () => () => h(GroupInputRoutingEditor, {
    modelValue: config.value,
    platform: sourcePlatform.value,
    groupId: 42,
    'onUpdate:modelValue': (value: GroupInputRoutingConfig) => { config.value = value },
  }) }), { global: { stubs: { Icon: true } } })
  return { wrapper, config, sourcePlatform, editor: wrapper.getComponent(GroupInputRoutingEditor) }
}

describe('GroupInputRoutingEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(adminAPI.groups.getAll).mockResolvedValue([
      targetGroup(42, 'Source', 'openai'), targetGroup(51, 'Target pool', 'openai'),
      targetGroup(52, 'Other provider', 'anthropic'), targetGroup(53, 'Disabled', 'openai', 'inactive'),
      targetGroup(54, 'Composite', 'composite'),
    ])
    vi.mocked(adminAPI.accounts.getById).mockResolvedValue({ id: 7, name: 'Existing account', platform: 'openai', status: 'active' } as never)
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({ items: [] } as never)
  })
  afterEach(() => { document.body.innerHTML = ''; vi.useRealTimers(); vi.resetAllMocks() })

  it('uses the shared Select and labels the editable inclusive token range', async () => {
    const { wrapper, config } = mountEditor({ enabled: true, rules: [rule()] })
    const requestType = wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-request-type')!
    expect(wrapper.find('select').exists()).toBe(false)
    expect(requestType.props('options').map(option => option.value)).toEqual(['all', 'sync', 'stream'])
    expect(requestType.get('button').classes()).toContain('select-trigger')
    const inputMode = wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!
    expect(inputMode.props('options').map(option => option.value)).toEqual(['all', 'range'])
    expect(inputMode.get('button').classes()).toContain('select-trigger')
    expect(inputMode.get('button').attributes('id')).not.toBe(requestType.get('button').attributes('id'))
    expect(inputMode.get('button').attributes('aria-label')).toBe('admin.groups.inputRouting.inputCondition')
    expect(zhInputRouting.range).toBe('预估输入Token长度区间')
    for (const value of ['sync', 'stream', 'all'] as const) {
      await requestType.get('button').trigger('click')
      const option = new DOMWrapper(document.body).findAll('[role="option"]')
        .find(item => item.text() === `admin.groups.inputRouting.${value}`)!
      await option.trigger('click')
      expect(config.value.rules[0]!.request_type).toBe(value)
      expect(requestType.get('button').text()).toBe(`admin.groups.inputRouting.${value}`)
    }
    wrapper.unmount()
  })

  it('keeps new rules and fallback disabled while preserving drafts across master toggles', async () => {
    const { wrapper, config } = mountEditor({ enabled: false, rules: [] })
    await wrapper.get('[data-testid="input-routing-add-rule"]').trigger('click')
    expect(config.value.enabled).toBe(false)
    expect(config.value.rules[0]).toMatchObject({ enabled: false, input_tokens_mode: 'all', min_input_tokens: 0, max_input_tokens: 0, fallback_to_original: false, account_ids: [], group_ids: [] })
    expect(wrapper.find('[data-testid="input-routing-min-tokens"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="input-routing-max-tokens"]').exists()).toBe(false)
    await wrapper.get('[data-testid="input-routing-name"]').setValue('Draft route')
    await wrapper.get('[data-testid="input-routing-toggle"]').trigger('click')
    await wrapper.get('[data-testid="input-routing-toggle"]').trigger('click')
    expect(config.value).toMatchObject({ enabled: false, rules: [{ name: 'Draft route', enabled: false }] })
    wrapper.unmount()
  })

  it('supports adding rules on HTTP origins without crypto.randomUUID', async () => {
    const original = crypto.randomUUID
    Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true })
    const { wrapper, config } = mountEditor({ enabled: false, rules: [] })
    try {
      await wrapper.get('[data-testid="input-routing-add-rule"]').trigger('click')
      await wrapper.get('[data-testid="input-routing-add-rule"]').trigger('click')
      expect(config.value.rules).toHaveLength(2)
      expect(config.value.rules[0]!.id).not.toBe(config.value.rules[1]!.id)
    } finally {
      Object.defineProperty(crypto, 'randomUUID', { value: original, configurable: true })
      wrapper.unmount()
    }
  })

  it('edits request type, threshold and fallback and persists priority order after moves and removals', async () => {
    const { wrapper, config } = mountEditor({ enabled: true, rules: [rule(), rule({ id: 'second', name: 'Second' })] })
    const first = wrapper.findAll('[data-testid="input-routing-rule"]')[0]!
    first.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-request-type')!.vm.$emit('update:modelValue', 'stream')
    await wrapper.vm.$nextTick()
    await first.get('[data-testid="input-routing-min-tokens"]').setValue('100')
    await first.get('[data-testid="input-routing-max-tokens"]').setValue('4096')
    await first.get('[data-testid="input-routing-fallback"]').trigger('click')
    await first.get('[data-testid="input-routing-move-down"]').trigger('click')
    expect(config.value.rules.map(value => value.id)).toEqual(['second', 'small'])
    expect(config.value.rules[1]).toMatchObject({ request_type: 'stream', min_input_tokens: 100, max_input_tokens: 4096, fallback_to_original: true })
    await wrapper.findAll('[data-testid="input-routing-move-up"]')[1]!.trigger('click')
    expect(config.value.rules.map(value => value.id)).toEqual(['small', 'second'])
    await wrapper.findAll('[data-testid="input-routing-remove-rule"]')[0]!.trigger('click')
    expect(config.value.rules.map(value => value.id)).toEqual(['second'])
    wrapper.unmount()
  })

  it('searches accounts across platforms and group memberships while preserving active union targets', async () => {
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({ items: [
      { id: 81, name: 'Cross-group account', platform: 'openai', status: 'active', group_ids: [999] },
      { id: 82, name: 'Anthropic account', platform: 'anthropic', status: 'active' },
      { id: 83, name: 'Disabled account', platform: 'openai', status: 'inactive' },
    ] } as never)
    const { wrapper, config } = mountEditor({ enabled: true, rules: [rule({ account_ids: [] })] })
    await flushPromises()
    const groupOptions = wrapper.get('[data-testid="input-routing-group-options"]')
    expect(groupOptions.findAll('input').map(input => input.attributes('data-group-id'))).toEqual(['51', '52', '54'])
    await groupOptions.get('[data-group-id="51"]').setValue(true)
    await wrapper.get('[data-testid="input-routing-account-search"]').setValue('Cross')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(adminAPI.accounts.list).toHaveBeenCalledWith(1, 30, { search: 'Cross', status: 'active' }, { signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-testid="input-routing-account-results"]').text()).toContain('Anthropic account')
    expect(wrapper.get('[data-testid="input-routing-account-results"]').text()).not.toContain('Disabled account')
    await wrapper.get('[data-account-id="81"]').trigger('click')
    expect(config.value.rules[0]).toMatchObject({ account_ids: [81], group_ids: [51] })
    expect(wrapper.get('[data-testid="input-routing-account-tags"]').text()).toContain('Cross-group account')
    await wrapper.get('[data-testid="input-routing-account-tags"] button').trigger('click')
    expect(config.value.rules[0]).toMatchObject({ account_ids: [], group_ids: [51] })
    wrapper.unmount()
  })

  it.each(['openai', 'anthropic', 'composite', 'registered-source'] as GroupPlatform[])('allows targets from every platform for a %s source without sending a platform filter', async source => {
    const groups = [...targetPlatforms, 'composite'].map((platform, index) => targetGroup(100 + index, `${platform} group`, platform))
    const accounts = targetPlatforms.map((platform, index) => ({ id: 200 + index, name: `${platform} account`, platform, status: 'active' }))
    vi.mocked(adminAPI.groups.getAll).mockResolvedValue([targetGroup(42, 'Source', source), ...groups])
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({ items: accounts } as never)
    const { wrapper, config, editor } = mountEditor({ enabled: true, rules: [rule({ account_ids: [] })] }, source)
    await flushPromises()
    expect(adminAPI.groups.getAll).toHaveBeenCalledWith()
    expect(wrapper.find('[data-group-id="42"]').exists()).toBe(false)
    for (const group of groups) await wrapper.get(`[data-group-id="${group.id}"]`).setValue(true)
    await wrapper.get('[data-testid="input-routing-account-search"]').trigger('focus')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(adminAPI.accounts.list).toHaveBeenCalledWith(1, 30, { search: '', status: 'active' }, { signal: expect.any(AbortSignal) })
    for (const account of accounts) await wrapper.get(`[data-account-id="${account.id}"]`).trigger('click')
    expect(config.value.rules[0]!.group_ids).toEqual(groups.map(group => group.id))
    expect(config.value.rules[0]!.account_ids).toEqual(accounts.map(account => account.id))
    expect(editor.vm.validate()).toBe(true)
    wrapper.unmount()
  })

  it('filters and searches composite target choices without including the source or inactive groups', async () => {
    const { wrapper } = mountEditor({ enabled: true, rules: [rule()] }, 'composite')
    await flushPromises()
    expect(wrapper.get('[data-testid="input-routing-group-options"]').findAll('input').map(input => input.attributes('data-group-id'))).toEqual(['51', '52', '54'])
    await wrapper.get('[data-testid="input-routing-group-search"]').setValue('Other')
    expect(wrapper.get('[data-testid="input-routing-group-options"]').findAll('input').map(input => input.attributes('data-group-id'))).toEqual(['52'])
    wrapper.unmount()
  })

  it.each([
    { min: -1, max: 100 },
    { min: 101, max: 100 },
    { min: 0, max: 1.5 },
    { min: 0, max: 10_000_001 },
  ])('blocks invalid token range $min-$max even on a disabled rule', async ({ min, max }) => {
    const { wrapper, editor } = mountEditor({ enabled: false, rules: [rule({ min_input_tokens: min, max_input_tokens: max, enabled: false })] })
    expect(editor.vm.validate()).toBe(false)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="input-routing-validation-error"]').text()).toContain('rangeError')
    wrapper.unmount()
  })

  it('switches input conditions with the shared dropdown and preserves the range draft', async () => {
    const { wrapper, config } = mountEditor({ enabled: true, rules: [rule({ min_input_tokens: 200, max_input_tokens: 800 })] })
    const mode = wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!
    for (const value of ['all', 'range'] as const) {
      await mode.get('button').trigger('click')
      const key = value === 'all' ? 'inputAll' : 'tokenRange'
      const option = new DOMWrapper(document.body).findAll('[role="option"]')
        .find(item => item.text() === `admin.groups.inputRouting.${key}`)!
      await option.trigger('click')
      expect(config.value.rules[0]).toMatchObject({ input_tokens_mode: value, min_input_tokens: 200, max_input_tokens: 800 })
      expect(wrapper.find('[data-testid="input-routing-min-tokens"]').exists()).toBe(value === 'range')
      expect(wrapper.find('[data-testid="input-routing-max-tokens"]').exists()).toBe(value === 'range')
    }
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-min-tokens"]').element.value).toBe('200')
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-max-tokens"]').element.value).toBe('800')
    wrapper.unmount()
  })

  it('ignores a hidden invalid range in all mode but still requires a target', async () => {
    const { wrapper, editor, config } = mountEditor({ enabled: true, rules: [rule({ input_tokens_mode: 'all', min_input_tokens: -1, max_input_tokens: 1.5, account_ids: [] })] })
    await flushPromises()
    expect(editor.vm.validate()).toBe(false)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="input-routing-validation-error"]').text()).toContain('targetsError')
    await wrapper.get('[data-group-id="51"]').setValue(true)
    expect(editor.vm.validate()).toBe(true)
    wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!.vm.$emit('update:modelValue', 'range')
    await wrapper.vm.$nextTick()
    expect(editor.vm.validate()).toBe(false)
    expect(wrapper.get('[data-testid="input-routing-validation-error"]').text()).toContain('rangeError')
    expect(config.value.rules[0]).toMatchObject({ min_input_tokens: -1, max_input_tokens: 1.5, group_ids: [51] })
    wrapper.unmount()
  })

  it('defaults saved rules without an input mode to range when displayed and cloned', async () => {
    const legacyRule = rule()
    Reflect.deleteProperty(legacyRule, 'input_tokens_mode')
    const saved = { enabled: true, rules: [legacyRule] }
    const { wrapper } = mountEditor(saved)
    const mode = wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!
    expect(mode.props('modelValue')).toBe('range')
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-max-tokens"]').element.value).toBe('2000')
    const clone = cloneGroupInputRouting(saved)
    expect(clone.rules[0]!.input_tokens_mode).toBe('range')
    clone.rules[0]!.account_ids.push(99)
    expect(saved.rules[0]!.account_ids).toEqual([7])
    expect(saved.rules[0]).not.toHaveProperty('input_tokens_mode')
    wrapper.unmount()
  })

  it('keeps request and input condition labels unique across rules and editors', () => {
    const wrapper = mount(defineComponent({ setup: () => () => h('div', [
      h(GroupInputRoutingEditor, { modelValue: { enabled: true, rules: [rule(), rule({ id: 'second' })] }, platform: 'openai' }),
      h(GroupInputRoutingEditor, { modelValue: { enabled: true, rules: [rule()] }, platform: 'openai' }),
    ]) }), { global: { stubs: { Icon: true } } })
    const ids = wrapper.findAllComponents(Select).map(select => select.get('button').attributes('id'))
    expect(ids).toHaveLength(6)
    expect(new Set(ids).size).toBe(ids.length)
    for (const select of wrapper.findAllComponents(Select)) {
      const id = select.get('button').attributes('id')
      expect(wrapper.find(`label[for="${id}"]`).exists()).toBe(true)
    }
    wrapper.unmount()
  })

  it('requires a target and keeps a group-only choice valid after a source platform change', async () => {
    const { wrapper, editor, sourcePlatform } = mountEditor({ enabled: false, rules: [rule({ account_ids: [] })] })
    await flushPromises()
    expect(editor.vm.validate()).toBe(false)
    await wrapper.get('[data-group-id="51"]').setValue(true)
    expect(editor.vm.validate()).toBe(true)
    sourcePlatform.value = 'anthropic'
    await wrapper.vm.$nextTick()
    expect(editor.vm.validate()).toBe(true)
    expect(wrapper.get('[data-testid="input-routing-group-tags"]').text()).toContain('Target pool')
    wrapper.unmount()
  })

  it('keeps a platform-independent account search available when the source platform changes', async () => {
    let resolveSearch!: (result: never) => void
    vi.mocked(adminAPI.accounts.list).mockImplementationOnce(() => new Promise(resolve => { resolveSearch = resolve }))
    const { wrapper, sourcePlatform } = mountEditor({ enabled: true, rules: [rule()] })
    await wrapper.get('[data-testid="input-routing-account-search"]').setValue('old')
    await vi.advanceTimersByTimeAsync(300)
    const signal = vi.mocked(adminAPI.accounts.list).mock.calls[0]![3]!.signal!
    sourcePlatform.value = 'anthropic'
    await wrapper.vm.$nextTick()
    expect(signal.aborted).toBe(false)
    resolveSearch({ items: [{ id: 81, name: 'Stale result', platform: 'openai', status: 'active' }] } as never)
    await flushPromises()
    expect(wrapper.get('[data-testid="input-routing-account-results"]').text()).toContain('Stale result')
    wrapper.unmount()
  })

  it('preserves selected accounts and groups and allows saving after a source platform change', async () => {
    const initial = { enabled: true, rules: [rule({ group_ids: [52, 54] })] }
    const { wrapper, config, sourcePlatform, editor } = mountEditor(initial)
    await flushPromises()
    expect(editor.vm.validate()).toBe(true)
    sourcePlatform.value = 'anthropic'
    await wrapper.vm.$nextTick()
    expect(editor.vm.validate()).toBe(true)
    expect(wrapper.get('[data-testid="input-routing-account-tags"]').text()).toContain('Existing account')
    expect(config.value).toEqual(initial)
    expect(wrapper.get('[data-testid="input-routing-group-tags"]').text()).toContain('Other provider')
    expect(wrapper.get('[data-testid="input-routing-group-tags"]').text()).toContain('Composite')
    wrapper.unmount()
  })

  it('still rejects the source group as a saved target without dropping it from the draft', async () => {
    const { wrapper, config, editor } = mountEditor({ enabled: true, rules: [rule({ group_ids: [42] })] })
    await flushPromises()
    expect(editor.vm.validate()).toBe(false)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="input-routing-validation-error"]').text()).toContain('selfTargetError')
    expect(config.value.rules[0]!.group_ids).toEqual([42])
    wrapper.unmount()
  })
})
