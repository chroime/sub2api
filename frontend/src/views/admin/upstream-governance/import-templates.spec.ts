import { describe, expect, it } from 'vitest'
import { defaultImportConfig } from './import-config'
import { applyImportTemplateSettings, importTemplateSettings, validImportTemplateSettings } from './import-templates'

const settings = { concurrency: 30, priority: 0, quota_enabled: false, quota_daily_limit: 100, quota_weekly_limit: 500, quota_limit: 1000, upstream_billing_rate_sync_enabled: false, openai_long_context_billing_enabled: true }
describe('import template parameter boundaries', () => {
  it('extracts only reusable parameters and supplies the existing default priority', () => {
    const config = { ...defaultImportConfig(), priority: undefined, password: 'private', model_mapping: { 'upstream-model': 'target-model' } }
    expect(importTemplateSettings(config, false)).toEqual({ concurrency: 5000, priority: 1, quota_enabled: false, quota_daily_limit: 10000, quota_weekly_limit: 700000, quota_limit: 10000000, upstream_billing_rate_sync_enabled: true, openai_long_context_billing_enabled: true })
  })
  it('applies account parameters without replacing models or mutating the source', () => {
    const config = { ...defaultImportConfig(), model_mapping: { 'client-*': 'upstream' } }
    const result = applyImportTemplateSettings(config, settings)
    expect(result.quotaEnabled).toBe(false)
    expect(result.config).toEqual({ concurrency: 30, priority: 0, model_mapping: { 'client-*': 'upstream' }, quota_daily_limit: 100, quota_weekly_limit: 500, quota_limit: 1000, upstream_billing_rate_sync_enabled: false, openai_long_context_billing_enabled: true })
    expect(config.concurrency).toBe(5000)
    expect(result.config).not.toHaveProperty('quota_enabled')
  })
  it.each([
    { ...settings, concurrency: 0 }, { ...settings, concurrency: 1.5 }, { ...settings, concurrency: 2147483648 },
    { ...settings, priority: -1 }, { ...settings, priority: 0.5 }, { ...settings, priority: 2147483648 },
    { ...settings, quota_daily_limit: Number.NaN }, { ...settings, quota_weekly_limit: Infinity }, { ...settings, quota_limit: -1 },
    { ...settings, quota_enabled: 1 }, { ...settings, password: 'secret' }, { ...settings, model_mapping: {} },
    { concurrency: 1 }, null, [],
  ])('rejects invalid or extra template fields %#', value => {
    expect(validImportTemplateSettings(value)).toBe(false)
    expect(() => applyImportTemplateSettings(defaultImportConfig(), value as typeof settings)).toThrow()
  })
  it('accepts integer boundaries and finite nonnegative quotas including zero', () => {
    expect(validImportTemplateSettings({ ...settings, concurrency: 2147483647, priority: 2147483647, quota_daily_limit: 0, quota_weekly_limit: 0.001 })).toBe(true)
  })
})
