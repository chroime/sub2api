import { describe, expect, it } from 'vitest'
import { formatGovernanceGroupName, formatGovernanceRate, formatGovernanceTime } from './format'
describe('governance timestamp presentation', () => {
  it('formats exact second precision in the requested timezone without changing the instant', () => {
    const timestamp = '2026-09-26T15:08:02.675Z'
    expect(formatGovernanceTime(timestamp, 'Asia/Shanghai')).toBe('2026-09-26 23:08:02')
    expect(formatGovernanceTime(timestamp, 'America/New_York')).toBe('2026-09-26 11:08:02')
    expect(formatGovernanceTime(timestamp, 'UTC')).toBe('2026-09-26 15:08:02')
    expect(timestamp).toBe('2026-09-26T15:08:02.675Z')
  })
  it('uses midnight 00 rather than 24 and retains calendar rollover', () => {
    expect(formatGovernanceTime('2026-09-26T16:00:00Z', 'Asia/Shanghai')).toBe('2026-09-27 00:00:00')
  })
  it('uses Asia/Shanghai by default', () => {
    const timestamp = '2026-09-26T15:08:02Z'
    expect(formatGovernanceTime(timestamp)).toBe('2026-09-26 23:08:02')
  })
  it('shows a neutral placeholder for unknown and invalid timestamps', () => {
    for (const value of [null, undefined, '', 'not-a-time']) expect(formatGovernanceTime(value)).toBe('—')
  })
})

describe('governance rate presentation', () => {
  it('rounds binary floating point noise without losing useful precision', () => {
    expect(formatGovernanceRate(0.030000000000000027)).toBe('0.03')
    expect(formatGovernanceRate(0.93)).toBe('0.93')
    expect(formatGovernanceRate(0.123456789)).toBe('0.123457')
    expect(formatGovernanceRate(null)).toBe('—')
  })
})

describe('governance group presentation', () => {
  it('wraps the group label once for run history summaries', () => {
    expect(formatGovernanceGroupName('Claude Max')).toBe('【Claude Max】')
    expect(formatGovernanceGroupName('【Claude Max】')).toBe('【Claude Max】')
  })
})
