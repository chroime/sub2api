import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api from '@/api/admin/upstream-governance'
import ObservationPricingPanel from './ObservationPricingPanel.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${JSON.stringify(values)}` : key }) }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: {
  observationPolicy: vi.fn(), saveObservationPolicy: vi.fn(),
  pricingPolicies: vi.fn(), savePricingPolicies: vi.fn(),
} }))

enableAutoUnmount(afterEach)

const observation = {
  version: 3,
  policy: {
    enabled: true,
    fast_interval_seconds: 10,
    full_interval_seconds: 900,
    decrease_stability_seconds: 60,
    max_rate_increase_percent: 30,
  },
  status: {
    last_fast_observed_at: '2026-10-01T01:02:03Z',
    last_full_collected_at: '2026-10-01T01:00:00Z',
    next_fast_observation_at: '2026-10-01T01:02:13Z',
    fast_observe_status: 'healthy',
    fast_observe_error: '',
    fast_observe_revision: 7,
  },
}

const pricing = {
  version: 5,
  policies: [{
    local_group_id: 12,
    local_group_name: 'Premium',
    enabled: true,
    mode: 'keep_margin',
    baseline_cost: 0.2,
    baseline_sale: 0.3,
    current_cost: 0.22,
    current_sale: 0.3,
    target_sale: 0.33,
    min_margin: 0.2,
    safety_buffer: 0.05,
    max_increase_percent: 30,
    decrease_stability_seconds: 60,
    protected: false,
    manual_owner: false,
    status: 'managed',
    sources: [{ source_id: 'site-1', source_name: 'Upstream A', cost: 0.22, eligible: true, comparable: true }],
  }],
  notifications: {
    enabled: true,
    recipients: ['ops@example.test'],
    group_changes: true,
    rate_changes: true,
    pricing_changes: true,
    protection_changes: true,
  },
}

describe('observation and pricing policy', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.observationPolicy).mockResolvedValue(observation)
    vi.mocked(api.pricingPolicies).mockResolvedValue(pricing)
  })

  it('loads seconds and pricing status without saving on mount', async () => {
    const wrapper = mount(ObservationPricingPanel, { props: { siteId: 4 } })
    await flushPromises()

    expect(api.observationPolicy).toHaveBeenCalledWith(4)
    expect(api.pricingPolicies).toHaveBeenCalledWith(4)
    expect(api.saveObservationPolicy).not.toHaveBeenCalled()
    expect(api.savePricingPolicies).not.toHaveBeenCalled()
    expect((wrapper.get('[data-test=fast-interval-seconds]').element as HTMLInputElement).value).toBe('10')
    expect((wrapper.get('[data-test=full-interval-seconds]').element as HTMLInputElement).value).toBe('900')
    expect(wrapper.get('[data-test=pricing-row-12]').text()).toContain('Premium')
    expect(wrapper.get('[data-test=pricing-row-12]').text()).toContain('0.22')
  })

  it('saves freely configured second intervals and protection thresholds with the version', async () => {
    vi.mocked(api.saveObservationPolicy).mockResolvedValue({ ...observation, version: 4 })
    const wrapper = mount(ObservationPricingPanel, { props: { siteId: 4 } })
    await flushPromises()
    await wrapper.get('[data-test=fast-interval-seconds]').setValue('1')
    await wrapper.get('[data-test=full-interval-seconds]').setValue('3600')
    await wrapper.get('[data-test=observation-form]').trigger('submit')
    await flushPromises()

    expect(api.saveObservationPolicy).toHaveBeenCalledWith(4, {
      version: 3,
      policy: expect.objectContaining({ fast_interval_seconds: 1, full_interval_seconds: 3600 }),
    })
    expect(wrapper.emitted('observation-saved')?.[0]?.[0]).toMatchObject({ version: 4 })
  })

  it('previews and explicitly applies a local pricing policy without changing prices on load', async () => {
    vi.mocked(api.savePricingPolicies).mockResolvedValue({ ...pricing, version: 6, policies: [{ ...pricing.policies[0], current_sale: 0.33 }] })
    const wrapper = mount(ObservationPricingPanel, { props: { siteId: 4 } })
    await flushPromises()
    expect(wrapper.get('[data-test=pricing-preview-12]').text()).toContain('governance.pricingPreview')
    await wrapper.get('[data-test=pricing-preview-12]').trigger('click')
    expect(wrapper.get('[data-test=pricing-apply-12]').exists()).toBe(true)
    expect(wrapper.get('[data-test=pricing-apply-12]').text()).toContain('governance.pricingApply')
    expect(api.savePricingPolicies).not.toHaveBeenCalled()
    await wrapper.get('[data-test=pricing-apply-12]').trigger('click')
    await flushPromises()
    expect(api.savePricingPolicies).toHaveBeenCalledWith(4, expect.objectContaining({ version: 5, policies: expect.arrayContaining([expect.objectContaining({ local_group_id: 12 })]) }))
    expect(wrapper.emitted('pricing-saved')?.[0]?.[0]).toMatchObject({ version: 6 })
  })

  it('persists notification recipients and subscriptions separately from pricing changes', async () => {
    vi.mocked(api.savePricingPolicies).mockResolvedValue({ ...pricing, version: 6 })
    const wrapper = mount(ObservationPricingPanel, { props: { siteId: 4 } })
    await flushPromises()
    await wrapper.get('[data-test=notification-recipients]').setValue('ops@example.test\nfinance@example.test\nops@example.test')
    await wrapper.get('[data-test=notification-form]').trigger('submit')
    await flushPromises()
    expect(api.savePricingPolicies).toHaveBeenCalledWith(4, expect.objectContaining({
      version: 5,
      notifications: expect.objectContaining({ recipients: ['ops@example.test', 'finance@example.test'] }),
    }))
  })
})
