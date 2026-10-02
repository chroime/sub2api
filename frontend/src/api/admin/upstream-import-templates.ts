import { apiClient } from '@/api/client'

export interface ImportTemplateSettings {
  concurrency: number
  priority: number
  quota_enabled: boolean
  quota_daily_limit: number
  quota_weekly_limit: number
  quota_limit: number
  upstream_billing_rate_sync_enabled: boolean
  openai_long_context_billing_enabled: boolean
}
export interface ImportTemplate {
  id: string
  name: string
  is_default: boolean
  settings: ImportTemplateSettings
}
export interface ImportTemplateCollection {
  version: number
  templates: ImportTemplate[]
}

const endpoint = '/admin/upstream-governance/import-templates'
const api = {
  async list(): Promise<ImportTemplateCollection> {
    return (await apiClient.get<ImportTemplateCollection>(endpoint)).data
  },
  async save(value: ImportTemplateCollection): Promise<ImportTemplateCollection> {
    const templates = value.templates.map(({ id, name, is_default, settings }) => ({
      id,
      name,
      is_default,
      settings: {
        concurrency: settings.concurrency,
        priority: settings.priority,
        quota_enabled: settings.quota_enabled,
        quota_daily_limit: settings.quota_daily_limit,
        quota_weekly_limit: settings.quota_weekly_limit,
        quota_limit: settings.quota_limit,
        upstream_billing_rate_sync_enabled: settings.upstream_billing_rate_sync_enabled,
        openai_long_context_billing_enabled: settings.openai_long_context_billing_enabled,
      },
    }))
    return (await apiClient.put<ImportTemplateCollection>(endpoint, { version: value.version, templates })).data
  },
}
export default api
