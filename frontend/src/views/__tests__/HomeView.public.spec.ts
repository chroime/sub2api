import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { config, mount, flushPromises, RouterLinkStub, type VueWrapper } from '@vue/test-utils'

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
async function mountHome(locale: 'en' | 'zh' = 'zh') {
  const i18n = config.global.plugins[0] as { global: { locale: { value: string } } }
  i18n.global.locale.value = locale
  const wrapper = mount(HomeView, {
    global: {
      stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true },
    },
  })
  await flushPromises()
  await vi.waitFor(() => expect(wrapper.find('header').exists()).toBe(true))
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

  it('keeps the main homepage navigation and footer requirements', async () => {
    const wrapper = await mountHome()

    expect(wrapper.get('h1').text()).toBe('Test gateway')
    expect(wrapper.get('header').text()).toContain('模型广场')
    expect(wrapper.get('header').text()).toContain('接入文档')
    expect(wrapper.get('header a[href="https://docs.example.test/start"]').text()).toBe('接入文档')
    expect(wrapper.get('footer').findAll('a')).toHaveLength(0)
  })

  it('switches the public protocol preview across Claude and Gemini', async () => {
    const wrapper = await mountHome()

    await wrapper.get('[data-testid="home-route-anthropic"]').trigger('click')
    expect(wrapper.get('[data-testid="home-route-endpoint"]').text()).toBe('POST /v1/messages')
    expect(wrapper.get('.home-code-tool code').text()).toContain('/v1/messages')
    expect(wrapper.get('.home-code-tool code').text()).toContain('x-api-key: YOUR_API_KEY')
    expect(wrapper.get('.home-code-tool code').text()).toContain('anthropic-version: 2023-06-01')
    expect(wrapper.get('.home-code-tool code').text()).toContain('"max_tokens": 1024')
    await wrapper.get('[data-testid="home-route-gemini"]').trigger('click')
    expect(wrapper.get('[data-testid="home-route-endpoint"]').text()).toBe('POST /v1beta/models/{model}:generateContent')
    expect(wrapper.get('.home-code-tool code').text()).toContain('/v1beta/models/MODEL_ID:generateContent')
    expect(wrapper.get('.home-code-tool code').text()).toContain('x-goog-api-key: YOUR_API_KEY')
    expect(wrapper.get('.home-code-tool code').text()).toContain('"contents": [')
    expect(wrapper.get('.home-code-tool code').text()).not.toContain('"messages":')
    expect(wrapper.get('[data-testid="home-route-gemini"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="home-route-anthropic"]').attributes('aria-pressed')).toBe('false')
  })

  it.each([false, true])('takes API key creation to the keys page after authentication (signed in=%s)', async (authenticated) => {
    authStore.isAuthenticated = authenticated
    const wrapper = await mountHome()
    const createLink = wrapper.findAllComponents(RouterLinkStub).find(link => link.attributes('data-testid') === 'home-create-key')
    expect(createLink?.props('to')).toEqual(authenticated ? '/keys' : { path: '/login', query: { redirect: '/keys' } })
  })

  it('does not mark a different protocol example copied when an earlier clipboard request finishes', async () => {
    const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
    let finishCopy!: () => void
    const writeText = vi.fn(() => new Promise<void>(resolve => { finishCopy = resolve }))
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    try {
      const wrapper = await mountHome()
      await wrapper.get('.home-code-header button').trigger('click')
      expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/v1/chat/completions'))
      await wrapper.get('[data-testid="home-route-anthropic"]').trigger('click')
      finishCopy()
      await flushPromises()
      expect(wrapper.get('.home-copy-feedback').text()).not.toContain('已复制')
      expect(wrapper.get('.home-code-tool code').text()).toContain('/v1/messages')
    } finally {
      if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard)
      else Reflect.deleteProperty(navigator, 'clipboard')
    }
  })

  it('switches the shared document theme from the restored homepage header', async () => {
    const wrapper = await mountHome()

    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('切换到浅色模式')
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
  })

  it('renders the restored request preview and integration copy in English', async () => {
    const wrapper = await mountHome('en')

    expect(wrapper.get('.home-flow-caption').text()).toBe('Request path preview')
    expect(wrapper.get('#home-integration-title').text()).toBe('Start with one API request')
    expect(wrapper.text()).not.toContain('home.experience.')
  })
})
