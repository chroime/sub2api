vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' },
}))
import { mount, flushPromises, enableAutoUnmount, config } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import View from './UpstreamGovernanceView.vue'
import ConnectDialog from './ConnectDialog.vue'
import OnboardDialog from './OnboardDialog.vue'
import BalanceMonitorPanel from './BalanceMonitorPanel.vue'
import BalanceHealthPanel from './BalanceHealthPanel.vue'
import ImportPanel from './ImportPanel.vue'
import ReconciliationPanel from './ReconciliationPanel.vue'
import SiteEditDialog from './SiteEditDialog.vue'
import SiteOverview from './SiteOverview.vue'
import AutomationPolicyPanel from './AutomationPolicyPanel.vue'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import GovernanceSitesOverview from './GovernanceSitesOverview.vue'
import ModelMonitorPanel from './ModelMonitorPanel.vue'
import modelAPI from '@/api/admin/upstream-model-monitoring'
import operationsAPI from '@/api/admin/upstream-operations'
import api, { type BalanceHealth, type Site, type Snapshot } from '@/api/admin/upstream-governance'
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
    modelTemplates: vi.fn().mockResolvedValue({ version: 0, templates: [] }),
    balanceMonitor: vi.fn(),
    remove: vi.fn(),
    loginCredentials: vi.fn(),
    update: vi.fn(),
    automation: vi.fn().mockResolvedValue({ version: 0, policy: { enabled: false, sync_rate: true, sync_name: true, pause_missing: true, restore_returned: true, missing_confirmations: 2, max_rate_increase_percent: 20 } }),
    observationPolicy: vi.fn().mockResolvedValue({ version: 0, policy: { enabled: false, fast_interval_seconds: 10, full_interval_seconds: 900, decrease_stability_seconds: 60, max_rate_increase_percent: 20 }, status: {} }),
    saveObservationPolicy: vi.fn(),
    pricingPolicies: vi.fn().mockResolvedValue({ version: 0, policies: [], notifications: { enabled: false, recipients: [], group_changes: true, rate_changes: true, pricing_changes: true, protection_changes: true } }),
    savePricingPolicies: vi.fn(),
    balanceHealth: vi.fn().mockResolvedValue(null),
    readiness: vi.fn().mockResolvedValue({ site_id: 1, evaluated_at: '2026-10-01T10:00:00Z', checks: [] }),
    reconciliation: vi.fn().mockResolvedValue({ snapshot_id: 0, observed_at: null, rows: [] }),
    reconcilePreview: vi.fn(),
    rechargePlan: vi.fn().mockResolvedValue({ version: 0, policy: { mode: 'disabled', threshold: 10, unit: 'usd', amount_minor: 1000, currency: 'USD', daily_budget_minor: 10000, cooldown_minutes: 1440 }, capability: { available: false, reason: 'provider_unavailable' }, status: 'disabled', evaluation: null }),
    revealKey: vi.fn(),
  },
}))
vi.mock('@/api/admin/groups', () => ({
  default: { getAll: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/api/admin/upstream-model-monitoring', () => ({ default: { policies: vi.fn().mockResolvedValue([]), runs: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, counts: {} }), stats: vi.fn().mockResolvedValue({ days: 7, groups: [] }) } }))
vi.mock('@/api/admin/upstream-operations', () => ({ default: { workbench: vi.fn(), timeline: vi.fn() } }))
vi.mock('@/api/admin/upstream-import-templates', () => ({ default: { list: vi.fn().mockResolvedValue({ version: 0, templates: [] }), save: vi.fn() } }))
vi.mock('@/api/admin/proxies', () => ({
  default: { getAll: vi.fn().mockResolvedValue([]) },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
describe('governance page', () => {
  let router: Router
  beforeEach(async () => {
    vi.clearAllMocks()
    vi.mocked(api.keys).mockResolvedValue([])
    vi.mocked(api.readiness).mockResolvedValue({ site_id: 1, evaluated_at: '2026-10-01T10:00:00Z', checks: [] })
    vi.mocked(operationsAPI.workbench).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, evaluated_at: '2026-10-01T14:00:00Z', summary: { critical: 0, warning: 0, info: 0 } })
    vi.mocked(operationsAPI.timeline).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, evaluated_at: '2026-10-01T14:00:00Z' })
    router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/admin/upstream-governance/:section?', component: View },
        { path: '/login', component: { template: '<div>Login</div>' } },
      ],
    })
    await router.push('/admin/upstream-governance')
    await router.isReady()
    config.global.plugins = [router]
  })
  afterEach(() => { vi.useRealTimers(); config.global.plugins = [] })
  function setupNavigationSites() {
    const site: Site = { id: 1, name: 'Upstream A', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site, { ...site, id: 2, name: 'Upstream B' }])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
  }
  it('shows the all-site current workbench before selection without running upstream operations', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.get('[data-test=operations-workbench]').isVisible()).toBe(true)
    expect(operationsAPI.workbench).toHaveBeenCalled()
    expect(vi.mocked(operationsAPI.workbench).mock.calls.every(([query]) => !query?.site_id)).toBe(true)
    expect(api.catalog).not.toHaveBeenCalled()
    expect(api.sync).not.toHaveBeenCalled()
    expect(api.connect).not.toHaveBeenCalled()
    expect(api.check).not.toHaveBeenCalled()
  })
  it('scopes current tasks to the selected site and offers a route back to all-site overview', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance?site=2')
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.getComponent({ name: 'GovernanceWorkbench' }).props('siteId')).toBe(2)
    await wrapper.get('#governance-all-sites').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance')
    expect(wrapper.getComponent({ name: 'GovernanceWorkbench' }).props('siteId')).toBeUndefined()
    expect(wrapper.findAll('[data-test=operations-workbench]')).toHaveLength(1)
  })
  it('opens a task destination without applying actions and rejects missing sites or unknown sections', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    const board = wrapper.getComponent({ name: 'GovernanceWorkbench' })
    board.vm.$emit('navigate', { siteId: 99, section: 'monitor' })
    board.vm.$emit('navigate', { siteId: 2, section: 'external' })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance')
    board.vm.$emit('navigate', { siteId: 2, section: 'monitor' })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance/monitor?site=2')
    expect(wrapper.get('#governance-site-2').attributes('aria-current')).toBe('true')
    expect(api.sync).not.toHaveBeenCalled()
    expect(api.connect).not.toHaveBeenCalled()
    expect(api.check).not.toHaveBeenCalled()
  })
  it('keeps task navigation and all-site navigation blocked during authorization', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance?site=1')
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-reconnect').trigger('click')
    wrapper.getComponent({ name: 'GovernanceWorkbench' }).vm.$emit('navigate', { siteId: 2, section: 'monitor' })
    await wrapper.get('#governance-all-sites').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance?site=1')
  })
  it('loads the factual timeline only in history while keeping existing raw records accessible', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance?site=1')
    const wrapper = mount(View)
    await flushPromises()
    expect(operationsAPI.timeline).not.toHaveBeenCalled()
    await wrapper.get('#governance-history-tab').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=operations-timeline]').isVisible()).toBe(true)
    expect(operationsAPI.timeline).toHaveBeenCalledWith(1, expect.any(Object))
    expect(wrapper.get('[data-test=raw-governance-history]').exists()).toBe(true)
    expect(wrapper.text()).toContain('governance.workbench.readDoesNotResolve')
    expect(api.check).not.toHaveBeenCalled()
    await wrapper.get('#governance-models-tab').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=operations-timeline]').exists()).toBe(false)
  })
  it('shows functional tabs before choosing an upstream without fetching site details', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.findAll('[role=tab]')).toHaveLength(5)
    expect(wrapper.get('[data-test=smart-operations-site-rail]').isVisible()).toBe(true)
    await wrapper.get('#governance-models-tab').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/upstream-governance/models')
    expect(wrapper.get('[data-test=sites-overview-compact]').isVisible()).toBe(true)
    expect(api.catalog).not.toHaveBeenCalled()
    expect(modelAPI.policies).not.toHaveBeenCalled()
  })
  it('restores the selected site and model section from a direct link', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance/models?site=2')
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.get('#governance-models-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(2)
    expect(api.catalog).toHaveBeenCalledTimes(1)
    expect(api.catalog).toHaveBeenCalledWith(2)
    expect(api.revealKey).not.toHaveBeenCalled()
  })
  it('preserves the selected site across tabs and switches sites without resetting the section', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-models-tab').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance/models?site=1')
    expect(api.catalog).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test=smart-operations-site-rail]').isVisible()).toBe(true)
    expect(wrapper.get('#governance-site-1').attributes('aria-current')).toBe('true')
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(1)
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance/models?site=2')
    expect(wrapper.get('#governance-site-2').attributes('aria-current')).toBe('true')
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(2)
    expect(wrapper.get('#governance-models-tab').attributes('aria-selected')).toBe('true')
  })
  it('refreshes key incident counts in the left rail without reloading the selected site', async () => {
    let refresh!: () => void
    const timer = vi.spyOn(globalThis, 'setInterval').mockImplementation((callback) => {
      refresh = callback as () => void
      return 9001 as ReturnType<typeof setInterval>
    })
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    const selected = wrapper.get('#governance-site-1')
    vi.mocked(api.list).mockResolvedValue([
      { id: 1, name: 'Upstream A', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null, key_issue_count: 2 },
      { id: 2, name: 'Upstream B', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null },
    ])
    refresh()
    await flushPromises()
    expect(wrapper.get('#governance-site-1').element).toBe(selected.element)
    expect(wrapper.get('#governance-site-1 [data-test=site-key-issues]').text()).toContain('governance.keyIssues')
    expect(wrapper.getComponent(GovernanceSitesOverview).props('sites')[0].key_issue_count).toBe(2)
    expect(router.currentRoute.value.query.site).toBe('1')
    expect(api.catalog).toHaveBeenCalledTimes(1)
    timer.mockRestore()
  })
  it('responds to browser history without reloading unchanged site data', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance?site=1')
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-history-tab').trigger('click')
    await flushPromises()
    await new Promise<void>(resolve => {
      const remove = router.afterEach(() => { remove(); resolve() })
      router.back()
    })
    await flushPromises()
    expect(wrapper.get('#governance-overview-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-test=site-overview-tab]').isVisible()).toBe(true)
    expect(api.catalog).toHaveBeenCalledTimes(1)
  })
  it('clears rail selection and hides cached details when history returns to a route without a site', async () => {
    setupNavigationSites()
    await router.push('/admin/upstream-governance?site=1')
    const wrapper = mount(View, { attachTo: document.body })
    await flushPromises()
    await router.push('/admin/upstream-governance/history')
    await flushPromises()
    expect(wrapper.get('[data-test=site-selection-empty]').isVisible()).toBe(true)
    expect(wrapper.get('[data-test=active-site-workspace]').isVisible()).toBe(false)
    expect(wrapper.findAll('[data-test=smart-operations-site-rail] [aria-current=true]')).toHaveLength(0)
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance/history?site=1')
    expect(wrapper.get('[data-test=active-site-workspace]').isVisible()).toBe(true)
    expect(api.catalog).toHaveBeenCalledTimes(1)
  })
  it('keeps the active site and route unchanged when rail selection is attempted during authorization', async () => {
    setupNavigationSites()
    const wrapper = mount(View, { global: { stubs: { ConnectDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-reconnect').trigger('click')
    expect(wrapper.get('#governance-site-2').attributes('disabled')).toBeDefined()
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.site).toBe('1')
    expect(wrapper.getComponent(ConnectDialog).props('siteId')).toBe(1)
    expect(api.catalog).toHaveBeenCalledTimes(1)
    wrapper.getComponent(ConnectDialog).vm.$emit('close')
    await flushPromises()
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.site).toBe('2')
  })
  it('blocks rail selection while onboarding is open', async () => {
    setupNavigationSites()
    const wrapper = mount(View, { global: { stubs: { OnboardDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-add-site').trigger('click')
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.site).toBeUndefined()
    expect(api.catalog).not.toHaveBeenCalled()
    expect(wrapper.getComponent(OnboardDialog).exists()).toBe(true)
  })
  it.each(['999', '-1', 'abc', '1&site=2'])('shows the site picker for missing or invalid selection %s', async site => {
    setupNavigationSites()
    await router.push(`/admin/upstream-governance/import?site=${site}`)
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.get('[data-test=sites-overview-compact]').isVisible()).toBe(true)
    expect(wrapper.text()).toContain('governance.smartOperations.siteNotFound')
    expect(api.catalog).not.toHaveBeenCalled()
  })
  it('blocks sidebar-style route changes during a write and unlocks afterwards', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    wrapper.getComponent(BalanceMonitorPanel).vm.$emit('busy', true)
    await flushPromises()
    expect(wrapper.get('#governance-site-2').attributes('disabled')).toBeDefined()
    await wrapper.get('#governance-site-2').trigger('click')
    await router.push('/admin/upstream-governance/models?site=2')
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance?site=1')
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').id).toBe(1)
    wrapper.getComponent(BalanceMonitorPanel).vm.$emit('busy', false)
    await flushPromises()
    await router.push('/admin/upstream-governance/models?site=2')
    await flushPromises()
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(2)
  })
  it('blocks route changes during explicit collection and releases the lock on failure', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    let reject!: (reason: unknown) => void
    vi.mocked(api.sync).mockImplementationOnce(() => new Promise((_, fail) => { reject = fail }))
    await wrapper.get('#governance-collect').trigger('click')
    await router.push('/admin/upstream-governance/models?site=2')
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance?site=1')
    reject({ reason: 'timeout' })
    await flushPromises()
    await router.push('/admin/upstream-governance/models?site=2')
    await flushPromises()
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(2)
  })
  it('closes the plaintext key dialog when browser navigation returns to the site picker', async () => {
    setupNavigationSites()
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-view-keys').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ManagedKeysPanel).exists()).toBe(true)
    await router.push('/admin/upstream-governance')
    await flushPromises()
    expect(wrapper.get('[data-test=sites-overview-compact]').isVisible()).toBe(true)
    expect(wrapper.findComponent(ManagedKeysPanel).exists()).toBe(false)
  })
  it('keeps authorization inside its site context until the dialog is closed', async () => {
    setupNavigationSites()
    const wrapper = mount(View, { global: { stubs: { ConnectDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-reconnect').trigger('click')
    await router.push('/admin/upstream-governance/models?site=2')
    expect(router.currentRoute.value.fullPath).toBe('/admin/upstream-governance?site=1')
    wrapper.getComponent(ConnectDialog).vm.$emit('close')
    await flushPromises()
    await router.push('/admin/upstream-governance/models?site=2')
    await flushPromises()
    expect(wrapper.getComponent(ModelMonitorPanel).props('siteId')).toBe(2)
  })
  it('refreshes key metadata when navigation dismisses its dialog for the retained site', async () => {
    setupNavigationSites()
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-view-keys').trigger('click')
    await flushPromises()
    const key = { id: 9, site_id: 1, remote_group_id: 'r', platform: 'openai' as const, remote_key_id: 'created-key', marker: 'new', has_key: true, created_at: '', updated_at: '' }
    vi.mocked(api.keys).mockResolvedValue([key])
    await router.push('/admin/upstream-governance')
    await flushPromises()
    await router.push('/admin/upstream-governance/models?site=1')
    await flushPromises()
    expect(wrapper.getComponent(ModelMonitorPanel).props('managedKeys')).toEqual([key])
    expect(api.catalog).toHaveBeenCalledTimes(1)
    expect(api.revealKey).not.toHaveBeenCalled()
  })
  it('invalidates import previews only after a committed key repair and preserves the import draft', async () => {
    let refresh!: () => void
    const timer = vi.spyOn(globalThis, 'setInterval').mockImplementation(callback => {
      refresh = callback as () => void
      return 9002 as ReturnType<typeof setInterval>
    })
    setupNavigationSites()
    vi.mocked(api.catalog).mockResolvedValue({ id: 1, site_id: 1, site_version: 1, created_at: 'now', catalog: { groups: [{ id: 'r', name: 'Remote', platform: 'openai', rate_multiplier: 1, user_rate_multiplier: null, resolved_rate_multiplier: 1, models: ['fixture-model'], prices: [], source: 'user', peak_rate_enabled: false }], channels: [], warnings: [] } })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    const importPanel = wrapper.getComponent(ImportPanel)
    await importPanel.get('[data-test=select]').setValue(true)
    await importPanel.get('[data-test=priority]').setValue(7)
    const initialEpoch = importPanel.props('previewEpoch')
    importPanel.vm.$emit('applied')
    await flushPromises()
    refresh()
    await flushPromises()
    await wrapper.get('#governance-view-keys').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test=close-dialog]').trigger('click')
    await flushPromises()
    expect(importPanel.props('previewEpoch')).toBe(initialEpoch)

    await wrapper.get('#governance-view-keys').trigger('click')
    await flushPromises()
    const bindingsBeforeRepair = vi.mocked(api.bindings).mock.calls.length
    wrapper.getComponent(ManagedKeysPanel).vm.$emit('repaired')
    await flushPromises()
    expect(importPanel.props('previewEpoch')).not.toBe(initialEpoch)
    expect(api.bindings).toHaveBeenCalledTimes(bindingsBeforeRepair + 1)
    expect(wrapper.getComponent(ImportPanel).element).toBe(importPanel.element)
    expect((importPanel.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
    expect((importPanel.get('[data-test=priority]').element as HTMLInputElement).value).toBe('7')
    timer.mockRestore()
  })
  it('does not trap logout or expired-session redirects behind a write lock', async () => {
    setupNavigationSites()
    const wrapper = mount(View)
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    wrapper.getComponent(BalanceMonitorPanel).vm.$emit('busy', true)
    await flushPromises()
    await router.push('/login')
    expect(router.currentRoute.value.path).toBe('/login')
  })
  it('loads model monitoring only in its separate tab and unmounts it when returning to automation', async () => {
    const site: Site = { id: 1, name: 'Models site', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    vi.mocked(api.list).mockResolvedValue([site]); vi.mocked(api.catalog).mockRejectedValue({ status: 404 }); vi.mocked(api.bindings).mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page); vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View); await flushPromises(); await wrapper.get('#governance-site-1').trigger('click'); await flushPromises()
    expect(modelAPI.policies).not.toHaveBeenCalled()
    await wrapper.get('#governance-models-tab').trigger('click'); await flushPromises()
    expect(modelAPI.policies).toHaveBeenCalledWith(1)
    expect(wrapper.findComponent(ModelMonitorPanel).exists()).toBe(true)
    await wrapper.get('#governance-monitor-tab').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ModelMonitorPanel).exists()).toBe(false)
  })
  async function setupProbeMonitor() {
    const site: Site = { id: 1, name: 'Monitor upstream', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([{ id: 5, site_id: 1, remote_group_id: 'remote', platform: 'openai', local_group_id: 3, account_id: 8, probe_enabled: true, probe_model: 'fixture-model', probe_interval_minutes: 30 }])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-monitor-tab').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'governance.monitor')!.trigger('click')
    return wrapper
  }
  it.each([1, 1441, 10081, 2147483647])('saves a freely entered probe interval of %s minutes unchanged', async minutes => {
    const wrapper = await setupProbeMonitor()
    const input = wrapper.get('[data-test=probe-interval]')
    expect(input.attributes('min')).toBe('1')
    expect(input.attributes('step')).toBe('1')
    expect(input.attributes('max')).toBeUndefined()
    await input.setValue(String(minutes))
    expect((input.element as HTMLInputElement).checkValidity()).toBe(true)
    await input.element.closest('form')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
    await flushPromises()
    expect(api.monitor).toHaveBeenCalledWith(1, 5, { enabled: true, model: 'fixture-model', interval_minutes: minutes })
    expect(api.check).not.toHaveBeenCalled()
  })
  it.each(['', '0', '-1', '1.5', 'Infinity', '1e309', '2147483648'])('rejects invalid probe interval %j before saving', async minutes => {
    const wrapper = await setupProbeMonitor()
    const input = wrapper.get('[data-test=probe-interval]')
    await input.setValue(minutes)
    await input.element.closest('form')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
    await flushPromises()
    expect(api.monitor).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(minutes === '2147483648' ? 'governance.intervalTooLarge' : 'governance.intervalPositiveInteger')
  })
  it('starts with all sites and does not fetch per-site data or plaintext keys before selection', async () => {
    const site: Site = { id: 1, name: 'First upstream', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    vi.mocked(api.list).mockResolvedValue([site])
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    expect(wrapper.get('[data-test=sites-overview-compact]').isVisible()).toBe(true)
    expect(wrapper.find('[data-test=site-overview-tab]').exists()).toBe(false)
    expect(api.catalog).not.toHaveBeenCalled()
    expect(api.keys).not.toHaveBeenCalled()
    expect(api.revealKey).not.toHaveBeenCalled()
    expect(api.rechargePlan).not.toHaveBeenCalled()
  })
  it('preserves import selections between tabs and opens one on-demand key dialog from either entry point', async () => {
    const site: Site = { id: 1, name: 'Imported upstream', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const snapshot: Snapshot = { id: 1, site_id: 1, site_version: 1, created_at: '2026-09-27T01:00:00Z', catalog: { groups: [{ id: 'r', name: 'Remote', platform: 'openai', rate_multiplier: 1, user_rate_multiplier: null, resolved_rate_multiplier: 1, models: [], prices: [], source: 'user', peak_rate_enabled: false }], channels: [], warnings: [] } }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    const key = { id: 9, site_id: 1, remote_group_id: 'r', platform: 'openai' as const, remote_key_id: 'fixture-key', marker: 'existing', has_key: true, created_at: '', updated_at: '' }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockResolvedValue(snapshot)
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.keys).mockResolvedValue([key])
    vi.mocked(api.revealKey).mockResolvedValue({ managed_key: key, key: 'fixture-plaintext-key' })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=site-overview-tab]').isVisible()).toBe(true)
    expect(wrapper.findComponent(ManagedKeysPanel).exists()).toBe(false)
    expect(api.revealKey).not.toHaveBeenCalled()
    await wrapper.get('#governance-import-tab').trigger('click')
    const importPanel = wrapper.getComponent(ImportPanel)
    await importPanel.get('[data-test=select]').setValue(true)
    await importPanel.get('[data-test=priority]').setValue(7)
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(api.catalog).toHaveBeenCalledTimes(1)
    expect(wrapper.getComponent(ImportPanel).element).toBe(importPanel.element)
    await wrapper.get('#governance-monitor-tab').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-history-tab').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-import-tab').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(ImportPanel).element).toBe(importPanel.element)
    expect((importPanel.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
    expect((importPanel.get('[data-test=priority]').element as HTMLInputElement).value).toBe('7')
    await wrapper.get('#governance-view-keys').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(ManagedKeysPanel)).toHaveLength(1)
    expect(wrapper.get('[data-test=key-secret]').text()).toBe('fixture-plaintext-key')
    expect(api.revealKey).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-test=close-dialog]').trigger('click')
    expect(wrapper.findComponent(ManagedKeysPanel).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('fixture-plaintext-key')
    await wrapper.get('#governance-import-keys').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(ManagedKeysPanel)).toHaveLength(1)
    expect(wrapper.getComponent(ManagedKeysPanel).props('selections')).toEqual([{ remote_group_id: 'r', platform: 'openai' }])
    expect(api.revealKey).toHaveBeenCalledTimes(2)
  })
  it.each([false, true])('refreshes the balance observation as a pair without resetting import or frozen changes (catalog failure: %s)', async catalogFails => {
    const site: Site = { id: 1, name: 'Observed upstream', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const snapshot: Snapshot = { id: 1, site_id: 1, site_version: 1, created_at: '2026-09-27T01:00:00Z', catalog: { groups: [{ id: 'r', name: 'Remote', platform: 'openai', rate_multiplier: 1, user_rate_multiplier: null, resolved_rate_multiplier: 1, models: [], prices: [], source: 'user', peak_rate_enabled: false }], account: { user_id: 1, username: 'fixture', email: '', balance: 100, frozen_balance: 0, used_balance: 0, unit: 'usd', source: 'user' }, channels: [], warnings: [] } }
    const latest: Snapshot = { ...snapshot, id: 2, created_at: '2026-09-27T01:15:00Z', catalog: { ...snapshot.catalog, account: { ...snapshot.catalog.account!, balance: 5 } } }
    const health: BalanceHealth = { collection_enabled: true, interval_minutes: 15, last_attempt_at: snapshot.created_at, observed_at: snapshot.created_at, next_run_at: latest.created_at, stale: false, monitor_enabled: true, state: 'healthy', delivery_ready: false, recipient_count: 1, reason: 'healthy', delivery_reason: 'smtp_not_configured', last_notified_at: null, last_delivery_error: '' }
    const nextHealth: BalanceHealth = { ...health, observed_at: latest.created_at, state: 'low', reason: 'low' }
    const row = { binding_id: 11, account_id: 21, remote_group_id: 'r', remote_group_name: 'Remote', account_name: 'Local account', action: 'update' as const, state: 'ready' as const, reason: '', changes: [{ field: 'rate_multiplier', before: 1, after: 1.1 }] }
    const reconciliation = { snapshot_id: snapshot.id, observed_at: snapshot.created_at, rows: [row] }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockResolvedValueOnce(snapshot)
    if (catalogFails) vi.mocked(api.catalog).mockRejectedValueOnce({ status: 503 })
    else vi.mocked(api.catalog).mockResolvedValueOnce(latest)
    vi.mocked(api.balanceHealth).mockResolvedValueOnce(health).mockResolvedValueOnce(nextHealth)
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.reconciliation).mockResolvedValueOnce(reconciliation)
    vi.mocked(api.reconcilePreview).mockResolvedValueOnce({ ...reconciliation, id: 'frozen-preview', site_version: 1, expires_at: '2099-01-01T00:00:00Z' })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test=account-balance]').text()).toBe('100 usd')
    const importPanel = wrapper.getComponent(ImportPanel)
    await importPanel.get('[data-test=select]').setValue(true)
    await importPanel.get('[data-test=priority]').setValue(7)
    const changes = wrapper.getComponent(ReconciliationPanel)
    await changes.get('[data-test=reconcile-select]').setValue(true)
    await changes.get('[data-test=reconcile-preview]').trigger('click')
    await flushPromises()
    wrapper.getComponent(BalanceHealthPanel).vm.$emit('reload')
    await flushPromises()
    expect(wrapper.get('[data-test=account-balance]').text()).toBe(catalogFails ? 'governance.unknown' : '5 usd')
    expect(wrapper.getComponent(BalanceHealthPanel).props('health')).toEqual(catalogFails ? null : nextHealth)
    expect(wrapper.getComponent(ImportPanel).element).toBe(importPanel.element)
    expect(importPanel.props('snapshot').id).toBe(snapshot.id)
    expect((importPanel.get('[data-test=select]').element as HTMLInputElement).checked).toBe(true)
    expect((importPanel.get('[data-test=priority]').element as HTMLInputElement).value).toBe('7')
    expect(changes.find('[data-test=reconcile-apply]').exists()).toBe(true)
    expect(api.reconciliation).toHaveBeenCalledTimes(1)
    expect(api.sync).not.toHaveBeenCalled()
  })
  it('refreshes the readiness checklist after an automation policy save', async () => {
    setupNavigationSites()
    const initial = { site_id: 1, evaluated_at: '2026-10-01T10:00:00Z', checks: [{ key: 'automation', state: 'not_enabled' as const, detail: 'automation_disabled', target_tab: 'monitor' as const, count: 0 }] }
    const refreshed = { ...initial, evaluated_at: '2026-10-01T10:01:00Z', checks: [{ ...initial.checks[0], state: 'configured' as const, detail: '' }] }
    vi.mocked(api.readiness).mockResolvedValue(initial)
    await router.push('/admin/upstream-governance/monitor?site=1')
    const wrapper = mount(View)
    await flushPromises()
    expect(wrapper.getComponent(SiteOverview).props('readiness')).toEqual(initial)

    vi.mocked(api.readiness).mockResolvedValue(refreshed)
    wrapper.getComponent(AutomationPolicyPanel).vm.$emit('saved', { version: 2, policy: { enabled: true, sync_rate: true, sync_name: true, pause_missing: true, restore_returned: true, missing_confirmations: 2, max_rate_increase_percent: 20 } })
    await flushPromises()
    expect(wrapper.getComponent(SiteOverview).props('readiness')).toEqual(refreshed)
    expect(api.readiness).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
  it('refreshes the readiness checklist after operational settings are saved', async () => {
    const site: Site = { id: 1, name: 'Readiness upstream', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.readiness).mockResolvedValue({ site_id: 1, evaluated_at: '2026-10-01T10:00:00Z', checks: [] })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(api.readiness).toHaveBeenCalledTimes(1)
    await wrapper.get('#governance-monitor-tab').trigger('click')
    await flushPromises()
    vi.mocked(api.readiness).mockClear()

    wrapper.getComponent({ name: 'AutomationPolicyPanel' }).vm.$emit('saved', { version: 2, policy: {} })
    await flushPromises()
    expect(api.readiness).toHaveBeenCalledTimes(1)
    wrapper.getComponent({ name: 'ObservationPricingPanel' }).vm.$emit('observation-saved', { version: 2, policy: {} })
    await flushPromises()
    expect(api.readiness).toHaveBeenCalledTimes(2)
    wrapper.getComponent(BalanceMonitorPanel).vm.$emit('saved', { ...site, version: 2 })
    await flushPromises()

    expect(api.readiness).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })
  it('opens the site editor with saved login and refreshes metadata after saving without reconnecting', async () => {
    const site: Site = { id: 1, name: 'Editable', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.loginCredentials).mockResolvedValue({ username: 'fixture-user', password: 'fixture-password', version: 1 })
    vi.mocked(api.update).mockResolvedValue({ ...site, name: 'Renamed', version: 2 })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(SiteEditDialog).exists()).toBe(true)
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe('fixture-password')
    await wrapper.get('#governance-edit-name').setValue('Renamed')
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(wrapper.findComponent(SiteEditDialog).exists()).toBe(false)
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').name).toBe('Renamed')
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').version).toBe(2)
    expect(api.connect).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('waits for existing key metadata before enabling imports and preserves key-only Grok compatibility', async () => {
    const site: Site = { id: 1, name: 'Legacy Grok', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const snapshot: Snapshot = { id: 1, site_id: 1, site_version: 1, created_at: '2026-09-26T15:08:02Z', catalog: { groups: [], channels: [], warnings: [] } }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockResolvedValue(snapshot)
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    let finish!: (value: Awaited<ReturnType<typeof api.keys>>) => void
    vi.mocked(api.keys).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ImportPanel).exists()).toBe(false)
    const key = { id: 9, site_id: 1, remote_group_id: 'grok', platform: 'openai' as const, remote_key_id: 'old-key', marker: 'existing', has_key: false, created_at: '', updated_at: '' }
    finish([key])
    await flushPromises()
    expect(wrapper.getComponent(ImportPanel).props('managedKeys')).toEqual([key])
    expect(wrapper.getComponent(ImportPanel).props('sitePlatform')).toBe('sub2api')
    wrapper.unmount()
  })
  it('blocks imports when existing key metadata fails to load', async () => {
    const site: Site = { id: 1, name: 'Unavailable metadata', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockResolvedValue({ id: 1, site_id: 1, site_version: 1, created_at: '', catalog: { groups: [], channels: [], warnings: [] } })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.keys).mockRejectedValueOnce({ status: 503 })
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ImportPanel).exists()).toBe(false)
    expect(wrapper.text()).toContain('governance.importStateUnavailable')
    wrapper.unmount()
  })
  it('does not surface ancillary history or key read failures as a monitor-page operation error', async () => {
    const site: Site = { id: 1, name: 'Monitor site', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockRejectedValue({ status: 503 })
    vi.mocked(api.checks).mockRejectedValue({ status: 503 })
    vi.mocked(api.keys).mockRejectedValue({ status: 503 })

    await router.push('/admin/upstream-governance/monitor?site=1')
    const wrapper = mount(View, { global: { stubs: { BalanceMonitorPanel: true } } })
    await flushPromises()

    expect(wrapper.get('#governance-monitor-panel').isVisible()).toBe(true)
    expect(wrapper.find('#governance-monitor-panel > p[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('explains an in-use deletion conflict and selects the remaining site after successful deletion', async () => {
    const site: Site = { id: 1, name: 'Sample', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const real = { ...site, id: 2, name: 'Real upstream' }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site, real])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    vi.mocked(api.remove).mockRejectedValueOnce({ status: 409, reason: 'site_in_use' }).mockResolvedValueOnce(undefined)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('[aria-label="common.delete"]').trigger('click')
    await wrapper.get('.btn-danger').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.siteInUse')
    expect(wrapper.text()).not.toContain('governance.stale')
    vi.mocked(api.list).mockResolvedValue([real])
    await wrapper.get('.btn-danger').trigger('click')
    await flushPromises()
    expect(api.remove).toHaveBeenLastCalledWith(1)
    expect(wrapper.find('#governance-site-1').exists()).toBe(false)
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').id).toBe(2)
    wrapper.unmount()
  })
  it.each(['sync', 'apply'] as const)('blocks stale imports after a %s metadata reload fails and recovers by reselecting the site', async action => {
    const site: Site = { id: 1, name: 'Metadata reload', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const snapshot: Snapshot = { id: 1, site_id: 1, site_version: 1, created_at: '', catalog: { groups: [], channels: [], warnings: [] } }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.catalog).mockResolvedValue(snapshot)
    vi.mocked(api.sync).mockResolvedValue({ ...snapshot, id: 2 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ImportPanel).exists()).toBe(true)
    vi.mocked(api.keys).mockRejectedValueOnce({ status: 503 })
    if (action === 'sync') await wrapper.get('#governance-collect').trigger('click')
    else wrapper.getComponent(ImportPanel).vm.$emit('applied')
    await flushPromises()
    expect(wrapper.findComponent(ImportPanel).exists()).toBe(false)
    expect(wrapper.text()).toContain('governance.importStateUnavailable')
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ImportPanel).exists()).toBe(true)
    wrapper.unmount()
  })
  it('locks site changes during a balance save and retains the returned site version for later visits', async () => {
    const site: Site = { id: 1, name: 'Site A', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.list).mockResolvedValue([site, { ...site, id: 2, name: 'Site B' }])
    vi.mocked(api.catalog).mockRejectedValue({ status: 404 })
    vi.mocked(api.bindings).mockResolvedValue([])
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    let finish!: (value: Site) => void
    vi.mocked(api.balanceMonitor).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-monitor-tab').trigger('click')
    const monitor = wrapper.getComponent(BalanceMonitorPanel)
    await monitor.get('#governance-balance-settings').trigger('click')
    await monitor.get('form').trigger('submit')
    expect(wrapper.get('#governance-site-2').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#governance-collect').attributes('disabled')).toBeDefined()
    finish({ ...site, version: 2, balance_monitor: { enabled: true, threshold: 10, unit: 'usd', recipients: [], cooldown_minutes: 1440 } })
    await flushPromises()
    expect(wrapper.get('#governance-site-2').attributes('disabled')).toBeUndefined()
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').version).toBe(2)
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').balance_monitor?.enabled).toBe(true)
    wrapper.unmount()
  })
  it('keeps the newly selected site when an earlier catalog request completes later', async () => {
    const site: Site = { id: 1, name: 'Slow A', platform: 'sub2api', base_url: 'https://fixture.example', enabled: true, interval_minutes: 15, proxy_id: null, version: 1, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
    const snapshot: Snapshot = { id: 1, site_id: 1, site_version: 1, created_at: '2026-09-26T15:08:02Z', catalog: { groups: [], channels: [], warnings: [] } }
    let finish!: (value: Snapshot) => void
    vi.mocked(api.list).mockResolvedValue([site, { ...site, id: 2, name: 'Fast B' }])
    vi.mocked(api.catalog).mockImplementation(id => id === 1 ? new Promise(resolve => { finish = resolve }) : Promise.resolve({ ...snapshot, id: 2, site_id: 2 }))
    vi.mocked(api.bindings).mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, pages: 0, page_size: 20 }
    vi.mocked(api.events).mockResolvedValue(page)
    vi.mocked(api.checks).mockResolvedValue(page)
    const wrapper = mount(View, { global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    await wrapper.get('#governance-site-1').trigger('click')
    await flushPromises()
    await wrapper.get('#governance-site-2').trigger('click')
    await flushPromises()
    finish(snapshot)
    await flushPromises()
    expect(wrapper.getComponent(ImportPanel).props('snapshot').site_id).toBe(2)
    expect(wrapper.getComponent(BalanceMonitorPanel).props('site').id).toBe(2)
    wrapper.unmount()
  })
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
    const snapshot = { id: 10, site_id: 1, site_version: 1, created_at: '2026-09-26T15:08:02+08:00', catalog: { groups: [], channels: [], warnings: [] } }
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
    expect(wrapper.text()).toMatch(/2026-09-26 \d{2}:08:02/)
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
    expect(wrapper.getComponent(OnboardDialog).findAll('input')).toHaveLength(3)
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
