import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PublicHomeB2Header from '../PublicHomeB2Header.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { setLocale } from '@/i18n'

vi.mock('@/i18n', () => ({
  availableLocales: [
    { code: 'zh', name: 'Chinese', flagSrc: '/flags/cn.png' },
    { code: 'en', name: 'English', flagSrc: '/flags/us.png' },
  ],
  setLocale: vi.fn(),
}))

const messages = {
  en: { home: { switchToLight: () => 'Switch to light', switchToDark: () => 'Switch to dark', login: () => 'Log in', dashboard: () => 'Dashboard', public: { nav: { modelPlaza: () => 'Models', docs: () => 'Docs' } } } },
  zh: { home: { switchToLight: () => 'Use light theme', switchToDark: () => 'Use dark theme', login: () => 'Sign in', dashboard: () => 'Console', public: { nav: { modelPlaza: () => 'Model plaza', docs: () => 'API docs' } } } },
}

let wrapper: VueWrapper | undefined

function render(props = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages })
  wrapper = mount(PublicHomeB2Header, {
    props: {
      siteName: 'XenoAI',
      subtitle: 'API Conversion',
      destination: '/login',
      authenticated: false,
      docsHref: '/docs',
      modelPlazaHref: '/model-plaza',
      ...props,
    },
    global: { plugins: [i18n], stubs: { RouterLink: RouterLinkStub } },
  })
  return { wrapper, i18n }
}

describe('PublicHomeB2Header', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
    vi.mocked(setLocale).mockReset()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('uses shared public header styling with dynamic branding and account navigation', async () => {
    const { wrapper } = render()
    expect(wrapper.element.tagName).toBe('HEADER')
    expect(wrapper.classes()).toEqual(['public-site-header', 'public-site-header-dark'])
    expect(wrapper.get('.public-brand').element.tagName).toBe('A')
    expect(wrapper.get('.public-brand-icon img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('.public-brand-icon img').attributes('alt')).toBe('XenoAI')
    expect(wrapper.get('.public-brand-name').text()).toBe('XenoAI')
    expect(wrapper.get('.public-brand-subtitle').text()).toBe('API Conversion')
    expect(wrapper.get('nav.public-header-inner').findAll('.public-nav-link').map(link => link.text())).toEqual(['Models', 'Docs'])
    expect(wrapper.get('.public-nav-cta').classes()).toEqual(['public-nav-cta'])
    expect(wrapper.get('.public-nav-cta').text()).toBe('Log in')
    expect(wrapper.findComponent(LocaleSwitcher).classes()).toContain('public-header-locale')
    await wrapper.setProps({ siteName: 'Updated gateway', siteLogo: '/custom-logo.svg', subtitle: 'New subtitle', authenticated: true, destination: '/dashboard' })
    expect(wrapper.get('.public-brand-icon img').attributes('src')).toBe('/custom-logo.svg')
    expect(wrapper.get('.public-brand-icon img').attributes('alt')).toBe('Updated gateway')
    expect(wrapper.get('.public-brand-name').text()).toBe('Updated gateway')
    expect(wrapper.get('.public-brand-subtitle').text()).toBe('New subtitle')
    expect(wrapper.get('.public-nav-cta').text()).toBe('Dashboard')
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/dashboard')
  })

  it('keeps custom docs accessible and hides optional model and subtitle entries', () => {
    const { wrapper } = render({ docsHref: 'https://docs.example.test/start', modelPlazaHref: '', subtitle: '' })
    expect(wrapper.findAll('.public-nav-link')).toHaveLength(1)
    expect(wrapper.get('.public-nav-link').attributes('href')).toBe('https://docs.example.test/start')
    expect(wrapper.find('.public-brand-subtitle').exists()).toBe(false)
  })

  it.each([
    { authenticated: false, destination: '/login', english: 'Log in', translated: 'Sign in' },
    { authenticated: true, destination: '/admin/dashboard', english: 'Dashboard', translated: 'Console' },
  ])('keeps text-only account navigation working for authenticated=$authenticated', async ({ authenticated, destination, english, translated }) => {
    const { wrapper, i18n } = render({ authenticated, destination })
    const link = wrapper.get('.public-nav-cta')
    expect(link.text()).toBe(english)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe(destination)

    expect(link.find('svg').exists()).toBe(false)

    i18n.global.locale.value = 'zh'
    await flushPromises()
    expect(link.text()).toBe(translated)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe(destination)
    expect(link.find('svg').exists()).toBe(false)
  })

  it('toggles both ways with matching document class, saved preference, icon and label', async () => {
    const { wrapper } = render()
    const button = wrapper.get('.public-theme-toggle')
    expect(button.attributes('aria-label')).toBe('Switch to dark')
    expect(button.getComponent(Icon).props('name')).toBe('moon')
    const moonPath = button.get('svg path').attributes('d')
    expect(moonPath).toBeTruthy()
    await button.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(button.attributes('aria-label')).toBe('Switch to light')
    expect(button.attributes('title')).toBe('Switch to light')
    expect(button.getComponent(Icon).props('name')).toBe('sun')
    expect(button.get('svg path').attributes('d')).not.toBe(moonPath)
    await button.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    expect(button.attributes('aria-label')).toBe('Switch to dark')
    expect(button.getComponent(Icon).props('name')).toBe('moon')
    expect(wrapper.find('.public-home-light').exists()).toBe(false)
  })

  it('reflects external theme class changes and reads the current state after remounting', async () => {
    const { wrapper: first } = render()
    document.documentElement.classList.add('dark')
    await flushPromises()
    expect(first.get('.public-theme-toggle').getComponent(Icon).props('name')).toBe('sun')
    expect(first.get('.public-theme-toggle').attributes('aria-label')).toBe('Switch to light')
    first.unmount()
    const { wrapper: second } = render()
    expect(second.get('.public-theme-toggle').getComponent(Icon).props('name')).toBe('sun')
    document.documentElement.classList.remove('dark')
    await flushPromises()
    expect(second.get('.public-theme-toggle').getComponent(Icon).props('name')).toBe('moon')
    expect(second.get('.public-theme-toggle').attributes('aria-label')).toBe('Switch to dark')
  })

  it('still changes theme and icon when preference storage is unavailable', async () => {
    const { wrapper } = render()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Storage unavailable', 'SecurityError') })
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('.public-theme-toggle').getComponent(Icon).props('name')).toBe('sun')
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('Switch to light')
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(wrapper.get('.public-theme-toggle').getComponent(Icon).props('name')).toBe('moon')
  })

  it('reuses locale switching and updates translated navigation and theme labels', async () => {
    const { wrapper, i18n } = render()
    vi.mocked(setLocale).mockImplementation(async code => {
      if (code === 'en' || code === 'zh') i18n.global.locale.value = code
    })
    const localeControl = wrapper.getComponent(LocaleSwitcher)
    expect(localeControl.get('button img').attributes('src')).toBe('/flags/us.png')
    await localeControl.get('button').trigger('click')
    await localeControl.get('button[lang="zh"]').trigger('click')
    await flushPromises()
    expect(setLocale).toHaveBeenCalledWith('zh')
    expect(localeControl.get('button img').attributes('src')).toBe('/flags/cn.png')
    expect(wrapper.get('.public-nav-cta').text()).toBe('Sign in')
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('Use dark theme')
    expect(wrapper.findAll('.public-nav-link').map(link => link.text())).toEqual(['Model plaza', 'API docs'])
  })
})
