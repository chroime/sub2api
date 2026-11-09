import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import Select from '@/components/common/Select.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

const { auth } = vi.hoisted(() => ({ auth: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

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
    global: { stubs: { PlazaVendorTree: true, PlazaVendorPriceTable: true, Icon: true, Teleport: true } }
  })
}

describe('model plaza group selection', () => {
  beforeEach(() => auth.mockReturnValue({ isAuthenticated: false }))

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
    expect(wrapper.findAll('[role="option"]').map(option => option.text())).toEqual([
      'modelPlaza.priceTable.allGroups', 'Z first · 2x', 'B same order · 4x', 'A cheap · 0.01x'
    ])
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
})
