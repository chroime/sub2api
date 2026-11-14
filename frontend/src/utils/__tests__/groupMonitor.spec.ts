import { describe, expect, it } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import { buildGroupMonitorStates } from '../groupMonitor'

const now = Date.parse('2026-11-14T09:30:30Z')
const stamp = (minutes = 0) => new Date(now - minutes * 60000).toISOString()
const group = (id: number, name = 'same name') => ({ id, name, platform: 'openai' })
function monitor(id: number, groupId: number, overrides: Partial<UserMonitorView> = {}): UserMonitorView {
  return {
    id, group_id: groupId, name: 'old monitor name', group_name: 'old group name', provider: 'openai',
    primary_model: 'model-a', primary_status: 'operational', primary_latency_ms: 1324,
    primary_ping_latency_ms: null, availability_7d: 100, extra_models: [],
    probe_valid_until: new Date(now + 120000).toISOString(),
    timeline: [{ status: 'operational', checked_at: stamp(), latency_ms: 1324, ping_latency_ms: null }],
    ...overrides,
  }
}

describe('API Key group monitor projection', () => {
  it('matches exact allowed group IDs across renames, never names or other platforms', () => {
    const result = buildGroupMonitorStates([group(1), group(2)], [monitor(4, 1), monitor(5, 3), monitor(6, 2, { provider: 'grok' })], { now })
    expect(result.get(1)?.availability.bars.at(-1)?.status).toBe('operational')
    expect(result.get(2)?.availability.state).toBe('unconfigured')
    expect(result.has(3)).toBe(false)
  })

  it('keeps neutral slots for both missing monitors and missing IQ configuration', () => {
    const result = buildGroupMonitorStates([group(1), group(2)], [monitor(4, 1)], { now })
    for (const column of [result.get(1)!.intelligence, result.get(2)!.availability, result.get(2)!.intelligence]) {
      expect(column.state).toBe('unconfigured')
      expect(column.bars).toHaveLength(20)
      expect(column.bars.every(bar => ['empty', 'unknown'].includes(bar.status))).toBe(true)
    }
  })

  it('preserves real chronology and measured latency without inventing successful padding', () => {
    const result = buildGroupMonitorStates([group(1)], [monitor(4, 1, { timeline: [
      { status: 'operational', checked_at: stamp(), latency_ms: 0, ping_latency_ms: null },
      { status: 'failed', checked_at: stamp(1), latency_ms: null, ping_latency_ms: null },
    ] })], { now }).get(1)!.availability
    expect(result.bars.slice(-2).map(bar => bar.status)).toEqual(['failed', 'operational'])
    expect(result.bars.at(-1)?.point?.latency_ms).toBe(0)
    expect(result.bars.slice(0, 18).every(bar => bar.status === 'empty')).toBe(true)
  })

  it('turns expired probe results gray according to the server deadline', () => {
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { probe_valid_until: stamp(1) })], { now }).get(1)!.availability
    expect(column.state).toBe('stale')
    expect(column.bars.some(bar => bar.status === 'operational')).toBe(false)
  })

  it('does not let a healthy monitor hide another monitor failure or missing result', () => {
    const failed = monitor(5, 1, { primary_model: 'model-b', timeline: [
      { status: 'failed', checked_at: stamp(), latency_ms: null, ping_latency_ms: null },
    ] })
    let column = buildGroupMonitorStates([group(1)], [monitor(4, 1), failed], { now }).get(1)!.availability
    expect(column.bars.at(-1)?.status).toBe('failed')
    expect(column.details.map(detail => detail.model)).toEqual(['model-a', 'model-b'])
    column = buildGroupMonitorStates([group(1)], [monitor(4, 1), monitor(5, 1, { timeline: [] })], { now }).get(1)!.availability
    expect(column.bars.at(-1)?.status).toBe('empty')
  })

  it('aligns different probe schedules by event time instead of pairing unrelated array indices', () => {
    const column = buildGroupMonitorStates([group(1)], [
      monitor(4, 1, { timeline: [
        { status: 'operational', checked_at: stamp(), latency_ms: 1200, ping_latency_ms: null },
        { status: 'failed', checked_at: stamp(2), latency_ms: null, ping_latency_ms: null },
      ] }),
      monitor(5, 1, { timeline: [
        { status: 'operational', checked_at: stamp(1), latency_ms: 1800, ping_latency_ms: null },
        { status: 'operational', checked_at: stamp(3), latency_ms: 1700, ping_latency_ms: null },
      ] }),
    ], { now }).get(1)!.availability
    expect(column.bars.slice(-4).map(bar => bar.status)).toEqual(['empty', 'failed', 'failed', 'operational'])
    expect(column.bars.at(-1)?.point?.latency_ms).toBe(1800)
  })

  it('rejects invalid and future checks and keeps a missing server freshness deadline gray', () => {
    const result = buildGroupMonitorStates([group(1), group(2)], [
      monitor(4, 1, { timeline: [
        { status: 'operational', checked_at: stamp(-1), latency_ms: 500, ping_latency_ms: null },
        { status: 'operational', checked_at: 'invalid', latency_ms: 500, ping_latency_ms: null },
      ] }),
      monitor(5, 2, { probe_valid_until: undefined }),
    ], { now })
    expect(result.get(1)?.availability.state).toBe('empty')
    expect(result.get(2)?.availability.state).toBe('stale')
    expect([...result.values()].flatMap(value => value.availability.bars).some(bar => bar.status === 'operational')).toBe(false)
  })

  it('retains every model/effort verdict in IQ details', () => {
    const intelligence = { window_end: stamp(), series: [
      { model: 'model-a', effort: 'high', points: [{ status: 'pass' as const, checked_at: stamp() }] },
      { model: 'model-a', effort: 'medium', points: [{ status: 'fail' as const, checked_at: stamp() }] },
    ] }
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { intelligence })], { now }).get(1)!.intelligence
    expect(column.bars.at(-1)?.status).toBe('fail')
    expect(column.bars.at(-2)?.status).toBe('unknown')
    expect(column.details.map(detail => detail.effort)).toEqual(['high', 'medium'])
  })

  it('keeps untested IQ series neutral instead of hiding it behind another series pass', () => {
    const intelligence = { window_end: stamp(), series: [
      { model: 'model-a', effort: 'high', points: [{ status: 'pass' as const, checked_at: stamp() }] },
      { model: 'model-b', effort: 'high', points: [] },
    ] }
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { intelligence })], { now }).get(1)!.intelligence
    expect(column.bars.at(-1)?.status).toBe('unknown')
    expect(column.details).toHaveLength(2)
  })

  it('deduplicates the same IQ series attached to multiple monitors', () => {
    const intelligence = { window_end: stamp(), series: [{ model: 'model-a', effort: 'high', points: [{ status: 'pass' as const, checked_at: stamp() }] }] }
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { intelligence }), monitor(5, 1, { intelligence })], { now }).get(1)!.intelligence
    expect(column.details).toHaveLength(1)
    expect(column.bars.at(-1)?.status).toBe('pass')
  })

  it('keeps distinct same-minute checks, so a later pass does not erase the earlier failure', () => {
    const makeIntelligence = (status: 'fail' | 'pass', seconds: number) => ({ window_end: stamp(), series: [{
      model: 'model-a', effort: 'high', points: [{ status, checked_at: new Date(now - seconds * 1000).toISOString() }],
    }] })
    const column = buildGroupMonitorStates([group(1)], [
      monitor(4, 1, { intelligence: makeIntelligence('fail', 20) }),
      monitor(5, 1, { intelligence: makeIntelligence('pass', 10) }),
    ], { now }).get(1)!.intelligence
    expect(column.bars.slice(-2).map(bar => bar.status)).toEqual(['fail', 'pass'])
    expect(column.details).toHaveLength(1)
  })

  it('keeps expired IQ snapshots neutral while retaining old checks in fresh snapshots', () => {
    const result = buildGroupMonitorStates([group(1), group(2)], [
      monitor(4, 1, { intelligence: { window_end: stamp(2), series: [{ model: 'model-a', effort: 'high', points: [{ status: 'pass', checked_at: stamp(2) }] }] } }),
      monitor(5, 2, { intelligence: { window_end: stamp(), series: [{ model: 'model-a', effort: 'high', points: [
        { status: 'pass', checked_at: stamp(60) }, { status: 'pass', checked_at: stamp(-1) }, { status: 'pass', checked_at: 'invalid' },
      ] }] } }),
    ], { now })
    expect(result.get(1)?.intelligence.state).toBe('stale')
    expect(result.get(1)?.intelligence.bars.every(bar => bar.status === 'unknown')).toBe(true)
    expect(result.get(2)?.intelligence.state).toBe('ready')
    expect(result.get(2)?.intelligence.bars.at(-1)?.point?.checked_at).toBe(stamp(60))
  })

  it('deduplicates actual check IDs across monitors, preserving different checks at the same instant', () => {
    const intelligence = { window_end: stamp(), series: [{ model: 'model-a', effort: 'high', points: [
      { id: 'check-a', status: 'fail' as const, checked_at: stamp() },
      { id: 'check-b', status: 'pass' as const, checked_at: stamp() },
    ] }] }
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { intelligence }), monitor(5, 1, { intelligence })], { now }).get(1)!.intelligence
    expect(column.bars.filter(bar => bar.point)).toHaveLength(2)
    expect(column.bars.slice(-2).map(bar => bar.status)).toEqual(['fail', 'pass'])
  })

  it('aligns IQ series by actual event time rather than pairing unrelated check positions', () => {
    const intelligence = { window_end: stamp(), series: [
      { model: 'model-a', effort: 'high', points: [
        { status: 'pass' as const, checked_at: stamp(0) }, { status: 'fail' as const, checked_at: stamp(60) },
      ] },
      { model: 'model-b', effort: 'medium', points: [
        { status: 'pass' as const, checked_at: stamp(30) }, { status: 'pass' as const, checked_at: stamp(90) },
      ] },
    ] }
    const column = buildGroupMonitorStates([group(1)], [monitor(4, 1, { intelligence })], { now }).get(1)!.intelligence
    expect(column.bars.slice(-4).map(bar => bar.status)).toEqual(['unknown', 'fail', 'fail', 'pass'])
    expect(column.bars.slice(-4).map(bar => bar.point?.checked_at)).toEqual([stamp(90), stamp(60), stamp(30), stamp(0)])
  })

  it.each(['loading', 'unavailable', 'disabled'] as const)('never presents cached successes as current when %s', state => {
    const result = buildGroupMonitorStates([group(1)], [monitor(4, 1)], { now, state }).get(1)!
    expect(result.availability.state).toBe(state)
    expect(result.intelligence.state).toBe(state)
    expect(result.availability.bars.every(bar => bar.status === 'empty')).toBe(true)
  })
})
