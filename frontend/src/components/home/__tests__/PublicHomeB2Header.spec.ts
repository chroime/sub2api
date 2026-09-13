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

  it('uses the B2 header structure with dynamic branding and plain account navigation', async () => {
    const { wrapper } = render()
    expect(wrapper.element.tagName).toBe('HEADER')
    expect(wrapper.classes()).toEqual(['site-header', 'shell'])
    expect(wrapper.get('.wordmark').element.tagName).toBe('A')
    expect(wrapper.get('.brand-logo img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('.brand-logo img').attributes('alt')).toBe('XenoAI')
    expect(wrapper.get('.wordmark-name').text()).toBe('XenoAI')
    expect(wrapper.get('.site-subtitle').text()).toBe('API Conversion')
    expect(wrapper.get('nav.header-links').findAll('.desktop-link').map(link => link.text())).toEqual(['Models', 'Docs'])
    expect(wrapper.get('.login').classes()).toEqual(['login'])
    expect(wrapper.get('.login').text()).toBe('Log in')
    expect(wrapper.find('.public-nav-cta').exists()).toBe(false)
    expect(wrapper.findComponent(LocaleSwitcher).classes()).toContain('b2-locale')
    await wrapper.setProps({ siteName: 'Updated gateway', siteLogo: '/custom-logo.svg', subtitle: 'New subtitle', authenticated: true, destination: '/dashboard' })
    expect(wrapper.get('.brand-logo img').attributes('src')).toBe('/custom-logo.svg')
    expect(wrapper.get('.brand-logo img').attributes('alt')).toBe('Updated gateway')
    expect(wrapper.get('.wordmark-name').text()).toBe('Updated gateway')
    expect(wrapper.get('.site-subtitle').text()).toBe('New subtitle')
    expect(wrapper.get('.login').text()).toBe('Dashboard')
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/dashboard')
  })

  it('keeps custom docs accessible and hides optional model and subtitle entries', () => {
    const { wrapper } = render({ docsHref: 'https://docs.example.test/start', modelPlazaHref: '', subtitle: '' })
    expect(wrapper.findAll('.desktop-link')).toHaveLength(1)
    expect(wrapper.get('.desktop-link').attributes('href')).toBe('https://docs.example.test/start')
    expect(wrapper.find('.site-subtitle').exists()).toBe(false)
  })

  it.each([
    { authenticated: false, destination: '/login', english: 'Log in', translated: 'Sign in' },
    { authenticated: true, destination: '/admin/dashboard', english: 'Dashboard', translated: 'Console' },
  ])('keeps account navigation and its decorative arrow stable for authenticated=$authenticated', async ({ authenticated, destination, english, translated }) => {
    const { wrapper, i18n } = render({ authenticated, destination })
    const link = wrapper.get('.login')
    expect(link.get('.login-label').text()).toBe(english)
    expect(link.text()).toBe(english)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe(destination)

    const arrows = wrapper.findAllComponents(Icon).filter(icon => icon.classes().includes('login-arrow'))
    expect(arrows).toHaveLength(1)
    const arrow = arrows[0]!
    expect(arrow.props('name')).toBe('arrowRight')
    expect(arrow.attributes('aria-hidden')).toBe('true')
    expect(link.findAll('svg')).toHaveLength(1)
    const arrowElement = arrow.element

    i18n.global.locale.value = 'zh'
    await flushPromises()
    expect(link.get('.login-label').text()).toBe(translated)
    expect(link.text()).toBe(translated)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe(destination)
    expect(link.get('.login-arrow').element).toBe(arrowElement)
    expect(link.get('.login-arrow').attributes('aria-hidden')).toBe('true')
  })

  it('toggles both ways with matching document class, saved preference, icon and label', async () => {
    const { wrapper } = render()
    const button = wrapper.get('#theme-toggle.icon-button')
    expect(button.attributes('aria-label')).toBe('Switch to dark')
    expect(button.get('svg').attributes('data-theme-icon')).toBe('moon')
    const moonPath = button.get('svg path').attributes('d')
    expect(moonPath).toBeTruthy()
    await button.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(button.attributes('aria-label')).toBe('Switch to light')
    expect(button.attributes('title')).toBe('Switch to light')
    expect(button.get('svg').attributes('data-theme-icon')).toBe('sun')
    expect(button.get('svg path').attributes('d')).not.toBe(moonPath)
    await button.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    expect(button.attributes('aria-label')).toBe('Switch to dark')
    expect(button.get('svg').attributes('data-theme-icon')).toBe('moon')
    expect(wrapper.find('.public-home-light').exists()).toBe(false)
  })

  it('reflects external theme class changes and reads the current state after remounting', async () => {
    const { wrapper: first } = render()
    document.documentElement.classList.add('dark')
    await flushPromises()
    expect(first.get('[data-theme-icon="sun"]').exists()).toBe(true)
    expect(first.get('#theme-toggle').attributes('aria-label')).toBe('Switch to light')
    first.unmount()
    const { wrapper: second } = render()
    expect(second.get('[data-theme-icon="sun"]').exists()).toBe(true)
    document.documentElement.classList.remove('dark')
    await flushPromises()
    expect(second.get('[data-theme-icon="moon"]').exists()).toBe(true)
    expect(second.get('#theme-toggle').attributes('aria-label')).toBe('Switch to dark')
  })

  it('still changes theme and icon when preference storage is unavailable', async () => {
    const { wrapper } = render()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Storage unavailable', 'SecurityError') })
    await wrapper.get('#theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('[data-theme-icon="sun"]').exists()).toBe(true)
    expect(wrapper.get('#theme-toggle').attributes('aria-label')).toBe('Switch to light')
    await wrapper.get('#theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(wrapper.get('[data-theme-icon="moon"]').exists()).toBe(true)
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
    expect(wrapper.get('.login').text()).toBe('Sign in')
    expect(wrapper.get('#theme-toggle').attributes('aria-label')).toBe('Use dark theme')
    expect(wrapper.findAll('.desktop-link').map(link => link.text())).toEqual(['Model plaza', 'API docs'])
  })
})
