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

function mountTable(models: PlazaModel[], g: ModelPlazaGroup | null = null) {
  return mount(PlazaVendorPriceTable, {
    props: { models, group: g, caption: 'test' }
  })
}

describe('PlazaVendorPriceTable 本站价与官方价', () => {
  it('未选分组时本站价按 1x，等于单价', () => {
    const text = mountTable([tokenModel()]).text()
    expect(text).toContain('$3.00')
  })

  it('选中分组后本站价按生效倍率折算，官方价不受影响', () => {
    const m = tokenModel({ official_pricing: { input_price: 3e-6, output_price: 1.5e-5, cache_write_price: null, cache_read_price: null } })
    const text = mountTable([m], group({ rate_multiplier: 0.5 })).text()
    // 本站价：input $3.00 × 0.5 = $1.50；官方价仍为未折算的 $3.00。
    expect(text).toContain('$1.50')
    expect(text).toContain('$3.00')
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
  it('支持 5m/1h 分档时显示 1h 缓存写入价', () => {
    const m = tokenModel()
    m.pricing!.cache_write_1h_price = 6e-6
    expect(mountTable([m]).text()).toContain('1h $6.00')
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
    const text = mountTable([cnyModel()]).text()
    expect(text).toContain('¥3.00')
    expect(text).toContain('CNY')
  })

  it('国产模型来源为官方时仍按美元显示', () => {
    const text = mountTable([cnyModel()].map((m) => ({ ...m, pricing_source: 'official' as PlazaPricingSource })))
      .text()
    expect(text).toContain('$3.00')
    expect(text).not.toContain('¥')
  })

  it('非国产模型始终按美元显示', () => {
    expect(mountTable([tokenModel()]).text()).not.toContain('¥')
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
    // 档位标签只出现在输入列：本站价、官方价两侧各一次
    expect(text.match(/≤32K/g)).toHaveLength(2)
    expect(text.match(/>32K/g)).toHaveLength(2)
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
  it('每个时段单独一行，单价按时段倍率折算', () => {
    const m = tokenModel({
      time_pricing: {
        timezone: 'Asia/Shanghai',
        periods: [{ start_time: '00:00:00', end_time: '08:00:00', multiplier: 0.5 }]
      }
    })
    const wrapper = mountTable([m])
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    const text = wrapper.text()
    expect(text).toContain('00:00–08:00')
    expect(text).toContain('×0.5')
    expect(text).toContain('$1.50')
  })
})
