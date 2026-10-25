import type { Run } from '@/api/admin/account-quality'

export function runTime(run: Run): string | undefined {
  return [run.finished_at, run.started_at, run.created_at].find(value => value && Number.isFinite(Date.parse(value))) || undefined
}

export function chronologicalSamples(runs: Run[] = []): Run[] {
  return [...runs].sort((left, right) => {
    const difference = (Date.parse(runTime(left) || '') || 0) - (Date.parse(runTime(right) || '') || 0)
    return difference || left.id.localeCompare(right.id)
  }).slice(-30)
}

export function responsePreview(run?: Run & { result_preview?: string }): string {
  const lines = (run?.result_preview || run?.response_text || '').split(/\r?\n/)
  const line = lines.map(value => value.trim()).filter(value => value && !/^(```|~~~)/.test(value)).at(-1) || ''
  const characters = Array.from(line)
  return characters.length > 240 ? `…${characters.slice(-240).join('')}` : line
}
