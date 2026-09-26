import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import ImportPanel from './ImportPanel.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { preview: vi.fn(), apply: vi.fn(), keys: vi.fn().mockResolvedValue([]), createKeys: vi.fn(), revealKey: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const props = {
  siteId: 1,
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
  beforeEach(() => vi.clearAllMocks())
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
    await wrapper.get('[data-test=target]').setValue(3)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const prices = wrapper.get('[data-test=preview-prices]')
    expect(prices.text()).toContain('priced-model')
    expect(prices.text()).toContain('governance.usdPerMillionTokens')
    expect(prices.get('[data-test=price-input]').text()).toBe('2')
    expect(prices.get('[data-test=price-output]').text()).toBe('0')
    expect(prices.text()).toContain('time_pricing')
    expect(api.apply).not.toHaveBeenCalled()
  })
  it('does not silently drop incompatible selected groups from a bulk mapping or preview request', async () => {
    const remote = props.snapshot.catalog.groups[0]!
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog, groups: [remote, { ...remote, id: 'a', platform: 'anthropic' }] } } } })
    await wrapper.get('[data-test=select-all]').setValue(true)
    await wrapper.get('[data-test=bulk-target]').setValue(3)
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.findAll('[data-test=select]').every(input => (input.element as HTMLInputElement).checked)).toBe(true)
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
    expect(wrapper.findAll('[data-test=platform]').map(input => (input.element as HTMLSelectElement).value)).toEqual(['openai', 'openai', ''])
    await wrapper.get('[data-test=bulk-target]').setValue(9)
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.findAll('[data-test=target]').map(input => (input.element as HTMLSelectElement).value)).toEqual(['9', '9', '0'])
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('[data-test=platform]')[2]!.setValue('anthropic')
    await wrapper.get('[data-test=assign-target]').trigger('click')
    expect(wrapper.get('[data-test=preview]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(api.preview).toHaveBeenCalledWith(1, { selections: [
      { remote_group_id: 'r', platform: 'openai', local_group_id: 9, account_name: 'Remote', cost_multiplier: 1 },
      { remote_group_id: 'grok', platform: 'openai', local_group_id: 9, account_name: 'Grok', cost_multiplier: 1 },
      { remote_group_id: 'mixed', platform: 'anthropic', local_group_id: 9, account_name: 'Mixed', cost_multiplier: 1 },
    ] })
  })
  it('preserves zero account balances and unknown amounts in original units', () => {
    const wrapper = mount(ImportPanel, { props: { ...props, snapshot: { ...props.snapshot, catalog: { ...props.snapshot.catalog,
      account: { user_id: 4, username: 'upstream-user', email: '', balance: 0, frozen_balance: null, used_balance: 2500, unit: 'quota', source: 'user/self' },
    } } } })
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('0 quota')
    expect(wrapper.get('[data-test=account-frozen]').text()).toContain('governance.unknown')
    expect(wrapper.get('[data-test=account-used]').text()).toBe('2500 quota')
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
      rows: [],
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
    await wrapper.get('[data-test=target]').setValue(3)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalled()
    expect(api.apply).not.toHaveBeenCalled()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(api.apply).toHaveBeenCalledWith(1, 'p')
    expect(wrapper.text()).toContain('fixture failure')
  })
  it('invalidates stale previews and requires another preview', async () => {
    vi.mocked(api.apply).mockRejectedValue({ status: 409 })
    const wrapper = mount(ImportPanel, { props })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(3)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.stale')
    expect(wrapper.find('[data-test=apply]').exists()).toBe(false)
  })
})
