vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' },
}))
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import View from './UpstreamGovernanceView.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: {
    list: vi.fn(),
    check: vi.fn(),
    monitor: vi.fn(),
    catalog: vi.fn(),
    bindings: vi.fn(),
    events: vi.fn(),
    checks: vi.fn(),
  },
}))
vi.mock('@/api/admin/groups', () => ({
  default: { getAll: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/api/admin/proxies', () => ({
  default: { getAll: vi.fn().mockResolvedValue([]) },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('governance page', () => {
  it('loads successful catalog/history without issuing billable probes or enabling monitoring', async () => {
    vi.mocked(api.list).mockResolvedValue([
      {
        id: 1,
        name: 'Fixture site',
        platform: 'sub2api',
        base_url: 'https://fixture.example',
        enabled: true,
        interval_minutes: 15,
        proxy_id: null,
        version: 1,
        has_credential: true,
        status: 'connected',
        last_error: 'persistent_encryption_required',
        last_sync_at: null,
      },
    ])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([{ id: 5, site_id: 1, remote_group_id: 'pending', platform: 'openai', local_group_id: 3, account_id: 0, probe_enabled: false, probe_model: '', probe_interval_minutes: 30 }])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          BaseDialog: true,
        },
      },
    })
    await flushPromises()
    const site = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Fixture site'))!
    await site.trigger('click')
    await flushPromises()
    expect(api.catalog).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('governance.noSnapshot')
    expect(wrapper.text()).toContain('governance.stateConnected')
    expect(wrapper.text()).toContain('governance.encryption')
    expect(wrapper.text()).toContain('governance.pendingImport')
    expect(wrapper.text()).not.toContain('→ #0')
    for (const key of ['governance.check', 'governance.monitor']) {
      const button = wrapper.findAll('button').find(b => b.text() === key)!
      expect(button.attributes('disabled')).toBeDefined()
      await button.trigger('click')
    }
    expect(api.check).not.toHaveBeenCalled()
    expect(api.monitor).not.toHaveBeenCalled()
  })
})
