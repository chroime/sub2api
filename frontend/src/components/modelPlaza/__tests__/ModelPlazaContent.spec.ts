import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaVendorTree from '../PlazaVendorTree.vue'
import PlazaVendorPriceTable from '../PlazaVendorPriceTable.vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

const { auth, monitorList, monitorAnalytics, settings } = vi.hoisted(() => ({
  auth: vi.fn(), monitorList: vi.fn(), monitorAnalytics: vi.fn(),
  settings: { channel_monitor_enabled: true, channel_monitor_mode: 'v1' }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: settings }) }))
vi.mock('@/api/channelMonitor', () => ({ list: monitorList, analytics: monitorAnalytics }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function group(overrides: Partial<ModelPlazaGroup>): ModelPlazaGroup {
  return {
    id: 1,
    name: 'Group',
    description: '',
    platform: 'openai',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    long_context_pricing_enabled: false,
    models: [],
    ...overrides
  }
}

function render(groups: ModelPlazaGroup[], embedded = false) {
  return mount(ModelPlazaContent, {
    props: { response: { description: '', groups }, loading: false, embedded },
    global: { stubs: { PlazaVendorPriceTable: true, ModelIcon: true, Icon: true, Teleport: true } }
  })
}

function model(name: string, platform = 'openai'): PlazaModel {
  return { name, platform, pricing: null, official_pricing: null }
}

async function selectGroup(wrapper: ReturnType<typeof render>, id: number | 'all') {
  await wrapper.get('#plaza-group-select').trigger('click')
  const index = wrapper.getComponent(Select).props('options').findIndex(option => option.value === id)
  expect(index).toBeGreaterThanOrEqual(0)
  await wrapper.findAll('[role="option"]')[index]!.trigger('click')
  expect(wrapper.getComponent(Select).props('modelValue')).toBe(id)
}

function expectModelScope(wrapper: ReturnType<typeof render>, models: PlazaModel[], groupId: number | null) {
  const treeModels = wrapper.getComponent(PlazaVendorTree).props('vendors').flatMap(node => node.models)
  expect(treeModels).toHaveLength(models.length)
  expect(treeModels).toEqual(expect.arrayContaining(models))
  const table = wrapper.findComponent(PlazaVendorPriceTable)
  expect(table.exists()).toBe(models.length > 0)
  if (models.length > 0) {
    expect(table.props('models')).toHaveLength(models.length)
    expect(table.props('models')).toEqual(expect.arrayContaining(models))
    expect(table.props('group')?.id ?? null).toBe(groupId)
  }
}

function expectVendorCounts(wrapper: ReturnType<typeof render>, labels: string[]) {
  expect(wrapper.findAll('[data-vendor-chips] > button').map(chip => chip.text())).toEqual(labels)
  expect(wrapper.get('[role="tree"]').findAll('.tabular-nums').map(count => count.text()))
    .toEqual(labels.map(label => label.split(' ').at(-1)))
}

describe('model plaza group selection', () => {
  beforeEach(() => {
    auth.mockReturnValue({ isAuthenticated: false })
    monitorList.mockReset().mockResolvedValue({ items: [] })
    monitorAnalytics.mockReset().mockResolvedValue({ items: [] })
    settings.channel_monitor_enabled = true
    settings.channel_monitor_mode = 'v1'
  })

  it('loads availability and model checks on opening the same interactive group picker as API keys', async () => {
    auth.mockReturnValue({ isAuthenticated: true })
    const stamp = new Date().toISOString()
    monitorList.mockResolvedValue({ items: [{
      id: 7, group_id: 10, name: 'Monitor', group_name: 'Flagship', provider: 'openai',
      primary_model: 'gpt-6.1-sol', primary_status: 'operational', primary_latency_ms: 1200,
      primary_ping_latency_ms: null, availability_7d: 100, extra_models: [],
      probe_valid_until: new Date(Date.now() + 120000).toISOString(),
      timeline: [{ status: 'operational', checked_at: stamp, latency_ms: 1200, ping_latency_ms: null }]
    }] })
    monitorAnalytics.mockResolvedValue({ items: [{ id: 7, intelligence: { window_end: stamp, series: [
      { model: 'gpt-6.1-sol', effort: 'high', points: [{ status: 'pass', checked_at: stamp }] }
    ] } }] })
    const wrapper = render([group({ id: 10, name: 'Flagship' })], true)
    try {
      expect(monitorList).not.toHaveBeenCalled()
      await wrapper.get('#plaza-group-select').trigger('click')
      await flushPromises()
      expect(monitorList).toHaveBeenCalledTimes(1)
      expect(monitorAnalytics).toHaveBeenCalledTimes(1)
      expect(wrapper.find('[data-group-monitor-header]').exists()).toBe(true)
      expect(wrapper.getComponent(Select).props('dropdownWidth')).toBe(600)
      expect(wrapper.getComponent(Select).props('interactiveOptions')).toBe(true)
      const option = wrapper.getComponent(GroupOptionItem)
      const availability = option.get('[data-monitor-kind=availability]')
      expect(availability.attributes('data-monitor-state')).toBe('ready')
      expect(availability.findAll('[data-monitor-bar]').at(-1)!.attributes('data-status')).toBe('operational')
      expect(option.get('[data-monitor-kind=intelligence]').attributes('data-monitor-state')).toBe('ready')
      expect(option.get('[data-monitor-kind=intelligence]').findAll('[data-monitor-bar]').at(-1)!.attributes('data-status')).toBe('pass')
      await availability.trigger('click')
      expect(wrapper.getComponent(Select).props('modelValue')).toBe('all')
      expect(wrapper.find('[role=listbox]').exists()).toBe(true)
      expect(wrapper.get('[data-group-monitor-detail]').text()).toContain('gpt-6.1-sol')
      await wrapper.findAll('[role=option]')[1]!.trigger('click')
      expect(wrapper.getComponent(Select).props('modelValue')).toBe(10)
    } finally { wrapper.unmount() }
  })

  it.each(['anonymous', 'disabled', 'v2'])('keeps monitor columns neutral and avoids protected requests when %s', async mode => {
    auth.mockReturnValue({ isAuthenticated: mode !== 'anonymous' })
    settings.channel_monitor_enabled = mode !== 'disabled'
    settings.channel_monitor_mode = mode === 'v2' ? 'v2' : 'v1'
    const wrapper = render([group({ id: 10 })])
    try {
      await wrapper.get('#plaza-group-select').trigger('click')
      await flushPromises()
      expect(monitorList).not.toHaveBeenCalled()
      expect(monitorAnalytics).not.toHaveBeenCalled()
      const option = wrapper.getComponent(GroupOptionItem)
      expect(option.findAll('[data-monitor-kind]')).toHaveLength(2)
      expect(option.get('[data-monitor-kind=availability]').attributes('data-monitor-state')).toBe('disabled')
      expect(option.get('[data-monitor-kind=intelligence]').attributes('data-monitor-state')).toBe('disabled')
    } finally { wrapper.unmount() }
  })

  it.each([
    { view: 'anonymous public', authenticated: false, embedded: false },
    { view: 'authenticated public', authenticated: true, embedded: false },
    { view: 'embedded', authenticated: true, embedded: true }
  ])('uses admin order before rates or names in the $view view', async ({ authenticated, embedded }) => {
    auth.mockReturnValue({ isAuthenticated: authenticated })
    const groups = [
      group({ id: 30, name: 'A cheap', sort_order: 10, rate_multiplier: 0.1, user_rate_multiplier: 0.01 }),
      group({ id: 20, name: 'Z first', sort_order: -5, rate_multiplier: 5, user_rate_multiplier: 2 }),
      group({ id: 10, name: 'B same order', sort_order: 10, rate_multiplier: 3, user_rate_multiplier: 4 })
    ]
    const wrapper = render(groups, embedded)
    await wrapper.get('#plaza-group-select').trigger('click')
    expect(wrapper.findAllComponents(GroupOptionItem).map(option => option.props('name'))).toEqual(['Z first', 'B same order', 'A cheap'])
    expect(wrapper.findAllComponents(GroupOptionItem).map(option => option.props('userRateMultiplier'))).toEqual([2, 4, 0.01])
    expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 20, 10, 30])
    expect(groups.map(item => item.id)).toEqual([30, 20, 10])
    wrapper.unmount()
  })

  it('uses zero and a stable ID tie-break for older responses without sort_order', () => {
    const wrapper = render([
      group({ id: 30, name: 'A', rate_multiplier: 0.1 }),
      group({ id: 10, name: 'Z', rate_multiplier: 5 }),
      group({ id: 20, name: 'First', sort_order: -1 })
    ])
    expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 20, 10, 30])
    wrapper.unmount()
  })

  it('shows explicit group identity and descriptions while retaining custom and peak rates', async () => {
    const identity = { label: '自营', color: 'teal' } as const
    const selected = group({ id: 10, name: 'Flagship', description: 'Full description', identity, rate_multiplier: 1, user_rate_multiplier: 0.35, peak_rate_enabled: true, peak_start: '14:00', peak_end: '18:00', peak_rate_multiplier: 2 })
    const wrapper = render([selected, group({ id: 20, name: 'Unlabelled', is_exclusive: true })], true)
    await wrapper.get('#plaza-group-select').trigger('click')
    const option = wrapper.findAllComponents(GroupOptionItem).find(item => item.props('name') === 'Flagship')!
    expect(option).toBeDefined()
    expect(option.get('[data-group-identity]').text()).toBe('自营')
    expect(option.text()).toContain('Full description')
    expect(option.text()).toContain('0.35x')
    expect(option.props()).toMatchObject({ peakRateEnabled: true, peakStart: '14:00', peakEnd: '18:00', peakRateMultiplier: 2 })
    const unlabelled = wrapper.findAllComponents(GroupOptionItem).find(item => item.props('name') === 'Unlabelled')!
    expect(unlabelled.find('[data-group-identity]').exists()).toBe(false)
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')
    const current = wrapper.get('#plaza-group-select').getComponent(GroupBadge)
    expect(current.props('identity')).toEqual(identity)
    expect(current.props('rateMultiplier')).toBe(1)
    expect(current.props('userRateMultiplier')).toBe(0.35)
    expect(current.props('alwaysShowRate')).toBe(true)
    expectModelScope(wrapper, [], selected.id)
    await selectGroup(wrapper, 'all')
    expect(wrapper.get('#plaza-group-select').find('[data-group-identity]').exists()).toBe(false)
    expect(wrapper.get('#plaza-group-select').text()).toBe('modelPlaza.priceTable.allGroups')
    wrapper.unmount()
  })

  it('keeps selection through a reorder and removes groups no longer returned as visible', async () => {
    const selected = group({ id: 20, name: 'Selected', sort_order: -1 })
    const other = group({ id: 10, name: 'Other', sort_order: 10 })
    const wrapper = render([other, selected], true)
    await wrapper.get('#plaza-group-select').trigger('click')
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(20)
    await wrapper.setProps({ response: { description: '', groups: [group({ ...other, sort_order: -2 }), selected] } })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(20)
    expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 10, 20])
    await wrapper.setProps({ response: { description: '', groups: [other] } })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('all')
    expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 10])
    wrapper.unmount()
  })

  it('scopes the table and vendor counts to disjoint groups and restores their union in all', async () => {
    const a = group({ id: 10, name: 'A', models: [model('gpt-a'), model('kimi-a')] })
    const b = group({ id: 20, name: 'B', models: [model('claude-b', 'anthropic'), model('claude-b-2', 'anthropic')] })
    const wrapper = render([a, b])
    expectModelScope(wrapper, [...a.models, ...b.models], null)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 4', 'Anthropic 2', 'Moonshot 1', 'OpenAI 1'])

    await selectGroup(wrapper, a.id)
    expectModelScope(wrapper, a.models, a.id)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 2', 'Moonshot 1', 'OpenAI 1'])

    await selectGroup(wrapper, b.id)
    expectModelScope(wrapper, b.models, b.id)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 2', 'Anthropic 2'])

    await selectGroup(wrapper, 'all')
    expectModelScope(wrapper, [...a.models, ...b.models], null)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 4', 'Anthropic 2', 'Moonshot 1', 'OpenAI 1'])
    wrapper.unmount()
  })

  it('deduplicates across platforms without replacing a selected group model with another group entry', async () => {
    const official: PlazaModel = { ...model('gpt-shared'), pricing_source: 'official' }
    const channel: PlazaModel = { ...model('gpt-shared', 'anthropic'), pricing_source: 'channel' }
    const wrapper = render([
      group({ id: 10, name: 'A', models: [official] }),
      group({ id: 20, name: 'B', models: [channel] })
    ])
    expectModelScope(wrapper, [channel], null)

    await selectGroup(wrapper, 10)
    expectModelScope(wrapper, [official], 10)
    await selectGroup(wrapper, 20)
    expectModelScope(wrapper, [channel], 20)
    await selectGroup(wrapper, 'all')
    expectModelScope(wrapper, [channel], null)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'OpenAI 1'])
    wrapper.unmount()
  })

  it('resets an unavailable vendor when changing groups without retaining its models', async () => {
    const kimi = model('kimi-a')
    const b = group({ id: 20, name: 'B', models: [model('claude-b', 'anthropic')] })
    const wrapper = render([group({ id: 10, name: 'A', models: [model('gpt-a'), kimi] }), b])
    await selectGroup(wrapper, 10)
    await wrapper.findAll('[data-vendor-chips] > button').find(chip => chip.text() === 'Moonshot 1')!.trigger('click')
    expect(wrapper.getComponent(PlazaVendorTree).props('vendor')).toBe('moonshot')
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([kimi])

    await selectGroup(wrapper, b.id)
    expect(wrapper.getComponent(PlazaVendorTree).props('vendor')).toBe('all')
    expectModelScope(wrapper, b.models, b.id)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'Anthropic 1'])
    wrapper.unmount()
  })

  it('keeps search within the selected group and restores only that group when cleared', async () => {
    const kimi = model('kimi-a')
    const b = group({ id: 20, name: 'B', models: [model('claude-b', 'anthropic')] })
    const wrapper = render([group({ id: 10, name: 'A', models: [model('gpt-a'), kimi] }), b])
    await selectGroup(wrapper, 10)
    await wrapper.get('input[type="search"]').setValue('KiMi')
    expectModelScope(wrapper, [kimi], 10)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'Moonshot 1'])

    await selectGroup(wrapper, b.id)
    expect(wrapper.get<HTMLInputElement>('input[type="search"]').element.value).toBe('KiMi')
    expectModelScope(wrapper, [], b.id)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 0'])
    expect(wrapper.get('section .plaza-empty').text()).toBe('modelPlaza.noSearchResult')

    await wrapper.get('input[type="search"]').setValue('')
    expectModelScope(wrapper, b.models, b.id)
    expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'Anthropic 1'])
    wrapper.unmount()
  })

  it('keeps an empty group selectable and shows its empty state without falling back to other groups', async () => {
    const a = group({ id: 10, name: 'A', models: [model('gpt-a')] })
    const b = group({ id: 20, name: 'B', models: [] })
    const wrapper = render([a, b])
    try {
      expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 10, 20])
      await selectGroup(wrapper, a.id)
      expectModelScope(wrapper, a.models, a.id)

      await selectGroup(wrapper, b.id)
      expectModelScope(wrapper, [], b.id)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 0'])
      expect(wrapper.get('section .plaza-empty').text()).toBe('modelPlaza.detail.noModels')
      expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 10, 20])

      await selectGroup(wrapper, a.id)
      expectModelScope(wrapper, a.models, a.id)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'OpenAI 1'])
      expect(wrapper.find('section .plaza-empty').exists()).toBe(false)
      expect(wrapper.getComponent(Select).props('options').map(option => option.value)).toEqual(['all', 10, 20])
    } finally { wrapper.unmount() }
  })

  it('shows all nine catalog vendors in all groups while selections and search use the appropriate model scope', async () => {
    const available = model('gpt-live')
    const catalog = [
      available, model('claude-catalog', 'anthropic'), model('gemini-catalog', 'gemini'),
      model('kimi-catalog'), model('qwen-catalog'), model('deepseek-catalog'),
      model('glm-catalog'), model('minimax-catalog'), model('grok-catalog', 'grok')
    ]
    const a = group({ id: 10, name: 'A', models: [available], catalog_models: [available] })
    const b = group({ id: 20, name: 'B', models: [], catalog_models: catalog.slice(1) })
    const wrapper = render([a, b])
    const catalogCounts = [
      'modelPlaza.vendor.all 9', 'Anthropic 1', 'DeepSeek 1', 'Google 1', 'MiniMax 1',
      'Moonshot 1', 'OpenAI 1', 'Qwen 1', 'xAI 1', 'Zhipu GLM 1'
    ]
    try {
      expectModelScope(wrapper, catalog, null)
      expectVendorCounts(wrapper, catalogCounts)
      await selectGroup(wrapper, a.id)
      expectModelScope(wrapper, [available], a.id)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'OpenAI 1'])

      await selectGroup(wrapper, b.id)
      expectModelScope(wrapper, [], b.id)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 0'])
      expect(wrapper.get('section .plaza-empty').text()).toBe('modelPlaza.detail.noModels')
      await selectGroup(wrapper, 'all')
      expectModelScope(wrapper, catalog, null)
      expectVendorCounts(wrapper, catalogCounts)

      await wrapper.get('input[type="search"]').setValue('KiMi')
      expectModelScope(wrapper, [catalog[3]!], null)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 1', 'Moonshot 1'])
      await selectGroup(wrapper, a.id)
      expectModelScope(wrapper, [], a.id)
      expectVendorCounts(wrapper, ['modelPlaza.vendor.all 0'])
      expect(wrapper.get('section .plaza-empty').text()).toBe('modelPlaza.noSearchResult')
      await selectGroup(wrapper, 'all')
      expectModelScope(wrapper, [catalog[3]!], null)
      await wrapper.get('input[type="search"]').setValue('')
      expectModelScope(wrapper, catalog, null)
      expectVendorCounts(wrapper, catalogCounts)
    } finally { wrapper.unmount() }
  })
})
