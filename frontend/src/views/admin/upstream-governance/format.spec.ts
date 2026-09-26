import { describe, expect, it } from 'vitest'
import { formatGovernanceTime } from './format'
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
  it('uses the browser system timezone by default', () => {
    const timestamp = '2026-09-26T15:08:02Z'
    expect(formatGovernanceTime(timestamp)).toBe(formatGovernanceTime(timestamp, Intl.DateTimeFormat().resolvedOptions().timeZone))
  })
  it('shows a neutral placeholder for unknown and invalid timestamps', () => {
    for (const value of [null, undefined, '', 'not-a-time']) expect(formatGovernanceTime(value)).toBe('—')
  })
})
