import type { ImportAccountConfig, Transport } from '@/api/admin/upstream-governance'

export type ModelSelection = { enabled: boolean; models: string[] }
export type ModelSelections = Record<Transport, ModelSelection>
export const defaultAccountName = (baseURL: string, rate: number) => `${baseURL}--${Math.round(rate * 10000) / 10000}`
export const defaultImportConfig = (): ImportAccountConfig => ({
  concurrency: 5000,
  model_mapping: {},
  upstream_billing_rate_sync_enabled: true,
  quota_daily_limit: 10000,
  quota_weekly_limit: 700000,
  quota_limit: 10000000,
  openai_long_context_billing_enabled: true,
})
export function importAccountConfig(config: ImportAccountConfig, platform: Transport, models: ModelSelection, quotaEnabled: boolean): ImportAccountConfig {
  return {
    ...config,
    model_mapping: models.enabled ? Object.fromEntries(models.models.map(model => [model, model])) : {},
    quota_daily_limit: quotaEnabled ? config.quota_daily_limit : 0,
    quota_weekly_limit: quotaEnabled ? config.quota_weekly_limit : 0,
    quota_limit: quotaEnabled ? config.quota_limit : 0,
    openai_long_context_billing_enabled: platform === 'openai' && config.openai_long_context_billing_enabled,
  }
}
