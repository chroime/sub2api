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

  it('全部分组合并通过不同平台提供的同名模型', () => {
    const groups = [group(1, [model('grok-4', 'grok', 1e-6), model('grok-4', 'openai', 1e-6)])]
    expect(collectPlazaModels(groups, 'all')).toHaveLength(1)
  })

  it.each([
    ['deepseek-v4.1-flash', 'deepseek'],
    ['glm-5.3', 'zhipu'],
    ['kimi-k3.1-pro', 'kimi'],
    ['minimax-m3.1-flash', 'minimax']
  ])('国模全套与独立分组的 %s 只展示一次，仍按定价来源取价', (name, platform) => {
    const compatible = model(name, 'openai', 2e-6, 'official')
    const native = model(name, platform, 3e-6, 'channel')
    const groups = [group(1, [compatible]), group(2, [native])]

    expect(collectPlazaModels(groups, 'all')).toEqual([native])
    expect(collectPlazaModels(groups, 1)).toEqual([compatible])
    expect(collectPlazaModels(groups, 2)).toEqual([native])
  })

  it('跨平台合并使用目录且保留不同版本与模型 ID，不修改分组数据', () => {
    const compatible = model('kimi-k2.7', 'openai', 1e-6, 'channel')
    const native = model('kimi-k2.7', 'kimi', 2e-6, 'group')
    const newer = model('kimi-k3.1-pro', 'kimi', 3e-6, 'channel')
    const groups: ModelPlazaGroup[] = [
      { ...group(1, []), catalog_models: [compatible] },
      { ...group(2, [native, newer]), catalog_models: [native, newer] }
    ]

    expect(collectPlazaModels(groups, 'all')).toEqual([native, newer])
    expect(collectPlazaModels(groups, 1)).toEqual([])
    expect(collectPlazaModels(groups, 2)).toEqual([native, newer])
    expect(groups[0].catalog_models).toEqual([compatible])
  })

  it('全部分组使用目录并保持来源去重，具体分组只使用实际可用模型', () => {
    const available = model('gpt-live', 'openai', 1e-6, 'channel')
    const official = model('kimi-catalog', 'openai', 2e-6, 'official')
    const configured = model('kimi-catalog', 'openai', 3e-6, 'channel')
    const groups: ModelPlazaGroup[] = [
      { ...group(1, [available]), catalog_models: [official] },
      { ...group(2, []), catalog_models: [configured] }
    ]
    expect(collectPlazaModels(groups, 'all')).toEqual([configured])
    expect(collectPlazaModels(groups, 1)).toEqual([available])
    expect(collectPlazaModels(groups, 2)).toEqual([])
    expect(collectPlazaModels(groups, 'all')).toEqual([configured])
    expect(groups[0].catalog_models).toEqual([official])
    expect(groups[0].models).toEqual([available])
  })

  it('显式空目录不回落到实际模型，旧响应缺少目录时仍使用 models', () => {
    const available = model('gpt-live', 'openai', 1e-6, 'channel')
    const legacy = model('claude-legacy', 'anthropic', 3e-6, 'channel')
    const groups: ModelPlazaGroup[] = [
      { ...group(1, [available]), catalog_models: [] },
      group(2, [legacy])
    ]
    expect(collectPlazaModels(groups, 'all')).toEqual([legacy])
    expect(collectPlazaModels(groups, 1)).toEqual([available])
    expect(collectPlazaModels(groups, 2)).toEqual([legacy])
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
