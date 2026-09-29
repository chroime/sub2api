import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en/governance'
import zh from '@/i18n/locales/zh/governance'
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
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { governance: en }, zh: { governance: zh } } })]
    }
  })
}

describe('GovernanceSitesOverview compact mode', () => {
  it('renders a compact site rail with count, controls, scrollable rows, and selected state', () => {
    const wrapper = render({ selectedSiteId: 2 })

    expect(wrapper.get('[data-test="sites-overview-compact"]').classes()).toContain('border')
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
})
