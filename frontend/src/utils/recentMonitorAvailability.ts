import type { MonitorTimelinePoint } from '@/api/channelMonitor'

const MEASURED_STATUSES = new Set(['operational', 'degraded', 'failed', 'error'])
const AVAILABLE_STATUSES = new Set(['operational', 'degraded'])

export function recentMonitorAvailability(points: MonitorTimelinePoint[], limit = 24): number | null {
  const sample = points.slice(0, Math.max(0, limit)).filter(point => MEASURED_STATUSES.has(point.status))
  if (!sample.length) return null
  const available = sample.filter(point => AVAILABLE_STATUSES.has(point.status)).length
  return available * 100 / sample.length
}
