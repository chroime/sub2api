import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import AppSidebar from '../AppSidebar.vue'
import applicationRouter from '@/router'

const appStore = reactive({
  siteName: 'Test gateway',
  siteLogo: '',
  siteVersion: 'v0.2.4',
  contactInfo: '',
  publicSettingsLoaded: true,
  sidebarCollapsed: false,
  sidebarScrollTop: 0,
  mobileOpen: false,
  backendModeEnabled: false,
  cachedPublicSettings: null,
  toggleSidebar() { this.sidebarCollapsed = !this.sidebarCollapsed },
  setSidebarCollapsed(collapsed: boolean) { this.sidebarCollapsed = collapsed },
  setMobileOpen(open: boolean) { this.mobileOpen = open },
})
const authStore = reactive({ isAdmin: true, isSimpleMode: false })
const adminSettingsStore = {
  customMenuItems: [],
  fetch: vi.fn().mockResolvedValue(null),
}

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useAdminSettingsStore: () => adminSettingsStore,
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: ref(false),
    refreshBatchImageAccess: vi.fn().mockResolvedValue(false),
  }),
}))
vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
  }),
}))
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({ triggerPrefetch: vi.fn() }),
}))

enableAutoUnmount(afterEach)

const rootPath = '/admin/upstream-governance'
const sectionCases = [
  ['', 'overview'],
  ['/overview', 'overview'],
  ['/import', 'import'],
  ['/models', 'models'],
  ['/monitor', 'monitor'],
  ['/history', 'history'],
] as const

async function mountSidebar(path = '/admin/dashboard') {
  const governanceRoute = applicationRouter.getRoutes().find((route) => route.name === 'AdminUpstreamGovernance')!
  const placeholder = { template: '<div />' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: governanceRoute.path, name: governanceRoute.name, meta: governanceRoute.meta, component: placeholder },
      { path: '/:pathMatch(.*)*', component: placeholder },
    ],
  })
  await router.push(path)
  await router.isReady()

  const wrapper = mount(AppSidebar, {
    global: { plugins: [router], stubs: { VersionBadge: true, SiteLogo: true } },
  })
  return { wrapper, router }
}

function smartOperationsButton(wrapper: VueWrapper) {
  const button = wrapper.findAll('button').find((item) => item.text() === 'governance.smartOperations.title')
  expect(button, 'Smart Operations must be an expandable navigation group').toBeDefined()
  return button!
}

function activeSectionLinks(wrapper: VueWrapper) {
  return wrapper.findAll(`a[href^="${rootPath}"].sidebar-link-active`)
}

describe('Smart Operations route registration', () => {
  it.each(sectionCases)('resolves %s to the guarded governance route', (suffix, section) => {
    const route = applicationRouter.resolve(`${rootPath}${suffix}?site=site-42`)

    expect(route.name).toBe('AdminUpstreamGovernance')
    expect(route.params.section || 'overview').toBe(section)
    expect(route.meta.requiresAuth).toBe(true)
    expect(route.meta.requiresAdmin).toBe(true)
    expect(route.meta.titleKey).toBe('governance.smartOperations.title')
    expect(route.query.site).toBe('site-42')
  })

  it.each(['/unknown', '/models/extra'])('does not match unsupported sections: %s', (suffix) => {
    expect(applicationRouter.resolve(`${rootPath}${suffix}`).name).toBe('NotFound')
  })

  it('keeps existing named links to the overview working', () => {
    expect(applicationRouter.resolve({ name: 'AdminUpstreamGovernance' }).path).toBe(rootPath)
  })
})

describe('AppSidebar Smart Operations navigation', () => {
  beforeEach(() => {
    appStore.sidebarCollapsed = false
    appStore.mobileOpen = false
    authStore.isAdmin = true
    authStore.isSimpleMode = false
    vi.clearAllMocks()
  })

  it('expands five ordered sections without navigating away from the current page', async () => {
    const { wrapper, router } = await mountSidebar()
    const button = smartOperationsButton(wrapper)
    expect(button.attributes('aria-expanded')).toBe('false')

    await button.trigger('click')

    expect(button.attributes('aria-expanded')).toBe('true')
    const children = wrapper.get(`#${button.attributes('aria-controls')}`)
    expect(children.findAll('a').map((link) => [link.text(), link.attributes('href')])).toEqual([
      ['governance.smartOperations.sections.overview', rootPath],
      ['governance.smartOperations.sections.import', `${rootPath}/import`],
      ['governance.smartOperations.sections.models', `${rootPath}/models`],
      ['governance.smartOperations.sections.monitor', `${rootPath}/monitor`],
      ['governance.smartOperations.sections.history', `${rootPath}/history`],
    ])
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it.each(sectionCases)('expands and highlights only the selected section on %s', async (suffix, section) => {
    const { wrapper } = await mountSidebar(`${rootPath}${suffix}`)

    expect(smartOperationsButton(wrapper).attributes('aria-expanded')).toBe('true')
    expect(activeSectionLinks(wrapper).map((link) => link.text())).toEqual([
      `governance.smartOperations.sections.${section}`,
    ])
  })

  it('retains the current site when navigating between governance sections', async () => {
    const { wrapper, router } = await mountSidebar(`${rootPath}/models?site=site-42&unrelated=discard`)

    await wrapper.get(`a[href="${rootPath}/import?site=site-42"]`).trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe(`${rootPath}/import`)
    expect(router.currentRoute.value.query).toEqual({ site: 'site-42' })
    expect(activeSectionLinks(wrapper).map((link) => link.text())).toEqual([
      'governance.smartOperations.sections.import',
    ])
  })

  it('does not carry a site query from another feature into governance', async () => {
    const { wrapper, router } = await mountSidebar('/admin/accounts?site=unrelated-site')
    await smartOperationsButton(wrapper).trigger('click')
    await wrapper.get(`a[href="${rootPath}/models"]`).trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe(`${rootPath}/models`)
    expect(router.currentRoute.value.query).toEqual({})
  })

  it('opens a collapsed sidebar and reveals the Smart Operations children on click', async () => {
    appStore.sidebarCollapsed = true
    const { wrapper, router } = await mountSidebar()
    const button = smartOperationsButton(wrapper)
    expect(button.attributes('aria-expanded')).toBe('false')

    await button.trigger('click')

    expect(wrapper.get('aside').classes()).toContain('w-64')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll(`a[href^="${rootPath}"]`)).toHaveLength(5)
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('keeps unrelated collapsed groups unchanged', async () => {
    appStore.sidebarCollapsed = true
    const { wrapper } = await mountSidebar()
    const channelButton = wrapper.findAll('button').find((button) => button.text() === 'nav.channelManagement')!

    await channelButton.trigger('click')

    expect(wrapper.get('aside').classes()).toContain('w-[72px]')
    expect(wrapper.find('a[href="/admin/channels/pricing"]').exists()).toBe(false)
  })

  it('lets an active group be collapsed and expanded without changing the selected section', async () => {
    const { wrapper, router } = await mountSidebar(`${rootPath}/monitor?site=site-42`)
    const button = smartOperationsButton(wrapper)

    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find(`a[href^="${rootPath}"]`).exists()).toBe(false)
    await button.trigger('click')

    expect(button.attributes('aria-expanded')).toBe('true')
    expect(router.currentRoute.value.fullPath).toBe(`${rootPath}/monitor?site=site-42`)
    expect(activeSectionLinks(wrapper).map((link) => link.text())).toEqual([
      'governance.smartOperations.sections.monitor',
    ])
  })

  it('tracks browser history after navigating between sections', async () => {
    const { wrapper, router } = await mountSidebar(`${rootPath}?site=site-42`)
    await wrapper.get(`a[href="${rootPath}/models?site=site-42"]`).trigger('click')
    await flushPromises()
    await wrapper.get(`a[href="${rootPath}/history?site=site-42"]`).trigger('click')
    await flushPromises()

    router.back()
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe(`${rootPath}/models?site=site-42`)
    expect(activeSectionLinks(wrapper).map((link) => link.text())).toEqual([
      'governance.smartOperations.sections.models',
    ])
    router.forward()
    await flushPromises()
    expect(activeSectionLinks(wrapper).map((link) => link.text())).toEqual([
      'governance.smartOperations.sections.history',
    ])
  })

  it('keeps Smart Operations available in simple admin mode', async () => {
    authStore.isSimpleMode = true
    const { wrapper } = await mountSidebar(rootPath)

    expect(smartOperationsButton(wrapper).attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll(`a[href^="${rootPath}"]`)).toHaveLength(5)
  })

  it('does not expose Smart Operations to regular users', async () => {
    authStore.isAdmin = false
    const { wrapper } = await mountSidebar('/dashboard')

    expect(wrapper.text()).not.toContain('governance.smartOperations')
    expect(wrapper.find(`a[href^="${rootPath}"]`).exists()).toBe(false)
  })
})
