import type { AdminUsageLog } from '@/types'

type RawCostUsage = Pick<AdminUsageLog, 'raw_cost_audit' | 'actual_cost'>
type Translate = (key: string) => string

const rawCostFormatter = new Intl.NumberFormat('en-US', {
  useGrouping: false,
  minimumFractionDigits: 8,
  maximumFractionDigits: 8,
})

export function getRawUsageCostAmount(row: RawCostUsage): number | null {
  const audit = row.raw_cost_audit
  if (!audit || !['recorded', 'calculated', 'estimated'].includes(audit.status)) return null
  // total_cost is the unamplified price of the upstream token buckets. Keep
  // amount as a compatibility fallback for older API responses.
  const value = audit.total_cost ?? audit.amount
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}

export function formatRawUsageCost(row: RawCostUsage): string {
  const amount = getRawUsageCostAmount(row)
  return amount === null ? '—' : rawCostFormatter.format(amount)
}

export function getRawUsageCostDifference(row: RawCostUsage): number | null {
  const amount = getRawUsageCostAmount(row)
  if (amount === null) return null
  const auditDifference = row.raw_cost_audit?.difference
  if (typeof auditDifference === 'number' && Number.isFinite(auditDifference)) return auditDifference
  const billed = row.raw_cost_audit?.billed_amount ?? row.actual_cost
  return typeof billed === 'number' && Number.isFinite(billed) ? billed - amount : null
}

export function formatRawUsageCostDifference(row: RawCostUsage): string {
  const difference = getRawUsageCostDifference(row)
  if (difference === null) return '—'
  const sign = difference > 0 ? '+' : difference < 0 ? '-' : ''
  return `${sign}$${rawCostFormatter.format(Math.abs(difference))}`
}

export function getRawUsageCostStatus(row: RawCostUsage, t: Translate): string {
  if (getRawUsageCostAmount(row) === null) return t('admin.usage.rawCostUnknown')
  return row.raw_cost_audit?.status === 'calculated' || row.raw_cost_audit?.status === 'estimated'
    ? t('admin.usage.rawCostCalculated')
    : t('admin.usage.rawCostRecorded')
}

export function getRawUsageCostTooltip(row: RawCostUsage, t: Translate): string {
  if (getRawUsageCostAmount(row) !== null) {
    return row.raw_cost_audit?.status === 'calculated' || row.raw_cost_audit?.status === 'estimated'
      ? t('admin.usage.rawCostCalculatedTooltip')
      : t('admin.usage.rawCostRecordedTooltip')
  }
  if (row.raw_cost_audit?.status !== 'unavailable') return t('admin.usage.rawCostUnavailable')
  switch (row.raw_cost_audit.reason) {
    case 'missing_upstream_usage': return t('admin.usage.rawCostMissingUpstreamUsage')
    case 'missing_upstream_cache_usage': return t('admin.usage.rawCostMissingUpstreamCacheUsage')
    case 'unsupported_billing_mode': return t('admin.usage.rawCostUnsupportedBillingMode')
    case 'invalid_usage': return t('admin.usage.rawCostInvalidUsage')
    case 'invalid_rate_multiplier': return t('admin.usage.rawCostInvalidRateMultiplier')
    case 'group_unavailable': return t('admin.usage.rawCostGroupUnavailable')
    case 'pricing_unavailable': return t('admin.usage.rawCostPricingUnavailable')
    case 'billing_model_unavailable': return t('admin.usage.rawCostBillingModelUnavailable')
    case 'incomplete_cost_breakdown': return t('admin.usage.rawCostIncompleteBreakdown')
    default: return t('admin.usage.rawCostUnavailable')
  }
}
