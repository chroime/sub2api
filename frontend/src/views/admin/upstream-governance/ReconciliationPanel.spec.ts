import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ReconciliationPanel from './ReconciliationPanel.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: { reconciliation: vi.fn(), reconcilePreview: vi.fn(), applyReconciliation: vi.fn() } }))
enableAutoUnmount(afterEach)
const row = { binding_id: 11, account_id: 21, remote_group_id: 'r', remote_group_name: 'Remote', account_name: 'Local account', action: 'update' as const, state: 'ready' as const, reason: '', changes: [{ field: 'rate_multiplier', before: '1', after: '1.1' }] }
const data = { snapshot_id: 7, observed_at: '2026-09-27T01:00:00Z', rows: [row] }
const preview = { ...data, id: 'preview-one', site_version: 3, expires_at: '2099-01-01T00:00:00Z' }
describe('governance reconciliation', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.reconciliation).mockResolvedValue(data)
    vi.mocked(api.reconcilePreview).mockResolvedValue(preview)
    vi.mocked(api.applyReconciliation).mockResolvedValue({ preview_id: preview.id, items: [{ binding_id: 11, account_id: 21, status: 'applied' }] })
  })
  it('reads pending work without mutation and requires preview before applying selected bindings', async () => {
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    expect(api.reconciliation).toHaveBeenCalledWith(1)
    expect(api.reconcilePreview).not.toHaveBeenCalled()
    expect(api.applyReconciliation).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test=reconcile-apply]').exists()).toBe(false)
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    expect(api.reconcilePreview).toHaveBeenCalledWith(1)
    expect(api.applyReconciliation).not.toHaveBeenCalled()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    await flushPromises()
    expect(api.applyReconciliation).toHaveBeenCalledWith(1, 'preview-one', [11])
    expect(wrapper.emitted('applied')).toHaveLength(1)
  })
  it('refreshes pending changes after success and keeps the completed result visible', async () => {
    vi.mocked(api.reconciliation).mockResolvedValueOnce(data).mockResolvedValueOnce({ ...data, rows: [] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    await flushPromises()
    expect(api.reconciliation).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('governance.noPendingChanges')
    expect(wrapper.text()).toContain('#21 · governance.success')
    expect(wrapper.find('[data-test=reconcile-apply]').exists()).toBe(false)
  })
  it('keeps a partially failed frozen preview available for retry', async () => {
    vi.mocked(api.applyReconciliation).mockResolvedValue({ preview_id: preview.id, items: [{ binding_id: 11, account_id: 21, status: 'failed', error: 'account_conflict' }] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    await flushPromises()
    expect(api.reconciliation).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test=reconcile-apply]').text()).toBe('governance.retry')
    expect(wrapper.text()).toContain('governance.failed')
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    expect(api.applyReconciliation).toHaveBeenLastCalledWith(1, preview.id, [11])
  })
  it('limits a batch to 100 selections and leaves observation-only rows unselectable', async () => {
    vi.mocked(api.reconciliation).mockResolvedValue({ ...data, rows: [
      ...Array.from({ length: 101 }, (_, index) => ({ ...row, binding_id: index + 1 })),
      { ...row, binding_id: 102, action: 'none', state: 'review', reason: 'missing_confirmation_pending', changes: [] },
    ] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    const choices = wrapper.findAll('[data-test=reconcile-select]')
    for (const choice of choices.slice(0, 100)) await choice.setValue(true)
    expect(choices[100]!.attributes('disabled')).toBeDefined()
    expect(choices[101]!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('governance.reconcileSelectionLimit')
  })
  it('rejects an expired preview before sending an apply request', async () => {
    vi.mocked(api.reconcilePreview).mockResolvedValue({ ...preview, expires_at: '2000-01-01T00:00:00Z' })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    expect(api.applyReconciliation).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test=reconcile-apply]').exists()).toBe(false)
    expect(wrapper.text()).toContain('governance.stale')
  })
  it('describes scheduling and rate sources in user-facing language', async () => {
    vi.mocked(api.reconciliation).mockResolvedValue({ ...data, rows: [{ ...row, reason: 'catalog_stale', changes: [
      { field: 'rate_source', before: 'native', after: 'governance' },
      { field: 'schedulable', before: true, after: false },
    ] }] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    expect(wrapper.text()).toContain('governance.catalogStale')
    expect(wrapper.text()).toContain('governance.rateSourceNative')
    expect(wrapper.text()).toContain('governance.rateSourceGovernance')
    expect(wrapper.text()).toContain('governance.schedulingPaused')
  })
  it('permits explicit review rows but disables conflicts and unavailable accounts', async () => {
    vi.mocked(api.reconciliation).mockResolvedValue({ ...data, rows: [
      { ...row, state: 'review', reason: 'rate_increase_exceeds_limit' },
      { ...row, binding_id: 12, state: 'conflict', reason: 'account_changed' },
      { ...row, binding_id: 13, state: 'unavailable', reason: 'account_missing' },
    ] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    const choices = wrapper.findAll('[data-test=reconcile-select]')
    expect(choices.map(choice => choice.attributes('disabled') !== undefined)).toEqual([false, true, true])
    expect(wrapper.text()).toContain('governance.reconcileStateReview')
    expect(wrapper.text()).toContain('governance.reconcileStateConflict')
    await choices[0]!.setValue(true)
    expect(wrapper.get('[data-test=reconcile-preview]').attributes('disabled')).toBeUndefined()
  })
  it('blocks confirmation when selected work becomes conflicted in the frozen preview', async () => {
    vi.mocked(api.reconcilePreview).mockResolvedValue({ ...preview, rows: [{ ...row, state: 'conflict', reason: 'account_changed' }] })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=reconcile-apply]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    expect(api.applyReconciliation).not.toHaveBeenCalled()
  })
  it('invalidates an expired or rejected preview and never applies it to another site', async () => {
    vi.mocked(api.applyReconciliation).mockRejectedValue({ status: 409 })
    const wrapper = mount(ReconciliationPanel, { props: { siteId: 1 } })
    await flushPromises()
    await wrapper.get('[data-test=reconcile-select]').setValue(true)
    await wrapper.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=reconcile-apply]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=reconcile-apply]').exists()).toBe(false)
    expect(wrapper.text()).toContain('governance.stale')
    await wrapper.setProps({ siteId: 2 })
    await flushPromises()
    expect((wrapper.get('[data-test=reconcile-select]').element as HTMLInputElement).checked).toBe(false)
    expect(api.applyReconciliation).toHaveBeenCalledTimes(1)
  })
})
