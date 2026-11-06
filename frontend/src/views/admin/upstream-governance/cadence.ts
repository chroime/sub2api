import type { Site } from '@/api/admin/upstream-governance'

type Translate = (key: string, params?: Record<string, unknown>) => string

const validSeconds = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value > 0

/**
 * The cadence the scheduler actually runs: full collection follows
 * full_interval_seconds (interval_minutes is only its legacy alias), and the
 * fast group/rate observation runs on its own interval when it is enabled and
 * faster than full collection.
 */
export function siteCadence(site: Pick<Site, 'interval_minutes' | 'full_interval_seconds' | 'fast_interval_seconds' | 'fast_observe_enabled'>) {
  const full = validSeconds(site.full_interval_seconds) ? site.full_interval_seconds : Math.max(1, site.interval_minutes || 15) * 60
  const fast = site.fast_observe_enabled && validSeconds(site.fast_interval_seconds) && site.fast_interval_seconds < full ? site.fast_interval_seconds : null
  return { full, fast }
}

/** "20 秒", "5 分钟", "2 小时"; uneven values stay in the smaller unit. */
export function intervalText(seconds: number, t: Translate): string {
  if (seconds % 3600 === 0) return t('governance.cadence.hours', { n: seconds / 3600 })
  if (seconds % 60 === 0) return t('governance.cadence.minutes', { n: seconds / 60 })
  return t('governance.cadence.seconds', { n: seconds })
}

/** "每 20 秒 · 观察 1 秒" for the site rail and overview. */
export function cadenceText(site: Parameters<typeof siteCadence>[0], t: Translate): string {
  const { full, fast } = siteCadence(site)
  return fast === null
    ? t('governance.cadence.every', { interval: intervalText(full, t) })
    : t('governance.cadence.everyWithFast', { full: intervalText(full, t), fast: intervalText(fast, t) })
}
