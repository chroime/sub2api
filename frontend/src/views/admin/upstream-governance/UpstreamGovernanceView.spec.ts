vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' },
}))
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import View from './UpstreamGovernanceView.vue'
import ConnectDialog from './ConnectDialog.vue'
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
    detect: vi.fn(),
    create: vi.fn(),
    connect: vi.fn(),
    sync: vi.fn(),
    keys: vi.fn().mockResolvedValue([]),
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
  beforeEach(() => vi.clearAllMocks())
  it('retains newly connected authorization when automatic collection fails so collection can be retried', async () => {
    const site = { id: 1, name: 'Disconnected site', platform: 'sub2api' as const, base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: false, status: 'disconnected', last_error: '', last_sync_at: null }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.sync).mockRejectedValue({ reason: 'timeout' })
    vi.mocked(api.bindings).mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.get('#governance-collect').attributes('disabled')).toBeDefined()
    await wrapper.get('#governance-reconnect').trigger('click')
    wrapper.getComponent(ConnectDialog).vm.$emit('connected', { ...site, has_credential: true, status: 'connected', version: 2 })
    await flushPromises()
    expect(wrapper.text()).toContain('governance.timeout')
    expect(wrapper.get('#governance-collect').attributes('disabled')).toBeUndefined()
    await wrapper.get('#governance-collect').trigger('click')
    await flushPromises()
    expect(api.sync).toHaveBeenCalledTimes(2)
    expect(api.connect).not.toHaveBeenCalled()
  })
  it('automatically collects after reconnecting an existing site', async () => {
    const site = { id: 1, name: 'Reconnect site', platform: 'sub2api' as const, base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'connected', last_error: '', last_sync_at: null }
    const snapshot = { id: 10, site_id: 1, site_version: 1, created_at: 'new-collection', catalog: { groups: [], channels: [], warnings: [] } }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.sync).mockResolvedValue(snapshot)
    vi.mocked(api.bindings).mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-reconnect').trigger('click')
    wrapper.getComponent(ConnectDialog).vm.$emit('connected')
    await flushPromises()
    expect(api.sync).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('new-collection')
    expect(wrapper.findComponent(ConnectDialog).exists()).toBe(false)
  })
  it('onboards from URL, username and password and automatically collects without a second user action', async () => {
    const site = { id: 8, name: 'Detected site', platform: 'sub2api' as const, base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'connected', last_error: '', last_sync_at: null }
    const snapshot = { id: 10, site_id: 8, site_version: 1, created_at: 'now', catalog: { groups: [], channels: [], warnings: [] } }
    vi.mocked(api.list).mockResolvedValue([])
    vi.mocked(api.detect).mockResolvedValue({ platform: 'sub2api', name: 'Detected site', base_url: site.base_url, captcha_required: false })
    vi.mocked(api.create).mockResolvedValue({ ...site, has_credential: false })
    vi.mocked(api.connect).mockResolvedValue({ site })
    vi.mocked(api.sync).mockResolvedValue(snapshot)
    vi.mocked(api.bindings).mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
    } } })
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'governance.add')!.trigger('click')
    expect(wrapper.findAll('input')).toHaveLength(3)
    await wrapper.get('#governance-onboard-url').setValue(site.base_url)
    await wrapper.get('#governance-onboard-username').setValue('fixture-user')
    await wrapper.get('#governance-onboard-password').setValue('fixture-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.detect).toHaveBeenCalledWith({ base_url: site.base_url, proxy_id: null })
    expect(api.create).toHaveBeenCalledWith({ name: 'Detected site', platform: 'sub2api', base_url: site.base_url, proxy_id: null, enabled: true, interval_minutes: 15 })
    expect(api.connect).toHaveBeenCalledWith(8, expect.objectContaining({ username: 'fixture-user', password: 'fixture-password' }))
    expect(api.sync).toHaveBeenCalledWith(8)
    expect(wrapper.text()).toContain('governance.emptyCatalog')
    expect(wrapper.find('#governance-onboard-password').exists()).toBe(false)
  })
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
