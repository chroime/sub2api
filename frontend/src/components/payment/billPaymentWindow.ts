// Silently check delayed bills after showing unconfirmed fixed-code orders as expired.
// Confirmed-payment fulfillment has a separate deadline and is not shortened.
// This never extends payment or amount reservation deadlines or blocks navigation.
export const BILL_RECONCILIATION_WAIT_MS = 30_000

export function billReconciliationSecondsRemaining(expiresAt: string, now = Date.now()): number {
  const deadline = Date.parse(expiresAt)
  if (!Number.isFinite(deadline) || !Number.isFinite(now)) return 0
  return Math.min(BILL_RECONCILIATION_WAIT_MS / 1000, Math.max(0, Math.ceil((deadline + BILL_RECONCILIATION_WAIT_MS - now) / 1000)))
}

export function canWaitForBill(enabled: boolean, expiresAt: string, now = Date.now()): boolean {
  return enabled && billReconciliationSecondsRemaining(expiresAt, now) > 0
}
