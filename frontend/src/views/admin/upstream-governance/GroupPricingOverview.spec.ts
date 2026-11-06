import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { RemoteGroup } from '@/api/admin/upstream-governance'
import GroupPricingOverview from './GroupPricingOverview.vue'
import PriceDetails from './PriceDetails.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const group = (id: string, name: string, models: string[], rate: number): RemoteGroup => ({
  id, name, platform: 'openai', models, rate_multiplier: rate, user_rate_multiplier: null, resolved_rate_multiplier: rate, peak_rate_enabled: false, source: 'fixture',
  prices: models.map(model => ({ model, platform: 'openai', unit: 'usd_per_token', input: 0.000001, output: 0.000002, per_request: null })),
})

describe('GroupPricingOverview', () => {
  it('lists every group with its rate and opens one price table at a time', async () => {
    const wrapper = mount(GroupPricingOverview, { props: { groups: [group('1', 'Cheap', ['deepseek-v4'], 0.033), group('2', 'Claude', ['claude-haiku-5-5'], 2.5)] } })
    expect(wrapper.findAll('[data-test=pricing-group]')).toHaveLength(2)
    expect(wrapper.findAll('[data-test=pricing-group-rate]').map(rate => rate.text())).toEqual(['0.033×', '2.5×'])
    expect(wrapper.findComponent(PriceDetails).exists()).toBe(false)

    await wrapper.findAll('[data-test=pricing-group] > button')[1]!.trigger('click')
    const details = wrapper.getComponent(PriceDetails)
    expect(details.props('open')).toBe(true)
    expect(details.props('prices')).toHaveLength(1)
    await wrapper.findAll('[data-test=pricing-group] > button')[0]!.trigger('click')
    expect(wrapper.findAllComponents(PriceDetails)).toHaveLength(1)
    expect(wrapper.getComponent(PriceDetails).props('prices')![0]!.model).toBe('deepseek-v4')
  })

  it('finds groups by name or by a model they sell and links to the rate history', async () => {
    const wrapper = mount(GroupPricingOverview, { props: { groups: [group('1', 'Cheap', ['deepseek-v4'], 0.033), group('2', 'Claude', ['claude-haiku-5-5'], 2.5)] } })
    await wrapper.get('[data-test=pricing-group-search]').setValue('HAIKU')
    expect(wrapper.findAll('[data-test=pricing-group]').map(row => row.text())).toEqual([expect.stringContaining('Claude')])
    await wrapper.get('[data-test=pricing-group-search]').setValue('nothing')
    expect(wrapper.text()).toContain('governance.noMatchingGroups')
    await wrapper.get('[data-test=pricing-history]').trigger('click')
    expect(wrapper.emitted('history')).toHaveLength(1)
  })

  it('explains an empty catalog', () => {
    expect(mount(GroupPricingOverview, { props: { groups: [] } }).text()).toContain('governance.pricingTab.empty')
  })
})
