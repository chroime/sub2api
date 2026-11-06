import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { RemotePrice } from '@/api/admin/upstream-governance'
import PriceDetails from './PriceDetails.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
}))

const render = (prices: RemotePrice[]) => mount(PriceDetails, { props: { prices } })
const cells = (row: { findAll: (selector: string) => { text: () => string }[] }) => row.findAll('td').map(cell => cell.text())

describe('PriceDetails', () => {
  it('lists token prices per million tokens with cache prices instead of raw metadata', async () => {
    const wrapper = render([{
      model: 'cn:deepseek-v4-flash', platform: 'openai', unit: 'usd_per_token', input: 0.000002, output: 0.000008, per_request: null,
      details: { channel: '国模', intervals: [], input_price: 0.000002, output_price: 0.000008, cache_read_price: 0.00000004, cache_write_price: 0, cache_write_1h_price: null, billing_mode: 'token' },
    }])
    const section = wrapper.get('[data-test=price-section-token]')
    expect(section.text()).toContain('governance.usdPerMillionTokens')
    expect(cells(section.get('[data-test=price-row]'))).toEqual([
      expect.stringContaining('cn:deepseek-v4-flash'), '2', '8', '0.04', '0',
    ])
    expect(section.text()).toContain('OpenAI')
    expect(section.text()).toContain('governance.priceTable.channel:{"name":"国模"}')
    expect(wrapper.text()).not.toContain('billing_mode')
    expect(wrapper.find('[data-test=price-raw]').exists()).toBe(false)

    await wrapper.get('[data-test=price-show-raw]').setValue(true)
    expect(wrapper.get('[data-test=price-raw]').text()).toContain('"billing_mode": "token"')
  })

  it('expands tiered sub2api pricing into labelled tier rows', () => {
    const wrapper = render([{
      model: 'claude-haiku-5-5', platform: 'anthropic', unit: 'sub2api_complex_pricing', input: null, output: null, per_request: null,
      details: {
        long_context_pricing_enabled: true,
        intervals: [
          { min_tokens: 0, max_tokens: 100000, tier_label: '≤100K', input_price: 0.0000001, output_price: 0.0000005, cache_read_price: 0.00000001, cache_write_price: 0.000000125, cache_write_1h_price: 0.0000002 },
          { min_tokens: 100000, max_tokens: null, input_price: null, input_multiplier: 2, output_price: 0.0000025, cache_read_price: 0.00000005, cache_write_price: 0.000000625 },
        ],
      },
    }])
    const row = wrapper.get('[data-test=price-row]')
    expect(row.text()).toContain('governance.priceTable.tiered:{"count":2}')
    expect(row.text()).toContain('governance.priceTable.longContext')
    expect(cells(row).slice(1)).toEqual(['', '', '', '', ''])
    const tiers = wrapper.findAll('[data-test=price-tier]')
    expect(wrapper.get('[data-test=price-section-token] thead').text()).toContain('governance.priceTable.cacheWrite1h')
    expect(cells(tiers[0]!)).toEqual(['≤100K', '0.1', '0.5', '0.01', '0.125', '0.2'])
    expect(cells(tiers[1]!)).toEqual(['>100K', '×2', '2.5', '0.05', '0.625', '—'])
  })

  it('groups per-request prices, New API ratios and unflattened rules into their own tables', () => {
    const wrapper = render([
      { model: 'gpt-image-free', platform: 'unknown', unit: 'usd_per_request', input: null, output: null, per_request: 0.022, details: { quota_type: 1, model_price: 0.022 } },
      { model: 'gpt-5.5', platform: 'unknown', unit: 'newapi_ratio', input: 0.625, output: 8, per_request: null, details: { pricing_group_ratio: 1.5, cache_ratio: 0.1 } },
      { model: 'odd-model', platform: 'unknown', unit: 'newapi_unknown', input: null, output: null, per_request: null, details: { billing_plugin_variants: [{ plugin_key: 'x' }] } },
    ])
    expect(cells(wrapper.get('[data-test=price-section-request] [data-test=price-row]'))).toEqual([expect.stringContaining('gpt-image-free'), '0.022'])
    expect(cells(wrapper.get('[data-test=price-section-ratio] [data-test=price-row]'))).toEqual([expect.stringContaining('gpt-5.5'), '0.625', '8', '1.5', '0.1'])
    const other = wrapper.get('[data-test=price-section-other]')
    expect(other.text()).toContain('governance.priceTable.complexPricing')
    expect(other.text()).toContain('governance.priceTable.plugins')
    expect(wrapper.text()).not.toContain('unknown')
    expect(wrapper.find('[data-test=price-section-token]').exists()).toBe(false)
  })

  it('offers a model search once a group lists many prices', async () => {
    const prices = Array.from({ length: 9 }, (_, index) => ({
      model: index === 4 ? 'needle-model' : `model-${index}`, platform: 'openai', unit: 'usd_per_token', input: 0.000001, output: 0.000002, per_request: null,
    }))
    const wrapper = render(prices)
    expect(wrapper.findAll('[data-test=price-row]')).toHaveLength(9)
    await wrapper.get('[data-test=price-search]').setValue('NEEDLE')
    expect(wrapper.findAll('[data-test=price-row]').map(row => row.text())).toEqual([expect.stringContaining('needle-model')])
    await wrapper.get('[data-test=price-search]').setValue('absent')
    expect(wrapper.text()).toContain('governance.priceTable.noMatch')
    expect(render(prices.slice(0, 8)).find('[data-test=price-search]').exists()).toBe(false)
  })

  it('shows a platform or rule every row shares once in the section header', () => {
    const longContext = (model: string) => ({
      model, platform: 'anthropic', unit: 'usd_per_token', input: 0.000003, output: 0.000015, per_request: null,
      details: { long_context_pricing_enabled: true, cache_read_price: 0.0000003, cache_write_price: 0.00000375 },
    })
    const wrapper = render([longContext('claude-a'), longContext('claude-b'), { ...longContext('claude-c'), details: { long_context_pricing_enabled: true, time_pricing: [{ multiplier: 2 }] } }])
    const section = wrapper.get('[data-test=price-section-token]')
    expect(section.get('h5').text()).toContain('Anthropic')
    expect(section.get('[data-test=price-shared-tags]').text()).toContain('governance.priceTable.longContext')
    const rows = section.findAll('[data-test=price-row]')
    expect(rows[0]!.text()).not.toContain('Anthropic')
    expect(rows[0]!.findAll('[data-test=price-tag]')).toHaveLength(0)
    expect(rows[2]!.findAll('[data-test=price-tag]').map(tag => tag.text())).toEqual(['governance.priceTable.timePricing'])
    // A single row keeps its own platform and tags.
    const single = render([longContext('claude-only')]).get('[data-test=price-row]')
    expect(single.text()).toContain('Anthropic')
    expect(single.text()).toContain('governance.priceTable.longContext')
  })
})
