import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/governance'
import SiteOverview from './SiteOverview.vue'
import type { ReadinessOverview, Site, Snapshot } from '@/api/admin/upstream-governance'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
enableAutoUnmount(afterEach)

const site: Site = {
  id: 1,
  name: '上游一',
  platform: 'sub2api',
  base_url: 'https://fixture.example',
  proxy_id: null,
  enabled: true,
  interval_minutes: 15,
  version: 1,
  has_credential: true,
  status: 'healthy',
  last_error: '',
  last_sync_at: null,
}
const snapshot: Snapshot = {
  id: 1,
  site_id: 1,
  site_version: 1,
  created_at: '2026-10-01T10:00:00Z',
  catalog: { groups: [], channels: [], warnings: [] },
}
const readiness: ReadinessOverview = {
  site_id: 1,
  evaluated_at: '2026-10-01T10:00:00Z',
  checks: [{ key: 'bindings', state: 'not_configured', detail: 'no_bindings', target_tab: 'import', count: 0 }],
}

describe('SiteOverview readiness integration', () => {
  it('displays USD quota balances while retaining unknown amounts and original quota in tooltips', () => {
    const wrapper = mount(SiteOverview, {
      props: { site, snapshot: { ...snapshot, catalog: { ...snapshot.catalog, account: { user_id: 262, username: 'TokenGo', email: '', balance: 4993033, frozen_balance: null, used_balance: 7004, unit: 'quota', quota_per_usd: 500000, source: '/api/user/self' } } }, bindingCount: 0, readiness },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] },
    })
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('$9.986066')
    expect(wrapper.get('[data-test=account-balance]').attributes('title')).toContain('4993033 quota')
    expect(wrapper.get('[data-test=account-used]').text()).toBe('$0.014008')
    expect(wrapper.get('[data-test=account-frozen]').text()).toBe('未知')
  })

  it.each([undefined, 0, -1, Infinity, NaN])('retains raw quota when the USD divisor is invalid: %s', (divisor) => {
    const wrapper = mount(SiteOverview, {
      props: { site, snapshot: { ...snapshot, catalog: { ...snapshot.catalog, account: { user_id: 262, username: '', email: '', balance: 4993033, frozen_balance: null, used_balance: 0, unit: 'quota', quota_per_usd: divisor, source: '/api/user/self' } } }, bindingCount: 0, readiness },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] },
    })
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('4993033 quota')
  })

  it.each([
    { name: 'known amounts', frozen: 1.25, used: 2.5, expectedFrozen: '1.25 USD', expectedUsed: '2.5 USD' },
    { name: 'zero amounts', frozen: 0, used: 0, expectedFrozen: '0 USD', expectedUsed: '0 USD' },
    { name: 'unknown amounts', frozen: null, used: null, expectedFrozen: '未知', expectedUsed: '未知' },
  ])('shows frozen and used balances directly for $name', ({ frozen, used, expectedFrozen, expectedUsed }) => {
    const wrapper = mount(SiteOverview, {
      props: { site, snapshot: { ...snapshot, catalog: { ...snapshot.catalog, account: { user_id: 42, username: '余额账户', email: '', balance: 10, frozen_balance: frozen, used_balance: used, unit: 'USD', source: '/api/v1/user/profile' } } }, bindingCount: 0, readiness },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] },
    })
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('10 USD')
    expect(wrapper.text()).toContain('余额账户')
    expect(wrapper.text()).toContain('冻结余额')
    expect(wrapper.text()).toContain('已用额度')
    const frozenValue = wrapper.get('[data-test=account-frozen]')
    const usedValue = wrapper.get('[data-test=account-used]')
    expect(frozenValue.text()).toBe(expectedFrozen)
    expect(usedValue.text()).toBe(expectedUsed)
    expect(frozenValue.element.closest('details')).toBeNull()
    expect(usedValue.element.closest('details')).toBeNull()
  })

  it('renders the read-only checklist and relays navigation without write actions', async () => {
    const wrapper = mount(SiteOverview, {
      props: { site, snapshot, bindingCount: 0, readiness },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })],
      },
    })
    expect(wrapper.get('[data-test="readiness-checklist"]')).toBeTruthy()
    await wrapper.get('[data-target="import"]').trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['import']])
  })

  it('shows the real collection cadence and folds the checklist once everything is configured', () => {
    const configured: ReadinessOverview = { ...readiness, checks: [{ key: 'bindings', state: 'configured', target_tab: 'import' }, { key: 'keys', state: 'configured', target_tab: 'import' }] }
    const wrapper = mount(SiteOverview, {
      props: { site: { ...site, interval_minutes: 1, full_interval_seconds: 20, fast_interval_seconds: 1, fast_observe_enabled: true }, snapshot, bindingCount: 3, readiness: configured },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })] },
    })
    expect(wrapper.get('[data-test=site-cadence]').text()).toBe('每 20 秒 · 观察 1 秒')
    const folded = wrapper.get('[data-test=readiness-complete]')
    expect(folded.attributes('open')).toBeUndefined()
    expect(folded.text()).toContain('2')
  })
})
