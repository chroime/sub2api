export const MAX_BALANCE_PRECHARGE_USD = 1_000_000

export function isBalancePrechargeMoney(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 &&
    value <= MAX_BALANCE_PRECHARGE_USD && Number(value.toFixed(8)) === value
}

// Text inputs preserve the user's decimal precision until validation succeeds.
export function parseBalancePrechargeMoney(value: string): number | null {
  if (!/^\d+(?:\.\d{1,8})?$/.test(value.trim())) return null
  const parsed = Number(value)
  return isBalancePrechargeMoney(parsed) ? parsed : null
}

export function validBalancePrechargeAmounts(threshold: number | null, amount: number | null, required: boolean): boolean {
  if (threshold === null || amount === null) return false
  return !required || (threshold > 0 && amount > 0 && amount <= threshold)
}

export function formatBalancePrechargeMoney(value: number): string {
  return value.toFixed(8).replace(/\.?0+$/, '') || '0'
}
