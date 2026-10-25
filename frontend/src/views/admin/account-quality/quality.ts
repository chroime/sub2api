import type { Capability, Policy, Settings } from '@/api/admin/account-quality'

export function newPolicy(): Policy {
  return { id: 0, version: 0, name: '', enabled: false, platform: 'openai', model: '', api_mode: 'responses', effort: 'high', templates: ['logic'], max_output_tokens: 4096, timeout_seconds: 120, interval_seconds: 600, confirm_seconds: 60, failure_threshold: 2, recovery_threshold: 2, cooldown_seconds: 900, recovery_interval_seconds: 300, daily_request_limit: 200, control_mode: 'record_notify', isolation_scope: 'model' }
}
export function newSettings(): Settings {
  return { version: 0, concurrency: 2, daily_request_limit: 2000, daily_cost_limit_usd: 0, notifications: { email_enabled: false, email_recipients: [], qq_enabled: false, qq_targets: [], events: ['suspected', 'quarantined', 'recovered', 'manual', 'budget_paused'], reminder_seconds: 3600 } }
}
export function canAutoIsolate(capabilities: Capability[], platform: string, mode: string, accountType?: string): boolean {
  return Boolean(accountType && capabilities.some(item => item.platform === platform && item.api_mode === mode && item.automatic_control && item.account_types.includes(accountType)))
}
export function sampleTone(run: { execution_status: string; quality_verdict: string }): 'pass' | 'fail' | 'error' | 'unknown' {
  if (run.execution_status === 'completed') return run.quality_verdict === 'pass' ? 'pass' : run.quality_verdict === 'fail' ? 'fail' : 'unknown'
  return ['queued', 'running', 'cancelled', 'not_run', 'budget_paused', 'skipped', ''].includes(run.execution_status) ? 'unknown' : 'error'
}
export function selectedAccountIds(value: string): number[] {
  const values = value.trim().split(/[\s,，]+/).filter(Boolean)
  if (values.some(id => !/^[1-9]\d*$/.test(id) || !Number.isSafeInteger(Number(id)))) throw new Error('invalid_account_ids')
  return [...new Set(values.map(Number))]
}
export function requestId(): string { return globalThis.crypto?.randomUUID?.() ?? `quality-${Date.now()}-${Math.random().toString(36).slice(2)}` }
export async function queueChecks<T>(items: T[], execute: (item: T) => Promise<unknown>): Promise<{ success: number; failed: number }> {
  let index = 0, success = 0, failed = 0
  await Promise.all(Array.from({ length: Math.min(4, items.length) }, async () => {
    while (index < items.length) {
      const item = items[index++]!
      try { await execute(item); success++ } catch { failed++ }
    }
  }))
  return { success, failed }
}
export function dateTime(value?: string | null): string { return value ? new Date(value).toLocaleString() : '—' }
export function duration(value?: number): string { return value == null ? '—' : `${(value / 1000).toFixed(1)}s` }
export function qualityError(value: unknown): string {
  const error = value as { response?: { data?: { message?: string; detail?: string } }; message?: string }
  return error?.response?.data?.message || error?.response?.data?.detail || error?.message || ''
}
