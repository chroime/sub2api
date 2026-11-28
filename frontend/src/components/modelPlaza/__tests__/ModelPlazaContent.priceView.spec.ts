import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { config, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaVendorPriceTable from '../PlazaVendorPriceTable.vue'
import Select from '@/components/common/Select.vue'
import dashboard from '@/i18n/locales/zh/dashboard'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))

const model = (name: string): PlazaModel => ({ name, platform: 'openai', pricing: null, official_pricing: null })
const qwen = model('qwen3.8-max')
const kimi = model('kimi-k3')
const gpt = model('gpt-6.2-sol')
function group(id: number, name: string, models: PlazaModel[], extra: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return { id, name, models, description: '', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, is_exclusive: false,
    image_rate_independent: false, image_rate_multiplier: 1, video_rate_independent: false, video_rate_multiplier: 1,
    long_context_pricing_enabled: false, ...extra }
}
const domestic = () => group(19, '国模全套', [kimi], { catalog_models: [kimi, qwen], sort_order: 2 })
const openai = () => group(23, 'GPT Max', [gpt], { sort_order: 1 })
const wrappers: ReturnType<typeof mount>[] = []
const defaultPlugins = config.global.plugins
function render(groups = [domestic(), openai()]) {
  const wrapper = mount(ModelPlazaContent, {
    props: { response: { description: '', groups }, loading: false },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: dashboard }, missingWarn: false, fallbackWarn: false })],
      stubs: { PlazaVendorPriceTable: true, ModelIcon: true, Icon: true, Teleport: true }
    }
  })
  wrappers.push(wrapper)
  return wrapper
}
async function selectGroup(wrapper: ReturnType<typeof render>, id: number) {
  await wrapper.get('#plaza-group-select').trigger('click')
  const index = wrapper.getComponent(Select).props('options').findIndex(option => option.value === id)
  expect(index).toBeGreaterThanOrEqual(0)
  await wrapper.findAll('[role=option]')[index]!.trigger('click')
}

describe('model plaza price views', () => {
  beforeEach(() => { document.body.innerHTML = ''; config.global.plugins = [] })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); config.global.plugins = defaultPlugins })

  it('starts with an explicit official view and a catalog explanation, without a group picker', () => {
    const wrapper = render()
    expect(wrapper.get('[data-price-view=official]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-price-view=site]').text()).toBe('本站实付价')
    expect(wrapper.find('#plaza-group-select').exists()).toBe(false)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('group')).toBeNull()
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual(expect.arrayContaining([qwen, kimi, gpt]))
    expect(wrapper.get('[data-price-view-hint]').text()).toContain('各接入分组')
    expect(wrapper.get('[data-vendor=all]').text()).toContain('全部厂商')
    expect(wrapper.get('[data-vendor=all]').text()).toContain('3 款模型')
  })

  it('selects the first configured access group and offers only actual groups for site prices', async () => {
    const wrapper = render()
    await wrapper.get('[data-price-view=site]').trigger('click')
    const picker = wrapper.getComponent(Select)
    expect(picker.props('modelValue')).toBe(23)
    expect(picker.props('options').map(option => option.value)).toEqual([23, 19])
    expect(picker.props('ariaLabel')).toBe('接入分组')
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('group')?.id).toBe(23)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([gpt])
  })

  it('remembers the chosen group, vendor and search while changing price views', async () => {
    const wrapper = render()
    await wrapper.get('[data-price-view=site]').trigger('click')
    await selectGroup(wrapper, 19)
    await wrapper.get('[data-vendor=moonshot]').trigger('click')
    await wrapper.get('input[type=search]').setValue('kimi')
    await wrapper.get('#plaza-group-select').trigger('click')
    await wrapper.get('[data-price-view=official]').trigger('click')
    expect(wrapper.find('#plaza-group-select').exists()).toBe(false)
    expect(wrapper.find('[role=listbox]').exists()).toBe(false)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('group')).toBeNull()
    await wrapper.get('[data-price-view=site]').trigger('click')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(19)
    expect(wrapper.get('[data-vendor=moonshot]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get<HTMLInputElement>('input[type=search]').element.value).toBe('kimi')
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([kimi])
  })

  it('explains the absent vendor instead of calling a populated group empty, and recovers within that group', async () => {
    const wrapper = render()
    await wrapper.get('[data-vendor=qwen]').trigger('click')
    await wrapper.get('[data-price-view=site]').trigger('click')
    await selectGroup(wrapper, 19)
    expect(wrapper.get('[data-vendor=qwen]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-vendor=qwen]').text()).toContain('0 款模型')
    const empty = wrapper.get('section .plaza-empty')
    expect(empty.text()).toContain('国模全套')
    expect(empty.text()).toContain('暂无 Qwen 模型')
    expect(empty.text()).not.toContain('该分组暂无可用模型')
    expect(wrapper.get('[data-empty-clear]').text()).toBe('查看该分组全部模型')
    await wrapper.get('[data-empty-clear]').trigger('click')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(19)
    expect(wrapper.get('[data-price-view=site]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([kimi])
  })

  it('distinguishes a search miss from an empty group and clears filters in the selected group', async () => {
    const wrapper = render()
    await wrapper.get('[data-price-view=site]').trigger('click')
    await selectGroup(wrapper, 19)
    await wrapper.get('[data-vendor=moonshot]').trigger('click')
    await wrapper.get('input[type=search]').setValue('no-such-model')
    expect(wrapper.get('section .plaza-empty').text()).toContain('没有匹配的模型')
    await wrapper.get('[data-empty-clear]').trigger('click')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(19)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([kimi])
  })

  it('keeps a truly empty access group selected and describes its empty availability', async () => {
    const wrapper = render([group(7, 'Empty', [])])
    await wrapper.get('[data-price-view=site]').trigger('click')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(7)
    expect(wrapper.get('section .plaza-empty').text()).toContain('该分组暂无可用模型')
    expect(wrapper.findComponent(PlazaVendorPriceTable).exists()).toBe(false)
  })

  it('preserves a visible selected group across sorting and selects the next available group if removed', async () => {
    const wrapper = render()
    await wrapper.get('[data-price-view=site]').trigger('click')
    await selectGroup(wrapper, 19)
    await wrapper.setProps({ response: { description: '', groups: [group(23, 'GPT Max', [gpt], { sort_order: -1 }), domestic()] } })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(19)
    await wrapper.setProps({ response: { description: '', groups: [openai()] } })
    expect(wrapper.get('[data-price-view=site]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe(23)
    expect(wrapper.getComponent(PlazaVendorPriceTable).props('models')).toEqual([gpt])
  })
})
