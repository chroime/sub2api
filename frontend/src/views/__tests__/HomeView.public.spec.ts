import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { config, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    apiBaseUrl: '',
    get backendModeEnabled() {
      return this.cachedPublicSettings.backend_mode_enabled === true
    },
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null,
    checkAuth: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))

const wrappers: VueWrapper[] = []
function mountHome(locale: 'en' | 'zh' = 'zh') {
  const i18n = config.global.plugins[0] as { global: { locale: { value: string } } }
  i18n.global.locale.value = locale
  const wrapper = mount(HomeView, {
    global: {
      stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('HomeView main public home', () => {
  beforeEach(() => {
    appStore.cachedPublicSettings = {
      site_name: 'Test gateway',
      site_subtitle: 'Unified model access',
      doc_url: 'https://docs.example.test/start',
      model_plaza_enabled: true,
      model_plaza_require_auth: false,
    }
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    document.documentElement.classList.remove('dark')
    localStorage.clear()
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('keeps the main homepage navigation and footer requirements', () => {
    const wrapper = mountHome()

    expect(wrapper.get('h1').text()).toBe('Test gateway')
    expect(wrapper.get('header').text()).toContain('模型广场')
    expect(wrapper.get('header').text()).toContain('接入文档')
    expect(wrapper.get('header a[href="https://docs.example.test/start"]').text()).toBe('接入文档')
    expect(wrapper.get('footer').findAll('a')).toHaveLength(0)
  })

  it('switches the public protocol preview across Claude and Gemini', async () => {
    const wrapper = mountHome()

    await wrapper.get('[data-testid="home-route-anthropic"]').trigger('click')
    expect(wrapper.get('[data-testid="home-route-endpoint"]').text()).toBe('POST /v1/messages')
    await wrapper.get('[data-testid="home-route-gemini"]').trigger('click')
    expect(wrapper.get('[data-testid="home-route-endpoint"]').text()).toBe('POST /v1beta/models/{model}:generateContent')
    expect(wrapper.get('[data-testid="home-route-gemini"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="home-route-anthropic"]').attributes('aria-pressed')).toBe('false')
  })

  it('switches the shared document theme from the restored homepage header', async () => {
    const wrapper = mountHome()

    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('切换到浅色模式')
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
  })

  it('renders the restored request preview and integration copy in English', () => {
    const wrapper = mountHome('en')

    expect(wrapper.get('.home-flow-caption').text()).toBe('Request path preview')
    expect(wrapper.get('#home-integration-title').text()).toBe('Start with one API request')
    expect(wrapper.text()).not.toContain('home.experience.')
  })
})
