import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

import HomeView from '../HomeView.vue'
import PublicHomePricing from '@/components/home/PublicHomePricing.vue'
import PublicHomeHero from '@/components/home/PublicHomeHero.vue'
import PublicHomeModels from '@/components/home/PublicHomeModels.vue'
import PublicHomeIntegration from '@/components/home/PublicHomeIntegration.vue'
import PublicHomeB2Integration from '@/components/home/PublicHomeB2Integration.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

const { appStore, authStore, publicHomeData } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    checkAuth: vi.fn().mockResolvedValue(undefined),
  },
  publicHomeData: {
    channelStatus: { value: 'ready' },
    pricingStatus: { value: 'ready' },
    channelRows: { value: [
      {
        platform: 'openai',
        channelCount: 2,
        groupCount: 3,
        modelCount: 4,
        channelNames: ['Primary gateway'],
        groupNames: ['Public GPT'],
        modelNames: ['gpt-5.6-sol'],
      },
    ] },
    pricingRows: { value: [
      {
        platform: 'openai',
        model: 'gpt-5.6-sol',
        inputPrice: 0.000002,
        outputPrice: 0.000008,
        cacheWritePrice: null,
        cacheReadPrice: 0.000001,
        perRequestPrice: null,
        billingMode: 'token',
        pricingSource: 'configured',
        groupCount: 2,
      },
    ] },
    channelCount: { value: 2 },
    modelCount: { value: 1 },
    platformCount: { value: 1 },
    load: vi.fn(),
    abort: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/composables/usePublicPlatformHome', () => ({
  usePublicPlatformHome: () => publicHomeData,
}))

function createTestI18n(locale: 'en' | 'zh' = 'en') {
  return createI18n({ legacy: false, locale, messages: { en, zh } })
}

describe('HomeView public home', () => {
  beforeEach(() => {
    appStore.cachedPublicSettings = {
      site_name: 'Test gateway',
      site_subtitle: 'Unified model access',
      model_plaza_enabled: true,
      model_plaza_require_auth: false,
    }
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.checkAuth.mockClear().mockResolvedValue(undefined)
    publicHomeData.channelStatus.value = 'ready'
    publicHomeData.channelRows.value = [{ platform: 'openai', channelCount: 2, groupCount: 3, modelCount: 4, channelNames: ['Primary gateway'], groupNames: ['Public GPT'], modelNames: ['gpt-5.6-sol'] }]
    publicHomeData.pricingRows.value = [{ platform: 'openai', model: 'gpt-5.6-sol', inputPrice: 0.000002, outputPrice: 0.000008, cacheWritePrice: null, cacheReadPrice: 0.000001, perRequestPrice: null, billingMode: 'token', pricingSource: 'configured', groupCount: 2 }]
    publicHomeData.load.mockClear()
    publicHomeData.abort.mockClear()
    localStorage.clear()
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  })

  it('uses the complete B2 page structure without legacy section wrappers', () => {
    appStore.cachedPublicSettings.contact_info = 'QQ 群:123456;微信:987654'
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })

    expect(wrapper.find('.public-grid').exists()).toBe(false)
    expect(wrapper.find('header.public-site-header').exists()).toBe(true)
    expect(wrapper.find('main.shell > #story-scene').exists()).toBe(true)
    expect(wrapper.find('main.shell > .integration').exists()).toBe(true)
    expect(wrapper.find('footer.footer.shell').exists()).toBe(true)
    expect(wrapper.findAll('.footer-contacts > span')).toHaveLength(2)
    expect(wrapper.find('.code-toolbar .icon-button').exists()).toBe(true)
    expect(wrapper.find('.integration-line-number').exists()).toBe(false)
    expect(wrapper.find('.story-compatibility-gateway').exists()).toBe(false)
    expect(wrapper.findComponent(PublicHomeIntegration).exists()).toBe(false)
    wrapper.unmount()
  })

  it('switches complete B2 themes without a second home-specific light state', async () => {
    document.documentElement.classList.remove('dark')
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })

    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('切换到浅色模式')
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(wrapper.find('.public-home-light').exists()).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('切换到深色模式')
    wrapper.unmount()
  })

  it('keeps model plaza access without channel status or homepage pricing sections', async () => {
    const wrapper = mount(HomeView, {
      global: {
        plugins: [createTestI18n()],
        stubs: {
          RouterLink: RouterLinkStub,
          LocaleSwitcher: { template: '<div data-testid="locale-switcher" />' },
          Icon: { template: '<span data-testid="icon" />' },
        },
      },
    })

    await wrapper.vm.$nextTick()

    expect(wrapper.find('.public-home').exists()).toBe(true)
    expect(wrapper.find('#channels').exists()).toBe(false)
    expect(wrapper.find('a[href="#channels"]').exists()).toBe(false)
    expect(wrapper.find('.channel-status-badge').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Channel status')
    expect(wrapper.text()).not.toContain('Visible channels')
    expect(wrapper.find('#pricing').exists()).toBe(false)
    expect(wrapper.find('a[href="#pricing"]').exists()).toBe(false)
    expect(wrapper.findComponent(PublicHomePricing).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Pricing and model coverage')
    expect(wrapper.get('.providers a').attributes('href')).toBe('/model-plaza')
    expect(wrapper.findAllComponents(RouterLinkStub).some((link) => link.props('to') === '/model-plaza')).toBe(true)
    expect(publicHomeData.load).toHaveBeenCalledOnce()
  })

  it('uses the B2 story as the only default public hero', () => {
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })

    expect(wrapper.find('.public-home-b2').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-story"]').exists()).toBe(true)
    expect(wrapper.findComponent(PublicHomeHero).exists()).toBe(false)
    expect(wrapper.get('#mascot-svg').attributes('viewBox')).toBe('-24 -24 304 304')
    expect(wrapper.find('#mascot-svg image').exists()).toBe(false)
    expect(wrapper.find('#mascot-svg .animated-mascot').exists()).toBe(true)
  })

  it('renders configured support contacts and removes the protocol footer label', () => {
    appStore.cachedPublicSettings.contact_info = 'QQ群：123456；QQ：987654'
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    expect(wrapper.find('footer.footer').exists()).toBe(true)
    expect(wrapper.findAll('.footer-contacts > span')).toHaveLength(2)
    expect(wrapper.text()).toContain('123456')
    expect(wrapper.text()).toContain('987654')
    expect(wrapper.text()).not.toContain('客服联系方式来自后台配置')
    expect(wrapper.text()).not.toContain('/v1 · 兼容 OpenAI')
  })

  it('hides the contact panel when the admin contact setting is empty', () => {
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    expect(wrapper.find('.footer-contacts').exists()).toBe(false)
    expect(wrapper.find('footer.footer').exists()).toBe(true)
  })

  it('keeps the public header navigation labels text-only', () => {
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    const links = wrapper.get('header').findAll('.public-nav-link')
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(link.find('svg').exists()).toBe(false)
      expect(link.text()).toBeTruthy()
      expect(link.find('span.hidden').exists()).toBe(false)
    }
  })

  it('renders the approved B2 coverage figures and provider bridge without legacy terminal statistics', () => {
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    expect(wrapper.get('#story-title').text().replace(/\s+/g, '')).toContain('1个API8+模型厂家128+前沿大模型')
    expect(wrapper.find('.metric-cell').exists()).toBe(false)
    expect(wrapper.find('.model-family-card').exists()).toBe(false)
    expect(wrapper.find('.story-compatibility-gateway').exists()).toBe(false)
    expect(wrapper.findAll('[data-provider]').length).toBe(8)
    expect(wrapper.text()).not.toContain('已收录模型')
  })

  it('keeps all eight model provider entries accessible without hidden compatibility nodes', () => {
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    const links = wrapper.findAll('.providers-grid a')
    expect(links).toHaveLength(8)
    for (const link of links) {
      expect(link.attributes('href')).toBe('/model-plaza')
      expect(link.text()).toBeTruthy()
    }
    expect(wrapper.find('.story-compatibility-gateway').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
    ['/logo.svg', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
    ['/xeno-alien-emotions.svg', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
    ['javascript:alert(1)', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
    ['/custom-logo.png', '/custom-logo.png'],
  ])('uses one configured gateway logo with siteLogo=%s', (siteLogo, expected) => {
    const wrapper = mount(PublicHomeModels, {
      props: { siteName: 'Test gateway', siteLogo },
      global: { plugins: [createTestI18n()], stubs: { Icon: true } },
    })
    try {
      const mascot = wrapper.get('.routing-gateway img')
      expect(mascot.attributes('src')).toBe(expected)
      expect(mascot.attributes('alt')).toBe('Test gateway')
      expect(mascot.attributes('width')).toBe('80')
      expect(mascot.attributes('height')).toBe('96')
      expect(wrapper.findAll('.routing-gateway img')).toHaveLength(1)
      expect(wrapper.find('.dancing-frame').exists()).toBe(false)
      expect(wrapper.findAll('.model-family-list h3').map(item => item.text())).toEqual([
        'GPT', 'Claude', 'Gemini', 'Grok', 'GLM', 'Kimi', 'DeepSeek', 'MiniMax',
      ])
    } finally {
      wrapper.unmount()
    }
  })

  it.each([
    ['', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
    ['/custom-logo.png', '/custom-logo.png'],
    ['javascript:alert(1)', '/xeno-alien-spin.svg?rev=20260914-round-head-2'],
  ])('uses configured branding in the B2 header and compact copyright footer: %s', (siteLogo, expected) => {
    appStore.cachedPublicSettings.site_logo = siteLogo
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })

    expect(wrapper.get('.public-brand img').attributes('src')).toBe(expected)
    expect(wrapper.get('footer.footer').text()).toContain('Test gateway')
    expect(wrapper.find('footer img').exists()).toBe(false)
    wrapper.unmount()
  })

  it('connects the application and gateway vertically before branching to models', async () => {
    const rects = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      if (this.classList.contains('routing-graph')) return new DOMRect(0, 0, 800, 600)
      if (this.classList.contains('routing-app')) return new DOMRect(300, 10, 200, 100)
      if (this.classList.contains('routing-gateway')) return new DOMRect(280, 160, 240, 180)
      return new DOMRect(40, 420, 160, 68)
    })
    const wrapper = mount(PublicHomeModels, { global: { plugins: [createTestI18n()], stubs: { Icon: true } } })
    try {
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()
      expect(wrapper.get('.routing-wire').attributes('d')).toBe('M 400 110 V 160')
      expect(wrapper.get('.routing-branch').attributes('d')).toBe('M 400 340 V 404 H 120 V 420')
    } finally {
      wrapper.unmount()
      rects.mockRestore()
    }
  })

  it('switches translated homepage copy with the live locale', async () => {
    const i18n = createTestI18n()
    const wrapper = mount(HomeView, { global: { plugins: [i18n], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: { template: '<div />' }, Icon: { template: '<span />' } } } })
    expect(wrapper.get('#story-title').text().replace(/\s+/g, '')).toBe('1API8+modelproviders128+frontiermodels')
    i18n.global.locale.value = 'zh'
    await wrapper.vm.$nextTick()
    expect(wrapper.get('#story-title').text().replace(/\s+/g, '')).toBe('1个API8+模型厂家128+前沿大模型')
    expect(wrapper.get('#integration-title').text()).toBe('1 分钟接入')
  })

  it('shows all eight supported model families even before public catalog data is configured', async () => {
    publicHomeData.channelRows.value = []
    publicHomeData.pricingRows.value = []
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n('zh')], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true } } })
    const families = wrapper.get('[aria-labelledby="providers-title"]')
    expect(families.findAll('li').map(item => item.get('span').text())).toEqual(['OpenAI', 'Anthropic', 'Google', 'xAI', 'Z.ai', 'Moonshot', 'DeepSeek', 'MiniMax'])
    expect(families.findAll('.provider img')).toHaveLength(8)
    expect(families.text()).toContain('支持的模型厂商')
    expect(wrapper.text()).toContain('接入文档')
    expect(wrapper.text()).not.toContain('查看 API 文档')
  })

  it.each([
    [false, false, false, false],
    [true, true, false, false],
    [true, true, true, true],
  ])('respects model plaza visibility for the model family pricing link (%s, %s, %s)', (enabled, requiresAuth, authenticated, visible) => {
    appStore.cachedPublicSettings.model_plaza_enabled = enabled
    appStore.cachedPublicSettings.model_plaza_require_auth = requiresAuth
    authStore.isAuthenticated = authenticated
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true } } })
    const pricingLink = wrapper.find('.providers a')
    expect(pricingLink.exists()).toBe(visible)
    if (visible) expect(pricingLink.attributes('href')).toBe('/model-plaza')
    expect(wrapper.find('a[href="#pricing"]').exists()).toBe(false)
  })

  it('updates hero props when public settings and pricing arrive asynchronously', async () => {
    const wrapper = mount(PublicHomeHero, { props: { model: 'gpt-5.6-sol', title: 'Initial' }, global: { plugins: [createTestI18n()], stubs: { Icon: { template: '<span />' } } } })
    await wrapper.setProps({ model: 'claude-4', title: 'Updated' })
    await wrapper.vm.$nextTick()
    expect(wrapper.props('model')).toBe('claude-4')
    expect(wrapper.text()).toContain('claude-4')
    expect(wrapper.text()).toContain('Updated')
  })

  it('uses a single terminal frame without a separate outer border', () => {
    const wrapper = mount(PublicHomeHero, { global: { plugins: [createTestI18n()], stubs: { Icon: true } } })
    expect(wrapper.findAll('.terminal-container')).toHaveLength(1)
    expect(wrapper.find('.terminal-glow').exists()).toBe(false)
    expect(wrapper.get('.terminal-container').classes()).toContain('terminal-breathing')
    wrapper.unmount()
  })

  it.each([
    ['', '/docs'],
    ['https://docs.example.test/guide', 'https://docs.example.test/guide'],
  ])('uses configured docs URL or local docs fallback', async (configured, expected) => {
    appStore.cachedPublicSettings = { site_name: 'Test gateway', doc_url: configured }
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: { template: '<div />' }, Icon: { template: '<span />' } } } })
    await wrapper.vm.$nextTick()
    const docsRoute = wrapper.findAllComponents(RouterLinkStub).find(link => link.text() === 'Docs')
    const docsHref = docsRoute?.props('to') ?? wrapper.get('header a.public-nav-link').attributes('href')
    expect(docsHref).toBe(expected)
  })

  it('does not show channel errors after removing channel status from the homepage', async () => {
    publicHomeData.channelStatus.value = 'unavailable'
    publicHomeData.channelRows.value = []
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: { template: '<div />' }, Icon: { template: '<span />' } } } })
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).not.toContain('Temporarily unavailable')
    expect(wrapper.find('#channels').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('operational')
  })

  it('keeps the compact docs link on the local fallback', () => {
    appStore.cachedPublicSettings = { site_name: 'Test gateway', compact_home_enabled: true }
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: { template: '<div />' }, Icon: { template: '<span />' } } } })
    expect(wrapper.get('[data-testid="compact-home"] a').attributes('href')).toBe('/docs')
  })

  it.each([
    ['https://gateway.example.test', 'https://gateway.example.test/v1/chat/completions'],
    ['https://gateway.example.test/v1/', 'https://gateway.example.test/v1/chat/completions'],
  ])('uses the sanitized configured API base URL (%s)', async (apiBaseUrl, expectedEndpoint) => {
    appStore.cachedPublicSettings = { site_name: 'Test gateway', api_base_url: apiBaseUrl }
    const wrapper = mount(HomeView, { global: { plugins: [createTestI18n()], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: { template: '<div />' }, Icon: { template: '<span />' } } } })
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent(PublicHomeB2Integration).props('baseUrl')).toBe(expectedEndpoint.replace(/\/chat\/completions$/, ''))
  })
})
