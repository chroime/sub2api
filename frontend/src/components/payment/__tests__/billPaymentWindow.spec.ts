import { describe, expect, it } from 'vitest'
import { billReconciliationSecondsRemaining, canWaitForBill } from '../billPaymentWindow'

describe('fixed-code reconciliation deadline', () => {
  const deadline = Date.parse('2026-11-11T04:01:00Z')
  const expiresAt = new Date(deadline).toISOString()
  const end = deadline + 30_000

  it('counts from the immutable server expiry and rounds the last fractional second up', () => {
    expect(billReconciliationSecondsRemaining(expiresAt, deadline)).toBe(30)
    expect(billReconciliationSecondsRemaining(expiresAt, deadline - 500)).toBe(30)
    expect(billReconciliationSecondsRemaining(expiresAt, end - 14_000)).toBe(14)
    expect(billReconciliationSecondsRemaining(expiresAt, end - 1)).toBe(1)
    expect(billReconciliationSecondsRemaining(expiresAt, end)).toBe(0)
    expect(billReconciliationSecondsRemaining(expiresAt, end + 1)).toBe(0)
    expect(canWaitForBill(true, expiresAt, end - 1)).toBe(true)
    expect(canWaitForBill(true, expiresAt, end)).toBe(false)
    expect(canWaitForBill(false, expiresAt, deadline)).toBe(false)
  })

  it('does not start reconciliation for an invalid timestamp', () => {
    expect(billReconciliationSecondsRemaining('', deadline)).toBe(0)
    expect(billReconciliationSecondsRemaining('invalid', deadline)).toBe(0)
    expect(billReconciliationSecondsRemaining(expiresAt, NaN)).toBe(0)
    expect(canWaitForBill(true, 'invalid', deadline)).toBe(false)
  })
})
