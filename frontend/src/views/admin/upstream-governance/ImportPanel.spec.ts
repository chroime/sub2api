import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import ImportPanel from './ImportPanel.vue'
import api from '@/api/admin/upstream-governance'
import SiteOverview from './SiteOverview.vue'
import { defaultImportConfig } from './import-config'
import { transportPlatforms } from './providers'
vi.mock('./TransportSelect.vue', () => ({ default: {
  props: ['modelValue', 'disabled'], emits: ['update:modelValue'],
  template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)"><option value=""></option><option v-for="p in ['openai','anthropic','gemini','antigravity','grok','kimi','zhipu','deepseek','minimax','opencode_go']" :value="p">{{ p }}</option></select>`,
} }))
vi.mock('./TargetGroupSelect.vue', () => ({ default: {
  props: ['modelValue', 'disabled', 'groups'], emits: ['update:modelValue'],
  template: `<select multiple :disabled="disabled" @change="$emit('update:modelValue', Array.from($event.target.selectedOptions, option => Number(option.value)))"><option v-for="group in groups" :value="group.id" :selected="modelValue.includes(group.id)">{{ group.name }}</option></select>`,
} }))
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { preview: vi.fn(), apply: vi.fn(), keys: vi.fn().mockResolvedValue([]), createKeys: vi.fn(), revealKey: vi.fn(), modelTemplates: vi.fn().mockResolvedValue({ version: 0, templates: [] }) },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const props = {
  siteId: 1,
  siteBaseUrl: 'https://fixture.example',
  groups: [{ id: 3, name: 'Local', platform: 'openai', rate_multiplier: 2 }],
  snapshot: {
    id: 1,
    site_id: 1,
    site_version: 1,
    created_at: 'now',
    catalog: {
      groups: [
        {
          id: 'r',
          name: 'Remote',
          platform: 'openai',
          rate_multiplier: 1,
          user_rate_multiplier: null,
          resolved_rate_multiplier: 1,
          models: ['fixture-model'],
          prices: [],
          source: 'user',
          peak_rate_enabled: false,
        },
      ],
      channels: [],
      warnings: [],
    },
  },
}
describe('import confirmation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.preview).mockResolvedValue({ id: 'p', site_id: 1, site_version: 1, snapshot_id: 1, created_at: '2026-09-26T15:08:02Z', expires_at: '2099-01-01', rows: [] })
    vi.mocked(api.apply).mockResolvedValue({ preview_id: 'p', items: [] })
  })
  it('submits one selection for multiple destinations with priority 1 and renders every frozen target rate', async () => {
    const targets = [
      { id: 3, name: 'Frozen OpenAI destination', platform: 'openai', sale_multiplier: 2.25 },
      { id: 9, name: 'Frozen composite destination', platform: 'composite', sale_multiplier: 0 },
    ]
    vi.mocked(api.preview).mockResolvedValue({
      id: 'multiple-targets', site_id: 1, site_version: 1, snapshot_id: 1, created_at: '2026-09-26T15:08:02Z', expires_at: '2099-01-01',
      rows: [{
        selection: { remote_group_id: 'r', platform: 'openai', local_group_ids: [3, 9], account_name: 'https://fixture.example--1', cost_multiplier: 1, account_config: { ...defaultImportConfig(), priority: 1 } },
        remote_group: props.snapshot.catalog.groups[0]!, target: targets[0]!, targets, existing: null, will_create_key: true, marker: 'one-account',
      }],
    })
    const wrapper = mount(ImportPanel, { props: { ...props, groups: [
      ...props.groups,
      { id: 9, name: 'Current composite destination', platform: 'composite', rate_multiplier: 4 },
    ] } })
    await flushPromises()
    expect((wrapper.get('[data-test=priority]').element as HTMLInputElement).value).toBe('1')
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3', '9'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.preview).toHaveBeenCalledTimes(1)
    const selections = vi.mocked(api.preview).mock.calls[0]![1].selections
    expect(selections).toHaveLength(1)
    expect(selections[0]).toMatchObject({ remote_group_id: 'r', local_group_ids: [3, 9], account_config: { priority: 1 } })
    expect(selections[0]).not.toHaveProperty('local_group_id')
    const frozenTargets = wrapper.get('[data-test=preview-targets]').findAll('span.inline-flex')
    expect(frozenTargets.map(target => ({ name: target.get('.break-all').text(), rate: target.get('span.shrink-0').text() }))).toEqual([
      { name: 'Frozen OpenAI destination', rate: 'governance.saleRate 2.25×' },
      { name: 'Frozen composite destination', rate: 'governance.saleRate 0×' },
    ])
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.get('article').text()).toContain('governance.accountPriority 1')
    expect(api.createKeys).not.toHaveBeenCalled()
    expect(api.apply).not.toHaveBeenCalled()
  })
  it.each([0, 7])('preserves explicitly configured account priority %i in the preview request', async priority => {
    const wrapper = mount(ImportPanel, { props })
    await flushPromises()
    const input = wrapper.get('[data-test=priority]')
    expect(input.attributes()).toMatchObject({ min: '0', step: '1' })
    await input.setValue(priority)
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [expect.objectContaining({ account_config: expect.objectContaining({ priority }) })] })
  })
  it('clears all destinations when changing a row protocol and requires a new compatible mapping', async () => {
    const wrapper = mount(ImportPanel, { props: { ...props,
      snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [{ ...props.snapshot.catalog.groups[0]!, platform: 'unknown' }] } },
      groups: [
        ...props.groups,
        { id: 4, name: 'Anthropic destination', platform: 'anthropic', rate_multiplier: 1.5 },
        { id: 9, name: 'Shared destination', platform: 'composite', rate_multiplier: 1 },
      ],
    } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await flushPromises()
    await wrapper.get('[data-test=target]').setValue(['3', '9'])
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test=platform]').setValue('anthropic')
    await flushPromises()
    expect((wrapper.get('[data-test=target]').element as HTMLSelectElement).selectedOptions).toHaveLength(0)
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
    await wrapper.get('[data-test=target]').setValue(['4', '9'])
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [expect.objectContaining({ platform: 'anthropic', local_group_ids: [4, 9] })] })
  })
  it('assigns compatible subsets of mixed bulk targets without dropping selected upstream rows', async () => {
    const remote = props.snapshot.catalog.groups[0]!
    const wrapper = mount(ImportPanel, { props: { ...props,
      groups: [
        ...props.groups,
        { id: 4, name: 'Anthropic destination', platform: 'anthropic', rate_multiplier: 1.5 },
        { id: 9, name: 'Shared destination', platform: 'composite', rate_multiplier: 1 },
      ],
      snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [remote, { ...remote, id: 'a', name: 'Anthropic upstream', platform: 'anthropic' }] } },
    } })
    await flushPromises()
    await wrapper.get('[data-test=select-all]').setValue(true)
    await wrapper.get('[data-test=bulk-target]').setValue(['3', '4', '9'])
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.findAll('[data-test=select]').every(input => (input.element as HTMLInputElement).checked)).toBe(true)
    expect(wrapper.findAll('[data-test=target]').map(input => Array.from((input.element as HTMLSelectElement).selectedOptions, option => Number(option.value)))).toEqual([[3, 9], [4, 9]])
    expect(wrapper.text()).toContain('governance.bulkSkippedTargets')
    expect(wrapper.text()).not.toContain('governance.bulkUnresolved')
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledTimes(1)
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [
      expect.objectContaining({ remote_group_id: 'r', platform: 'openai', local_group_ids: [3, 9] }),
      expect.objectContaining({ remote_group_id: 'a', platform: 'anthropic', local_group_ids: [4, 9] }),
    ] })
  })
  it.each(['removed', 'incompatible'] as const)('blocks preview when one of several chosen groups becomes %s', async change => {
    const shared = { id: 9, name: 'Shared destination', platform: 'composite', rate_multiplier: 1 }
    const wrapper = mount(ImportPanel, { props: { ...props, groups: [...props.groups, shared] } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3', '9'])
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ groups: change === 'removed' ? props.groups : [...props.groups, { ...shared, platform: 'anthropic' }] })
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.mappingIncomplete')
    expect((wrapper.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
  })
  it('blocks more than 100 destinations and accepts 100 as one selection', async () => {
    const groups = Array.from({ length: 101 }, (_, index) => ({ id: index + 1, name: `Destination ${index + 1}`, platform: 'openai', rate_multiplier: 1 }))
    const wrapper = mount(ImportPanel, { props: { ...props, groups } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(groups.map(group => String(group.id)))
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
    await wrapper.get('[data-test=target]').setValue(groups.slice(0, 100).map(group => String(group.id)))
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledTimes(1)
    const selections = vi.mocked(api.preview).mock.calls[0]![1].selections
    expect(selections).toHaveLength(1)
    expect(selections[0]!.local_group_ids).toEqual(groups.slice(0, 100).map(group => group.id))
  })
  it('reloads model defaults and permits preview after a new snapshot of the same site', async () => {
    vi.mocked(api.preview).mockResolvedValue({ id: 'next-snapshot', site_id: 1, site_version: 1, snapshot_id: 2, created_at: '2026-09-26T15:08:02Z', expires_at: '2099-01-01', rows: [] })
    const wrapper = mount(ImportPanel, { props })
    await flushPromises()
    await wrapper.setProps({ snapshot: { ...props.snapshot, id: 2, catalog: { ...props.snapshot.catalog, groups: [{ ...props.snapshot.catalog.groups[0]!, models: ['new-snapshot-model'] }] } } })
    await flushPromises()
    expect(api.modelTemplates).toHaveBeenCalledTimes(2)
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [expect.objectContaining({ account_config: expect.objectContaining({ model_mapping: { 'new-snapshot-model': 'new-snapshot-model' } }) })] })
    wrapper.unmount()
  })
  it('uses real NewAPI models when the administrator resolves an unknown upstream protocol', async () => {
    const remote = { ...props.snapshot.catalog.groups[0]!, platform: 'unknown', models: ['newapi-private-model'] }
    vi.mocked(api.preview).mockResolvedValue({ id: 'newapi', site_id: 1, site_version: 1, snapshot_id: 1, created_at: '2026-09-26T15:08:02Z', expires_at: '2099-01-01', rows: [] })
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [remote] } } } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [{ remote_group_id: 'r', platform: 'openai', local_group_ids: [3], account_name: 'https://fixture.example--1', cost_multiplier: 1, account_config: { ...defaultImportConfig(), model_mapping: { 'newapi-private-model': 'newapi-private-model' } } }] })
    wrapper.unmount()
  })
  it('prevents an empty enabled whitelist from silently turning into unrestricted models', async () => {
    const wrapper = mount(ImportPanel, { props })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('[data-test=clear-models]').trigger('click')
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.emptyWhitelist')
    wrapper.unmount()
  })
  it('translates known partial-collection warnings instead of showing internal warning codes', () => {
    const warnings = ['sub2api_channels_pricing_unavailable', 'newapi_channels_not_exposed', 'newapi_pricing_unavailable', 'sub2api_complex_pricing_not_flattened', 'newapi_complex_pricing_not_flattened', 'sub2api_model_plaza_unavailable', 'sub2api_channels_unavailable']
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, warnings } } } })
    for (const warning of warnings) expect(wrapper.text()).not.toContain(warning)
    for (const key of ['channelsPricingUnavailable', 'channelsNotExposed', 'pricingUnavailable', 'complexPricingNotFlattened', 'plazaUnavailable', 'channelsUnavailable']) expect(wrapper.text()).toContain('governance.' + key)
  })
  it('keeps upstream prices in the frozen preview with token units converted to million tokens', async () => {
    const remote = { ...props.snapshot.catalog.groups[0]!, prices: [{ model: 'priced-model', platform: 'openai', unit: 'usd_per_token', input: 0.000002, output: 0, per_request: null, details: { time_pricing: [{ multiplier: 1.5 }] } }] }
    vi.mocked(api.preview).mockResolvedValue({ id: 'priced', site_id: 1, site_version: 1, snapshot_id: 1, created_at: 'now', expires_at: '2099-01-01', rows: [{ selection: { remote_group_id: 'r', platform: 'openai', local_group_id: 3, account_name: 'Remote', cost_multiplier: 1 }, remote_group: remote, target: { id: 3, name: 'Local', platform: 'openai', sale_multiplier: 2 }, existing: null, will_create_key: true, marker: 'priced' }] })
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [remote] } } } })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const prices = wrapper.get('[data-test=preview-prices]')
    expect(prices.text()).toContain('priced-model')
    expect(prices.text()).toContain('governance.usdPerMillionTokens')
    expect(prices.get('[data-test=price-input]').text()).toBe('2')
    expect(prices.get('[data-test=price-output]').text()).toBe('0')
    expect(prices.text()).toContain('time_pricing')
    expect(wrapper.get('[data-test=preview-targets]').text()).toContain('Local')
    expect(wrapper.get('[data-test=preview-targets]').text()).toContain('governance.saleRate 2×')
    expect(api.apply).not.toHaveBeenCalled()
  })
  it('does not silently drop incompatible selected groups from a bulk mapping or preview request', async () => {
    const remote = props.snapshot.catalog.groups[0]!
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [remote, { ...remote, id: 'a', platform: 'anthropic' }] } } } })
    await wrapper.get('[data-test=select-all]').setValue(true)
    await wrapper.get('[data-test=bulk-target]').setValue(['3'])
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.findAll('[data-test=select]').every(input => (input.element as HTMLInputElement).checked)).toBe(true)
    expect(wrapper.text()).toContain('governance.bulkSkippedTargets')
    expect(wrapper.text()).toContain('governance.bulkUnresolved')
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.mappingIncomplete')
  })
  it('selects every visible group, infers known transports, and leaves unknown choices explicit', async () => {
    const remote = props.snapshot.catalog.groups[0]!
    const wrapper = mount(ImportPanel, { props: {
      ...props,
      groups: [{ id: 9, name: 'Mixed destination', platform: 'composite', rate_multiplier: 2 }],
      snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [
        remote,
        { ...remote, id: 'grok', name: 'Grok', platform: 'grok' },
        { ...remote, id: 'mixed', name: 'Mixed', platform: 'composite' },
      ] } },
    } })
    await wrapper.get('[data-test=select-all]').setValue(true)
    expect(wrapper.findAll('[data-test=select]').every(input => (input.element as HTMLInputElement).checked)).toBe(true)
    expect(wrapper.findAll('[data-test=platform]').map(input => (input.element as HTMLSelectElement).value)).toEqual(['openai', 'grok', ''])
    await wrapper.get('[data-test=bulk-target]').setValue(['9'])
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.findAll('[data-test=target]').map(input => Array.from((input.element as HTMLSelectElement).selectedOptions, option => Number(option.value)))).toEqual([[9], [9], []])
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('[data-test=platform]')[2]!.setValue('anthropic')
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [
      { remote_group_id: 'r', platform: 'openai', local_group_ids: [9], account_name: 'https://fixture.example--1', cost_multiplier: 1, account_config: { ...defaultImportConfig(), model_mapping: { 'fixture-model': 'fixture-model' } } },
      { remote_group_id: 'grok', platform: 'grok', local_group_ids: [9], account_name: 'https://fixture.example--1', cost_multiplier: 1, account_config: { ...defaultImportConfig(), model_mapping: { 'fixture-model': 'fixture-model' }, openai_long_context_billing_enabled: false } },
      { remote_group_id: 'mixed', platform: 'anthropic', local_group_ids: [9], account_name: 'https://fixture.example--1', cost_multiplier: 1, account_config: { ...defaultImportConfig(), model_mapping: { 'fixture-model': 'fixture-model' }, openai_long_context_billing_enabled: false } },
    ] })
  })
  it('preserves zero account balances and unknown amounts in original units', () => {
    const wrapper = mount(SiteOverview, { props: { site: { id: 1, name: 'Fixture', base_url: props.siteBaseUrl, platform: 'sub2api', proxy_id: null, enabled: true, interval_minutes: 15, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }, bindingCount: 0, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog,
      account: { user_id: 4, username: 'upstream-user', email: '', balance: 0, frozen_balance: null, used_balance: 2500, unit: 'quota', source: 'user/self' },
    } } } })
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('0 quota')
    expect(wrapper.get('[data-test=account-frozen]').text()).toContain('governance.unknown')
    expect(wrapper.get('[data-test=account-used]').text()).toBe('2500 quota')
  })
  it('maps every native platform to its own group and includes its collected model restrictions in preview', async () => {
    const remote = props.snapshot.catalog.groups[0]!
    const wrapper = mount(ImportPanel, { props: {
      ...props, sitePlatform: 'sub2api',
      groups: transportPlatforms.map((platform, index) => ({ id: index + 10, name: platform, platform, rate_multiplier: 1 })),
      snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: transportPlatforms.map(platform => ({ ...remote, id: platform, platform, models: [platform + '-model'] })) } },
    } })
    await flushPromises()
    await wrapper.get('[data-test=select-all]').setValue(true)
    expect(wrapper.findAll('[data-test=platform]').map(input => (input.element as HTMLSelectElement).value)).toEqual(transportPlatforms)
    for (const [index, target] of wrapper.findAll('[data-test=target]').entries()) await target.setValue([String(index + 10)])
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledWith(1, { selections: transportPlatforms.map((platform, index) => expect.objectContaining({
      remote_group_id: platform, platform, local_group_ids: [index + 10],
      account_config: expect.objectContaining({ model_mapping: { [platform + '-model']: platform + '-model' } }),
    })) })
    wrapper.unmount()
  })
  it.each(['binding', 'key'] as const)('preserves legacy Grok to OpenAI protocol from an existing %s across refreshes', async source => {
    const legacy = { remote_group_id: 'r', platform: 'openai' as const }
    const wrapper = mount(ImportPanel, { props: { ...props,
      bindings: source === 'binding' ? [legacy] : [], managedKeys: source === 'key' ? [legacy] : [],
      snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [{ ...props.snapshot.catalog.groups[0]!, platform: 'grok' }] } },
    } })
    await flushPromises()
    expect((wrapper.get('[data-test=platform]').element as HTMLSelectElement).value).toBe('openai')
    await wrapper.get('[data-test=platform]').setValue('grok')
    await wrapper.setProps({ bindings: [legacy] })
    expect((wrapper.get('[data-test=platform]').element as HTMLSelectElement).value).toBe('grok')
    wrapper.unmount()
  })
  it('allows mixed local groups for an explicitly chosen transport', async () => {
    const wrapper = mount(ImportPanel, { props: { ...props, groups: [{ id: 9, name: 'Mixed destination', platform: 'composite', rate_multiplier: 2 }] } })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    expect(wrapper.get('[data-test=target]').text()).toContain('Mixed destination')
  })
  it('requires selection, mapping, preview and explicit apply; displays partial result', async () => {
    vi.mocked(api.preview).mockResolvedValue({
      id: 'p',
      rows: [{
        selection: { remote_group_id: 'r', platform: 'openai', local_group_ids: [3], account_name: 'Imported account', cost_multiplier: 1, account_config: defaultImportConfig() },
        remote_group: props.snapshot.catalog.groups[0]!, target: { id: 3, name: 'Local', platform: 'openai', sale_multiplier: 2 },
        existing: null, will_create_key: true, marker: 'import-result',
      }],
      expires_at: '2099-01-01',
      site_id: 1,
      site_version: 1,
      snapshot_id: 1,
      created_at: 'now',
    })
    vi.mocked(api.apply).mockResolvedValue({
      preview_id: 'p',
      items: [
        {
          remote_group_id: 'r',
          platform: 'openai',
          status: 'failed',
          error: 'fixture failure',
        },
      ],
    })
    const wrapper = mount(ImportPanel, { props })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalled()
    expect(api.apply).not.toHaveBeenCalled()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(api.apply).toHaveBeenCalledWith(1, 'p')
    const item = wrapper.get('[data-test=import-result-item]')
    expect(item.text()).toContain('Remote')
    expect(item.text()).toContain('Imported account')
    expect(item.text()).toContain('OpenAI')
    expect(item.text()).toContain('governance.failed')
    expect(item.text()).toContain('governance.resultFailureReason')
    expect(item.text()).toContain('fixture failure')
    expect(wrapper.text()).toContain('governance.applyResultSummary')
  })
  it('invalidates stale previews and requires another preview', async () => {
    vi.mocked(api.apply).mockRejectedValue({ status: 409 })
    const wrapper = mount(ImportPanel, { props })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.stale')
    expect(wrapper.find('[data-test=apply]').exists()).toBe(false)
  })
  it('discards completed previews after a key repair while preserving selections and account settings', async () => {
    vi.mocked(api.apply).mockResolvedValue({ preview_id: 'p', items: [{ remote_group_id: 'r', platform: 'openai', account_id: 10, status: 'applied' }] })
    const wrapper = mount(ImportPanel, { props: { ...props, previewEpoch: 0 } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('[data-test=priority]').setValue(7)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=import-result-item]').text()).toContain('#10')

    await wrapper.setProps({ previewEpoch: 1 })
    expect(wrapper.find('[data-test=apply]').exists()).toBe(false)
    expect(wrapper.find('[data-test=import-result-item]').exists()).toBe(false)
    expect((wrapper.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
    expect(Array.from((wrapper.get('[data-test=target]').element as HTMLSelectElement).selectedOptions, option => option.value)).toEqual(['3'])
    expect((wrapper.get('[data-test=priority]').element as HTMLInputElement).value).toBe('7')
    expect(api.apply).toHaveBeenCalledTimes(1)
    expect(api.modelTemplates).toHaveBeenCalledTimes(1)

    vi.mocked(api.preview).mockResolvedValue({ id: 'after-repair', site_id: 1, site_version: 1, snapshot_id: 1, created_at: 'now', expires_at: '2099-01-01', rows: [] })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenLastCalledWith(1, { selections: [expect.objectContaining({ local_group_ids: [3], account_config: expect.objectContaining({ priority: 7, model_mapping: { 'fixture-model': 'fixture-model' } }) })] })
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(api.apply).toHaveBeenLastCalledWith(1, 'after-repair')
  })
  it('clears obsolete import errors after a key repair', async () => {
    vi.mocked(api.preview).mockRejectedValueOnce({ reason: 'upstream_key_missing' })
    const wrapper = mount(ImportPanel, { props: { ...props, previewEpoch: 0 } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.upstreamKeyMissing')
    await wrapper.setProps({ previewEpoch: 1 })
    expect(wrapper.text()).not.toContain('governance.upstreamKeyMissing')
    expect((wrapper.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
  })
  it.each(['preview', 'apply'] as const)('ignores a late %s response after a key repair invalidates the import', async action => {
    const wrapper = mount(ImportPanel, { props: { ...props, previewEpoch: 0 } })
    await flushPromises()
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=target]').setValue(['3'])
    let finish!: () => void
    if (action === 'preview') {
      vi.mocked(api.preview).mockImplementationOnce(() => new Promise(resolve => { finish = () => resolve({ id: 'late-preview', site_id: 1, site_version: 1, snapshot_id: 1, created_at: 'now', expires_at: '2099-01-01', rows: [] }) }))
    } else {
      vi.mocked(api.apply).mockImplementationOnce(() => new Promise(resolve => { finish = () => resolve({ preview_id: 'p', items: [{ remote_group_id: 'r', platform: 'openai', account_id: 10, status: 'applied' }] }) }))
    }
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    if (action === 'apply') await wrapper.get('[data-test=apply]').trigger('click')
    await wrapper.setProps({ previewEpoch: 1 })
    finish()
    await flushPromises()
    expect(wrapper.find('[data-test=apply]').exists()).toBe(false)
    expect(wrapper.find('[data-test=import-result-item]').exists()).toBe(false)
    expect(wrapper.emitted('applied')).toBeUndefined()
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })
})
