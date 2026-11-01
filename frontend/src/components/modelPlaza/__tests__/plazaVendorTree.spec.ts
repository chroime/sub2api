import { describe, expect, it } from 'vitest'
import { buildVendorTree, collectPlazaModels } from '../plazaVendorTree'
import type { ModelPlazaGroup, PlazaModel, PlazaPricingSource } from '@/api/modelPlaza'

function model(name: string, platform: string, input: number, source?: PlazaPricingSource): PlazaModel {
  return {
    name,
    platform,
    pricing: {
      billing_mode: 'token',
      input_price: input,
      output_price: input * 5,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: []
    },
    pricing_source: source,
    official_pricing: null
  }
}

function group(id: number, models: PlazaModel[]): ModelPlazaGroup {
  return {
    id,
    name: `g${id}`,
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
    long_context_pricing_enabled: true,
    models
  }
}

describe('collectPlazaModels', () => {
  it('跨分组去重时渠道定价覆盖官方目录价', () => {
    const groups = [
      group(1, [model('gpt-5', 'openai', 1e-6, 'official')]),
      group(2, [model('gpt-5', 'openai', 2e-6, 'channel')])
    ]
    const out = collectPlazaModels(groups, 'all')
    expect(out).toHaveLength(1)
    expect(out[0].pricing_source).toBe('channel')
    expect(out[0].pricing?.input_price).toBe(2e-6)
  })

  it('跨分组去重时分组定价优先于渠道定价，与单个分组口径一致', () => {
    const groups = [
      group(1, [model('gpt-5', 'openai', 2e-6, 'channel')]),
      group(2, [model('gpt-5', 'openai', 1e-6, 'group')])
    ]
    const out = collectPlazaModels(groups, 'all')
    expect(out).toHaveLength(1)
    expect(out[0].pricing_source).toBe('group')
    expect(out[0].pricing?.input_price).toBe(1e-6)
  })

  it('选中具体分组时只返回该分组的模型', () => {
    const groups = [
      group(1, [model('gpt-5', 'openai', 1e-6, 'official')]),
      group(2, [model('claude-sonnet', 'anthropic', 3e-6, 'channel')])
    ]
    const out = collectPlazaModels(groups, 2)
    expect(out.map((m) => m.name)).toEqual(['claude-sonnet'])
  })

  it('不同平台的同名模型不合并', () => {
    const groups = [group(1, [model('grok-4', 'grok', 1e-6), model('grok-4', 'openai', 1e-6)])]
    expect(collectPlazaModels(groups, 'all')).toHaveLength(2)
  })
})

describe('buildVendorTree', () => {
  const models = [
    model('gpt-5.9', 'openai', 1e-6),
    model('gpt-5.10', 'openai', 1e-6),
    model('claude-sonnet', 'anthropic', 3e-6),
    model('mystery-model', 'composite', 1e-6),
    model('house-model', 'anthropic', 1e-6)
  ]

  it('按厂商归类，平台兜底，未知厂商归入其他且排最后', () => {
    const tree = buildVendorTree(models, '', 'Other')
    expect(tree.map((n) => n.key)).toEqual(['claude', 'openai', 'other'])
    // house-model 名称识别不出，按 anthropic 平台兜底归入 Anthropic
    expect(tree[0].models.map((m) => m.name)).toContain('house-model')
    expect(tree[2].label).toBe('Other')
  })

  it('同厂商内版本号按数字降序', () => {
    const openai = buildVendorTree(models, '', 'Other').find((n) => n.key === 'openai')!
    expect(openai.models.map((m) => m.name)).toEqual(['gpt-5.10', 'gpt-5.9'])
  })

  it('搜索只保留命中的模型和厂商', () => {
    const tree = buildVendorTree(models, 'SONNET', 'Other')
    expect(tree).toHaveLength(1)
    expect(tree[0].models.map((m) => m.name)).toEqual(['claude-sonnet'])
  })
})
