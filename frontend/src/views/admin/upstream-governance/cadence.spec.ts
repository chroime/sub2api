import { describe, expect, it } from 'vitest'
import { cadenceText, intervalText, siteCadence } from './cadence'

const t = (key: string, params?: Record<string, unknown>) => `${key.replace('governance.cadence.', '')}(${Object.values(params ?? {}).join(',')})`

describe('site cadence', () => {
  it('follows the second-based intervals the scheduler runs, not the legacy minute alias', () => {
    expect(siteCadence({ interval_minutes: 1, full_interval_seconds: 20, fast_interval_seconds: 1, fast_observe_enabled: true })).toEqual({ full: 20, fast: 1 })
    expect(siteCadence({ interval_minutes: 15 })).toEqual({ full: 900, fast: null })
  })

  it('drops the fast observation when it is off or not faster than full collection', () => {
    expect(siteCadence({ interval_minutes: 1, full_interval_seconds: 60, fast_interval_seconds: 60, fast_observe_enabled: true }).fast).toBeNull()
    expect(siteCadence({ interval_minutes: 1, full_interval_seconds: 60, fast_interval_seconds: 10, fast_observe_enabled: false }).fast).toBeNull()
  })

  it('names intervals in the largest even unit', () => {
    expect(intervalText(20, t)).toBe('seconds(20)')
    expect(intervalText(300, t)).toBe('minutes(5)')
    expect(intervalText(7200, t)).toBe('hours(2)')
    expect(intervalText(90, t)).toBe('seconds(90)')
    expect(cadenceText({ interval_minutes: 1, full_interval_seconds: 20, fast_interval_seconds: 1, fast_observe_enabled: true }, t)).toBe('everyWithFast(seconds(20),seconds(1))')
    expect(cadenceText({ interval_minutes: 15 }, t)).toBe('every(minutes(15))')
  })
})
