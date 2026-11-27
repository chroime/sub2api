import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlazaVendorPriceTable from '../PlazaVendorPriceTable.vue'
import type { ModelPlazaGroup, PlazaModel, PlazaPricingSource } from '@/api/modelPlaza'

const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    // 只回显 key 与插值，断言聚焦取价逻辑而非文案。
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

function tokenModel(overrides: Partial<PlazaModel> = {}): PlazaModel {
  return {
    name: 'claude-sonnet-5-5',
    platform: 'anthropic',
    pricing: {
      billing_mode: 'token',
      input_price: 3e-6,
      output_price: 1.5e-5,
      cache_write_price: 3.75e-6,
      cache_read_price: 3e-7,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: []
    },
    pricing_source: 'channel' as PlazaPricingSource,
    official_pricing: null,
    ...overrides
  }
}

function group(overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return {
    id: 1,
    name: 'g1',
    description: '',
    platform: 'anthropic',
    subscription_type: 'standard',
    rate_multiplier: 0.5,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    long_context_pricing_enabled: true,
    models: [],
    ...overrides
  }
}

function mountTable(models: PlazaModel[], g: ModelPlazaGroup | null = group({ rate_multiplier: 1 })) {
  return mount(PlazaVendorPriceTable, {
    props: { models, group: g, caption: 'test' }
  })
}

describe('PlazaVendorPriceTable model ID copy', () => {
  it.each([false, true])('copies the selected model ID from its accessible row action (group=%s)', async selectedGroup => {
    copyToClipboard.mockClear()
    const models = [tokenModel(), tokenModel({ name: 'gpt-6.1-terra', platform: 'openai' })]
    const wrapper = mountTable(models, selectedGroup ? group() : null)
    const buttons = wrapper.findAll('[data-copy-model-id]')
    expect(buttons).toHaveLength(models.length)
    expect(buttons[1].attributes('type')).toBe('button')
    expect(buttons[1].attributes('aria-label')).toContain(models[1].name)
    await buttons[1].trigger('click')
    expect(copyToClipboard).toHaveBeenCalledOnce()
    expect(copyToClipboard).toHaveBeenCalledWith(models[1].name)
  })
})

describe('PlazaVendorPriceTable 本站价与官方价', () => {
  it.each(['group', 'channel'] as const)('参考价优先使用 %s 配置，节省继续以官方价计算', source => {
    const m = tokenModel({ pricing_source: source,
      official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 5e-6
    const cell = mountTable([m], group()).get('[data-price-field=input_price]')
    const reference = cell.get('[data-reference-price]')
    expect(reference.text()).toContain('$5.00')
    expect(reference.attributes('data-reference-source')).toBe(source)
    expect(cell.get('[data-site-price]').text()).toBe('$2.50')
    expect(cell.get('[data-savings]').text()).toContain('¥17.60')
    expect(cell.get('[data-savings]').attributes('data-savings-reference')).toBe('official')
  })

  it('未配置本站价时使用官方参考，零元本站配置不回退', async () => {
    const m = tokenModel({ pricing_source: 'official',
      official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    const wrapper = mountTable([m])
    expect(wrapper.get('[data-reference-price]').text()).toContain('$3.00')
    expect(wrapper.get('[data-reference-price]').attributes('data-reference-source')).toBe('official')
    await wrapper.setProps({ models: [{ ...m, pricing_source: 'channel', pricing: { ...m.pricing!, input_price: 0 } }] })
    expect(wrapper.get('[data-reference-price]').text()).toContain('$0.00')
    expect(wrapper.get('[data-reference-price]').attributes('data-reference-source')).toBe('channel')
  })

  it('官方价置于参考价上方，本站配置为零时仍相对官方节省 100%', () => {
    const m = tokenModel({ pricing_source: 'group',
      official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 0
    const cell = mountTable([m], group()).get('[data-price-field=input_price]')
    expect(cell.get('[data-official-reference]').text()).toContain('$3.00')
    expect(cell.get('[data-reference-price]').text()).toContain('$0.00')
    expect(cell.get('[data-site-price]').text()).toBe('$0.00')
    expect(cell.get('[data-savings]').text()).toContain('100.00%')
    expect(cell.get('[data-savings]').text()).toContain('¥20.10')
    const html = cell.html()
    expect(html.indexOf('data-official-reference')).toBeLessThan(html.indexOf('data-reference-price'))
    expect(html.indexOf('data-reference-price')).toBeLessThan(html.indexOf('data-site-price'))
  })

  it.each([0, null])('缓存写入区分官方零价和未收录，不把缺失当成免费（official=%s）', officialPrice => {
    const m = tokenModel({ name: 'glm-5.3-flash', platform: 'zhipu',
      official_pricing: { input_price: 1.5e-7, output_price: 5e-7, cache_read_price: 3e-8, cache_write_price: officialPrice, currency: 'CNY' } })
    m.pricing!.cache_write_price = 0
    const cell = mountTable([m]).get('[data-price-field=cache_write_price]')
    expect(cell.get('[data-reference-price]').text()).toContain('¥0.00')
    expect(cell.get('[data-site-price]').text()).toBe('¥0.00')
    if (officialPrice === 0) {
      expect(cell.get('[data-official-reference]').text()).toContain('¥0.00')
      expect(cell.get('[data-savings]').text()).toBe('modelPlaza.comparison.sameOfficialPrice')
    } else {
      expect(cell.get('[data-official-reference]').text()).toContain('—')
      expect(cell.find('[data-savings]').exists()).toBe(false)
      expect(cell.text()).toContain('modelPlaza.comparison.noOfficialReference')
    }
    expect(cell.text()).not.toContain('100.00%')
  })

  it('直接在表格显示实付与小字本站基础参考价，移除详情入口与弹窗', () => {
    const m = tokenModel({ pricing_source: 'group',
      official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    expect(wrapper.get('tbody td [data-site-price]').text()).toBe('$2.50')
    const reference = wrapper.get('tbody td [data-reference-price]')
    expect(reference.text()).toContain('$5.00')
    expect(reference.text()).not.toContain('$3.00')
    expect(reference.classes()).toContain('text-xs')
    expect(reference.classes()).not.toContain('line-through')
    expect(wrapper.find('[data-price-details-button], [data-model-price-details], [data-pricing-rules]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('单独定价')
  })

  it('国外模型按人民币额度计算节省金额和比例', () => {
    const m = tokenModel({ pricing_source: 'group', official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    const saving = wrapper.get('[data-price-field=input_price] [data-savings]')
    expect(saving.text()).toContain('¥17.60')
    expect(saving.text()).toContain('87.56%')
    expect(saving.classes()).toContain('pz-savings-highlight')
  })

  it('本站基础参考置顶，实付居中且节省独立以官方价计算', () => {
    const m = tokenModel({ pricing_source: 'group',
      official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    const cell = wrapper.get('[data-price-field=input_price]')
    expect(cell.get('[data-reference-price]').text()).toContain('$5.00')
    expect(cell.get('[data-site-price]').text()).toBe('$2.50')
    const saving = cell.get('[data-savings]')
    expect(saving.text()).toContain('¥17.60')
    expect(saving.text()).toContain('87.56%')
    expect(saving.attributes('data-savings-reference')).toBe('official')
    expect(saving.classes()).toContain('pz-savings-highlight')
    expect(cell.find('[data-configured-price]').exists()).toBe(false)
    const html = cell.html()
    expect(html.indexOf('data-reference-price')).toBeLessThan(html.indexOf('data-site-price'))
    expect(html.indexOf('data-site-price')).toBeLessThan(html.indexOf('data-savings'))
  })

  it('实付高于官方人民币成本时显示加价，并使用正数幅度', () => {
    const m = tokenModel({ official_pricing: { input_price: 1e-6, output_price: null, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 10e-6
    const saving = mountTable([m]).get('[data-price-field=input_price] [data-savings]')
    expect(saving.classes()).toContain('pz-savings-higher')
    expect(saving.text()).toContain('modelPlaza.priceTable.surchargeLabel')
    expect(saving.text()).toContain('49.25%')
    expect(saving.text()).toContain('¥3.30')
    expect(saving.text()).not.toContain('-49.25%')
  })

  it('人民币实际支出相等时显示一致，不把缺省比例作为节省', () => {
    const m = tokenModel({ official_pricing: { input_price: 1e-6, output_price: null, cache_read_price: null, cache_write_price: null } })
    m.pricing!.input_price = 6.7e-6
    const saving = mountTable([m]).get('[data-price-field=input_price] [data-savings]')
    expect(saving.text()).toBe('modelPlaza.comparison.sameOfficialPrice')
    expect(saving.text()).not.toContain('100%')
  })

  it('非零实付即使极低也不宣称节省 100%，零倍率才显示 100%', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: null, cache_read_price: null, cache_write_price: null } })
    const paid = mountTable([m], group({ rate_multiplier: 0.000001 }))
    expect(paid.get('[data-price-field=input_price] [data-savings]').text()).toContain('>99.99%')
    const free = mountTable([m], group({ rate_multiplier: 0 }))
    expect(free.get('[data-price-field=input_price] [data-savings]').text()).toContain('100.00%')
  })

  it('全部分组只展示四列官方价格，不展示本站价格', () => {
    const m = tokenModel({
      official_pricing: { input_price: 4e-6, output_price: 2e-5, cache_write_price: 5e-6, cache_read_price: 4e-7 }
    })
    const wrapper = mountTable([m], null)
    expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.official')
    expect(wrapper.get('thead').text()).not.toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.findAll('thead tr').at(1)!.findAll('th')).toHaveLength(4)
    const cells = wrapper.get('tbody tr').findAll('td')
    expect(cells.map((cell) => cell.text())).toEqual(['$4.00', '$20.00', '$0.40', '$5.00'])
    expect(wrapper.findAll('.pz-cell')).toHaveLength(0)
    expect(wrapper.find('[data-configured-price]').exists()).toBe(false)
  })

  it('全部分组缺少官方价格时显示空值，不冒充本站价格', () => {
    const wrapper = mountTable([tokenModel()], null)
    expect(wrapper.text()).not.toContain('$3.00')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(1)
    expect(wrapper.get('tbody td').attributes('colspan')).toBe('4')
    expect(wrapper.get('tbody td').text()).toBe('modelPlaza.priceTable.unpriced')
  })

  it('全部分组的媒体模型不遗留本站单价列', () => {
    const m = tokenModel()
    m.pricing!.billing_mode = 'image'
    m.pricing!.per_request_price = 0.1
    const wrapper = mountTable([m], null)
    expect(wrapper.findAll('.pz-cell')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('$0.10')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(1)
    expect(wrapper.get('tbody td').attributes('colspan')).toBe('4')
  })

  it('按张计价使用图片独立倍率，且不显示 Token 表头或跨单位官方参考价', () => {
    const m = tokenModel({ name: 'gpt-image-2.6', platform: 'openai', pricing_source: 'group',
      official_pricing: { currency: 'USD', input_price: 5e-6, output_price: null, cache_read_price: 1.25e-6, cache_write_price: null } })
    m.pricing!.billing_mode = 'image'
    m.pricing!.per_request_price = 0.03
    const wrapper = mountTable([m], group({ platform: 'openai', rate_multiplier: 0.03, image_rate_independent: true, image_rate_multiplier: 1 }))
    expect(wrapper.get('[data-site-price]').text()).toBe('$0.03modelPlaza.table.perUnitImage')
    expect(wrapper.find('[data-price-field]').exists()).toBe(false)
    expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.siteMediaUnitNote')
    expect(wrapper.get('thead').text()).not.toContain('modelPlaza.priceTable.siteUnitNote')
    expect(wrapper.get('[data-official-reference]').text()).toContain('—')
    expect(wrapper.find('[data-savings]').exists()).toBe(false)
  })

  it('全部分组按官方阶梯展示，不依赖本站是否配置阶梯', () => {
    const m = tokenModel({
      official_pricing: {
        input_price: 4e-6, output_price: 2e-5, cache_write_price: null, cache_read_price: null,
        intervals: [
          { min_tokens: 0, max_tokens: 200_000, input_price: 4e-6, output_price: 2e-5,
            cache_write_price: null, cache_read_price: null, per_request_price: null },
          { min_tokens: 200_000, max_tokens: null, input_price: 8e-6, output_price: 3e-5,
            cache_write_price: null, cache_read_price: null, per_request_price: null }
        ]
      }
    })
    const wrapper = mountTable([m], null)
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.findAll('[data-price-tier]').map(tier => tier.text())).toEqual(['≤200K', '>200K'])
    expect(wrapper.findAll('[data-price-field=input_price]').map(cell => cell.text())).toEqual(['$4.00', '$8.00'])
    expect(wrapper.get('tbody th.pz-sticky').attributes('rowspan')).toBe('2')
    expect(wrapper.text()).not.toContain('$3.00')
  })

  it('全部分组不显示本站专属的思考倍率与超出部分计价标记', () => {
    const m = tokenModel({ long_context_basis: 'marginal' })
    m.pricing!.reasoning_effort_multipliers = { high: 1.5 }
    const wrapper = mountTable([m], null)
    expect(wrapper.findAll('[data-reasoning-effort]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('modelPlaza.table.marginalBadge')
  })

  it('切换具体分组和全部分组时同步更新表头和价格列', async () => {
    const m = tokenModel({
      official_pricing: { input_price: 4e-6, output_price: 2e-5, cache_write_price: null, cache_read_price: null }
    })
    const wrapper = mountTable([m], group())
    expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(4)
    expect(wrapper.findAll('[data-site-price]')).toHaveLength(4)
    expect(wrapper.get('[data-site-price]').text()).toBe('$1.50')

    await wrapper.setProps({ group: null })
    expect(wrapper.get('thead').text()).not.toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(4)
    expect(wrapper.find('[data-site-credit]').exists()).toBe(false)
    expect(wrapper.text()).toContain('$4.00')

    await wrapper.setProps({ group: group() })
    expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(4)
    expect(wrapper.findAll('[data-site-price]')).toHaveLength(4)
    expect(wrapper.get('[data-site-price]').text()).toBe('$1.50')
  })

  it('未采用阶梯计费的模型直接显示按生效倍率折算的本站价', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    expect(wrapper.get('[data-site-price]').text()).toBe('$1.50')
    expect(wrapper.get('[data-reference-price]').text()).toContain('$3.00')
  })

  it('用户专属倍率优先于分组倍率', () => {
    // 输入 $3.00：专属 0.2x → $0.60，分组 0.5x → $1.50。
    const text = mountTable(
      [tokenModel()],
      group({ rate_multiplier: 0.5, user_rate_multiplier: 0.2 })
    ).text()
    expect(text).toContain('$0.60')
    expect(text).not.toContain('$1.50')
  })

  it('固定价显示实付价与本站配置的基础参考价', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1e-5, cache_write_price: null, cache_read_price: null } })
    m.pricing!.input_price = 5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    const input = wrapper.get('tbody td')
    expect(input.get('[data-site-price]').text()).toBe('$2.50')
    expect(input.get('[data-reference-price]').text()).toContain('$5.00')
    expect(input.find('[data-site-credit], [data-reference-cny], [data-price-difference], [data-comparison]').exists()).toBe(false)
  })

  it('固定价不随官方参考价改变展示形式', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1e-5, cache_write_price: null, cache_read_price: null } })
    m.pricing!.input_price = 20.1e-6
    m.pricing!.output_price = 72e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 1 }))
    const cells = wrapper.get('tbody tr').findAll('td')
    expect(cells[0].get('[data-site-price]').text()).toBe('$20.10')
    expect(cells[1].get('[data-site-price]').text()).toBe('$72.00')
    expect(wrapper.find('[data-price-difference], [data-comparison]').exists()).toBe(false)
  })

  it('固定价缺参考价仍可展示，零倍率显示零元本站价', () => {
    const missing = mountTable([tokenModel()], group())
    expect(missing.findAll('[data-price-difference]')).toHaveLength(0)
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const free = mountTable([m], group({ user_rate_multiplier: 0 }))
    expect(free.get('[data-site-price]').text()).toBe('$0.00')
    expect(free.find('[data-price-difference]').exists()).toBe(false)
  })

  it('极低倍率的本站固定价仍保留实际金额', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const wrapper = mountTable([m], group({ rate_multiplier: 0.000001 }))
    expect(wrapper.get('[data-site-price]').text()).toBe('$0.000003')
    expect(wrapper.find('[data-price-difference]').exists()).toBe(false)
  })

  it('本站固定价保留小数精度', () => {
    const m = tokenModel({ official_pricing: { input_price: 0.01e-6, output_price: null, cache_write_price: null, cache_read_price: null } })
    m.pricing!.input_price = 0.0603e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 1 }))
    expect(wrapper.get('[data-site-price]').text()).toBe('$0.0603')
  })
})

describe('PlazaVendorPriceTable 长上下文阶梯', () => {
  const tiered = tokenModel({
    pricing: {
      billing_mode: 'token',
      input_price: 3e-6,
      output_price: 1.5e-5,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: [
        { min_tokens: 0, max_tokens: 200_000, input_price: 3e-6, output_price: 1.5e-5,
          cache_write_price: null, cache_read_price: null, per_request_price: null },
        { min_tokens: 200_000, max_tokens: null, input_price: 6e-6, output_price: 2.25e-5,
          cache_write_price: null, cache_read_price: null, per_request_price: null }
      ]
    }
  })

  it('逐档展示每档单价与档位标签', () => {
    const text = mountTable([tiered]).text()
    expect(text).toContain('≤200K')
    expect(text).toContain('>200K')
    expect(text).toContain('$3.00')
    expect(text).toContain('$6.00')
    expect(text).toContain('$22.50')
  })

  it('本站实际阶梯各占一行，合并模型名称并只显示该档实付价', () => {
    const m = { ...tiered, official_pricing: {
      input_price: 4e-6, output_price: 2e-5, cache_read_price: null, cache_write_price: null,
      intervals: [
        { min_tokens: 0, max_tokens: 200000, input_price: 4e-6, output_price: 2e-5,
          cache_read_price: null, cache_write_price: null, per_request_price: null },
        { min_tokens: 200000, max_tokens: null, input_price: 8e-6, output_price: 3e-5,
          cache_read_price: null, cache_write_price: null, per_request_price: null }
      ]
    } }
    const wrapper = mountTable([m])
    expect(wrapper.findAll('[data-price-field=input_price] [data-site-price]').map(price => price.text())).toEqual(['$3.00', '$6.00'])
    expect(wrapper.get('[data-official-reference]').text()).toContain('$4.00')
    expect(wrapper.findAll('[data-price-tier]').map(tier => tier.text())).toEqual(['≤200K', '>200K'])
    expect(wrapper.get('tbody th.pz-sticky').attributes('rowspan')).toBe('2')
    expect(wrapper.find('[data-price-details-button]').exists()).toBe(false)
  })

  it('本站阶梯每档把配置参考置顶并按同一档官方价计算蓝底节省', () => {
    const m = { ...tiered, pricing_source: 'group' as PlazaPricingSource,
      official_pricing: {
        input_price: 4e-6, output_price: 2e-5, cache_read_price: null, cache_write_price: null,
        intervals: [
          { min_tokens: 0, max_tokens: 200000, input_price: 4e-6, output_price: 2e-5,
            cache_read_price: null, cache_write_price: null, per_request_price: null },
          { min_tokens: 200000, max_tokens: null, input_price: 8e-6, output_price: 3e-5,
            cache_read_price: null, cache_write_price: null, per_request_price: null }
        ]
      }
    }
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    const rows = wrapper.findAll('[data-price-field=input_price]')
    expect(rows).toHaveLength(2)
    expect(rows.map(row => row.get('[data-reference-price]').text())).toEqual([
      expect.stringContaining('$3.00'), expect.stringContaining('$6.00')
    ])
    expect(rows.map(row => row.get('[data-site-price]').text())).toEqual(['$1.50', '$3.00'])
    expect(rows.map(row => row.get('[data-savings]').text())).toEqual([
      expect.stringContaining('¥25.30'), expect.stringContaining('¥50.60')
    ])
    expect(rows.every(row => row.get('[data-savings]').attributes('data-savings-reference') === 'official')).toBe(true)
  })

  it('官方阶梯边界不会增加主表的本站计费档位', () => {
    const m = { ...tiered, official_pricing: {
      input_price: 4e-6, output_price: 2e-5, cache_read_price: null, cache_write_price: null,
      intervals: [
        { min_tokens: 0, max_tokens: 100000, input_price: 4e-6, output_price: 2e-5,
          cache_read_price: null, cache_write_price: null, per_request_price: null },
        { min_tokens: 100000, max_tokens: null, input_price: 8e-6, output_price: 3e-5,
          cache_read_price: null, cache_write_price: null, per_request_price: null }
      ]
    } }
    const wrapper = mountTable([m])
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.findAll('[data-price-tier]').map(tier => tier.text())).toEqual(['≤200K', '>200K'])
    expect(wrapper.text()).not.toContain('100K')
  })

  it('单档模型不显示阶梯标签', () => {
    expect(mountTable([tokenModel()]).text()).not.toContain('modelPlaza.priceTable.tiered')
  })

  it('本站固定价直接展示一次，不受官方阶梯和官方缓存档位影响', () => {
    const m = tokenModel({ name: 'gpt-6.1-terra', platform: 'openai', pricing_source: 'official',
      official_pricing: {
        input_price: 1e-6, output_price: 5e-6, cache_read_price: 0.05e-6, cache_write_price: 1.25e-6,
        intervals: [
          { min_tokens: 0, max_tokens: 272000, tier_label: '≤272K', input_price: 1e-6, output_price: 5e-6,
            cache_read_price: 0.05e-6, cache_write_price: 1.25e-6, cache_write_12h_price: 2.5e-6, per_request_price: null },
          { min_tokens: 272000, max_tokens: null, tier_label: '>272K', input_price: 2e-6, output_price: 7.5e-6,
            cache_read_price: 0.1e-6, cache_write_price: 2.5e-6, cache_write_12h_price: 5e-6, per_request_price: null }
        ]
      }
    })
    m.pricing!.input_price = 1e-6
    m.pricing!.output_price = 5e-6
    m.pricing!.cache_read_price = 0.05e-6
    m.pricing!.cache_write_price = 1.25e-6
    m.pricing!.cache_write_12h_price = 2.5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.09, long_context_pricing_enabled: false }))
    const cells = wrapper.get('tbody tr').findAll('td')
    expect(cells.map(cell => cell.findAll('[data-site-price]').map(price => price.text())))
      .toEqual([['$0.09'], ['$0.45'], ['$0.0045'], ['$0.1125', '$0.225']])
    expect(wrapper.find('[data-reference-cny], [data-price-difference]').exists()).toBe(false)
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[data-price-tier]').exists()).toBe(false)
    expect(cells[0].get('[data-reference-price]').text()).toContain('$1.00')
    expect(cells[0].get('[data-reference-tier]').text()).toContain('≤272K')
    expect(cells[0].get('[data-savings]').text()).toContain('98.66%')
    expect(cells[0].get('[data-savings]').text()).toContain('¥6.61')
    expect(cells[3].findAll('[data-reference-price]').map(price => price.text()))
      .toEqual([expect.stringContaining('$1.25'), expect.stringContaining('$2.50')])
    expect(wrapper.findAll('[data-cache-lifetime]').map(tier => tier.text())).toEqual(['12h'])
  })

  it('超出部分计价规则直接显示在模型名称下方', () => {
    const wrapper = mountTable([tokenModel({ long_context_basis: 'marginal' })])
    const rule = wrapper.get('tbody th.pz-sticky [data-marginal-pricing]')
    expect(rule.text()).toBe('modelPlaza.table.tierHintMarginal')
    expect(rule.classes()).toContain('text-violet-700')
  })
})

describe('PlazaVendorPriceTable 缓存与思考等级', () => {
  it('每个实际档位展示本站基础参考与实付，缺少官方价时不计算节省', () => {
    const m = tokenModel({ name: 'gpt-6.1-terra', platform: 'openai' })
    m.pricing!.intervals = [1, 2].map((factor, index) => ({
      min_tokens: index ? 272000 : 0, max_tokens: index ? null : 272000,
      input_price: factor * 10e-6, output_price: factor * 50e-6,
      cache_read_price: factor * 1e-6, cache_write_price: factor * 12.5e-6,
      cache_write_1h_price: factor * 12.5e-6, per_request_price: null
    }))
    const wrapper = mountTable([m], group({ rate_multiplier: 1.28 }))
    const rows = wrapper.findAll('tbody tr')
    expect(rows.map(row => row.get('[data-price-tier]').text())).toEqual(['≤272K', '>272K'])
    expect(rows[0].findAll('[data-site-price]').map(price => price.text())).toEqual(['$12.80', '$64.00', '$1.28', '$16.00'])
    expect(rows[0].findAll('[data-reference-price]').map(price => price.text())).toEqual(['$10.00', '$50.00', '$1.00', '$12.50'].map(price => `modelPlaza.priceTable.referenceLabel${price}`))
    expect(rows[1].get('[data-site-price]').text()).toBe('$25.60')
    expect(rows[1].get('[data-reference-price]').text()).toContain('$20.00')
    expect(wrapper.find('[data-savings]').exists()).toBe(false)
  })

  it.each([false, true])('keeps 12h cache prices distinct in the live vendor table (intervals=%s)', (intervals) => {
    const m = tokenModel({ name: 'gpt-6-luna', platform: 'openai' })
    m.pricing!.cache_write_price = null
    m.pricing!.cache_write_12h_price = 2e-6
    m.official_pricing = { input_price: 1e-6, output_price: 5e-6, cache_read_price: null, cache_write_price: null, cache_write_12h_price: 4e-6 }
    if (intervals) {
      m.pricing!.intervals = [{ min_tokens: 0, max_tokens: null, input_price: null, output_price: null, cache_read_price: null, cache_write_price: null, cache_write_12h_price: 2e-6, per_request_price: null }]
      m.official_pricing.intervals = [{ min_tokens: 0, max_tokens: null, input_price: 1e-6, output_price: 5e-6, cache_read_price: null, cache_write_price: null, cache_write_12h_price: 4e-6, per_request_price: null }]
    }
    const wrapper = mountTable([m])
    const twelveHour = wrapper.findAll('[data-price-field=cache_write_price] > div').find(pair => pair.find('[data-cache-lifetime]').exists() && pair.get('[data-cache-lifetime]').text() === '12h')!
    expect(twelveHour.get('[data-site-price]').text()).toBe('$2.00')
    expect(twelveHour.get('[data-official-reference]').text()).toContain('$4.00')
    expect(wrapper.text()).not.toContain('1h')
  })
  it('支持 5m/1h 分档时显示 1h 缓存写入价', () => {
    const m = tokenModel()
    m.pricing!.cache_write_1h_price = 6e-6
    const cache = mountTable([m]).get('tbody tr').findAll('td')[3]
    const oneHour = cache.findAll('[data-cache-lifetime]').find(label => label.text() === '1h')!.element.parentElement!
    expect(oneHour.querySelector('[data-site-price]')?.textContent).toBe('$6.00')
  })

  it('1h 价与 5m 价相同时不重复显示', () => {
    const m = tokenModel({
      official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: 0, cache_write_1h_price: 0, cache_read_price: null }
    })
    m.pricing!.cache_write_price = 0
    m.pricing!.cache_write_1h_price = 0
    expect(mountTable([m]).text()).not.toContain('1h')
  })

  it('模型名称下仅显示配置了正倍率的思考等级', () => {
    const m = tokenModel()
    m.pricing!.reasoning_effort_multipliers = { high: 1.5, low: 0, medium: null } as never
    const wrapper = mountTable([m, tokenModel({ name: 'claude-haiku-5-5' })])
    const rows = wrapper.findAll('tbody')
    const rule = rows[0].get('th.pz-sticky [data-reasoning-pricing]')
    expect(rule.classes()).toContain('text-blue-700')
    expect(rule.findAll('[data-reasoning-effort]').map(e => e.attributes('data-reasoning-effort'))).toEqual(['high'])
    expect(rule.text()).toContain('1.5')
    expect(rows[1].find('[data-pricing-rules]').exists()).toBe(false)
  })
})

describe('PlazaVendorPriceTable 币种', () => {
  const cnyModel = () =>
    tokenModel({ name: 'glm-5', platform: 'zhipu', pricing_source: 'channel', pricing_rules: { currency: 'CNY', context_status: 'flat', context_tier_count: 1, context_basis: 'whole_request', time_status: 'uniform', price_basis: 'standard' } })

  it('具体分组主表按模型厂商显示美元或人民币，并保持倍率后的金额', () => {
    const foreign = tokenModel({ name: 'gpt-6.1-terra', platform: 'openai' })
    const domestic = cnyModel()
    const image = tokenModel({ name: 'gpt-image-2.5-flare', platform: 'openai' })
    image.pricing!.billing_mode = 'image'
    image.pricing!.per_request_price = 0.2
    const wrapper = mountTable([foreign, domestic, image], group({ rate_multiplier: 0.5 }))
    const rows = wrapper.findAll('tbody')
    expect(rows[0].get('[data-site-price]').text()).toBe('$1.50')
    expect(rows[1].get('[data-site-price]').text()).toBe('¥1.50')
    expect(rows[2].get('[data-site-price]').text()).toBe('$0.10modelPlaza.table.perUnitImage')
  })

  it('全部分组的国产官方价来自独立参考，本站配置在所选分组生效', async () => {
    const m = { ...cnyModel(), official_pricing: { input_price: 2e-6, output_price: 8e-6, cache_read_price: null, cache_write_price: null, currency: 'CNY' as const } }
    const wrapper = mountTable([m], null)
    try {
      expect(wrapper.get('[data-price-field=input_price]').text()).toBe('¥2.00')
      await wrapper.setProps({ group: group({ rate_multiplier: 0.5 }) })
      expect(wrapper.get('[data-price-field=input_price] [data-site-price]').text()).toBe('¥1.50')
      expect(wrapper.get('[data-price-field=input_price] [data-reference-price]').text()).toContain('¥3.00')
      expect(wrapper.get('[data-price-field=input_price] [data-savings]').text()).toContain('¥0.50')
      await wrapper.setProps({ group: group({ id: 2 }) })
      expect(document.body.querySelector('[data-model-price-details]')).toBeNull()
    } finally { wrapper.unmount() }
  })

  it('国产模型的实付单价直接使用人民币符号', () => {
    const wrapper = mountTable([cnyModel()])
    const text = wrapper.text()
    expect(text).toContain('¥3.00')
    expect(wrapper.get('[data-price-currency]').attributes('data-currency')).toBe('CNY')
    expect(wrapper.find('[data-exchange-basis]').exists()).toBe(false)
  })

  it('国产模型来源为美元官方时仍按美元额度换算实付人民币', () => {
    const wrapper = mountTable([cnyModel()].map((m) => ({ ...m, pricing_source: 'official' as PlazaPricingSource })))
    expect(wrapper.get('[data-site-price]').text()).toBe('¥3.00')
    expect(wrapper.find('[data-site-credit]').exists()).toBe(false)
    expect(wrapper.get('[data-price-currency]').attributes('data-currency')).toBe('CNY')
  })

  it('非国产模型主表显示美元单价', () => {
    const wrapper = mountTable([tokenModel()])
    expect(wrapper.get('[data-site-price]').text()).toBe('$3.00')
    expect(wrapper.find('[data-site-credit], [data-exchange-basis]').exists()).toBe(false)
  })

  it('国产模型的本站阶梯按实际档位各显示一行', () => {
    const m = cnyModel()
    m.pricing!.intervals = [
      { min_tokens: 0, max_tokens: 32_000, input_price: 2e-6, output_price: 8e-6,
        cache_write_price: null, cache_read_price: null, per_request_price: null },
      { min_tokens: 32_000, max_tokens: null, input_price: 4e-6, output_price: 1.6e-5,
        cache_write_price: null, cache_read_price: null, per_request_price: null }
    ]
    const text = mountTable([m]).text()
    expect(text.match(/≤32K/g)).toHaveLength(1)
    expect(text.match(/>32K/g)).toHaveLength(1)
    expect(text).toContain('¥2.00')
    expect(text).toContain('¥4.00')
    expect(text).not.toContain('modelPlaza.comparison.configuredReference')
    expect(text).not.toContain('modelPlaza.comparison.officialReference')
  })
})

describe('PlazaVendorPriceTable 展示精度', () => {
  it('长小数最多保留 5 位有效数字', () => {
    const m = tokenModel()
    m.pricing!.output_price = 3.911111111e-6
    const text = mountTable([m]).text()
    expect(text).toContain('$3.9111')
    expect(text).not.toContain('3.91111')
  })

  it('整数部分不截断，仍保留两位小数', () => {
    const m = tokenModel()
    m.pricing!.output_price = 1.2345678e-3
    // 5 位有效数字会得到 1234.6；整数部分 4 位时放宽到保留两位小数
    expect(mountTable([m]).text()).toContain('$1234.57')
  })
})

describe('PlazaVendorPriceTable 分时倍率', () => {
  it('全部分组不展示仅影响本站价格的分时时段', () => {
    const m = tokenModel({
      time_pricing: {
        timezone: 'Asia/Shanghai',
        periods: [{ start_time: '00:00:00', end_time: '08:00:00', multiplier: 0.5 }]
      },
      official_pricing: { input_price: 4e-6, output_price: 2e-5, cache_write_price: null, cache_read_price: null }
    })
    const wrapper = mountTable([m], null)
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('00:00–08:00')
    expect(wrapper.text()).not.toContain('×0.5')
    expect(wrapper.text()).toContain('$4.00')
  })

  it('分时时段通过标记详情展示，单价保持标准时段价', async () => {
    const m = tokenModel({ pricing_rules: { currency: 'USD', context_status: 'flat', context_tier_count: 1,
      context_basis: 'whole_request', price_basis: 'standard', time_status: 'scheduled', time_rules: [{
        source: 'channel', timezone: 'Asia/Shanghai', weekdays_only: true, base_multiplier: 1,
        fields: ['input_price','output_price'], periods: [{ start_time: '09:00', end_time: '12:00', multiplier: 2 }, { start_time: '14:00', end_time: '18:00', multiplier: 2 }]
      }] } })
    const wrapper = mountTable([m])
    expect(wrapper.get('[data-site-price]').text()).toBe('$3.00')
    expect(wrapper.get('[data-time-rule]').attributes('data-status')).toBe('scheduled')
    expect(wrapper.find('[data-time-pricing]').exists()).toBe(false)
    await wrapper.get('[data-time-rule]').trigger('click')
    const details = document.body.querySelector('[data-pricing-rule-details]')!
    expect(details.textContent).toContain('Asia/Shanghai')
    expect(details.textContent).toContain('09:00–12:00')
    expect(details.textContent).toContain('14:00–18:00')
    wrapper.unmount()
  })

  it('分组高峰规则只显示在本站范围，切换官方参考后隐藏未知时段标记', async () => {
    const m = tokenModel({ pricing_rules: { currency: 'USD', context_status: 'flat', context_tier_count: 1,
      context_basis: 'whole_request', price_basis: 'standard', time_status: 'scheduled', time_rules: [{ source: 'group', timezone: 'Asia/Shanghai', base_multiplier: 1,
      fields: ['input_price'], periods: [{ start_time: '09:00', end_time: '18:00', multiplier: 2 }] }] } })
    const wrapper = mountTable([m])
    expect(wrapper.get('[data-time-rule]').attributes('data-status')).toBe('scheduled')
    await wrapper.setProps({ group: null })
    expect(wrapper.find('[data-time-rule]').exists()).toBe(false)
    expect(wrapper.find('[data-pricing-rules]').exists()).toBe(false)
    wrapper.unmount()
  })

})

describe('PlazaVendorPriceTable 按张与按秒计费', () => {
  const videoModel = (overrides: Partial<PlazaModel> = {}): PlazaModel => ({
    name: 'grok-imagine-video-1.5',
    platform: 'grok',
    pricing: {
      billing_mode: 'video',
      input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
      image_input_price: null, image_output_price: null, per_request_price: null,
      intervals: [
        { min_tokens: 0, max_tokens: null, tier_label: '480P', input_price: null, output_price: null,
          cache_write_price: null, cache_read_price: null, per_request_price: 0.08 },
        { min_tokens: 0, max_tokens: null, tier_label: '720P', input_price: null, output_price: null,
          cache_write_price: null, cache_read_price: null, per_request_price: 0.14 }
      ]
    },
    pricing_source: 'official',
    official_pricing: {
      billing_mode: 'video',
      input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
      intervals: [
        { min_tokens: 0, max_tokens: null, tier_label: '480P', input_price: null, output_price: null,
          cache_write_price: null, cache_read_price: null, per_request_price: 0.08 },
        { min_tokens: 0, max_tokens: null, tier_label: '720P', input_price: null, output_price: null,
          cache_write_price: null, cache_read_price: null, per_request_price: 0.14 }
      ]
    },
    ...overrides
  })

  it('全部分组按秒展示视频的官方分辨率单价，不当作阶梯', () => {
    const wrapper = mountTable([videoModel()], null)
    const text = wrapper.get('tbody').text()
    expect(wrapper.findAll('[data-price-tier]').map(tier => tier.text())).toEqual(['480P', '720P'])
    expect(text).toContain('$0.08modelPlaza.table.perUnitSecond')
    expect(text).toContain('$0.14modelPlaza.table.perUnitSecond')
    expect(text).not.toContain('modelPlaza.priceTable.tiered')
  })

  it('选中分组后视频各分辨率显示实付单价', () => {
    const pairs = mountTable([videoModel()], group({ rate_multiplier: 0.5 })).findAll('tbody tr')
    expect(pairs[0].get('[data-price-tier]').text()).toBe('480P')
    expect(pairs[0].get('[data-site-price]').text()).toBe('$0.04modelPlaza.table.perUnitSecond')
    expect(pairs[0].get('[data-reference-price]').text()).toContain('$0.08')
    expect(pairs[0].get('[data-reference-price]').text()).toContain('modelPlaza.table.perUnitSecond')
    expect(pairs[0].get('[data-official-reference]').text()).toContain('$0.08')
    expect(pairs[1].get('[data-price-tier]').text()).toBe('720P')
    expect(pairs[1].get('[data-site-price]').text()).toBe('$0.07modelPlaza.table.perUnitSecond')
  })

  it('视频独立倍率覆盖分组倍率', () => {
    const g = group({ rate_multiplier: 0.5, video_rate_independent: true, video_rate_multiplier: 2 })
    expect(mountTable([videoModel()], g).get('[data-site-price]').text()).toBe('$0.16modelPlaza.table.perUnitSecond')
  })

  it('图片按张独立定价直接展示实付，不与官方按 Token 价比较', () => {
    const m = tokenModel({ name: 'gpt-image-2.5-flare', platform: 'openai', pricing_source: 'group',
      official_pricing: { input_price: 5e-6, output_price: 30e-6, cache_read_price: null, cache_write_price: null } })
    m.pricing!.billing_mode = 'image'
    m.pricing!.per_request_price = 0.2
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5, image_rate_independent: true, image_rate_multiplier: 0.25 }))
    try {
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      expect(wrapper.find('[data-price-field]').exists()).toBe(false)
      expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.unitPrice')
      expect(wrapper.get('thead').text()).not.toContain('modelPlaza.table.input')
      expect(wrapper.get('thead').text()).not.toContain('modelPlaza.table.output')
      expect(wrapper.get('[data-site-price]').text()).toBe('$0.05modelPlaza.table.perUnitImage')
      expect(wrapper.text()).not.toContain('单独定价')
      expect(wrapper.get('[data-reference-price]').text()).toContain('$0.20')
      expect(wrapper.find('[data-savings]').exists()).toBe(false)
      expect(wrapper.find('[data-model-price-details], [data-price-difference]').exists()).toBe(false)
    } finally { wrapper.unmount() }
  })

  it('图片模型的官方单价按张展示', () => {
    const m = videoModel({ name: 'grok-imagine-image', pricing: null })
    m.official_pricing = { billing_mode: 'image', input_price: null, output_price: null,
      cache_write_price: null, cache_read_price: null, per_request_price: 0.02 }
    const text = mountTable([m], null).get('tbody tr').text()
    expect(text).toContain('$0.02modelPlaza.table.perUnitImage')
  })
})

describe('PlazaVendorPriceTable 暂未定价', () => {
  const unpriced = () => tokenModel({ name: 'grok-voice-tts', platform: 'grok', pricing: null, pricing_source: undefined, official_pricing: null })

  it.each([false, true])('本站与官方都没有价格时合并成一格说明，隐藏空详情（空价格配置=%s）', (emptyPricing) => {
    const model = unpriced()
    if (emptyPricing) model.pricing = { ...tokenModel().pricing!, input_price: null, output_price: null, cache_read_price: null, cache_write_price: null }
    const all = mountTable([model], null).get('tbody tr')
    expect(all.findAll('td')).toHaveLength(1)
    expect(all.get('[data-unpriced]').attributes('colspan')).toBe('4')
    expect(all.text()).toContain('modelPlaza.priceTable.unpriced')

    const scoped = mountTable([model], group()).get('tbody tr')
    expect(scoped.findAll('td')).toHaveLength(1)
    expect(scoped.get('[data-unpriced]').attributes('colspan')).toBe('4')
    expect(scoped.find('[data-price-details-button]').exists()).toBe(false)
  })

  it('只缺官方价时仍显示本站价', () => {
    const wrapper = mountTable([tokenModel()], group({ rate_multiplier: 1 }))
    expect(wrapper.find('[data-unpriced]').exists()).toBe(false)
    expect(wrapper.get('[data-site-price]').text()).toBe('$3.00')
  })
})

describe('PlazaVendorPriceTable 窄屏', () => {
  it('模型名一列固定在左侧', () => {
    const wrapper = mountTable([tokenModel()])
    expect(wrapper.get('thead th').classes()).toContain('pz-sticky')
    expect(wrapper.get('tbody th').classes()).toContain('pz-sticky')
  })
})
