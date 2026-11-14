import type { MonitorIntelligenceSeries, UserMonitorView } from '@/api/channelMonitor'
import { availabilityBars, intelligenceRows, INTELLIGENCE_COLORS, worstMonitorStatus, type AvailabilityPoint, type MonitorBar } from './monitorBars'

export type GroupMonitorLoadState = 'ready' | 'loading' | 'unavailable' | 'disabled'
export type GroupMonitorColumnState = GroupMonitorLoadState | 'unconfigured' | 'empty' | 'stale'
export interface GroupMonitorDetail {
  model: string
  effort?: string
  status: string
  checkedAt?: string
  latencyMs?: number | null
  state: GroupMonitorColumnState
}
export interface GroupMonitorColumn {
  state: GroupMonitorColumnState
  bars: MonitorBar[]
  details: GroupMonitorDetail[]
}
export interface GroupMonitorState { availability: GroupMonitorColumn; intelligence: GroupMonitorColumn }
type GroupIdentity = { id: number; platform: string }
const LENGTH = 20

function neutralColumn(state: GroupMonitorColumnState, intelligence = false): GroupMonitorColumn {
  return {
    state, details: [], bars: intelligence
      ? Array.from({ length: LENGTH }, () => ({ status: 'unknown', point: null, colorClass: INTELLIGENCE_COLORS.unknown!, heightPct: 65 }))
      : availabilityBars([], LENGTH).map(bar => ({ ...bar, heightPct: 65 })),
  }
}

function groupAvailability(monitors: UserMonitorView[], now: number): GroupMonitorColumn {
  if (!monitors.length) return neutralColumn('unconfigured')
  const sources = monitors.map(monitor => {
    const history = monitor.timeline.filter(point => Number.isFinite(Date.parse(point.checked_at)) && Date.parse(point.checked_at) <= now)
      .sort((a, b) => Date.parse(b.checked_at) - Date.parse(a.checked_at))
    const deadline = Date.parse(monitor.probe_valid_until || '')
    const stale = history.length > 0 && (!Number.isFinite(deadline) || deadline <= now)
    return { monitor, history, active: stale ? [] : history, state: (stale ? 'stale' : history.length ? 'ready' : 'empty') as GroupMonitorColumnState }
  })
  const details = sources.map(({ monitor, history, state }) => ({
    model: monitor.primary_model, status: state === 'ready' ? history[0]!.status : 'empty', state,
    checkedAt: history[0]?.checked_at, latencyMs: history[0]?.latency_ms,
  }))
  if (sources.every(source => source.state !== 'ready')) {
    return { ...neutralColumn(sources.some(source => source.state === 'stale') ? 'stale' : 'empty'), details }
  }
  let history: AvailabilityPoint[]
  if (sources.length === 1) history = sources[0]!.active
  else {
    // Align real check events by timestamp, holding each probe's latest known
    // result at that event. Different probe intervals must not align by index.
    const times = [...new Set(sources.flatMap(source => source.active.map(point => Date.parse(point.checked_at))))]
      .sort((a, b) => b - a).slice(0, LENGTH)
    history = times.map(time => {
      const points = sources.map(source => {
        const point = source.active.find(p => Date.parse(p.checked_at) <= time)
        const duration = Date.parse(source.monitor.probe_valid_until || '') - Date.parse(source.history[0]?.checked_at || '')
        return point && time - Date.parse(point.checked_at) < duration ? point : null
      })
      const status = worstMonitorStatus(points.map(point => point?.status || 'empty')) as AvailabilityPoint['status']
      const latencies = points.map(point => point?.latency_ms).filter((v): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0)
      return { status, checked_at: new Date(time).toISOString(), latency_ms: latencies.length ? Math.max(...latencies) : null, ping_latency_ms: null }
    })
  }
  return { state: 'ready', bars: availabilityBars(history, LENGTH), details }
}

function groupIntelligence(monitors: UserMonitorView[], now: number): GroupMonitorColumn {
  const series = new Map<string, MonitorIntelligenceSeries>()
  let stale = false
  for (const monitor of monitors) {
    const summary = monitor.intelligence
    if (!summary) continue
    const end = Date.parse(summary.window_end)
    const snapshotStale = !Number.isFinite(end) || now - end > 90000
    stale ||= snapshotStale
    for (const source of summary.series.filter(s => s.model.trim())) {
      const key = JSON.stringify([source.model, source.effort])
      const existing = series.get(key)
      const points = snapshotStale ? [] : source.points.filter(point => Date.parse(point.checked_at) <= Math.min(end, now))
      if (!existing) series.set(key, { ...source, points: [...points] })
      else existing.points.push(...points)
    }
  }
  if (!series.size) return neutralColumn('unconfigured', true)
  // A run may be projected by multiple monitors. Deduplicate that run only;
  // distinct checks in the same minute (or instant) must retain their history.
  for (const source of series.values()) {
    const checks = new Map<string, MonitorIntelligenceSeries['points'][number]>()
    for (const point of source.points) {
      const key = point.id ? `id:${point.id}` : `time:${point.checked_at}`
      const previous = checks.get(key)
      if (!previous) checks.set(key, point)
      else checks.set(key, {
        ...previous,
        status: worstMonitorStatus([previous.status, point.status], true) as typeof point.status,
      })
    }
    source.points = [...checks.values()]
  }
  const rows = intelligenceRows({ window_end: new Date(now).toISOString(), series: [...series.values()] })
  const details = rows.map(row => ({ model: row.model, effort: row.effort, status: row.latest?.status || 'unknown',
    checkedAt: row.latest?.checked_at, state: (row.latest ? 'ready' : stale ? 'stale' : 'empty') as GroupMonitorColumnState }))
  if (!details.some(detail => detail.state === 'ready')) return { ...neutralColumn(stale ? 'stale' : 'empty', true), details }
  if (rows.length === 1) return { state: 'ready', bars: rows[0]!.bars.slice(-LENGTH), details }
  // Different model/effort schedules share an event timeline. At each actual
  // event hold each series' latest known result, never pair unrelated indices.
  const histories = rows.map(row => row.bars.flatMap(bar => bar.point ? [bar.point] : []).reverse())
  const times = [...new Set(histories.flatMap(history => history.map(point => Date.parse(point.checked_at))))]
    .sort((a, b) => a - b).slice(-LENGTH)
  const bars: MonitorBar[] = times.map(time => {
    const statuses = histories.map(history => {
      const latest = history.find(point => Date.parse(point.checked_at) <= time)
      if (!latest) return 'unknown'
      return worstMonitorStatus(history.filter(point => point.checked_at === latest.checked_at).map(point => point.status || 'unknown'), true)
    })
    const status = worstMonitorStatus(statuses, true)
    return { status, point: { status, checked_at: new Date(time).toISOString() }, colorClass: INTELLIGENCE_COLORS[status]!, heightPct: 100 }
  })
  bars.unshift(...neutralColumn('empty', true).bars.slice(0, LENGTH - bars.length))
  return { state: 'ready', bars, details }
}

export function buildGroupMonitorStates(
  groups: GroupIdentity[], monitors: UserMonitorView[], options: { now: number; state?: GroupMonitorLoadState },
): Map<number, GroupMonitorState> {
  const result = new Map<number, GroupMonitorState>()
  const indexed = new Map<number, UserMonitorView[]>()
  for (const monitor of monitors) {
    if (typeof monitor.group_id !== 'number' || monitor.group_id <= 0) continue
    const list = indexed.get(monitor.group_id) || []
    list.push(monitor)
    indexed.set(monitor.group_id, list)
  }
  for (const group of groups) {
    const state = options.state || 'ready'
    const matches = (indexed.get(group.id) || []).filter(m => m.provider === group.platform)
    result.set(group.id, state === 'ready' ? {
      availability: groupAvailability(matches, options.now), intelligence: groupIntelligence(matches, options.now),
    } : { availability: neutralColumn(state), intelligence: neutralColumn(state, true) })
  }
  return result
}
