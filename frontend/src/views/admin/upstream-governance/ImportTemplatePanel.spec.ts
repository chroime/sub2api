import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import api, { type ImportTemplateCollection, type ImportTemplateSettings } from '@/api/admin/upstream-import-templates'
import zh from '@/i18n/locales/zh/governance'
import common from '@/i18n/locales/zh/common'
import ImportTemplatePanel from './ImportTemplatePanel.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
vi.mock('@/api/admin/upstream-import-templates', () => ({ default: { list: vi.fn(), save: vi.fn() } }))
enableAutoUnmount(afterEach)

const settings: ImportTemplateSettings = { concurrency: 30, priority: 0, quota_enabled: false, quota_daily_limit: 100, quota_weekly_limit: 500, quota_limit: 1000, upstream_billing_rate_sync_enabled: false, openai_long_context_billing_enabled: true }
const currentSettings: ImportTemplateSettings = { ...settings, concurrency: 5000, priority: 1 }
const library: ImportTemplateCollection = { version: 3, templates: [{ id: 'first', name: 'Saved parameters', is_default: false, settings }] }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function setup(props: { pristine?: boolean; disabled?: boolean; settings?: ImportTemplateSettings; onBusy?: (value: boolean) => void } = {}) {
  return mount(ImportTemplatePanel, {
    props: { settings: { ...currentSettings }, pristine: true, scopeKey: 'site-a', ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { ...common, governance: zh } } })] },
  })
}
type Panel = ReturnType<typeof setup>
async function select(wrapper: Panel, id = 'first') {
  wrapper.getComponent(Select).vm.$emit('update:modelValue', id)
  await flushPromises()
}
function defaultLibrary(): ImportTemplateCollection {
  return { version: 3, templates: [{ ...library.templates[0]!, is_default: true }] }
}
describe('safe import configuration templates', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.list).mockResolvedValue(library)
    vi.mocked(api.save).mockImplementation(async value => ({ ...value, version: value.version + 1 }))
  })

  it('loads while disabled and auto-fills only a pristine initial scope without a write lock', async () => {
    const pending = deferred<ImportTemplateCollection>()
    vi.mocked(api.list).mockReturnValue(pending.promise)
    const wrapper = setup({ disabled: true })
    expect(api.list).toHaveBeenCalledOnce()
    expect(wrapper.emitted('ready')).toEqual([[false]])
    expect(wrapper.emitted('busy')).toBeUndefined()
    pending.resolve(defaultLibrary())
    await flushPromises()
    expect(wrapper.emitted('apply')).toEqual([[{ settings, automatic: true }]])
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('first')
    expect(wrapper.getComponent(Select).text()).toContain('Saved parameters · 默认')
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('Saved parameters')
    expect((wrapper.get('[data-test=import-template-default]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.emitted('ready')?.at(-1)).toEqual([true])
    expect(api.save).not.toHaveBeenCalled()
  })

  it('never replaces hand edits with a late default or a later pristine transition', async () => {
    const pending = deferred<ImportTemplateCollection>()
    vi.mocked(api.list).mockReturnValue(pending.promise)
    const wrapper = setup()
    await wrapper.setProps({ pristine: false, settings: { ...currentSettings, concurrency: 9 } })
    pending.resolve(defaultLibrary())
    await flushPromises()
    await wrapper.setProps({ pristine: true })
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[data-test=import-template-default]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.emitted('ready')?.at(-1)).toEqual([true])
  })

  it('keeps current parameters when no default exists and never auto-applies on explicit reload', async () => {
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.emitted('apply')).toBeUndefined()
    vi.mocked(api.list).mockResolvedValue(defaultLibrary())
    await wrapper.get('[data-test=reload-import-templates]').trigger('click')
    await flushPromises()
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('apply')).toBeUndefined()
  })

  it('separates selection from draft fill and requires confirmation for different parameters', async () => {
    const wrapper = setup({ pristine: false })
    await flushPromises()
    await select(wrapper)
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('Saved parameters')
    await wrapper.get('[data-test=apply-import-template]').trigger('click')
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.find('[data-test=confirm-import-template-apply]').exists()).toBe(true)
    await wrapper.get('[data-test=cancel-import-template-apply]').trigger('click')
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.get('[data-test=apply-import-template]').trigger('click')
    await wrapper.get('[data-test=confirm-import-template-apply]').trigger('click')
    expect(wrapper.emitted('apply')).toEqual([[{ settings, automatic: false }]])
    expect(api.save).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('does not require overwrite confirmation when parameters are already identical', async () => {
    const wrapper = setup({ pristine: false, settings: { ...settings } })
    await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test=apply-import-template]').trigger('click')
    expect(wrapper.find('[data-test=confirm-import-template-apply]').exists()).toBe(false)
    expect(wrapper.emitted('apply')).toEqual([[{ settings, automatic: false }]])
  })

  it('shows all eight selected template parameters in a read-only expandable summary before fill confirmation', async () => {
    const wrapper = setup({ pristine: false })
    await flushPromises()
    expect(wrapper.find('[data-test=import-template-summary]').exists()).toBe(false)
    await select(wrapper)
    const summary = wrapper.get('[data-test=import-template-summary]')
    expect(summary.element.tagName).toBe('DETAILS')
    expect(summary.get('summary').text()).toContain('Saved parameters')
    expect(summary.get('[data-test=import-template-value-concurrency]').text()).toBe('30')
    expect(summary.get('[data-test=import-template-value-priority]').text()).toBe('0')
    expect(summary.get('[data-test=import-template-value-quota_enabled]').text()).toBe('关闭')
    expect(summary.get('[data-test=import-template-value-quota_daily_limit]').text()).toBe('100')
    expect(summary.get('[data-test=import-template-value-quota_weekly_limit]').text()).toBe('500')
    expect(summary.get('[data-test=import-template-value-quota_limit]').text()).toBe('1000')
    expect(summary.get('[data-test=import-template-value-upstream_billing_rate_sync_enabled]').text()).toBe('关闭')
    expect(summary.get('[data-test=import-template-value-openai_long_context_billing_enabled]').text()).toBe('开启')
    expect(summary.find('input, select, textarea, button').exists()).toBe(false)
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.get('[data-test=apply-import-template]').trigger('click')
    expect(wrapper.find('[data-test=confirm-import-template-apply]').exists()).toBe(true)
    expect(summary.get('[data-test=import-template-value-concurrency]').text()).toBe('30')
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(api.save).not.toHaveBeenCalled()
  })

  it('allows explicit current-parameter continuation after GET failure but not library writes', async () => {
    vi.mocked(api.list).mockRejectedValue({ status: 503 })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.emitted('ready')).toEqual([[false]])
    await wrapper.get('[data-test=continue-import-parameters]').trigger('click')
    expect(wrapper.emitted('ready')?.at(-1)).toEqual([true])
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.get('[data-test=save-new-import-template]').attributes('disabled')).toBeDefined()
    vi.mocked(api.list).mockResolvedValue(defaultLibrary())
    await wrapper.get('[data-test=reload-import-templates]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.get('[data-test=import-template-name]').setValue('Retry saved')
    expect(wrapper.get('[data-test=save-new-import-template]').attributes('disabled')).toBeUndefined()
  })

  it('saves new templates with current parameters and unique defaults without changing the draft', async () => {
    vi.mocked(api.list).mockResolvedValue(defaultLibrary())
    const wrapper = setup({ pristine: false })
    await flushPromises()
    await wrapper.get('[data-test=import-template-name]').setValue('  New default  ')
    await wrapper.get('[data-test=import-template-default]').setValue(true)
    await wrapper.get('[data-test=save-new-import-template]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ version: 3, templates: [
      { ...library.templates[0], is_default: false },
      { id: expect.stringMatching(/^[a-zA-Z0-9_-]{1,64}$/), name: 'New default', is_default: true, settings: currentSettings },
    ] })
    expect(wrapper.emitted('changed')).toEqual([[]])
    expect(wrapper.emitted('busy')).toEqual([[true], [false]])
    expect(wrapper.emitted('apply')).toBeUndefined()
  })

  it('updates only the selected template and deletes it only after explicit confirmation', async () => {
    const wrapper = setup({ pristine: false })
    await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test=import-template-name]').setValue('Updated')
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenLastCalledWith({ version: 3, templates: [{ id: 'first', name: 'Updated', is_default: false, settings: currentSettings }] })
    await wrapper.get('[data-test=delete-import-template]').trigger('click')
    expect(api.save).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-test=confirm-import-template-delete]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenLastCalledWith({ version: 4, templates: [] })
    expect(wrapper.emitted('changed')).toEqual([[], []])
    expect(wrapper.emitted('apply')).toBeUndefined()
  })

  it('retains conflict edits and selection, blocks blind retry and requires an explicit list reload', async () => {
    const wrapper = setup({ pristine: false })
    await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test=import-template-name]').setValue('My pending changes')
    await wrapper.get('[data-test=import-template-default]').setValue(true)
    vi.mocked(api.save).mockRejectedValueOnce({ status: 409 })
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    await flushPromises()
    expect(api.list).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test=update-import-template]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=save-new-import-template]').attributes('disabled')).toBeDefined()
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('My pending changes')
    expect((wrapper.get('[data-test=import-template-default]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('first')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.emitted('apply')).toBeUndefined()
    vi.mocked(api.list).mockResolvedValue({ ...library, version: 8 })
    await wrapper.get('[data-test=reload-import-templates]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('My pending changes')
    expect((wrapper.get('[data-test=import-template-default]').element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenLastCalledWith({ version: 8, templates: [{ id: 'first', name: 'My pending changes', is_default: true, settings: currentSettings }] })
  })

  it('does not silently recreate a selected template removed by another administrator', async () => {
    const wrapper = setup()
    await flushPromises()
    await select(wrapper)
    vi.mocked(api.save).mockRejectedValueOnce({ status: 409 })
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    await flushPromises()
    vi.mocked(api.list).mockResolvedValue({ version: 4, templates: [] })
    await wrapper.get('[data-test=reload-import-templates]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('first')
    expect(wrapper.get('[data-test=update-import-template]').attributes('disabled')).toBeDefined()
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('Saved parameters')
    expect(api.save).toHaveBeenCalledOnce()
  })

  it('resets scope and ignores an old GET response', async () => {
    const old = deferred<ImportTemplateCollection>()
    const next = deferred<ImportTemplateCollection>()
    vi.mocked(api.list).mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const wrapper = setup()
    await wrapper.setProps({ scopeKey: 'site-b' })
    next.resolve({ version: 0, templates: [] })
    await flushPromises()
    old.resolve(defaultLibrary())
    await flushPromises()
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.emitted('ready')).toEqual([[false], [false], [true]])
    expect(wrapper.getComponent(Select).props('options')).toHaveLength(1)
  })

  it('releases the write lock on a scope switch and ignores a late PUT response', async () => {
    const pending = deferred<ImportTemplateCollection>()
    vi.mocked(api.save).mockReturnValueOnce(pending.promise)
    const wrapper = setup()
    await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    vi.mocked(api.list).mockResolvedValue({ version: 0, templates: [] })
    await wrapper.setProps({ scopeKey: 'site-b' })
    await flushPromises()
    pending.resolve({ version: 4, templates: library.templates })
    await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
    expect((wrapper.get('[data-test=import-template-name]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
  })

  it('releases the parent write lock when unmounted before PUT resolves', async () => {
    const pending = deferred<ImportTemplateCollection>()
    vi.mocked(api.save).mockReturnValue(pending.promise)
    const onBusy = vi.fn()
    const wrapper = setup({ onBusy })
    await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    wrapper.unmount()
    expect(onBusy.mock.calls).toEqual([[true], [false]])
    pending.resolve(library)
    await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('disables user mutations and fill while externally locked or saving', async () => {
    const wrapper = setup()
    await flushPromises()
    await select(wrapper)
    await wrapper.setProps({ disabled: true })
    for (const action of ['apply-import-template', 'save-new-import-template', 'update-import-template', 'delete-import-template']) {
      expect(wrapper.get(`[data-test=${action}]`).attributes('disabled')).toBeDefined()
      await wrapper.get(`[data-test=${action}]`).trigger('click')
    }
    expect(api.save).not.toHaveBeenCalled()
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.setProps({ disabled: false })
    const pending = deferred<ImportTemplateCollection>()
    vi.mocked(api.save).mockReturnValue(pending.promise)
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    expect(wrapper.get('[data-test=apply-import-template]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=reload-import-templates]').attributes('disabled')).toBeDefined()
    pending.resolve(library)
    await flushPromises()
  })

  it.each(['   ', 'a'.repeat(101), 'invalid\u0001name', 'invalid\u0085name'])('blocks invalid template names %# without a write', async name => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=import-template-name]').setValue(name)
    await wrapper.get('[data-test=save-new-import-template]').trigger('click')
    expect(api.save).not.toHaveBeenCalled()
  })

  it('counts Unicode codepoints rather than UTF-16 units for valid names', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=import-template-name]').setValue('😀'.repeat(100))
    await wrapper.get('[data-test=save-new-import-template]').trigger('click')
    await flushPromises()
    expect(api.save).toHaveBeenCalledOnce()
  })

  it('blocks invalid draft settings and limits new templates to fifty without blocking updates', async () => {
    vi.mocked(api.list).mockResolvedValue({ version: 2, templates: Array.from({ length: 50 }, (_, index) => ({ ...library.templates[0]!, id: `template_${index}` })) })
    const wrapper = setup()
    await flushPromises()
    await select(wrapper, 'template_0')
    expect(wrapper.get('[data-test=save-new-import-template]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test=update-import-template]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ settings: { ...currentSettings, concurrency: 0 } })
    await wrapper.get('[data-test=update-import-template]').trigger('click')
    expect(api.save).not.toHaveBeenCalled()
  })

  it('rejects an invalid default response without applying any parameters', async () => {
    vi.mocked(api.list).mockResolvedValue({ version: 1, templates: [{ ...defaultLibrary().templates[0]!, settings: { ...settings, priority: -1 } }] })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.emitted('ready')).toEqual([[false]])
    expect(wrapper.find('[data-test=continue-import-parameters]').exists()).toBe(true)
  })
})
