/** Display-only formatting; governance timestamps always use UTC+8. */
export function formatGovernanceTime(value: string | null | undefined, timeZone = 'Asia/Shanghai'): string {
  if (!value) return '—'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '—'
  const parts = new Intl.DateTimeFormat('en-GB', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hourCycle: 'h23', ...(timeZone ? { timeZone } : {}),
  }).formatToParts(date)
  const get = (type: string) => parts.find(part => part.type === type)?.value || ''
  return `${get('year')}-${get('month')}-${get('day')} ${get('hour')}:${get('minute')}:${get('second')}`
}

export function formatGovernanceRate(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  const rounded = Math.round(value * 1_000_000) / 1_000_000
  return String(Object.is(rounded, -0) ? 0 : rounded)
}

export function formatGovernanceGroupName(value: string): string {
  if (!value) return value
  return value.startsWith('【') && value.endsWith('】') ? value : `【${value}】`
}
