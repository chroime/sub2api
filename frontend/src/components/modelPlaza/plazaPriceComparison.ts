import type { ModelPlazaGroup, PlazaModel, PlazaOfficialPricing } from '@/api/modelPlaza'
import type { UserPricingInterval, UserSupportedModelPricing } from '@/api/channels'
import { BILLING_MODE_IMAGE, BILLING_MODE_TOKEN, BILLING_MODE_VIDEO, type BillingMode } from '@/constants/channel'
import { isDomesticModelVendor, modelVendorOf } from '@/utils/modelVendor'
import { resolveIntervalPrices } from '@/utils/pricing'

export type TokenPriceField = 'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price'

export interface PlazaPriceComparison {
  key: string
  label: string
  /** Display-unit amounts: per 1M tokens, image, second or request. */
  reference: number | null
  site: number | null
  currency: 'USD' | 'CNY'
  referenceKind: 'official' | 'configured'
  comparable: boolean
  billingMode: BillingMode
  cache1h?: boolean
}

export interface PriceAmountComparison {
  status: 'saving' | 'equal' | 'higher' | 'unavailable'
  /** Positive when the site costs less, negative when it costs more. */
  difference: number | null
  /** Unrounded signed percentage; null when the reference is zero/unknown. */
  percent: number | null
}

function amount(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}

function scaled(value: number | null | undefined, multiplier: number, unit = 1): number | null {
  const valid = amount(value)
  return valid === null || amount(multiplier) === null ? null : amount(valid * multiplier * unit)
}

/** Compare raw amounts before presentation rounding; missing is never free. */
export function comparePriceAmounts(reference: number | null | undefined, site: number | null | undefined): PriceAmountComparison {
  const base = amount(reference), paid = amount(site)
  if (base === null || paid === null) return { status: 'unavailable', difference: null, percent: null }
  const difference = base - paid
  const tolerance = Math.max(base, paid) * 1e-10
  if (Math.abs(difference) <= tolerance) return { status: 'equal', difference: 0, percent: base > 0 ? 0 : null }
  let percent = base > 0 ? (difference / base) * 100 : null
  // Floating-point subtraction may round 1 - 1e-20 to 1. A positive price
  // still cannot claim 100% savings; UI should retain that distinction too.
  if (paid > 0 && percent !== null && percent >= 100) percent = 100 - Number.EPSILON * 100
  if (percent !== null && !Number.isFinite(percent)) percent = null
  return { status: difference > 0 ? 'saving' : 'higher', difference, percent }
}

function configuredCny(model: PlazaModel): boolean {
  return (model.pricing_source === 'group' || model.pricing_source === 'channel') &&
    isDomesticModelVendor(modelVendorOf(model.name, model.platform))
}

function mode(pricing: { billing_mode?: BillingMode } | null | undefined): BillingMode {
  return pricing?.billing_mode || BILLING_MODE_TOKEN
}

/** The API provides base prices. This is the sole group/personal multiplier. */
export function effectivePlazaRate(model: PlazaModel, group: ModelPlazaGroup | null): number {
  if (!group) return 1
  const billingMode = mode(model.pricing ?? model.official_pricing)
  let rate = group.user_rate_multiplier ?? group.rate_multiplier
  if (billingMode === BILLING_MODE_IMAGE && group.image_rate_independent) rate = group.image_rate_multiplier ?? 1
  if (billingMode === BILLING_MODE_VIDEO && group.video_rate_independent) rate = group.video_rate_multiplier ?? 1
  return amount(rate) ?? NaN
}

type PricingSource = UserSupportedModelPricing | PlazaOfficialPricing
type TokenValues = Pick<UserPricingInterval, TokenPriceField | 'cache_write_1h_price'>
interface TokenRange { min: number; max: number; label?: string; values: TokenValues }

function tokenRanges(pricing: PricingSource | null, resolveRelative: boolean): TokenRange[] {
  if (!pricing || mode(pricing) !== BILLING_MODE_TOKEN) return []
  if (!pricing.intervals?.length) return [{ min: 0, max: Infinity, values: pricing }]
  return pricing.intervals.flatMap(interval => {
    const min = amount(interval.min_tokens)
    const max = interval.max_tokens === null ? Infinity : amount(interval.max_tokens)
    if (min === null || max === null || max <= min) return []
    return [{ min, max, label: interval.tier_label?.trim(), values: resolveRelative ? resolveIntervalPrices(interval, pricing) : interval }]
  }).sort((a, b) => a.min - b.min)
}

function rangeValue(ranges: TokenRange[], min: number, max: number): TokenValues | null {
  const matches = ranges.filter(range => range.min <= min && range.max >= max)
  // Overlapping ranges do not provide an unambiguous comparative price.
  return matches.length === 1 ? matches[0].values : null
}

function tokenCount(value: number): string {
  // Boundaries are exact token counts, not prices: rounding 199999 to 200K
  // would hide the one-token interval created when two schedules differ.
  if (value >= 1_000_000) return `${value / 1_000_000}M`
  if (value >= 1_000) return `${value / 1_000}K`
  return String(value)
}

function tokenRangeLabel(min: number, max: number): string {
  if (min === 0 && max === Infinity) return ''
  if (max === Infinity) return `>${tokenCount(min)}`
  if (min === 0) return `≤${tokenCount(max)}`
  return `>${tokenCount(min)}–${tokenCount(max)}`
}

function comparisonRangeLabel(site: TokenRange[], reference: TokenRange[], min: number, max: number): { label: string; compatible: boolean } {
  const exactLabel = (ranges: TokenRange[]) => {
    const matches = ranges.filter(range => range.min === min && range.max === max)
    return matches.length === 1 ? matches[0].label : undefined
  }
  const siteLabel = exactLabel(site), referenceLabel = exactLabel(reference)
  // Explicit labels may distinguish < from ≤ even when API boundary numbers
  // match. Preserve that evidence rather than silently rewriting the boundary.
  if (siteLabel && referenceLabel && siteLabel !== referenceLabel) {
    return { label: `${siteLabel} / ${referenceLabel}`, compatible: false }
  }
  return { label: siteLabel || referenceLabel || tokenRangeLabel(min, max), compatible: true }
}

/**
 * Align the union of both schedules' boundaries. A flat price consequently
 * covers every tier on the other side, while gaps remain explicitly unknown.
 * Times of day, peak hours and reasoning multipliers are disclosed separately:
 * these figures intentionally remain standard-time unit prices.
 */
export function getTokenComparisons(model: PlazaModel, group: ModelPlazaGroup | null, field: TokenPriceField): PlazaPriceComparison[] {
  const configured = configuredCny(model)
  const siteRanges = group ? tokenRanges(model.pricing, true) : []
  const referenceRanges = tokenRanges(configured ? model.pricing : model.official_pricing, configured)
  const ranges = [...siteRanges, ...referenceRanges]
  const bounds = [...new Set([0, Infinity, ...ranges.flatMap(range => [range.min, range.max])])].sort((a, b) => a - b)
  const rate = effectivePlazaRate(model, group)
  const entries: PlazaPriceComparison[] = []
  for (let index = 0; index < bounds.length - 1; index++) {
    const min = bounds[index], max = bounds[index + 1]
    const site = rangeValue(siteRanges, min, max)
    const reference = rangeValue(referenceRanges, min, max)
    // Don't add an empty artificial segment after a finite-only schedule.
    if (!site && !reference && ranges.length > 0) continue
    const { label, compatible } = comparisonRangeLabel(siteRanges, referenceRanges, min, max)
    const entry = (priceField: TokenPriceField | 'cache_write_1h_price', cache1h = false): PlazaPriceComparison => {
      const referenceAmount = scaled(reference?.[priceField], 1, 1_000_000)
      const siteAmount = group ? scaled(site?.[priceField], rate, 1_000_000) : null
      return {
        key: `${field}:${min}:${max}${cache1h ? ':1h' : ''}`,
        label: cache1h ? [label, '1h'].filter(Boolean).join(' · ') : label,
        reference: referenceAmount, site: siteAmount,
        currency: configured ? 'CNY' : 'USD', referenceKind: configured ? 'configured' : 'official',
        comparable: referenceAmount !== null && siteAmount !== null && compatible && model.long_context_basis !== 'marginal',
        billingMode: BILLING_MODE_TOKEN,
        ...(cache1h ? { cache1h: true } : {})
      }
    }
    entries.push(entry(field))
    if (field === 'cache_write_price') {
      const differs = (values: TokenValues | null) => values?.cache_write_1h_price != null &&
        amount(values.cache_write_1h_price) !== null && values.cache_write_1h_price !== values.cache_write_price
      if (differs(site) || differs(reference)) entries.push(entry('cache_write_1h_price', true))
    }
  }
  return entries
}

interface UnitQuote { label: string; price: number | null }

function unitQuotes(pricing: PricingSource | null): UnitQuote[] {
  if (!pricing || mode(pricing) === BILLING_MODE_TOKEN) return []
  const tiers = (pricing.intervals ?? []).filter(interval => interval.per_request_price != null)
  if (tiers.length) return tiers.map(interval => ({ label: interval.tier_label?.trim() ?? '', price: amount(interval.per_request_price) }))
  return pricing.per_request_price == null ? [] : [{ label: '', price: amount(pricing.per_request_price) }]
}

function quoteForLabel(quotes: UnitQuote[], label: string): number | null {
  const matches = quotes.filter(quote => quote.label === label)
  if (matches.length === 1) return matches[0].price
  // A single unlabelled quote is the backend's all-size uniform rate.
  if (matches.length === 0 && quotes.length === 1 && quotes[0].label === '') return quotes[0].price
  return null
}

/** Match image sizes / video resolutions, never compare different units. */
export function getUnitComparisons(model: PlazaModel, group: ModelPlazaGroup | null): PlazaPriceComparison[] {
  const configured = configuredCny(model)
  const sitePricing = group ? model.pricing : null
  const referencePricing = configured ? model.pricing : model.official_pricing
  const siteMode = mode(sitePricing), referenceMode = mode(referencePricing)
  const siteQuotes = unitQuotes(sitePricing), referenceQuotes = unitQuotes(referencePricing)
  const rate = effectivePlazaRate(model, group)
  const currency = configured ? 'CNY' as const : 'USD' as const
  const referenceKind = configured ? 'configured' as const : 'official' as const
  const entry = (label: string, reference: number | null, site: number | null, billingMode: BillingMode, key = label): PlazaPriceComparison => ({
    key: `${billingMode}:${key}`, label, reference, site, currency, referenceKind,
    comparable: reference !== null && site !== null && siteMode === referenceMode,
    billingMode
  })
  if (siteQuotes.length && referenceQuotes.length && siteMode !== referenceMode) {
    return [
      ...siteQuotes.map((quote, index) => entry(quote.label, null, scaled(quote.price, rate), siteMode, `site:${index}`)),
      ...referenceQuotes.map((quote, index) => entry(quote.label, quote.price, null, referenceMode, `reference:${index}`))
    ]
  }
  if (!siteQuotes.length && !referenceQuotes.length) return []
  const labels = [...new Set([...siteQuotes, ...referenceQuotes].map(quote => quote.label).filter(Boolean))]
  if (!labels.length) labels.push('')
  return labels.map(label => entry(
    label,
    quoteForLabel(referenceQuotes, label),
    scaled(quoteForLabel(siteQuotes, label), rate),
    siteQuotes.length ? siteMode : referenceMode
  ))
}
