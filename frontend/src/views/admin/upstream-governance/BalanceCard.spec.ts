import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/governance'
import type { Site } from '@/api/admin/upstream-governance'
import BalanceCard from './BalanceCard.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
enableAutoUnmount(afterEach)

it('shows dollars in the balance card without changing the quota-based monitoring settings', () => {
  const site: Site = { id: 18, name: 'TokenGo', platform: 'newapi', base_url: 'https://upstream.example', proxy_id: null, enabled: true, interval_minutes: 15, version: 2, has_credential: true, status: 'reauth_required', last_error: '', last_sync_at: null }
  const wrapper = mount(BalanceCard, {
    props: { site, health: null, balance: 4993033, unit: 'quota', quotaPerUsd: 500000 },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })],
      stubs: { BalanceHealthPanel: true, BalanceMonitorPanel: { name: 'BalanceMonitorPanel', props: ['unit', 'site'], template: '<div />' } },
    },
  })
  expect(wrapper.get('[data-test=balance-card-amount]').text()).toBe('$9.986066')
  expect(wrapper.get('[data-test=balance-card-amount]').attributes('title')).toContain('4993033 quota')
  expect(wrapper.getComponent({ name: 'BalanceMonitorPanel' }).props('unit')).toBe('quota')
})
