import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en/governance'
import zh from '@/i18n/locales/zh/governance'
import enCommon from '@/i18n/locales/en/common'
import type { Site } from '@/api/admin/upstream-governance'
import GovernanceSitesOverview from './GovernanceSitesOverview.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
enableAutoUnmount(afterEach)

const sites: Site[] = [
  {
    id: 1, name: 'Primary', platform: 'sub2api', base_url: 'https://primary.example', proxy_id: null,
    enabled: true, interval_minutes: 10, version: 1, has_credential: true, status: 'connected',
    last_error: '', last_sync_at: '2026-09-29T08:00:00Z'
  },
  {
    id: 2, name: 'Paused', platform: 'newapi', base_url: 'https://paused.example', proxy_id: null,
    enabled: false, interval_minutes: 15, version: 1, has_credential: false, status: 'paused',
    last_error: '', last_sync_at: null
  }
]

function render(props: Record<string, unknown> = {}) {
  return mount(GovernanceSitesOverview, {
    props: { sites, compact: true, ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { ...enCommon, governance: en }, zh: { governance: zh } } })]
    }
  })
}

describe('GovernanceSitesOverview compact mode', () => {
  it('renders a compact site rail with count, controls, scrollable rows, and selected state', () => {
    const wrapper = render({ selectedSiteId: 2 })

    expect(wrapper.get('[data-test="sites-overview-compact"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="site-count"]').text()).toContain('2')
    expect(wrapper.get('[data-test="site-search"]')).toBeTruthy()
    expect(wrapper.findAll('button[aria-pressed]')).toHaveLength(3)
    expect(wrapper.get('[data-test="site-list"]').classes()).toContain('overflow-y-auto')
    expect(wrapper.get('#governance-site-1')).toBeTruthy()
    expect(wrapper.get('#governance-site-2').attributes('aria-current')).toBe('true')
    expect(wrapper.get('#governance-site-2').classes()).toContain('site-row--selected')
    expect(wrapper.find('dl').exists()).toBe(false)
  })

  it('emits the selected site and prevents selection while disabled', async () => {
    const wrapper = render()
    await wrapper.get('#governance-site-1').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[sites[0]]])

    const disabled = render({ disabled: true })
    await disabled.get('#governance-site-1').trigger('click')
    expect(disabled.emitted('select')).toBeUndefined()
    expect(disabled.get('#governance-site-1').attributes('disabled')).toBeDefined()
  })
  it('shows a low balance warning independently of connection health and retains collection state', () => {
    const wrapper = render({ sites: [{ ...sites[0], balance_monitor_status: { state: 'low', last_attempt_at: null, last_notified_at: null, last_error: '' } }, sites[1]] })
    expect(wrapper.get('#governance-site-1').text()).toContain(en.balanceState_low)
    expect(wrapper.get('#governance-site-1').text()).toContain('Every 10 min')
    expect(wrapper.get('#governance-site-1 [data-test=site-status-dot]').classes()).toContain('bg-amber-500')
    expect(wrapper.get('#governance-site-2').text()).toContain(en.siteRail.collectionPaused)
  })
  it('filters by address and paused state without changing the selected site', async () => {
    const wrapper = render({ selectedSiteId: 2 })
    await wrapper.get('[data-test=site-search]').setValue('primary.example')
    expect(wrapper.find('#governance-site-1').exists()).toBe(true)
    expect(wrapper.find('#governance-site-2').exists()).toBe(false)
    expect(wrapper.emitted('select')).toBeUndefined()
    await wrapper.get('[data-test=site-search]').setValue('')
    await wrapper.get('[data-test=site-filter-paused]').trigger('click')
    expect(wrapper.find('#governance-site-1').exists()).toBe(false)
    expect(wrapper.get('#governance-site-2').attributes('aria-current')).toBe('true')
  })
  it('shows confirmed upstream-key incidents in the compact site rail and attention filter', async () => {
    const wrapper = render({ sites: [{ ...sites[0], key_issue_count: 2 }, sites[1]] })
    expect(wrapper.get('#governance-site-1 [data-test=site-key-issues]').text()).toContain('2')
    await wrapper.get('[data-test=site-filter-attention]').trigger('click')
    expect(wrapper.find('#governance-site-1').exists()).toBe(true)
    expect(wrapper.find('#governance-site-2').exists()).toBe(false)
  })
  it('keeps each row to two lines: status dot, name, last sync, then issues or cadence', () => {
    vi.useFakeTimers({ now: new Date('2026-09-29T08:05:30Z') })
    try {
      const wrapper = render({ sites: [
        sites[0],
        { ...sites[0], id: 3, name: 'Broken', status: 'reauth_required', last_error: 'reauth_required', last_sync_at: '2026-09-29T08:06:00Z' },
        sites[1],
      ] })
      const healthy = wrapper.get('#governance-site-1')
      expect(healthy.get('[data-test=site-status-dot]').classes()).toContain('bg-emerald-500')
      expect(healthy.text()).toContain('5m ago')
      expect(healthy.text()).toContain('Every 10 min')
      expect(healthy.text()).not.toContain('Sub2API')
      expect(healthy.get('[data-test=site-platform-logo]').attributes('alt')).toBe('Sub2API')
      expect(wrapper.get('#governance-site-2 [data-test=site-platform-logo]').attributes('alt')).toBe('New API')
      expect(healthy.text()).not.toContain('https://primary.example')
      expect(healthy.attributes('title')).toContain('https://primary.example')
      expect(healthy.attributes('title')).toContain(en.autoOn)

      const broken = wrapper.get('#governance-site-3')
      expect(broken.get('[data-test=site-status-dot]').classes()).toContain('bg-red-500')
      expect(broken.text()).toContain(en.stateReauth)
      expect(broken.text()).toContain(enCommon.common.time.justNow)
      expect(wrapper.get('#governance-site-2 [data-test=site-status-dot]').classes()).toContain('bg-gray-300')
      expect(wrapper.get('#governance-site-2').text()).toContain(enCommon.common.time.never)
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows filter counts and a site picker for layouts where the rail stacks above the workspace', async () => {
    const wrapper = render({ sites: [{ ...sites[0], key_issue_count: 1 }, sites[1]], selectedSiteId: 1 })
    expect(wrapper.get('[data-test=site-filter-all]').text()).toContain('2')
    expect(wrapper.get('[data-test=site-filter-attention]').text()).toContain('1')
    expect(wrapper.get('[data-test=site-filter-paused]').text()).toContain('1')

    const picker = wrapper.getComponent({ name: 'Select' })
    expect(picker.props('modelValue')).toBe(1)
    expect(picker.props('options')).toEqual([
      { value: 1, label: `Primary · ${en.siteFilter_attention}` },
      { value: 2, label: `Paused · ${en.siteRail.collectionPaused}` },
    ])
    picker.vm.$emit('update:modelValue', 2)
    expect(wrapper.emitted('select')).toEqual([[sites[1]]])
  })

  it('shows the latest collected balance on the right of each row', () => {
    const observed = '2026-11-05T15:56:00Z'
    const wrapper = render({ sites: [
      { ...sites[0], balance: { amount: 8.15156006, unit: 'usd', observed_at: observed } },
      { ...sites[1], balance: { amount: 69792, unit: 'quota', observed_at: observed } },
      { ...sites[0], id: 3, name: 'Overdrawn', balance: { amount: -0.03516571, unit: 'usd', observed_at: observed } },
      { ...sites[0], id: 4, name: 'Low', balance: { amount: 0.25, unit: 'usd', observed_at: observed }, balance_monitor_status: { state: 'low', last_attempt_at: null, last_notified_at: null, last_error: '' } },
      { ...sites[0], id: 5, name: 'Uncollected' },
    ] })
    const balance = (id: number) => wrapper.find(`#governance-site-${id} [data-test=site-balance]`)
    expect(balance(1).text()).toBe('$8.15')
    expect(balance(1).classes()).toContain('text-gray-700')
    expect(balance(2).text()).toBe(`69.8K ${en.siteRail.quotaUnit}`)
    expect(balance(3).text()).toBe('-$0.04')
    expect(balance(3).classes()).toContain('text-red-600')
    expect(balance(4).classes()).toContain('text-amber-700')
    expect(balance(5).exists()).toBe(false)
    expect(wrapper.get('#governance-site-1').attributes('title')).toContain('8.15156006 usd')
    const picker = wrapper.getComponent({ name: 'Select' })
    expect(picker.props('options')).toContainEqual({ value: 1, label: 'Primary · $8.15' })
  })

  it('shows the collection cadence the scheduler actually runs', () => {
    const wrapper = render({ sites: [{ ...sites[0], interval_minutes: 1, full_interval_seconds: 20, fast_interval_seconds: 1, fast_observe_enabled: true }] })
    expect(wrapper.get('#governance-site-1').text()).toContain('Every 20s · observe 1s')
    expect(wrapper.get('#governance-site-1').text()).not.toContain('1 min')
  })
})
