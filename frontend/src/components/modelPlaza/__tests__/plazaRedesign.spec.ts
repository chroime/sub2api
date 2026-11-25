import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaVendorPriceTable from '../PlazaVendorPriceTable.vue'
import PlazaVendorFilter from '../PlazaVendorFilter.vue'
import PlazaPricingBadges from '../PlazaPricingBadges.vue'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

function group(models: PlazaModel[]): ModelPlazaGroup {
  return { id: 1, name: 'Qwen group', description: '', platform: 'openai', subscription_type: 'standard', rate_multiplier: .2,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, is_exclusive: false,
    image_rate_independent: false, image_rate_multiplier: 1, video_rate_independent: false, video_rate_multiplier: 1,
    long_context_pricing_enabled: false, models }
}

const qwen: PlazaModel = { name: 'qwen3.8-max', platform: 'openai', pricing: null, official_pricing: null }

describe('approved model plaza redesign', () => {
  beforeEach(() => { document.body.innerHTML = '' })

  it('uses a full-width toolbar instead of a permanent vendor aside', () => {
    const wrapper = mount(ModelPlazaContent, { props: { loading: false, response: { description: '', groups: [group([qwen])] } },
      global: { stubs: { ModelIcon: true, Icon: true, PlazaVendorPriceTable: true } } })
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.find('[data-plaza-toolbar]').exists()).toBe(true)
    expect(wrapper.find('[data-vendor-filter]').exists()).toBe(true)
    expect(wrapper.find('input[type="search"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders scope-aware pricing rules and opens the high/low price detail', async () => {
    const model: PlazaModel = { name: 'deepseek-v4-pro', platform: 'deepseek', pricing: null,
      official_pricing: { currency: 'CNY', input_price: 9e-6, output_price: 27e-6, cache_read_price: .3e-6, cache_write_price: null } }
    Object.assign(model, { official_pricing_rules: { currency: 'CNY', context_status: 'flat', context_tier_count: 1,
      context_basis: 'whole_request', time_status: 'peak_offpeak', price_basis: 'peak', time_rules: [{ source: 'official',
        timezone: 'Asia/Shanghai', weekdays_only: true, base_multiplier: .5, fields: ['input_price','output_price','cache_read_price'],
        periods: [{ start_time: '09:00', end_time: '12:00', multiplier: 1 }, { start_time: '14:00', end_time: '18:00', multiplier: 1 }] }] } })
    const wrapper = mount(PlazaVendorPriceTable, { props: { models: [model], group: null, caption: 'DeepSeek' },
      global: { stubs: { ModelIcon: true, Icon: true, Teleport: true } } })
    expect(wrapper.get('[data-context-rule]').attributes('data-status')).toBe('flat')
    expect(wrapper.get('[data-time-rule]').attributes('data-status')).toBe('peak_offpeak')
    expect(wrapper.get('[data-price-basis]').text()).toContain('modelPlaza.pricingRules.basis_peak')
    await wrapper.get('[data-time-rule]').trigger('click')
    expect(wrapper.get('[data-pricing-rule-details]').text()).toContain('Asia/Shanghai')
    expect(wrapper.get('[data-pricing-rule-details]').text()).toContain('¥4.50')
    expect(wrapper.get('[data-pricing-rule-details]').text()).toContain('¥9.00')
    wrapper.unmount()
  })

  it('renders every vendor as a direct filter without opening a second picker', async () => {
    const keys = ['openai','anthropic','google','deepseek','qwen','zhipu','moonshot','minimax','mimo']
    const wrapper = mount(PlazaVendorFilter, { props: { vendor: 'all', vendors: keys.map(key => ({ key, label: key, sampleModel: '', models: [qwen] })) },
      global: { stubs: { ModelIcon: true, Icon: true, Teleport: true } } })
    expect(wrapper.findAll('[data-vendor-chips] [data-vendor]')).toHaveLength(keys.length + 1)
    expect(wrapper.find('[data-all-vendors]').exists()).toBe(false)
    expect(wrapper.find('[data-vendor-options]').exists()).toBe(false)
    expect(wrapper.get('[data-vendor-chips]').classes()).toContain('flex-wrap')
    await wrapper.get('[data-vendor-chips] [data-vendor="minimax"]').trigger('click')
    expect(wrapper.emitted('update:vendor')).toEqual([['minimax']])
    wrapper.unmount()
  })

  it('orders common chips and the full vendor picker like the channel pricing platforms', async () => {
    const keys = ['openai','minimax','deepseek','claude','gemini','moonshot','zhipu','xai','qwen','mimo','other']
    const wrapper = mount(PlazaVendorFilter, { props: { vendor: 'all', vendors: keys.map(key => ({ key, label: key, sampleModel: '', models: [qwen] })) },
      global: { stubs: { ModelIcon: true, Icon: true, Teleport: true } } })
    const fullOrder = ['claude','openai','gemini','xai','moonshot','zhipu','deepseek','minimax','mimo','other','qwen']
    expect(wrapper.findAll('[data-vendor-chips] [data-vendor]').map(node => node.attributes('data-vendor')))
      .toEqual(['all', ...fullOrder])
    wrapper.unmount()
  })

  it('keeps absent rule metadata unconfirmed rather than claiming uniform pricing', () => {
    const wrapper = mount(PlazaVendorPriceTable, { props: { models: [qwen], group: null, caption: 'Unknown' },
      global: { stubs: { ModelIcon: true, Icon: true, Teleport: true } } })
    expect(wrapper.get('[data-context-rule]').attributes('data-status')).toBe('unknown')
    expect(wrapper.find('[data-time-rule]').exists()).toBe(false)
    expect(wrapper.get('[data-price-currency]').attributes('data-currency')).toBe('')
    wrapper.unmount()
  })

  it('keeps vendor choices stable while intersecting model search with the selected vendor', async () => {
    const openai: PlazaModel = { ...qwen, name: 'gpt-6-sol' }
    const wrapper = mount(ModelPlazaContent, { props: { loading: false, response: { description: '', groups: [group([qwen, openai])] } },
      global: { stubs: { ModelIcon: true, Icon: true, PlazaVendorPriceTable: true } } })
    const choices = wrapper.get('[data-vendor-chips]').text()
    await wrapper.get('input[type="search"]').setValue('qwen')
    expect(wrapper.get('[data-vendor-chips]').text()).toBe(choices)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([qwen])
    await wrapper.get('[data-vendor-chips] [data-vendor="openai"]').trigger('click')
    expect(wrapper.findComponent(PlazaVendorPriceTable).exists()).toBe(false)
    expect((wrapper.get('input[type="search"]').element as HTMLInputElement).value).toBe('qwen')
    wrapper.unmount()
  })

  it('shows both currencies when the site and official price cards differ', () => {
    const rules = { currency: 'CNY', context_status: 'flat' as const, context_tier_count: 1,
      context_basis: 'whole_request' as const, time_status: 'uniform' as const, price_basis: 'standard' as const }
    const model: PlazaModel = { ...qwen, pricing: { input_price: 1e-6, output_price: 2e-6, cache_read_price: null, cache_write_price: null },
      official_pricing: { currency: 'USD', input_price: 1e-6, output_price: 2e-6, cache_read_price: null, cache_write_price: null },
      pricing_rules: rules, official_pricing_rules: { ...rules, currency: 'USD' } }
    const wrapper = mount(PlazaPricingBadges, { props: { model, group: group([model]) }, global: { stubs: { Teleport: true } } })
    expect(wrapper.get('[data-price-currency]').text()).toContain('modelPlaza.pricingRules.scopedCurrencies')
    wrapper.unmount()
  })

  it('keeps the site currency unknown when only the official currency is known', () => {
    const model: PlazaModel = { ...qwen, official_pricing: { currency: 'USD', input_price: 1e-6, output_price: null, cache_read_price: null, cache_write_price: null },
      pricing_rules: { currency: '', context_status: 'unknown', context_tier_count: 0, context_basis: 'whole_request', time_status: 'unknown', price_basis: 'standard' } }
    const wrapper = mount(PlazaPricingBadges, { props: { model, group: group([model]) }, global: { stubs: { Teleport: true } } })
    expect(wrapper.get('[data-price-currency]').attributes('data-currency')).toBe('')
    expect(wrapper.get('[data-price-currency]').text()).toBe('modelPlaza.pricingRules.currencyUnknown')
    wrapper.unmount()
  })

  it('shows combined current prices from the backend snapshot for every context tier', async () => {
    const model: PlazaModel = { ...qwen, pricing: { input_price: 10e-6, output_price: 20e-6, cache_read_price: null, cache_write_price: null, cache_write_1h_price: 5e-6, cache_write_12h_price: 10e-6,
      intervals: [{ min_tokens: 0, max_tokens: 1000, input_price: 10e-6 }, { min_tokens: 1000, max_tokens: null, input_price: 20e-6 }] } }
    Object.assign(model, { pricing_rules: { currency: 'CNY', context_status: 'tiered', context_tier_count: 2,
      context_basis: 'whole_request', time_status: 'scheduled', price_basis: 'standard', evaluated_at: '2026-11-24T02:00:00Z',
      current_multipliers: { input_price: 1, output_price: 1 }, time_rules: [
        { source: 'channel', timezone: 'Asia/Shanghai', base_multiplier: 1, current_multiplier: .5, fields: ['input_price'], periods: [{ start_time: '09:00', end_time: '12:00', multiplier: .5 }] },
        { source: 'group', timezone: 'Asia/Shanghai', base_multiplier: 1, current_multiplier: 2, fields: ['input_price'], periods: [{ start_time: '09:00', end_time: '12:00', multiplier: 2 }] }
      ] } })
    const wrapper = mount(PlazaPricingBadges, { props: { model, group: group([model]) }, global: { stubs: { Teleport: true } } })
    await wrapper.get('[data-time-rule]').trigger('click')
    const current = wrapper.get('[data-current-prices]')
    expect(current.get('time').attributes('datetime')).toBe('2026-11-24T02:00:00Z')
    expect(current.text()).toContain('¥2.00')
    expect(current.text()).toContain('¥4.00')
    expect(current.text()).toContain('1h')
    expect(current.text()).toContain('12h')
    expect(current.text()).toContain('¥1.00')
    expect(wrapper.findAll('[data-current-rule-multiplier]').map(node => node.attributes('data-current-rule-multiplier'))).toEqual(['0.5', '2'])
    wrapper.unmount()
  })
})
