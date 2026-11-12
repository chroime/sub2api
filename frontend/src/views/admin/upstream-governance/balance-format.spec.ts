import { describe, expect, it } from 'vitest'
import { formatBalance } from './balance-format'

describe('quota balance display', () => {
  it.each([
    [4993033, '$9.986066'], [0, '$0.00'], [-500000, '-$1.00'], [1, '$0.000002'],
    [null, '未知'], [undefined, '未知'], [NaN, '未知'], [Infinity, '未知'],
  ])('formats native quota %s as %s', (amount, expected) => {
    expect(formatBalance(amount, 'quota', 500000, '未知')).toBe(expected)
  })

  it('does not apply a quota divisor to an existing currency', () => {
    expect(formatBalance(10, 'USD', 500000, '未知')).toBe('10 USD')
  })

  it('falls back to quota when dividing would overflow', () => {
    expect(formatBalance(4993033, 'quota', Number.MIN_VALUE, '未知')).toBe('4993033 quota')
  })
})
