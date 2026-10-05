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
})
