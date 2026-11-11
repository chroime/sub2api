import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlazaVendorPriceTable from '../PlazaVendorPriceTable.vue'
import type { ModelPlazaGroup, PlazaModel, PlazaPricingSource } from '@/api/modelPlaza'

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

describe('PlazaVendorPriceTable 本站价与官方价', () => {
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
  })

  it('全部分组缺少官方价格时显示空值，不冒充本站价格', () => {
    const wrapper = mountTable([tokenModel()], null)
    expect(wrapper.text()).not.toContain('$3.00')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(1)
    expect(wrapper.get('tbody td').attributes('colspan')).toBe('4')
    expect(wrapper.get('tbody td').text()).toBe('-')
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
    expect(wrapper.text()).toContain('modelPlaza.priceTable.tiered')
    const input = wrapper.get('tbody tr').findAll('td')[0].text()
    expect(input).toContain('≤200K$4.00')
    expect(input).toContain('>200K$8.00')
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
    expect(wrapper.get('[data-site-price]').text()).toBe('¥1.50')

    await wrapper.setProps({ group: null })
    expect(wrapper.get('thead').text()).not.toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(4)
    expect(wrapper.find('[data-site-credit]').exists()).toBe(false)
    expect(wrapper.text()).toContain('$4.00')

    await wrapper.setProps({ group: group() })
    expect(wrapper.get('thead').text()).toContain('modelPlaza.priceTable.sitePrice')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(4)
    expect(wrapper.findAll('[data-site-price]')).toHaveLength(4)
    expect(wrapper.get('[data-site-price]').text()).toBe('¥1.50')
  })

  it('选中分组后本站价按生效倍率折算，官方价不受影响', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const text = mountTable([m], group({ rate_multiplier: 0.5 })).text()
    // 本站扣除 $1.50 额度，按 ¥1/$1 充值相当于 ¥1.50；官方仍标原始美元价。
    expect(text).toContain('¥1.50')
    expect(text).toContain('$3.00')
  })

  it('用户专属倍率优先于分组倍率', () => {
    // 输入 $3.00：专属 0.2x → $0.60，分组 0.5x → $1.50。
    const text = mountTable(
      [tokenModel()],
      group({ rate_multiplier: 0.5, user_rate_multiplier: 0.2 })
    ).text()
    expect(text).toContain('¥0.60')
    expect(text).not.toContain('$1.50')
  })

  it('每个项目对比真实折后金额，不能把分组倍率当作官方折扣', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1e-5, cache_write_price: null, cache_read_price: null } })
    m.pricing!.input_price = 5e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 0.5 }))
    const input = wrapper.get('tbody td')
    expect(input.get('[data-reference-price]').text()).toBe('$3.00')
    expect(input.get('[data-reference-cny]').text()).toBe('≈ ¥20.10')
    expect(input.get('[data-site-price]').text()).toBe('¥2.50')
    expect(input.get('[data-price-difference]').text()).toContain('¥17.60')
    expect(input.get('[data-price-difference]').text()).toContain('87.56%')
    expect(input.get('[data-price-difference]').text()).not.toContain('50%')
  })

  it('同价和比官方更贵时如实显示，不展示节省', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1e-5, cache_write_price: null, cache_read_price: null } })
    m.pricing!.input_price = 20.1e-6
    m.pricing!.output_price = 72e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 1 }))
    const cells = wrapper.get('tbody tr').findAll('td')
    expect(cells[0].get('[data-comparison]').attributes('data-comparison')).toBe('equal')
    expect(cells[1].get('[data-comparison]').attributes('data-comparison')).toBe('higher')
    expect(cells[1].get('[data-price-difference]').text()).toContain('modelPlaza.comparison.extraAmount')
    expect(cells[1].get('[data-price-difference]').text()).toContain('¥5.00')
    expect(cells[1].find('[data-reference-price].line-through').exists()).toBe(false)
  })

  it('缺参考价不展示优惠，明确零元本站价可省100%', () => {
    const missing = mountTable([tokenModel()], group())
    expect(missing.findAll('[data-price-difference]')).toHaveLength(0)
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const free = mountTable([m], group({ user_rate_multiplier: 0 }))
    expect(free.get('[data-site-price]').text()).toBe('¥0.00')
    expect(free.get('[data-price-difference]').text()).toContain('100%')
  })

  it('正的本站价格不会因百分比舍入展示为100%优惠', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const wrapper = mountTable([m], group({ rate_multiplier: 0.000001 }))
    expect(wrapper.get('[data-price-difference]').text()).toContain('99.99%')
    expect(wrapper.get('[data-price-difference]').text()).not.toContain('100%')
  })

  it('整百分比优惠不会因浮点噪音少显示0.01%', () => {
    const m = tokenModel({ official_pricing: { input_price: 0.01e-6, output_price: null, cache_write_price: null, cache_read_price: null } })
    // Official $0.01 is ¥0.067; ¥0.0603 paid is exactly 10% less.
    m.pricing!.input_price = 0.0603e-6
    const wrapper = mountTable([m], group({ rate_multiplier: 1 }))
    expect(wrapper.get('[data-price-difference]').text()).toContain('10%')
    expect(wrapper.get('[data-price-difference]').text()).not.toContain('9.99%')
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

  it('单档模型不显示阶梯标签', () => {
    expect(mountTable([tokenModel()]).text()).not.toContain('modelPlaza.priceTable.tiered')
  })

  it('marginal 基准显示超出部分计价徽章', () => {
    const text = mountTable([tokenModel({ long_context_basis: 'marginal' })]).text()
    expect(text).toContain('modelPlaza.table.marginalBadge')
  })
})

describe('PlazaVendorPriceTable 缓存与思考等级', () => {
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
    const twelveHour = wrapper.findAll('.plaza-price-pair').find(pair => pair.find('[data-price-tier]').exists() && pair.get('[data-price-tier]').text().includes('12h'))!
    expect(twelveHour.get('[data-site-price]').text()).toBe('¥2.00')
    expect(twelveHour.get('[data-reference-price]').text()).toBe('$4.00')
    expect(wrapper.text()).not.toContain('1h')
  })
  it('支持 5m/1h 分档时显示 1h 缓存写入价', () => {
    const m = tokenModel()
    m.pricing!.cache_write_1h_price = 6e-6
    const cache = mountTable([m]).get('tbody tr').findAll('td')[3]
    const oneHour = cache.findAll('.plaza-price-pair').find((pair) => pair.find('[data-price-tier]').exists() && pair.get('[data-price-tier]').text().includes('1h'))!
    expect(oneHour.get('[data-site-price]').text()).toBe('¥6.00')
  })

  it('1h 价与 5m 价相同时不重复显示', () => {
    const m = tokenModel({
      official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: 0, cache_write_1h_price: 0, cache_read_price: null }
    })
    m.pricing!.cache_write_price = 0
    m.pricing!.cache_write_1h_price = 0
    expect(mountTable([m]).text()).not.toContain('1h')
  })

  it('只列出配置了正倍率的思考等级', () => {
    const m = tokenModel()
    m.pricing!.reasoning_effort_multipliers = { high: 1.5, low: 0, medium: null } as never
    const wrapper = mountTable([m])
    const efforts = wrapper.findAll('[data-reasoning-effort]').map((e) => e.attributes('data-reasoning-effort'))
    expect(efforts).toEqual(['high'])
  })
})

describe('PlazaVendorPriceTable 币种', () => {
  const cnyModel = () =>
    tokenModel({ name: 'glm-5', platform: 'zhipu', pricing_source: 'channel' })

  it('国产模型渠道价显示人民币并带 CNY 标记（本站价与官方价同为 ¥）', () => {
    const wrapper = mountTable([cnyModel()])
    const text = wrapper.text()
    expect(text).toContain('¥3.00')
    expect(text).toContain('CNY')
    expect(wrapper.find('[data-exchange-basis]').exists()).toBe(false)
  })

  it('国产模型来源为美元官方时仍按美元额度换算实付人民币', () => {
    const wrapper = mountTable([cnyModel()].map((m) => ({ ...m, pricing_source: 'official' as PlazaPricingSource })))
    expect(wrapper.get('[data-site-price]').text()).toBe('¥3.00')
    expect(wrapper.get('[data-site-credit]').text()).toContain('$3.00')
    expect(wrapper.text()).not.toContain('CNY')
  })

  it('非国产模型本站实付显示人民币，美元额度独立标注', () => {
    const wrapper = mountTable([tokenModel()])
    expect(wrapper.get('[data-site-price]').text()).toBe('¥3.00')
    expect(wrapper.get('[data-site-credit]').text()).toContain('$3.00')
    expect(wrapper.get('[data-exchange-basis]').text()).toContain('6.7')
  })

  it('国产模型的官方价阶梯与本站价一致', () => {
    const m = cnyModel()
    m.pricing!.intervals = [
      { min_tokens: 0, max_tokens: 32_000, input_price: 2e-6, output_price: 8e-6,
        cache_write_price: null, cache_read_price: null, per_request_price: null },
      { min_tokens: 32_000, max_tokens: null, input_price: 4e-6, output_price: 1.6e-5,
        cache_write_price: null, cache_read_price: null, per_request_price: null }
    ]
    const text = mountTable([m]).text()
    // 每项单价的参考与折后价共用相同档位标签。
    expect(text.match(/≤32K/g)).toHaveLength(4)
    expect(text.match(/>32K/g)).toHaveLength(4)
    expect(text).toContain('modelPlaza.comparison.configuredReference')
    expect(text).not.toContain('modelPlaza.comparison.officialReference')
  })
})

describe('PlazaVendorPriceTable 展示精度', () => {
  it('长小数最多保留 5 位有效数字', () => {
    const m = tokenModel()
    m.pricing!.output_price = 3.911111111e-6
    const text = mountTable([m]).text()
    expect(text).toContain('¥3.9111')
    expect(text).not.toContain('3.91111')
  })

  it('整数部分不截断，仍保留两位小数', () => {
    const m = tokenModel()
    m.pricing!.output_price = 1.2345678e-3
    // 5 位有效数字会得到 1234.6；整数部分 4 位时放宽到保留两位小数
    expect(mountTable([m]).text()).toContain('¥1234.57')
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

  it('时段与倍率显示在模型名下方，不再拆成单独的行', () => {
    const m = tokenModel({
      time_pricing: {
        timezone: 'Asia/Shanghai',
        weekdays_only: true,
        periods: [
          { start_time: '09:00:00', end_time: '12:00:00', multiplier: 2 },
          { start_time: '14:00:00', end_time: '18:00:00', multiplier: 2 }
        ]
      }
    })
    const wrapper = mountTable([m])
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    const periods = wrapper.get('[data-time-pricing]')
    expect(periods.text()).toContain('modelPlaza.table.timePricingWeekdays')
    expect(periods.text()).toContain('09:00–12:00×2')
    expect(periods.text()).toContain('14:00–18:00×2')
    expect(periods.attributes('title')).toContain('modelPlaza.table.timePricingRowHintWeekdays')
    // 价格列只展示标准时段价，不再按时段倍率另算一行
    expect(wrapper.get('[data-site-price]').text()).toBe('¥3.00')
    expect(wrapper.text()).not.toContain('$6.00')
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
    const text = wrapper.get('tbody tr').text()
    expect(text).toContain('modelPlaza.table.perSecond')
    expect(text).toContain('480P $0.08modelPlaza.table.perUnitSecond')
    expect(text).toContain('720P $0.14modelPlaza.table.perUnitSecond')
    expect(text).not.toContain('modelPlaza.priceTable.tiered')
  })

  it('选中分组后本站价按视频单价乘倍率，官方价不变', () => {
    const pairs = mountTable([videoModel()], group({ rate_multiplier: 0.5 })).findAll('.plaza-price-pair')
    expect(pairs[0].get('[data-price-tier]').text()).toBe('480P')
    expect(pairs[0].get('[data-site-price]').text()).toBe('¥0.04modelPlaza.table.perUnitSecond')
    expect(pairs[0].get('[data-reference-price]').text()).toBe('$0.08modelPlaza.table.perUnitSecond')
    expect(pairs[1].get('[data-price-tier]').text()).toBe('720P')
    expect(pairs[1].get('[data-site-price]').text()).toBe('¥0.07modelPlaza.table.perUnitSecond')
  })

  it('视频独立倍率覆盖分组倍率', () => {
    const g = group({ rate_multiplier: 0.5, video_rate_independent: true, video_rate_multiplier: 2 })
    expect(mountTable([videoModel()], g).get('[data-site-price]').text()).toBe('¥0.16modelPlaza.table.perUnitSecond')
  })

  it('图片模型的官方单价按张展示', () => {
    const m = videoModel({ name: 'grok-imagine-image', pricing: null })
    m.official_pricing = { billing_mode: 'image', input_price: null, output_price: null,
      cache_write_price: null, cache_read_price: null, per_request_price: 0.02 }
    const text = mountTable([m], null).get('tbody tr').text()
    expect(text).toContain('modelPlaza.table.perImage')
    expect(text).toContain('$0.02modelPlaza.table.perUnitImage')
  })
})

describe('PlazaVendorPriceTable 暂未定价', () => {
  const unpriced = () => tokenModel({ name: 'grok-voice-tts', platform: 'grok', pricing: null, pricing_source: undefined, official_pricing: null })

  it('本站与官方都没有价格时合并成一格说明', () => {
    const all = mountTable([unpriced()], null).get('tbody tr')
    expect(all.findAll('td')).toHaveLength(1)
    expect(all.get('[data-unpriced]').attributes('colspan')).toBe('4')
    expect(all.text()).toContain('modelPlaza.priceTable.unpriced')

    const scoped = mountTable([unpriced()], group()).get('tbody tr')
    expect(scoped.findAll('td')).toHaveLength(1)
    expect(scoped.get('[data-unpriced]').attributes('colspan')).toBe('4')
  })

  it('只缺官方价时仍显示本站价', () => {
    const wrapper = mountTable([tokenModel()], group({ rate_multiplier: 1 }))
    expect(wrapper.find('[data-unpriced]').exists()).toBe(false)
    expect(wrapper.get('[data-site-price]').text()).toBe('¥3.00')
  })
})

describe('PlazaVendorPriceTable 窄屏', () => {
  it('模型名一列固定在左侧', () => {
    const wrapper = mountTable([tokenModel()])
    expect(wrapper.get('thead th').classes()).toContain('pz-sticky')
    expect(wrapper.get('tbody th').classes()).toContain('pz-sticky')
  })
})
