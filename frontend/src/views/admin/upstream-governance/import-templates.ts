import type { ImportAccountConfig } from '@/api/admin/upstream-governance'
import type { ImportTemplateSettings } from '@/api/admin/upstream-import-templates'

const keys: (keyof ImportTemplateSettings)[] = [
  'concurrency', 'priority', 'quota_enabled', 'quota_daily_limit',
  'quota_weekly_limit', 'quota_limit', 'upstream_billing_rate_sync_enabled',
  'openai_long_context_billing_enabled',
]

export function importTemplateSettings(config: ImportAccountConfig, quotaEnabled: boolean): ImportTemplateSettings {
  return {
    concurrency: config.concurrency,
    priority: config.priority ?? 1,
    quota_enabled: quotaEnabled,
    quota_daily_limit: config.quota_daily_limit,
    quota_weekly_limit: config.quota_weekly_limit,
    quota_limit: config.quota_limit,
    upstream_billing_rate_sync_enabled: config.upstream_billing_rate_sync_enabled,
    openai_long_context_billing_enabled: config.openai_long_context_billing_enabled,
  }
}

export function validImportTemplateSettings(value: unknown): value is ImportTemplateSettings {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const fields = Object.keys(value)
  if (fields.length !== keys.length || fields.some(key => !keys.includes(key as keyof ImportTemplateSettings))) return false
  const settings = value as ImportTemplateSettings
  const integer = (number: unknown, min: number) => typeof number === 'number' && Number.isInteger(number) && number >= min && number <= 2147483647
  const quota = (number: unknown) => typeof number === 'number' && Number.isFinite(number) && number >= 0
  return integer(settings.concurrency, 1)
    && integer(settings.priority, 0)
    && quota(settings.quota_daily_limit)
    && quota(settings.quota_weekly_limit)
    && quota(settings.quota_limit)
    && typeof settings.quota_enabled === 'boolean'
    && typeof settings.upstream_billing_rate_sync_enabled === 'boolean'
    && typeof settings.openai_long_context_billing_enabled === 'boolean'
}

export function applyImportTemplateSettings(config: ImportAccountConfig, settings: ImportTemplateSettings): { config: ImportAccountConfig; quotaEnabled: boolean } {
  if (!validImportTemplateSettings(settings)) throw new RangeError('Invalid import template settings')
  const { quota_enabled, ...parameters } = settings
  return { config: { ...config, ...parameters }, quotaEnabled: quota_enabled }
}
