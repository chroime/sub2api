import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api from '@/api/admin/upstream-import-templates'
import governanceAPI from '@/api/admin/upstream-governance'
import Select from '@/components/common/Select.vue'
import ImportTemplatesView from './ImportTemplatesView.vue'
import ImportSettingsPanel from './ImportSettingsPanel.vue'
import { defaultImportConfig, defaultModelSelections } from './import-config'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${JSON.stringify(values)}` : key }) }))
vi.mock('@/api/admin/upstream-import-templates', () => ({ default: { list: vi.fn(), save: vi.fn() } }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: { modelTemplates: vi.fn(), saveModelTemplates: vi.fn() } }))
enableAutoUnmount(afterEach)

const settings = { concurrency: 30, priority: 0, quota_enabled: false, quota_daily_limit: 1, quota_weekly_limit: 2, quota_limit: 3, upstream_billing_rate_sync_enabled: false, openai_long_context_billing_enabled: true }
const library = { version: 2, templates: [{ id: 'first', name: 'Saved', is_default: true, settings }] }

describe('import templates in global settings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.list).mockResolvedValue(structuredClone(library))
    vi.mocked(api.save).mockImplementation(async value => ({ ...value, version: value.version + 1 }))
    vi.mocked(governanceAPI.modelTemplates).mockResolvedValue({ version: 0, templates: [] })
  })

  it('loads a chosen template into the editor and saves edits back to it', async () => {
    const wrapper = mount(ImportTemplatesView)
    await flushPromises()
    // The library never fills a draft by itself and has no fill action.
    expect(wrapper.find('[data-test=apply-import-template]').exists()).toBe(false)
    expect((wrapper.get('[data-test=concurrency]').element as HTMLInputElement).value).toBe('5000')
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'first')
    await flushPromises()
    expect((wrapper.get('[data-test=concurrency]').element as HTMLInputElement).value).toBe('30')
    expect((wrapper.get('[data-test=quota-enabled]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('[data-test=concurrency]').setValue('40')
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ version: 2, templates: [{ id: 'first', name: 'Saved', is_default: true, settings: { ...settings, concurrency: 40 } }] })
  })
})

describe('import parameters summary', () => {
  it('summarizes the draft in one line and keeps the dialog content mounted while closed', async () => {
    const models = { ...defaultModelSelections(), openai: { enabled: true, models: ['a', 'b', 'c'] } }
    vi.mocked(governanceAPI.modelTemplates).mockResolvedValue({ version: 0, templates: [] })
    const wrapper = mount(ImportSettingsPanel, {
      props: { config: { ...defaultImportConfig(), concurrency: 8, priority: 2 }, models, quotaEnabled: false, groups: [], templateName: 'Team' },
      global: { stubs: { teleport: true, transition: false } },
    })
    await flushPromises()
    const summary = wrapper.get('[data-test=import-parameters-summary]').text()
    expect(summary).toContain('governance.importParameters.template:{"name":"Team"}')
    expect(summary).toContain('governance.importParameters.concurrency:{"n":8}')
    expect(summary).toContain('governance.importParameters.priority:{"n":2}')
    expect(summary).toContain('"count":3')
    expect(summary).not.toContain('governance.importParameters.quota')
    // Default model templates must still load while the dialog is closed.
    expect(wrapper.find('[data-test=import-parameters-dialog]').exists()).toBe(true)
    expect(wrapper.get('[data-test=import-parameters-dialog]').isVisible()).toBe(false)
    await wrapper.get('[data-test=open-import-parameters]').trigger('click')
    expect(wrapper.get('[data-test=import-parameters-dialog]').isVisible()).toBe(true)
    await wrapper.get('[data-test=manage-model-templates]').trigger('click')
    expect(wrapper.emitted('manage')).toHaveLength(1)
    expect(wrapper.get('[data-test=import-parameters-dialog]').isVisible()).toBe(false)
  })
})
