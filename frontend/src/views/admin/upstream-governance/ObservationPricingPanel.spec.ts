import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api from '@/api/admin/upstream-governance'
import ObservationPricingPanel from './ObservationPricingPanel.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${JSON.stringify(values)}` : key }) }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: {
  observationPolicy: vi.fn(), saveObservationPolicy: vi.fn(), pricingPolicies: vi.fn(), savePricingPolicies: vi.fn(),
  previewPricingPolicy: vi.fn(), savePricingPolicy: vi.fn(), savePricingNotifications: vi.fn(),
} }))
enableAutoUnmount(afterEach)

const observation = {
  version: 3,
  policy: { enabled: true, fast_interval_seconds: 10, full_interval_seconds: 900, decrease_stability_seconds: 60, max_rate_increase_percent: 30 },
  status: { last_fast_observed_at: '2026-10-01T01:02:03Z', next_fast_observation_at: '2026-10-01T01:02:13Z', fast_observe_status: 'healthy', fast_observe_error: '', fast_observe_revision: 7 },
}
const row = {
  local_group_id: 12, local_group_name: 'Premium', enabled: true, mode: 'target_margin' as const,
  baseline_cost: 0.2, baseline_sale: 0.3, current_cost: 0.22, current_sale: 0.3, target_sale: 0.33,
  min_margin: 0.2, safety_buffer: 0.05, max_increase_percent: 30, decrease_stability_seconds: 60,
  protected: false, manual_owner: false, status: 'managed',
  sources: [{ source_id: 'site-1', source_name: 'Upstream A', cost: 0.22, eligible: true, comparable: true }],
}
const pricing = {
  version: 5, policies: [row, { ...row, local_group_id: 13, local_group_name: 'Backup' }],
  notifications: { enabled: true, recipients: ['ops@example.test'], group_changes: true, rate_changes: true, pricing_changes: true, protection_changes: true },
}
const draft = { enabled: true, mode: 'target_margin' as const, min_margin: 0.25, safety_buffer: 0.1, max_increase_percent: 30, decrease_stability_seconds: 60 }
const preview = {
  fingerprint: 'server-state-12', local_group_id: 12, current_sale: 0.3, current_cost: 0.22,
  target_sale: 0.3385, projected_margin: 0.3500738552437223, reason: 'price_ready', protected: false, blocked: false,
  sources: row.sources, bindings: [{ site_id: 4, site_name: 'Upstream A', binding_id: 8, remote_group_id: 'claude-max', account_id: 20 }, { site_id: 9, site_name: 'Upstream B', binding_id: 9, remote_group_id: 'backup', account_id: 21 }], policy: draft,
}
async function mounted() {
  const wrapper = mount(ObservationPricingPanel, { props: { siteId: 4 } })
  await flushPromises()
  return wrapper
}
describe('observation and pricing policy reliability', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.observationPolicy).mockResolvedValue(structuredClone(observation))
    vi.mocked(api.pricingPolicies).mockResolvedValue(structuredClone(pricing))
    vi.mocked(api.previewPricingPolicy).mockResolvedValue(structuredClone(preview))
    vi.mocked(api.savePricingPolicy).mockResolvedValue({ ...structuredClone(pricing), policies: [{ ...row, ...draft }, structuredClone(pricing.policies[1])] })
    vi.mocked(api.savePricingNotifications).mockResolvedValue({ ...structuredClone(pricing), version: 6, notifications: { ...pricing.notifications, recipients: ['finance@example.test'] } })
  })
  it('loads configuration without issuing writes or previews', async () => {
    const wrapper = await mounted()
    expect((wrapper.get('[data-test=fast-interval-seconds]').element as HTMLInputElement).value).toBe('10')
    expect(wrapper.get('[data-test=pricing-row-12]').text()).toContain('Premium')
    for (const method of [api.saveObservationPolicy, api.savePricingPolicy, api.savePricingNotifications, api.previewPricingPolicy]) expect(method).not.toHaveBeenCalled()
  })
  it('removes ineffective thresholds and saves only cadence fields without dropping other drafts', async () => {
    vi.mocked(api.saveObservationPolicy).mockResolvedValue({ ...observation, version: 4 })
    const wrapper = await mounted()
    expect(wrapper.find('[data-test=decrease-stability-seconds]').exists()).toBe(false)
    expect(wrapper.find('[data-test=max-rate-increase-percent]').exists()).toBe(false)
    await wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').setValue('35')
    await wrapper.get('[data-test=notification-recipients]').setValue('draft@example.test')
    await wrapper.get('[data-test=fast-interval-seconds]').setValue('1')
    await wrapper.get('[data-test=full-interval-seconds]').setValue('3600')
    await wrapper.get('[data-test=observation-form]').trigger('submit')
    await flushPromises()
    expect(api.saveObservationPolicy).toHaveBeenCalledWith(4, { version: 3, policy: { enabled: true, fast_interval_seconds: 1, full_interval_seconds: 3600 } })
    expect((wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').element as HTMLInputElement).value).toBe('35')
    expect((wrapper.get('[data-test=notification-recipients]').element as HTMLTextAreaElement).value).toBe('draft@example.test')
  })
  it('previews the edited row using only editable fields and renders the returned result and shared impact', async () => {
    const wrapper = await mounted()
    expect((wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').element as HTMLInputElement).value).toBe('20')
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').setValue('25')
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-safety-buffer]').setValue('10')
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    expect(api.previewPricingPolicy).toHaveBeenCalledWith(4, 12, { policy: draft })
    const result = wrapper.get('[data-test=pricing-preview-result-12]').text()
    expect(result).toContain('0.3385')
    expect(result).toContain('35.01%')
    expect(result).toContain('Upstream B')
    expect(api.savePricingPolicy).not.toHaveBeenCalled()
    expect(api.savePricingPolicies).not.toHaveBeenCalled()
  })
  it('saves just the confirmed row and preserves other row and notification drafts', async () => {
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').setValue('25')
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-safety-buffer]').setValue('10')
    await wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').setValue('35')
    await wrapper.get('[data-test=notification-recipients]').setValue('draft@example.test')
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=pricing-apply-12]').trigger('click')
    await flushPromises()
    expect(api.savePricingPolicy).toHaveBeenCalledWith(4, 12, { policy: draft, fingerprint: 'server-state-12' })
    expect(api.savePricingPolicies).not.toHaveBeenCalled()
    expect((wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').element as HTMLInputElement).value).toBe('35')
    expect((wrapper.get('[data-test=notification-recipients]').element as HTMLTextAreaElement).value).toBe('draft@example.test')
    expect(wrapper.text()).toContain('governance.reliability.savePolicyHint')
    expect(wrapper.find('[data-test=pricing-apply-12]').exists()).toBe(false)
  })
  it('invalidates confirmation when draft values change after preview', async () => {
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').setValue('35')
    expect(wrapper.find('[data-test=pricing-apply-12]').exists()).toBe(false)
    expect(api.savePricingPolicy).not.toHaveBeenCalled()
  })
  it('saves notifications without policies and preserves policy drafts', async () => {
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').setValue('35')
    await wrapper.get('[data-test=notification-recipients]').setValue('finance@example.test\nfinance@example.test')
    await wrapper.get('[data-test=notification-form]').trigger('submit')
    await flushPromises()
    expect(api.savePricingNotifications).toHaveBeenCalledWith(4, { version: 5, notifications: { ...pricing.notifications, recipients: ['finance@example.test'] } })
    expect(api.savePricingPolicy).not.toHaveBeenCalled()
    expect(api.savePricingPolicies).not.toHaveBeenCalled()
    expect((wrapper.get('[data-test=pricing-row-13] [data-test=pricing-min-margin]').element as HTMLInputElement).value).toBe('35')
  })
  it('blocks unknown-cost confirmation and exposes no confirmation after failed preview', async () => {
    vi.mocked(api.previewPricingPolicy).mockResolvedValue({ ...preview, current_cost: null, target_sale: null, projected_margin: null, reason: 'unknown_cost', protected: true, blocked: true })
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=pricing-apply-12]').exists()).toBe(false)
    expect(wrapper.get('[data-test=pricing-preview-result-12]').text()).toContain('governance.reliability.feedback.')
    vi.mocked(api.previewPricingPolicy).mockRejectedValue({ reason: 'site_busy' })
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=pricing-apply-12]').exists()).toBe(false)
  })
  it('retains user input after save conflict but requires another preview', async () => {
    vi.mocked(api.savePricingPolicy).mockRejectedValue({ reason: 'stale_preview', status: 409 })
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').setValue('25')
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=pricing-apply-12]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-test=pricing-row-12] [data-test=pricing-min-margin]').element as HTMLInputElement).value).toBe('25')
    expect(wrapper.find('[data-test=pricing-apply-12]').exists()).toBe(false)
  })
  it('ignores a late preview response when the selected site changes', async () => {
    let resolvePreview!: (value: typeof preview) => void
    vi.mocked(api.previewPricingPolicy).mockReturnValue(new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = await mounted()
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await wrapper.setProps({ siteId: 9 })
    await flushPromises()
    resolvePreview(preview)
    await flushPromises()
    expect(api.pricingPolicies).toHaveBeenLastCalledWith(9)
    expect(wrapper.find('[data-test=pricing-preview-result-12]').exists()).toBe(false)
  })
  it('does not label disabled policies as automatically managed', async () => {
    vi.mocked(api.pricingPolicies).mockResolvedValue({ ...pricing, policies: [{ ...row, enabled: false }] })
    const wrapper = await mounted()
    expect(wrapper.get('[data-test=pricing-status-12]').text()).toContain('governance.reliability.status.disabled')
  })
  it('does not silently rebase old notification drafts onto a newer version returned by another save', async () => {
    const latest = { ...structuredClone(pricing), version: 6, notifications: { ...pricing.notifications, recipients: ['other-admin@example.test'] } }
    vi.mocked(api.savePricingPolicy).mockResolvedValue(latest)
    const wrapper = await mounted()
    await wrapper.get('[data-test=notification-recipients]').setValue('my-draft@example.test')
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=pricing-apply-12]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.reliability.notificationConflict')
    await wrapper.get('[data-test=notification-form]').trigger('submit')
    await flushPromises()
    expect(api.savePricingNotifications).not.toHaveBeenCalled()
    expect((wrapper.get('[data-test=notification-recipients]').element as HTMLTextAreaElement).value).toBe('my-draft@example.test')
    vi.mocked(api.pricingPolicies).mockResolvedValue(latest)
    await wrapper.get('[data-test=reload-notifications]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-test=notification-recipients]').element as HTMLTextAreaElement).value).toBe('other-admin@example.test')
  })
  it('can render an initial disabled group when an older backend returns null cost sources', async () => {
    vi.mocked(api.pricingPolicies).mockResolvedValue({ ...pricing, policies: [{ ...row, enabled: false, current_cost: null, sources: null as unknown as typeof row.sources }] })
    const wrapper = await mounted()
    expect(wrapper.get('[data-test=pricing-status-12]').text()).toContain('governance.reliability.status.disabled')
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(wrapper.get('[data-test=pricing-preview-12]').exists()).toBe(true)
  })
})
