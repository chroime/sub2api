// Keep checking an already-paid fixed-code order while the statement collector
// catches up. This never extends the deadline for making a payment.
export const BILL_RECONCILIATION_WAIT_MS = 2 * 60_000

export function canWaitForBill(enabled: boolean, expiresAt: string, now = Date.now()): boolean {
  const deadline = Date.parse(expiresAt)
  return enabled && Number.isFinite(deadline) && now < deadline + BILL_RECONCILIATION_WAIT_MS
}
