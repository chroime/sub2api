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

describe('PlazaVendorPriceTable 单价与实付价', () => {
  it('未选分组时只显示单价，不显示实付价', () => {
    const text = mountTable([tokenModel()]).text()
    expect(text).toContain('$3.00')
    expect(text).not.toContain('modelPlaza.priceTable.paidShort')
  })

  it('选中分组后按生效倍率显示实付价', () => {
    const text = mountTable([tokenModel()], group({ rate_multiplier: 0.5 })).text()
    expect(text).toContain('$3.00')
    expect(text).toContain('$1.50')
  })

  it('用户专属倍率优先于分组倍率', () => {
    // 输入 $3.00：专属 0.2x → $0.60，分组 0.5x → $1.50。
    // 断言取输入列，避开输出价折算后与其他数字重合。
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

  it('国产模型渠道价显示人民币并带 CNY 标记', () => {
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
