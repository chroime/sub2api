import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PublicSiteHeader from '../PublicSiteHeader.vue'

describe('shared public site header', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('renders dynamic branding, navigation and a common account action', async () => {
    const wrapper = mount(PublicSiteHeader, {
      props: { siteName: 'Gateway', siteLogo: '/logo.svg', subtitle: 'Subtitle', destination: '/login', authenticated: false },
      slots: { default: '<a class="public-nav-link" href="/docs">Docs</a>' },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: {} })],
        stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true },
      },
    })
    expect(wrapper.get('header').classes()).toContain('public-site-header')
    expect(wrapper.get('header').classes()).toContain('public-site-header-dark')
    expect(wrapper.get('img').attributes('alt')).toBe('Gateway')
    expect(wrapper.get('.public-nav-link').text()).toBe('Docs')
    expect(wrapper.get('.public-nav-link').find('svg').exists()).toBe(false)
    expect(wrapper.find('locale-switcher-stub').exists()).toBe(true)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/login')
    await wrapper.setProps({ siteName: 'Updated' })
    expect(wrapper.get('img').attributes('alt')).toBe('Updated')
    expect(wrapper.find('.public-theme-toggle').exists()).toBe(true)
    await wrapper.setProps({ authenticated: true, destination: '/admin/dashboard' })
    expect(wrapper.get('.public-nav-cta').text()).toBe('home.dashboard')
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/admin/dashboard')
    wrapper.unmount()
  })

  function mountThemeHeader() {
    return mount(PublicSiteHeader, {
      props: { siteName: 'Gateway', destination: '/login', authenticated: false },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: {} })],
        stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true },
      },
    })
  }

  it('switches both ways and persists the theme with matching labels and icons', async () => {
    const wrapper = mountThemeHeader()
    const toggle = wrapper.get('.public-theme-toggle')
    expect(toggle.attributes('aria-label')).toBe('home.switchToDark')
    expect(toggle.get('icon-stub').attributes('name')).toBe('moon')
    await toggle.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(toggle.attributes('aria-label')).toBe('home.switchToLight')
    expect(toggle.get('icon-stub').attributes('name')).toBe('sun')
    await toggle.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    expect(toggle.attributes('aria-label')).toBe('home.switchToDark')
    expect(wrapper.emitted('theme-change')).toEqual([[true], [false]])
    wrapper.unmount()
  })

  it('tracks theme changes made elsewhere and toggles from the live document state', async () => {
    const wrapper = mountThemeHeader()
    document.documentElement.classList.add('dark')
    await flushPromises()
    const toggle = wrapper.get('.public-theme-toggle')
    expect(toggle.attributes('aria-label')).toBe('home.switchToLight')
    await toggle.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    wrapper.unmount()
  })

  it('keeps switching and notifying listeners when preference storage is unavailable', async () => {
    const wrapper = mountThemeHeader()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Storage unavailable', 'SecurityError') })
    await wrapper.get('.public-theme-toggle').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(wrapper.get('.public-theme-toggle').attributes('aria-label')).toBe('home.switchToLight')
    expect(wrapper.emitted('theme-change')).toEqual([[true]])
    wrapper.unmount()
  })
})
