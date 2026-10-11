import type { AdminUsageLog } from '@/types'

type RawCostUsage = Pick<AdminUsageLog, 'raw_cost_audit'>
type Translate = (key: string) => string

const rawCostFormatter = new Intl.NumberFormat('en-US', {
  useGrouping: false,
  minimumFractionDigits: 8,
  maximumFractionDigits: 8,
})

export function getRawUsageCostAmount(row: RawCostUsage): number | null {
  const audit = row.raw_cost_audit
  if (!audit || (audit.status !== 'recorded' && audit.status !== 'estimated')) return null
  return typeof audit.amount === 'number' && Number.isFinite(audit.amount) && audit.amount >= 0
    ? audit.amount
    : null
}

export function formatRawUsageCost(row: RawCostUsage): string {
  const amount = getRawUsageCostAmount(row)
  return amount === null ? '—' : rawCostFormatter.format(amount)
}

export function getRawUsageCostStatus(row: RawCostUsage, t: Translate): string {
  if (getRawUsageCostAmount(row) === null) return t('admin.usage.rawCostUnknown')
  return row.raw_cost_audit?.status === 'estimated'
    ? t('admin.usage.rawCostEstimated')
    : t('admin.usage.rawCostRecorded')
}

export function getRawUsageCostTooltip(row: RawCostUsage, t: Translate): string {
  if (getRawUsageCostAmount(row) !== null) {
    return row.raw_cost_audit?.status === 'estimated'
      ? t('admin.usage.rawCostEstimatedTooltip')
      : t('admin.usage.rawCostRecordedTooltip')
  }
  if (row.raw_cost_audit?.status !== 'unavailable') return t('admin.usage.rawCostUnavailable')
  switch (row.raw_cost_audit.reason) {
    case 'missing_upstream_usage': return t('admin.usage.rawCostMissingUpstreamUsage')
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
