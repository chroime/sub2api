import type { ModelPlazaGroup, PlazaModel, PlazaOfficialPricing } from '@/api/modelPlaza'
import type { UserPricingInterval, UserSupportedModelPricing } from '@/api/channels'
import { BILLING_MODE_IMAGE, BILLING_MODE_TOKEN, BILLING_MODE_VIDEO, type BillingMode } from '@/constants/channel'
import { isDomesticModelVendor, modelVendorOf } from '@/utils/modelVendor'
import { resolveIntervalPrices } from '@/utils/pricing'

export type TokenPriceField = 'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price'
// official-matched attaches official quotes without adding or splitting site tiers.
export type PlazaReferenceSource = 'auto' | 'official' | 'none' | 'official-matched'

/** Reference exchange rate for the plaza's CNY cost comparison. */
export const PLAZA_REFERENCE_USD_TO_CNY = 6.7
/** One CNY purchases one USD of site credit. */
export const PLAZA_CNY_PER_USD_CREDIT = 1

type PlazaCurrency = 'USD' | 'CNY' | ''

export interface PlazaPriceComparison {
  key: string
  label: string
  /** Display-unit amounts: per 1M tokens, image, second or request. */
  reference: number | null
  site: number | null
  configured?: number | null
  rate?: number
  currency: PlazaCurrency
  /** Raw currency of each side; differs while comparing a legacy USD quote with CNY site pricing. */
  referenceCurrency?: PlazaCurrency
  siteCurrency?: PlazaCurrency
  referenceKind: 'official' | 'configured'
  /** Official scope when a flat site price is compared with the official base tier. */
  referenceLabel?: string
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

export interface PlazaPayableComparison {
  reference: number | null
  site: number | null
  currency: 'CNY'
  converted: boolean
  comparable: boolean
}

function amount(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}

function scaled(value: number | null | undefined, multiplier: number, unit = 1): number | null {
  const valid = amount(value)
  return valid === null || amount(multiplier) === null ? null : amount(valid * multiplier * unit)
}

/** Normalize real CNY costs after the quote's unit and effective rate are resolved. */
export function getPlazaPayableComparison(price: PlazaPriceComparison): PlazaPayableComparison {
  const referenceCurrency = price.referenceCurrency ?? price.currency
  const siteCurrency = price.siteCurrency ?? price.currency
  const referenceConverted = referenceCurrency === 'USD'
  const siteConverted = siteCurrency === 'USD'
  const reference = !referenceCurrency ? null : referenceConverted ? scaled(price.reference, PLAZA_REFERENCE_USD_TO_CNY) : amount(price.reference)
  const site = !siteCurrency ? null : siteConverted ? scaled(price.site, PLAZA_CNY_PER_USD_CREDIT) : amount(price.site)
  return {
    reference, site, currency: 'CNY', converted: referenceConverted || siteConverted,
    comparable: price.comparable && reference !== null && site !== null
  }
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

function quoteCurrencies(model: PlazaModel, configured: boolean) {
  const normalize = (currency: string | undefined): PlazaCurrency => currency === 'USD' || currency === 'CNY' ? currency : ''
  const official = normalize(model.official_pricing_rules ? model.official_pricing_rules.currency : model.official_pricing?.currency ?? 'USD')
  const site = model.pricing_rules ? normalize(model.pricing_rules.currency)
    : isDomesticModelVendor(modelVendorOf(model.name, model.platform)) ? 'CNY' : 'USD'
  return { siteCurrency: site, referenceCurrency: configured ? site : official }
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
type TokenValues = Pick<UserPricingInterval, TokenPriceField | 'cache_write_1h_price' | 'cache_write_12h_price'>
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

/** A displayed site tier can only have one reference if coverage is complete and uniform. */
function uniformReference(ranges: TokenRange[], min: number, max: number, field: keyof TokenValues): number | null {
  const bounds = [...new Set([min, max, ...ranges.flatMap(range => [range.min, range.max])
    .filter(bound => bound > min && bound < max)])].sort((a, b) => a - b)
  let result: number | null = null
  for (let index = 0; index < bounds.length - 1; index++) {
    const value = amount(rangeValue(ranges, bounds[index], bounds[index + 1])?.[field])
    if (value === null || (result !== null && value !== result)) return null
    result = value
  }
  return scaled(result, 1, 1_000_000)
}

function officialBaseRange(ranges: TokenRange[]): TokenRange | undefined {
  // Only a complete, unambiguous official schedule has a safe base tier.
  if (ranges.length < 2 || ranges[0].min !== 0 || ranges[ranges.length - 1].max !== Infinity) return undefined
  if (ranges.some((range, index) => index > 0 && range.min !== ranges[index - 1].max)) return undefined
  return ranges[0]
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
export function getTokenComparisons(model: PlazaModel, group: ModelPlazaGroup | null, field: TokenPriceField, referenceSource: PlazaReferenceSource = 'auto', includeEqualCache1h = false): PlazaPriceComparison[] {
  const configured = referenceSource === 'auto' && configuredCny(model)
  const { siteCurrency, referenceCurrency } = quoteCurrencies(model, configured)
  const matchSite = referenceSource === 'official-matched' && group !== null
  const siteRanges = group ? tokenRanges(model.pricing, true) : []
  const referenceRanges = referenceSource === 'none' ? [] : tokenRanges(configured ? model.pricing : model.official_pricing, configured)
  const ranges = matchSite ? siteRanges : [...siteRanges, ...referenceRanges]
  const bounds = [...new Set([0, Infinity, ...ranges.flatMap(range => [range.min, range.max])])].sort((a, b) => a - b)
  const rate = effectivePlazaRate(model, group)
  const entries: PlazaPriceComparison[] = []
  for (let index = 0; index < bounds.length - 1; index++) {
    const min = bounds[index], max = bounds[index + 1]
    const site = rangeValue(siteRanges, min, max)
    const reference = rangeValue(referenceRanges, min, max)
    // Don't add an empty artificial segment after a finite-only schedule.
    if (!site && !reference && ranges.length > 0) continue
    const { label: comparisonLabel, compatible } = comparisonRangeLabel(siteRanges, referenceRanges, min, max)
    const label = matchSite ? comparisonRangeLabel(siteRanges, [], min, max).label : comparisonLabel
    const baseReference = matchSite && siteRanges.length === 1 && min === 0 && max === Infinity
      ? officialBaseRange(referenceRanges) : undefined
    const entry = (priceField: TokenPriceField | 'cache_write_1h_price' | 'cache_write_12h_price', ttl?: '1h' | '12h'): PlazaPriceComparison => {
      let referenceAmount = matchSite
        ? (compatible ? uniformReference(referenceRanges, min, max, priceField) : null)
        : scaled(reference?.[priceField], 1, 1_000_000)
      let referenceLabel: string | undefined
      if (referenceAmount === null && compatible && baseReference) {
        referenceAmount = scaled(baseReference.values[priceField], 1, 1_000_000)
        if (referenceAmount !== null) referenceLabel = baseReference.label || tokenRangeLabel(baseReference.min, baseReference.max)
      }
      const siteAmount = group ? scaled(site?.[priceField], rate, 1_000_000) : null
      return {
        key: `${field}:${min}:${max}${ttl ? `:${ttl}` : ''}`,
        label: ttl ? [label, ttl].filter(Boolean).join(' · ') : label,
        reference: referenceAmount, site: siteAmount,
        configured: group ? scaled(site?.[priceField], 1, 1_000_000) : null, rate,
        currency: group ? siteCurrency : referenceCurrency,
        referenceCurrency, siteCurrency,
        referenceKind: configured ? 'configured' : 'official',
        ...(referenceLabel ? { referenceLabel } : {}),
        comparable: Boolean(referenceCurrency && siteCurrency) && referenceAmount !== null && siteAmount !== null && compatible && model.long_context_basis !== 'marginal',
        billingMode: BILLING_MODE_TOKEN,
        ...(ttl === '1h' ? { cache1h: true } : {})
      }
    }
    entries.push(entry(field))
    if (field === 'cache_write_price') {
      const differs = (values: TokenValues | null) => values?.cache_write_1h_price != null &&
        amount(values.cache_write_1h_price) !== null && (includeEqualCache1h || values.cache_write_1h_price !== values.cache_write_price)
      if (differs(site) || (!matchSite && differs(reference))) entries.push(entry('cache_write_1h_price', '1h'))
      // OpenAI's 12h cache is an independent lifetime, never an Anthropic 1h
      // substitute. Compare only explicitly available prices for this lifetime.
      if (amount(site?.cache_write_12h_price) !== null || (!matchSite && amount(reference?.cache_write_12h_price) !== null)) {
        entries.push(entry('cache_write_12h_price', '12h'))
      }
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
export function getUnitComparisons(model: PlazaModel, group: ModelPlazaGroup | null, referenceSource: PlazaReferenceSource = 'auto'): PlazaPriceComparison[] {
  const configured = referenceSource === 'auto' && configuredCny(model)
  const { siteCurrency, referenceCurrency } = quoteCurrencies(model, configured)
  const sitePricing = group ? model.pricing : null
  const referencePricing = referenceSource === 'none' ? null : configured ? model.pricing : model.official_pricing
  const siteMode = mode(sitePricing), referenceMode = mode(referencePricing)
  const siteQuotes = unitQuotes(sitePricing), referenceQuotes = unitQuotes(referencePricing)
  const rate = effectivePlazaRate(model, group)
  const currency = group ? siteCurrency : referenceCurrency
  const referenceKind = configured ? 'configured' as const : 'official' as const
  const entry = (label: string, reference: number | null, base: number | null, billingMode: BillingMode, key = label): PlazaPriceComparison => ({
    key: `${billingMode}:${key}`, label, reference, site: scaled(base, rate), configured: base, rate,
    currency, referenceCurrency, siteCurrency, referenceKind,
    comparable: Boolean(referenceCurrency && siteCurrency) && reference !== null && scaled(base, rate) !== null && siteMode === referenceMode,
    billingMode
  })
  if (referenceSource === 'official-matched' && group) {
    return siteQuotes.map((quote, index) => entry(
      quote.label, siteMode === referenceMode ? quoteForLabel(referenceQuotes, quote.label) : null,
      quote.price, siteMode, `site:${index}`
    ))
  }
  if (siteQuotes.length && referenceQuotes.length && siteMode !== referenceMode) {
    return [
      ...siteQuotes.map((quote, index) => entry(quote.label, null, quote.price, siteMode, `site:${index}`)),
      ...referenceQuotes.map((quote, index) => entry(quote.label, quote.price, null, referenceMode, `reference:${index}`))
    ]
  }
  if (!siteQuotes.length && !referenceQuotes.length) return []
  const labels = [...new Set([...siteQuotes, ...referenceQuotes].map(quote => quote.label).filter(Boolean))]
  if (!labels.length) labels.push('')
  return labels.map(label => entry(
    label,
    quoteForLabel(referenceQuotes, label),
    quoteForLabel(siteQuotes, label),
    siteQuotes.length ? siteMode : referenceMode
  ))
}
