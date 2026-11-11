import { describe, expect, it } from 'vitest'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import type { UserPricingInterval, UserSupportedModelPricing } from '@/api/channels'
import {
  comparePriceAmounts,
  effectivePlazaRate,
  getPlazaPayableComparison,
  getTokenComparisons,
  getUnitComparisons
} from '../plazaPriceComparison'

function price(overrides: Partial<UserSupportedModelPricing> = {}): UserSupportedModelPricing {
  return {
    billing_mode: 'token', input_price: 5e-6, output_price: 10e-6,
    cache_read_price: null, cache_write_price: null,
    image_input_price: null, image_output_price: null, per_request_price: null,
    intervals: [], ...overrides
  }
}

function model(overrides: Partial<PlazaModel> = {}): PlazaModel {
  return {
    name: 'gpt-6-sol', platform: 'openai', pricing_source: 'channel', pricing: price(),
    official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_read_price: null, cache_write_price: null },
    ...overrides
  }
}

function group(overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return {
    id: 1, name: 'test', description: '', platform: 'openai', subscription_type: 'standard',
    rate_multiplier: 0.5, is_exclusive: false,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    image_rate_independent: false, image_rate_multiplier: 1,
    video_rate_independent: false, video_rate_multiplier: 1,
    long_context_pricing_enabled: true, models: [], ...overrides
  }
}

function tier(min: number, max: number | null, input: number): UserPricingInterval {
  return {
    min_tokens: min, max_tokens: max, input_price: input, output_price: null,
    cache_read_price: null, cache_write_price: null, per_request_price: null
  }
}

describe('model plaza price comparisons', () => {
  it('compares the CNY cost of official USD and the actual configured site credits', () => {
    const [entry] = getTokenComparisons(model(), group(), 'input_price')
    expect(entry).toMatchObject({ reference: 3, site: 2.5, currency: 'USD', referenceKind: 'official', comparable: true })
    const payable = getPlazaPayableComparison(entry)
    expect(payable).toEqual({ reference: 20.1, site: 2.5, currency: 'CNY', converted: true, comparable: true })
    const result = comparePriceAmounts(payable.reference, payable.site)
    expect(result.status).toBe('saving')
    expect(result.difference).toBeCloseTo(17.6)
    expect(result.percent).toBeCloseTo(87.5621890547)
  })

  it('applies a personal rate once and preserves a zero override', () => {
    const m = model()
    expect(getTokenComparisons(m, group({ user_rate_multiplier: 0.2 }), 'input_price')[0].site).toBeCloseTo(1)
    expect(getTokenComparisons(m, group({ user_rate_multiplier: 0 }), 'input_price')[0].site).toBe(0)
    expect(effectivePlazaRate(m, group({ rate_multiplier: -1 }))).toBeNaN()
    expect(getTokenComparisons(m, group({ rate_multiplier: NaN }), 'input_price')[0].site).toBeNull()
  })

  it('keeps the baseline standard-time amount when extra scheduling or reasoning multipliers exist', () => {
    const m = model({
      time_pricing: { timezone: 'Asia/Shanghai', periods: [{ start_time: '00:00', end_time: '23:59', multiplier: 2 }] },
      pricing: price({ reasoning_effort_multipliers: { high: 3 } })
    })
    expect(getTokenComparisons(m, group({ peak_rate_enabled: true, peak_rate_multiplier: 4 }), 'input_price')[0].site).toBe(2.5)
  })

  it('calls domestic configured CNY a configured reference without mixing the official USD quote', () => {
    const m = model({ name: 'glm-5', platform: 'zhipu' })
    const [entry] = getTokenComparisons(m, group(), 'input_price')
    expect(entry).toMatchObject({ reference: 5, site: 2.5, currency: 'CNY', referenceKind: 'configured', comparable: true })
    expect(getTokenComparisons({ ...m, pricing_source: 'official' }, group(), 'input_price')[0])
      .toMatchObject({ reference: 3, currency: 'USD', referenceKind: 'official' })
  })

  it('splits unequal token tiers at shared boundaries instead of pairing array indexes', () => {
    const m = model({ pricing: price({ intervals: [tier(0, 100_000, 2e-6), tier(100_000, null, 8e-6)] }) })
    m.official_pricing!.intervals = [tier(0, 200_000, 4e-6), tier(200_000, null, 10e-6)]
    const rows = getTokenComparisons(m, group(), 'input_price')
    expect(rows.map(row => [row.label, row.reference, row.site])).toEqual([
      ['≤100K', 4, 1], ['>100K–200K', 4, 4], ['>200K', 10, 4]
    ])
    expect(rows.every(row => row.comparable)).toBe(true)
  })

  it('compares a flat site price against each official tier when group tier pricing is off', () => {
    const m = model()
    m.official_pricing!.intervals = [tier(0, 200_000, 3e-6), tier(200_000, null, 6e-6)]
    expect(getTokenComparisons(m, group({ long_context_pricing_enabled: false }), 'input_price')
      .map(row => [row.reference, row.site])).toEqual([[3, 2.5], [6, 2.5]])
  })

  it('preserves explicit Grok boundary labels when the compared intervals match exactly', () => {
    const m = model({ name: 'grok-4.7-build-fast', platform: 'grok', pricing: price({ intervals: [
      { ...tier(0, 200_000, 2e-6), tier_label: '<200K' },
      { ...tier(200_000, null, 4e-6), tier_label: '≥200K' }
    ] }) })
    m.official_pricing!.intervals = [
      { ...tier(0, 200_000, 3e-6), tier_label: '<200K' },
      { ...tier(200_000, null, 6e-6), tier_label: '≥200K' }
    ]
    const entries = getTokenComparisons(m, group(), 'input_price')
    expect(entries.map(entry => entry.label)).toEqual(['<200K', '≥200K'])
    expect(entries.every(entry => entry.comparable)).toBe(true)

    // A flat site price should still retain the reference schedule's labels.
    m.pricing = price()
    expect(getTokenComparisons(m, group(), 'input_price').map(entry => entry.label)).toEqual(['<200K', '≥200K'])
  })

  it('preserves conflicting exact boundary labels without claiming comparable savings', () => {
    const m = model({ pricing: price({ intervals: [{ ...tier(0, 200_000, 2e-6), tier_label: '≤200K' }] }) })
    m.official_pricing!.intervals = [{ ...tier(0, 200_000, 3e-6), tier_label: '<200K' }]
    expect(getTokenComparisons(m, group(), 'input_price')[0])
      .toMatchObject({ label: '≤200K / <200K', reference: 3, site: 1, comparable: false })
  })

  it('retains an exact one-token segment between the actual 199999 and 200000 thresholds', () => {
    const m = model({ name: 'grok-4.7-build-fast', platform: 'grok', pricing: price({ intervals: [
      { ...tier(0, 200_000, 2e-6), tier_label: '≤200K' },
      { ...tier(200_000, null, 4e-6), tier_label: '>200K' }
    ] }) })
    m.official_pricing!.intervals = [
      { ...tier(0, 199_999, 3e-6), tier_label: '<200K' },
      { ...tier(199_999, null, 6e-6), tier_label: '≥200K' }
    ]
    const entries = getTokenComparisons(m, group(), 'input_price')
    expect(entries.map(entry => [entry.label, entry.reference, entry.site])).toEqual([
      ['<200K', 3, 1], ['>199.999K–200K', 6, 1], ['>200K', 6, 2]
    ])
    expect(entries[1].comparable).toBe(true)
    expect(comparePriceAmounts(entries[1].reference, entries[1].site).percent).toBeCloseTo(100 * 5 / 6)
  })

  it('resolves interval relative prices before applying the group multiplier', () => {
    const m = model({ pricing: price({ intervals: [{ ...tier(0, null, 0), input_price: null, input_multiplier: 2 }] }) })
    expect(getTokenComparisons(m, group(), 'input_price')[0].site).toBe(5)
  })

  it('does not compare a marginal schedule to a whole-request official schedule', () => {
    expect(getTokenComparisons(model({ long_context_basis: 'marginal' }), group(), 'input_price')[0])
      .toMatchObject({ site: 2.5, reference: 3, comparable: false })
  })

  it('keeps cache 1h separate and never compares it against a missing official 1h price', () => {
    const m = model({ pricing: price({ cache_write_price: 2e-6, cache_write_1h_price: 4e-6 }) })
    m.official_pricing!.cache_write_price = 3e-6
    const rows = getTokenComparisons(m, group(), 'cache_write_price')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({ reference: 3, site: 1, comparable: true })
    expect(rows[1]).toMatchObject({ label: '1h', reference: null, site: 2, comparable: false, cache1h: true })
    m.official_pricing!.cache_write_1h_price = 6e-6
    expect(getTokenComparisons(m, group(), 'cache_write_price')[1]).toMatchObject({ reference: 6, site: 2, comparable: true })
  })

  it('omits duplicate 1h prices and does not invent a zero from missing fields', () => {
    const m = model({ pricing: price({ cache_write_price: 2e-6, cache_write_1h_price: 2e-6 }) })
    expect(getTokenComparisons(m, group(), 'cache_write_price')).toHaveLength(1)
    expect(getTokenComparisons(m, group(), 'cache_read_price')[0]).toMatchObject({ reference: null, site: null, comparable: false })
    expect(getTokenComparisons(model({ pricing: null, official_pricing: null }), group(), 'input_price')[0])
      .toMatchObject({ reference: null, site: null, comparable: false })
  })
})

describe('CNY payable price comparisons', () => {
  it('normalizes output independently from input without changing the raw quote', () => {
    const [entry] = getTokenComparisons(model(), group(), 'output_price')
    const payable = getPlazaPayableComparison(entry)
    expect(payable).toEqual({ reference: 100.5, site: 5, currency: 'CNY', converted: true, comparable: true })
    const result = comparePriceAmounts(payable.reference, payable.site)
    expect(result.difference).toBeCloseTo(95.5)
    expect(result.percent).toBeCloseTo(95.0248756219)
    expect(entry).toMatchObject({ reference: 15, site: 5, currency: 'USD', comparable: true })
  })

  it('includes the credit purchase rate even when the configured USD price equals the official price', () => {
    const m = model({ pricing: price({ input_price: 3e-6 }) })
    const discounted = getPlazaPayableComparison(getTokenComparisons(m, group({ rate_multiplier: 0.15 }), 'input_price')[0])
    expect(discounted.reference).toBe(20.1)
    expect(discounted.site).toBeCloseTo(0.45)
    expect(comparePriceAmounts(discounted.reference, discounted.site).percent).toBeCloseTo(97.7611940299)

    const fullRate = getPlazaPayableComparison(getTokenComparisons(m, group({ rate_multiplier: 1 }), 'input_price')[0])
    expect(fullRate.site).toBe(3)
    expect(comparePriceAmounts(fullRate.reference, fullRate.site).percent).toBeCloseTo(85.0746268657)
  })

  it('does not apply personal or group rates a second time during conversion', () => {
    const discounted = getPlazaPayableComparison(getTokenComparisons(model(), group({ user_rate_multiplier: 0.2 }), 'input_price')[0])
    expect(discounted.site).toBeCloseTo(1)
    const free = getPlazaPayableComparison(getTokenComparisons(model(), group({ user_rate_multiplier: 0 }), 'input_price')[0])
    expect(free).toMatchObject({ reference: 20.1, site: 0, comparable: true })
    expect(comparePriceAmounts(free.reference, free.site)).toEqual({ status: 'saving', difference: 20.1, percent: 100 })
  })

  it('leaves configured domestic CNY amounts unchanged but converts its official USD source', () => {
    const m = model({ name: 'glm-5', platform: 'zhipu' })
    const configured = getPlazaPayableComparison(getTokenComparisons(m, group(), 'input_price')[0])
    expect(configured).toEqual({ reference: 5, site: 2.5, currency: 'CNY', converted: false, comparable: true })
    expect(comparePriceAmounts(configured.reference, configured.site).percent).toBe(50)
    const official = getPlazaPayableComparison(getTokenComparisons({ ...m, pricing_source: 'official' }, group(), 'input_price')[0])
    expect(official).toEqual({ reference: 20.1, site: 2.5, currency: 'CNY', converted: true, comparable: true })
  })

  it('converts aligned token tiers and separate cache durations without changing their basis', () => {
    const m = model({ pricing: price({ intervals: [tier(0, 100_000, 2e-6), tier(100_000, null, 8e-6)] }) })
    m.official_pricing!.intervals = [tier(0, 200_000, 4e-6), tier(200_000, null, 10e-6)]
    expect(getTokenComparisons(m, group(), 'input_price').map(getPlazaPayableComparison))
      .toEqual([
        { reference: 26.8, site: 1, currency: 'CNY', converted: true, comparable: true },
        { reference: 26.8, site: 4, currency: 'CNY', converted: true, comparable: true },
        { reference: 67, site: 4, currency: 'CNY', converted: true, comparable: true }
      ])

    const cache = model({ pricing: price({ cache_read_price: 0.5e-6, cache_write_price: 2e-6, cache_write_1h_price: 4e-6 }) })
    cache.official_pricing!.cache_read_price = 1e-6
    cache.official_pricing!.cache_write_price = 3e-6
    cache.official_pricing!.cache_write_1h_price = 6e-6
    expect(getPlazaPayableComparison(getTokenComparisons(cache, group(), 'cache_read_price')[0]))
      .toMatchObject({ reference: 6.7, site: 0.25, comparable: true })
    const writes = getTokenComparisons(cache, group(), 'cache_write_price').map(getPlazaPayableComparison)
    expect(writes).toHaveLength(2)
    expect(writes[0]).toMatchObject({ reference: 20.1, site: 1, comparable: true })
    expect(writes[1]).toMatchObject({ reference: 40.2, site: 2, comparable: true })
  })

  it('preserves missing prices and incompatible token schedules instead of inventing discounts', () => {
    const marginal = getPlazaPayableComparison(getTokenComparisons(model({ long_context_basis: 'marginal' }), group(), 'input_price')[0])
    expect(marginal).toMatchObject({ reference: 20.1, site: 2.5, comparable: false })
    const noOfficial = getPlazaPayableComparison(getTokenComparisons(model({ official_pricing: null }), group(), 'input_price')[0])
    expect(noOfficial).toMatchObject({ reference: null, site: 2.5, comparable: false })
    const noGroup = getPlazaPayableComparison(getTokenComparisons(model(), null, 'input_price')[0])
    expect(noGroup).toMatchObject({ reference: 20.1, site: null, comparable: false })
    const conflicting = model({ pricing: price({ intervals: [{ ...tier(0, 200_000, 2e-6), tier_label: '≤200K' }] }) })
    conflicting.official_pricing!.intervals = [{ ...tier(0, 200_000, 3e-6), tier_label: '<200K' }]
    expect(getPlazaPayableComparison(getTokenComparisons(conflicting, group(), 'input_price')[0]).comparable).toBe(false)
  })

  it.each([null, -1, NaN, Infinity])('keeps invalid amount %s unavailable after conversion', value => {
    const [entry] = getTokenComparisons(model(), group(), 'input_price')
    expect(getPlazaPayableComparison({ ...entry, reference: value })).toMatchObject({ reference: null, site: 2.5, comparable: false })
    expect(getPlazaPayableComparison({ ...entry, site: value })).toMatchObject({ reference: 20.1, site: null, comparable: false })
    expect(getPlazaPayableComparison({ ...entry, currency: 'CNY', reference: value })).toMatchObject({ reference: null, comparable: false })
  })

  it('rejects conversion overflow and preserves zero, equality and tiny positive payable amounts', () => {
    const [entry] = getTokenComparisons(model(), group(), 'input_price')
    expect(getPlazaPayableComparison({ ...entry, reference: Number.MAX_VALUE }))
      .toMatchObject({ reference: null, comparable: false })
    const zero = getPlazaPayableComparison({ ...entry, reference: 0, site: 0 })
    expect(comparePriceAmounts(zero.reference, zero.site)).toEqual({ status: 'equal', difference: 0, percent: null })
    const equal = getPlazaPayableComparison({ ...entry, reference: 0.1, site: 0.67 })
    expect(comparePriceAmounts(equal.reference, equal.site).status).toBe('equal')
    const higher = getPlazaPayableComparison({ ...entry, reference: 0.1, site: 0.68 })
    expect(comparePriceAmounts(higher.reference, higher.site).status).toBe('higher')
    const tiny = getPlazaPayableComparison({ ...entry, reference: 1, site: 1e-20 })
    expect(tiny.site).toBeGreaterThan(0)
    expect(comparePriceAmounts(tiny.reference, tiny.site).percent).toBeLessThan(100)
  })
})

describe('media comparison units and rates', () => {
  const video = () => model({
    name: 'grok-imagine-video-1.5', platform: 'grok',
    pricing: price({ billing_mode: 'video', per_request_price: null, intervals: [
      { ...tier(0, null, 0), tier_label: '720P', per_request_price: 0.14 },
      { ...tier(0, null, 0), tier_label: '480P', per_request_price: 0.08 }
    ] }),
    official_pricing: { billing_mode: 'video', input_price: null, output_price: null, cache_read_price: null, cache_write_price: null, intervals: [
      { ...tier(0, null, 0), tier_label: '480P', per_request_price: 0.1 },
      { ...tier(0, null, 0), tier_label: '720P', per_request_price: 0.2 }
    ] }
  })

  it('matches the same resolution by label despite different ordering', () => {
    const rows = getUnitComparisons(video(), group())
    expect(rows.find(row => row.label === '480P')).toMatchObject({ reference: 0.1, site: 0.04, comparable: true, billingMode: 'video' })
    expect(rows.find(row => row.label === '720P')).toMatchObject({ reference: 0.2, site: 0.07, comparable: true })
  })

  it('converts USD video references once without mixing resolutions or applying token scaling', () => {
    const rows = getUnitComparisons(video(), group())
    const low = getPlazaPayableComparison(rows.find(row => row.label === '480P')!)
    const high = getPlazaPayableComparison(rows.find(row => row.label === '720P')!)
    expect(low).toMatchObject({ reference: 0.67, site: 0.04, comparable: true })
    expect(high).toMatchObject({ reference: 1.34, site: 0.07, comparable: true })
    const result = comparePriceAmounts(low.reference, low.site)
    expect(result.difference).toBeCloseTo(0.63)
    expect(result.percent).toBeCloseTo(94.0298507463)
    const independent = group({ user_rate_multiplier: 0.1, video_rate_independent: true, video_rate_multiplier: 2 })
    expect(getPlazaPayableComparison(getUnitComparisons(video(), independent).find(row => row.label === '480P')!).site).toBe(0.16)
  })

  it.each(['image', 'per_request'] as const)('retains the per-unit amount for %s quotes', billingMode => {
    const m = model({
      pricing: price({ billing_mode: billingMode, per_request_price: 0.2 }),
      official_pricing: { billing_mode: billingMode, input_price: null, output_price: null, cache_read_price: null, cache_write_price: null, per_request_price: 0.5 }
    })
    const [raw] = getUnitComparisons(m, group())
    expect(raw.billingMode).toBe(billingMode)
    expect(getPlazaPayableComparison(raw)).toEqual({ reference: 3.35, site: 0.1, currency: 'CNY', converted: true, comparable: true })
  })

  it('independent media rates replace personal and group rates', () => {
    const g = group({ user_rate_multiplier: 0.1, video_rate_independent: true, video_rate_multiplier: 2 })
    expect(getUnitComparisons(video(), g).find(row => row.label === '480P')!.site).toBe(0.16)
    const image = model({ pricing: price({ billing_mode: 'image', per_request_price: 0.1 }), official_pricing: null })
    expect(effectivePlazaRate(image, group({ user_rate_multiplier: 0.1, image_rate_independent: true, image_rate_multiplier: 0 }))).toBe(0)
  })

  it('expands an all-size flat site quote across official tiers without pairing unlike labels', () => {
    const m = video()
    m.pricing = price({ billing_mode: 'video', per_request_price: 0.2 })
    expect(getUnitComparisons(m, group()).map(row => [row.reference, row.site])).toEqual([[0.1, 0.1], [0.2, 0.1]])
    m.pricing = price({ billing_mode: 'video', intervals: [{ ...tier(0, null, 0), tier_label: '1080P', per_request_price: 0.3 }] })
    const rows = getUnitComparisons(m, group())
    expect(rows.find(row => row.label === '1080P')).toMatchObject({ reference: null, site: 0.15, comparable: false })
    expect(rows.find(row => row.label === '480P')).toMatchObject({ reference: 0.1, site: null, comparable: false })
    expect(rows.map(getPlazaPayableComparison).every(row => !row.comparable)).toBe(true)
  })

  it('keeps different billing modes in separate rows and never calculates savings between them', () => {
    const m = video()
    m.pricing!.billing_mode = 'per_request'
    const rows = getUnitComparisons(m, group())
    expect(rows.some(row => row.site !== null && row.billingMode === 'per_request')).toBe(true)
    expect(rows.some(row => row.reference !== null && row.billingMode === 'video')).toBe(true)
    expect(rows.every(row => !row.comparable)).toBe(true)
    expect(rows.map(getPlazaPayableComparison).every(row => !row.comparable)).toBe(true)
  })

  it('uses configured CNY media reference rather than relabeling USD official prices', () => {
    const m = video()
    m.name = 'cogvideo-test'; m.platform = 'zhipu'
    const row = getUnitComparisons(m, group()).find(entry => entry.label === '480P')!
    expect(row).toMatchObject({ currency: 'CNY', referenceKind: 'configured', reference: 0.08, site: 0.04 })
    expect(getPlazaPayableComparison(row)).toEqual({ reference: 0.08, site: 0.04, currency: 'CNY', converted: false, comparable: true })
  })
})

describe('numeric comparison guards', () => {
  it.each([null, undefined, -1, NaN, Infinity])('does not compare invalid price %s', value => {
    expect(comparePriceAmounts(value, 1).status).toBe('unavailable')
    expect(comparePriceAmounts(1, value).status).toBe('unavailable')
  })

  it('handles free site prices, zero reference and equal or higher site prices honestly', () => {
    expect(comparePriceAmounts(2, 0)).toEqual({ status: 'saving', difference: 2, percent: 100 })
    expect(comparePriceAmounts(0, 0)).toEqual({ status: 'equal', difference: 0, percent: null })
    expect(comparePriceAmounts(0, 2)).toEqual({ status: 'higher', difference: -2, percent: null })
    expect(comparePriceAmounts(2, 3)).toEqual({ status: 'higher', difference: -1, percent: -50 })
    expect(comparePriceAmounts(0.3, 0.1 + 0.2).status).toBe('equal')
  })

  it('preserves tiny positive site prices without claiming a 100% discount', () => {
    const result = comparePriceAmounts(1, 1e-20)
    expect(result.status).toBe('saving')
    expect(result.percent).toBeLessThan(100)
    expect(comparePriceAmounts(1e-20, 2e-20).status).toBe('higher')
  })
})
