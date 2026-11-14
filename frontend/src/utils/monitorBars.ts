import type { MonitorIntelligence, MonitorIntelligenceSeries, MonitorTimelinePoint } from '@/api/channelMonitor'

export type AvailabilityPoint = Omit<MonitorTimelinePoint, 'status'> & { status: MonitorTimelinePoint['status'] | 'empty' }

export const AVAILABILITY_COLORS: Record<string, string> = {
  operational: 'bg-emerald-400 dark:bg-emerald-400/90',
  degraded: 'bg-amber-400 dark:bg-amber-300',
  failed: 'bg-red-400', error: 'bg-red-400', empty: 'bg-gray-300 dark:bg-dark-600',
}
export const AVAILABILITY_HEIGHTS: Record<string, number> = {
  operational: 100, degraded: 65, failed: 35, error: 35, empty: 15,
}
export const INTELLIGENCE_COLORS: Record<string, string> = {
  pass: 'bg-emerald-400 dark:bg-emerald-400/90',
  fail: 'bg-amber-400 dark:bg-amber-300', error: 'bg-orange-400 dark:bg-orange-400',
  pending: 'bg-sky-400 dark:bg-sky-400', unknown: 'bg-gray-200 dark:bg-dark-600',
}
export const INTELLIGENCE_STATUSES = ['pass', 'fail', 'error', 'pending', 'unknown'] as const
export type IntelligenceStatus = typeof INTELLIGENCE_STATUSES[number]
export type IntelligencePoint = MonitorIntelligenceSeries['points'][number]
export interface MonitorBar {
  status: string
  colorClass: string
  heightPct: number
  point: { checked_at: string; status?: string; latency_ms?: number | null; ping_latency_ms?: number | null } | null
}

export function availabilityBars(points: AvailabilityPoint[] = [], length = 60): MonitorBar[] {
  const real = points.slice(0, length).reverse()
  return [
    ...Array.from({ length: Math.max(0, length - real.length) }, () => ({
      status: 'empty', colorClass: AVAILABILITY_COLORS.empty!, heightPct: AVAILABILITY_HEIGHTS.empty!, point: null,
    })),
    ...real.map(point => ({
      status: point.status, point,
      colorClass: AVAILABILITY_COLORS[point.status] ?? AVAILABILITY_COLORS.empty!,
      heightPct: AVAILABILITY_HEIGHTS[point.status] ?? AVAILABILITY_HEIGHTS.empty!,
    })),
  ]
}

export function intelligenceRows(summary?: MonitorIntelligence | null, length = 60) {
  const endTime = Date.parse(summary?.window_end || '')
  return (summary?.series || []).filter(series => series.model.trim()).map(series => {
    const points = series.points.filter(point => {
      const timestamp = Date.parse(point.checked_at)
      return Number.isFinite(endTime) && Number.isFinite(timestamp) && timestamp <= endTime &&
        INTELLIGENCE_STATUSES.includes(point.status)
    })
      .sort((a, b) => Date.parse(a.checked_at) - Date.parse(b.checked_at) || (a.id || '').localeCompare(b.id || ''))
      .slice(-length)
    const bars: MonitorBar[] = [
      ...Array.from({ length: Math.max(0, length - points.length) }, () => ({
        status: 'unknown', point: null, colorClass: INTELLIGENCE_COLORS.unknown!, heightPct: 100,
      })),
      ...points.map(point => ({ status: point.status, point, colorClass: INTELLIGENCE_COLORS[point.status]!, heightPct: 100 })),
    ]
    return {
      key: JSON.stringify([series.model, series.effort]), model: series.model, effort: series.effort,
      bars, latest: points.at(-1),
    }
  })
}

/** Missing evidence must never be upgraded by another series' success. */
export function worstMonitorStatus(statuses: string[], intelligence = false): string {
  const priority: Record<string, number> = intelligence
    ? { pass: 0, unknown: 1, pending: 2, error: 3, fail: 4 }
    : { operational: 0, empty: 1, degraded: 2, error: 3, failed: 4 }
  const neutral = intelligence ? 'unknown' : 'empty'
  const first = statuses[0] || neutral
  return statuses.reduce((worst, status) => {
    const normalized = status in priority ? status : neutral
    return (priority[normalized] ?? 1) > (priority[worst] ?? -1) ? normalized : worst
  }, first in priority ? first : neutral)
}
