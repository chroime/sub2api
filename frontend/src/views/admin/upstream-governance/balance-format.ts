// A quota is displayed as dollars only when the upstream declares its USD scale.
// Monitoring thresholds and the raw API values retain their original units.
export function quotaBalanceUSD(value: number, unit?: string, quotaPerUsd?: number): number | null {
  if (unit !== 'quota' || !Number.isFinite(value) || quotaPerUsd == null || !Number.isFinite(quotaPerUsd) || quotaPerUsd <= 0) return null
  const dollars = value / quotaPerUsd
  return Number.isFinite(dollars) ? dollars : null
}

const usdDetail = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 6 })

export function formatBalance(value: number | null | undefined, unit: string | undefined, quotaPerUsd: number | undefined, unknown: string): string {
  if (value == null || !Number.isFinite(value)) return unknown
  const dollars = quotaBalanceUSD(value, unit, quotaPerUsd)
  return dollars == null ? `${Number(value.toFixed(6))} ${unit || ''}`.trim() : usdDetail.format(dollars)
}

export function rawBalance(value: number | null | undefined, unit?: string): string | undefined {
  return value == null || !Number.isFinite(value) ? undefined : `${value} ${unit || ''}`.trim()
}
